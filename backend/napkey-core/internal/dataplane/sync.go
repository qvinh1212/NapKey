package dataplane

import (
	"context"
	"errors"
	"time"

	"napkey-core/internal/logger"
	"napkey-core/internal/store"
)

// Syncer reconciles api_keys in Postgres against a data plane.
//
// # Which operations the reconciler can and cannot retry
//
// This is the part of the design worth being explicit about. napkey-core stores
// only SHA-256 of a key (DESIGN.md section 5), so the cleartext exists for exactly
// one moment: the request that created it.
//
// The consequence is that create is not retryable. It is therefore performed
// synchronously while the cleartext is still in memory, and if it fails the user
// is told immediately and no key is handed out. Update and delete only need the
// remote id, so those are fully retryable and are what this background loop
// handles.
//
// The alternative would be to keep the cleartext around until the push succeeds,
// which would reintroduce exactly the plaintext-key-at-rest problem that motivated
// hashing in the first place. Failing loudly at creation is the better trade.
type Syncer struct {
	store  *store.Store
	client Provider
	// interval is how often the loop sweeps.
	interval time.Duration
	// staleAfter is how long a key may sit unsynced before it is declared dead.
	staleAfter time.Duration
	// batch caps how many keys are handled per sweep.
	batch int
}

// NewSyncer builds the reconciler.
func NewSyncer(st *store.Store, client Provider, interval time.Duration) *Syncer {
	if interval <= 0 {
		interval = time.Minute
	}
	return &Syncer{
		store:      st,
		client:     client,
		interval:   interval,
		staleAfter: 10 * time.Minute,
		batch:      25,
	}
}

// Run sweeps until the context is canceled.
func (s *Syncer) Run(ctx context.Context) {
	// An immediate first pass picks up anything left pending by a restart.
	s.sweep(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Infof("key syncer stopped")
			return
		case <-ticker.C:
			s.sweep(ctx)
		}
	}
}

// sweep processes one batch of pending keys plus the stale-key check.
func (s *Syncer) sweep(ctx context.Context) {
	keys, err := s.store.ClaimKeysForSync(ctx, s.batch)
	if err != nil {
		logger.Errorf("claiming keys for sync failed: %v", err)
		return
	}
	for _, k := range keys {
		if ctx.Err() != nil {
			return
		}
		s.syncOne(ctx, k)
	}
	s.retireStaleKeys(ctx)
}

// syncOne pushes a single key's desired state to the data plane.
func (s *Syncer) syncOne(ctx context.Context, k store.APIKey) {
	opCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	switch k.SyncState {
	case store.SyncDeletePending:
		if k.RemoteID == "" {
			// Never reached the data plane, so there is nothing to delete.
			if err := s.store.DeleteSyncedKey(opCtx, k.ID); err != nil {
				logger.Errorf("finalizing deletion of key %s failed: %v", k.ID, err)
			}
			return
		}
		if err := s.client.DeleteKey(opCtx, k.RemoteID); err != nil {
			logger.Warnf("deleting key %s from the data plane failed: %v", k.ID, err)
			if markErr := s.store.MarkKeySyncFailed(opCtx, k.ID, err.Error(), true); markErr != nil {
				logger.Errorf("recording delete failure for key %s: %v", k.ID, markErr)
			}
			return
		}
		if err := s.store.DeleteSyncedKey(opCtx, k.ID); err != nil {
			logger.Errorf("finalizing deletion of key %s failed: %v", k.ID, err)
			return
		}
		logger.Infof("key %s removed from the data plane", k.ID)

	case store.SyncPending, store.SyncFailed:
		if k.RemoteID == "" {
			// No remote id means the create never landed. The cleartext is gone, so
			// this cannot be retried; retireStaleKeys deals with it once the grace
			// period passes.
			logger.Warnf("key %s has no data-plane id and cannot be re-pushed; awaiting retirement", k.ID)
			if err := s.store.MarkKeySyncFailed(opCtx, k.ID,
				"key was never registered in the data plane and cannot be recreated", false); err != nil {
				logger.Errorf("recording sync failure for key %s: %v", k.ID, err)
			}
			return
		}
		name := k.Name
		enabled := k.Enabled
		tokenLimit := k.TokenLimit
		creditLimit := k.CreditLimit
		rpmLimit := 0
		tpmLimit := 0
		if k.RPMLimit != nil {
			rpmLimit = *k.RPMLimit
		}
		if k.TPMLimit != nil {
			tpmLimit = *k.TPMLimit
		}
		err := s.client.UpdateKey(opCtx, k.RemoteID, UpdateKeyRequest{
			Name:        &name,
			Enabled:     &enabled,
			RPMLimit:    &rpmLimit,
			TPMLimit:    &tpmLimit,
			TokenLimit:  &tokenLimit,
			CreditLimit: &creditLimit,
		})
		if err != nil {
			// A key the data plane no longer knows about cannot be restored from a
			// hash. Retire it rather than retrying forever.
			if errors.Is(err, ErrNotFound) {
				logger.Warnf("key %s is gone from the data plane; retiring it", k.ID)
				if markErr := s.store.MarkKeyUnusable(opCtx, k.ID,
					"key no longer exists in the data plane; create a replacement"); markErr != nil {
					logger.Errorf("retiring key %s failed: %v", k.ID, markErr)
				}
				return
			}
			logger.Warnf("syncing key %s failed: %v", k.ID, err)
			if markErr := s.store.MarkKeySyncFailed(opCtx, k.ID, err.Error(), false); markErr != nil {
				logger.Errorf("recording sync failure for key %s: %v", k.ID, markErr)
			}
			return
		}
		if err := s.store.MarkKeySynced(opCtx, k.ID, k.RemoteID); err != nil {
			logger.Errorf("marking key %s synced failed: %v", k.ID, err)
			return
		}
		logger.Debugf("key %s synced to the data plane", k.ID)

	default:
		logger.Warnf("key %s has unexpected sync state %q", k.ID, k.SyncState)
	}
}

// retireStaleKeys revokes keys whose creation never reached the data plane.
//
// Without this a failed create leaves a row the console shows as a working key
// while the data plane rejects it, and the customer has no way to tell why.
func (s *Syncer) retireStaleKeys(ctx context.Context) {
	stale, err := s.store.ListStaleUnsyncedKeys(ctx, s.staleAfter, s.batch)
	if err != nil {
		logger.Errorf("listing stale unsynced keys failed: %v", err)
		return
	}
	for _, k := range stale {
		reason := "key was never registered in the data plane; create a replacement"
		if k.SyncError != "" {
			reason = "not registered in the data plane: " + k.SyncError
		}
		if err := s.store.MarkKeyUnusable(ctx, k.ID, reason); err != nil {
			logger.Errorf("retiring stale key %s failed: %v", k.ID, err)
			continue
		}
		logger.Warnf("retired key %s (created %s) because it never reached the data plane",
			k.ID, k.CreatedAt.Format(time.RFC3339))
	}
}

// DetectDrift reports keys that exist in the data plane but not in napkey-core.
//
// A key in the data plane with no owning row is unattributable traffic: it
// authenticates, spends upstream quota, and there is nobody to bill. This surfaces
// it for a human rather than deleting anything automatically, since a wrong
// automatic delete would cut off a paying customer.
func (s *Syncer) DetectDrift(ctx context.Context) ([]KeyView, error) {
	remote, err := s.client.ListKeys(ctx)
	if err != nil {
		return nil, err
	}
	known, err := s.store.ListSyncedRemoteIDs(ctx)
	if err != nil {
		return nil, err
	}
	var orphans []KeyView
	for _, rk := range remote {
		if _, ok := known[rk.ID]; !ok {
			orphans = append(orphans, rk)
		}
	}
	return orphans, nil
}
