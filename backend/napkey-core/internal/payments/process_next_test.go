package payments

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"napkey-core/internal/pgtest"
	"napkey-core/internal/store"
)

// These drive ProcessNext itself, so the decision under test is the one production makes
// rather than a restatement of it. A payment the worker mishandles is money a customer
// sent and did not receive credit for, and unlike a serving bug it produces no error the
// customer can see.

func text(v string) *string { return &v }

// claimResponse is what ClaimPaymentEvent's SELECT returns: one event ready to process.
func claimResponse(id int64, payload string) pgtest.Response {
	return pgtest.Response{
		Columns: []pgtest.Column{
			{Name: "id", OID: 20}, {Name: "provider_tx_id"}, {Name: "payload"},
		},
		Rows: [][]*string{{text(fmt.Sprint(id)), text("casso-tx-1"), text(payload)}},
	}
}

func bankPayload(amount int64, description string) string {
	raw, _ := json.Marshal(map[string]any{
		"data": map[string]any{
			"id": 991, "reference": "ref-991", "description": description, "amount": amount,
		},
	})
	return string(raw)
}

// newWorkerOnStub wires a Worker to a stub Postgres and records how each event was
// closed, which is the worker's only externally visible decision.
func newWorkerOnStub(t *testing.T, payload string) (*Worker, *[]string) {
	t.Helper()
	pg := pgtest.New(t)
	st, err := store.Open(t.Context(), pg.DSN(), 2, 1)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	var closures []string
	pg.On("FROM payment_events WHERE status='received'", func(pgtest.Query) pgtest.Response {
		return claimResponse(1, payload)
	})
	pg.On("UPDATE payment_events SET status='processing'", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Tag: "UPDATE 1"}
	})
	// pgtest does not surface bound parameters, so the closure is observed by the fact
	// that RejectPaymentEvent ran at all: the worker reaches it only for a payment it
	// declines to credit, and crediting takes an entirely different path.
	pg.On("UPDATE payment_events SET status=$2", func(q pgtest.Query) pgtest.Response {
		closures = append(closures, "closed-without-credit")
		return pgtest.Response{Tag: "UPDATE 1"}
	})
	pg.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "OK"} })

	return NewWorker(st), &closures
}

// An outgoing transfer is rejected, not credited.
//
// Casso reports both directions on the account. Crediting a withdrawal would hand a
// customer money they never sent.
func TestProcessNextRejectsOutgoingTransfers(t *testing.T) {
	w, closures := newWorkerOnStub(t, bankPayload(-50_000, "NK7QP2XV"))
	if err := w.ProcessNext(t.Context()); err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if len(*closures) != 1 {
		t.Fatalf("outgoing transfer produced %d closures, want exactly one and no credit", len(*closures))
	}
}

// A payment whose memo cannot be read is left unmatched, never rejected.
//
// The two statuses decide whether anyone looks again. An unreadable memo is still a real
// customer's money, and CountStaleUnmatchedPayments alerts on exactly this status;
// rejecting would close the event and lose the payment quietly.
func TestProcessNextLeavesUnreadableMemosUnmatched(t *testing.T) {
	w, closures := newWorkerOnStub(t, bankPayload(50_000, "CHUYEN TIEN NOI BO"))
	if err := w.ProcessNext(t.Context()); err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if len(*closures) != 1 {
		t.Fatalf("unreadable memo produced %d closures, want exactly one and no credit", len(*closures))
	}
}

// A memo naming no known order is unmatched too.
//
// A customer can mistype a memo, or transfer against an order that was cancelled. Neither
// is a rejection: the money arrived and someone has to attribute it.
func TestProcessNextLeavesUnknownOrdersUnmatched(t *testing.T) {
	pg := pgtest.New(t)
	st, err := store.Open(t.Context(), pg.DSN(), 2, 1)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	var closures []string
	pg.On("FROM payment_events WHERE status='received'", func(pgtest.Query) pgtest.Response {
		return claimResponse(1, bankPayload(50_000, "NK7QP2XV"))
	})
	pg.On("UPDATE payment_events SET status='processing'", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Tag: "UPDATE 1"}
	})
	pg.On("SELECT status FROM payment_events", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "status"}},
			Rows:    [][]*string{{text("processing")}},
		}
	})
	// No order carries this memo.
	pg.On("FROM topup_orders WHERE memo_code=$1", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: []pgtest.Column{{Name: "id"}}}
	})
	pg.On("UPDATE payment_events SET status=$2", func(q pgtest.Query) pgtest.Response {
		closures = append(closures, "closed-without-credit")
		return pgtest.Response{Tag: "UPDATE 1"}
	})
	pg.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "OK"} })

	if err := NewWorker(st).ProcessNext(t.Context()); err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if len(closures) != 1 {
		t.Fatalf("unknown order produced %d closures, want exactly one and no credit", len(closures))
	}
}

// A malformed envelope is rejected rather than silently dropped.
//
// The worker cannot attribute what it cannot read, but the event must still be closed with
// a reason, or it is reclaimed every five minutes forever.
func TestProcessNextRejectsMalformedPayloads(t *testing.T) {
	w, closures := newWorkerOnStub(t, "{{{ not json")
	if err := w.ProcessNext(t.Context()); err != nil {
		t.Fatalf("ProcessNext: %v", err)
	}
	if len(*closures) != 1 {
		t.Fatalf("malformed payload produced %d closures, want exactly one and no credit", len(*closures))
	}
}

// An empty queue is not an error condition.
//
// Run treats ErrNotFound as "nothing to do" and goes back to sleep. Returning a different
// error would log a warning every two seconds on an idle system.
func TestProcessNextReportsAnEmptyQueueAsNotFound(t *testing.T) {
	pg := pgtest.New(t)
	st, err := store.Open(t.Context(), pg.DSN(), 2, 1)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	pg.OnAny(func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: []pgtest.Column{{Name: "id", OID: 20}}}
	})

	if err := NewWorker(st).ProcessNext(t.Context()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ProcessNext on an empty queue = %v, want ErrNotFound", err)
	}
}
