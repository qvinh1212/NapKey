// Package payments reconciles journaled provider events into wallet credits.
package payments

import (
	"context"
	"errors"
	"fmt"
	"time"

	"napkey-core/internal/casso"
	"napkey-core/internal/logger"
	"napkey-core/internal/pricing"
	"napkey-core/internal/store"
)

type Worker struct {
	store *store.Store
}

func NewWorker(st *store.Store) *Worker {
	return &Worker{store: st}
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		for processed:=0;processed<100;processed++{
			err:=w.ProcessNext(ctx);if errors.Is(err,store.ErrNotFound)||errors.Is(err,context.Canceled){break};if err!=nil{logger.Warnf("processing Casso payment failed: %v",err);break}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) ProcessNext(ctx context.Context) error {
	event, err := w.store.ClaimPaymentEvent(ctx)
	if err != nil {
		return err
	}
	tx, err := casso.ParseTransaction(event.Payload)
	if err != nil {
		return w.reject(ctx, event.ID, "rejected", "invalid transaction payload: "+err.Error())
	}
	if tx.AmountVND <= 0 {
		return w.reject(ctx, event.ID, "rejected", "transaction is not an incoming payment")
	}
	memo := casso.ExtractMemoCode(tx.Description)
	if memo == "" {
		return w.reject(ctx, event.ID, "unmatched", "NapKey transfer memo was not found")
	}
	amountMicros, err := pricing.MicrosFromVND(tx.AmountVND)
	if err != nil {
		return w.reject(ctx, event.ID, "rejected", "payment amount is outside the supported range")
	}
	if err := w.store.CreditPaymentEvent(ctx, event.ID, event.ProviderTxID, memo, amountMicros); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return w.reject(ctx, event.ID, "unmatched", fmt.Sprintf("top-up order %s was not found", memo))
		}
		return err
	}
	return nil
}

func (w *Worker) reject(ctx context.Context, eventID int64, status, message string) error {
	return w.store.RejectPaymentEvent(ctx, eventID, status, message)
}
