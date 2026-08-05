package dataplane_test

import (
	"context"
	"errors"
	"testing"

	"napkey-core/internal/dataplane"
	"napkey-core/internal/kiro"
)

// fakeProvider is a data plane that speaks the contract without any HTTP, standing
// in for a future non-kiro-go engine.
//
// Its only job is to fail the build if Provider ever grows a method that cannot be
// satisfied outside the kiro-go adapter. That is the property this refactor exists
// to create, and a compile-time check is the cheapest way to keep it true.
type fakeProvider struct {
	created []dataplane.CreateKeyRequest
	updated map[string]dataplane.UpdateKeyRequest
	deleted []string
	keys    []dataplane.KeyView
	healthy bool
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{updated: map[string]dataplane.UpdateKeyRequest{}, healthy: true}
}

func (f *fakeProvider) CreateKey(_ context.Context, req dataplane.CreateKeyRequest) (string, error) {
	f.created = append(f.created, req)
	return "remote-" + req.Key, nil
}

func (f *fakeProvider) UpdateKey(_ context.Context, remoteID string, req dataplane.UpdateKeyRequest) error {
	if remoteID == "" {
		return errors.New("fake: update requires a remote id")
	}
	if _, known := f.updated[remoteID]; !known && len(f.keys) > 0 {
		return dataplane.ErrNotFound
	}
	f.updated[remoteID] = req
	return nil
}

func (f *fakeProvider) DeleteKey(_ context.Context, remoteID string) error {
	if remoteID == "" {
		return errors.New("fake: delete requires a remote id")
	}
	f.deleted = append(f.deleted, remoteID)
	return nil
}

func (f *fakeProvider) ListKeys(context.Context) ([]dataplane.KeyView, error) {
	return f.keys, nil
}

func (f *fakeProvider) OperationsStatus(context.Context) (*dataplane.OperationsStatus, error) {
	return &dataplane.OperationsStatus{Accounts: 2, Available: 2}, nil
}

func (f *fakeProvider) Health(context.Context) error {
	if !f.healthy {
		return dataplane.ErrUnauthorized
	}
	return nil
}

// TestProviderIsImplementableOutsideKiro is the point of the contract: a data plane
// that is not kiro-go can satisfy it.
func TestProviderIsImplementableOutsideKiro(t *testing.T) {
	var provider dataplane.Provider = newFakeProvider()

	remoteID, err := provider.CreateKey(context.Background(), dataplane.CreateKeyRequest{Key: "nk_live_x"})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	if remoteID == "" {
		t.Fatal("a provider must return the id it assigned, or update and delete become impossible")
	}
	if err := provider.UpdateKey(context.Background(), remoteID, dataplane.UpdateKeyRequest{}); err != nil {
		t.Fatalf("UpdateKey: %v", err)
	}
	if err := provider.DeleteKey(context.Background(), remoteID); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
}

// TestKiroClientSatisfiesProvider guards the direction that actually ships today.
func TestKiroClientSatisfiesProvider(t *testing.T) {
	var provider dataplane.Provider = kiro.New("http://127.0.0.1:1", "pw")
	if provider == nil {
		t.Fatal("the kiro adapter must satisfy the contract")
	}
}

// TestSentinelErrorsAreShared pins the behaviour that makes the seam safe to swap.
//
// Callers branch on these: an unauthorized data plane is fatal at startup, and a
// missing key is success for a delete. If an adapter returned its own error values
// instead of these, those branches would silently stop matching and a dead data
// plane would look merely unreachable.
func TestSentinelErrorsAreShared(t *testing.T) {
	if !errors.Is(kiro.ErrUnauthorized, dataplane.ErrUnauthorized) {
		t.Error("kiro.ErrUnauthorized must be the contract's sentinel so startup still fails closed")
	}
	if !errors.Is(kiro.ErrNotFound, dataplane.ErrNotFound) {
		t.Error("kiro.ErrNotFound must be the contract's sentinel so a missing key still counts as deleted")
	}

	f := newFakeProvider()
	f.healthy = false
	if err := f.Health(context.Background()); !errors.Is(err, dataplane.ErrUnauthorized) {
		t.Errorf("a rejected credential must be reported as ErrUnauthorized, got %v", err)
	}
}

// TestUpdateRequestOmitsUnsetFields protects the patch semantics.
//
// Every field is a pointer because nil means "leave alone" while a zero value means
// "set to zero". Collapsing the two would clear a paying customer's rate limits on
// an unrelated edit.
func TestUpdateRequestOmitsUnsetFields(t *testing.T) {
	enabled := false
	req := dataplane.UpdateKeyRequest{Enabled: &enabled}

	if req.TokenLimit != nil || req.CreditLimit != nil || req.RPMLimit != nil || req.TPMLimit != nil || req.Name != nil {
		t.Fatal("unset patch fields must stay nil so the data plane leaves them untouched")
	}
	if req.Enabled == nil || *req.Enabled {
		t.Fatal("an explicitly disabled key must be distinguishable from an unset field")
	}
}
