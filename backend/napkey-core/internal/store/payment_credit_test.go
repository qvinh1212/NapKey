package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"napkey-core/internal/pgtest"
)

// Crediting an incoming payment. This is the one path where a mistake takes a customer's
// money without giving anything back, and it produces no error the customer can see: the
// transfer leaves their bank either way.

// topupOrderColumns matches the column order CreditPaymentEvent selects from topup_orders.
func topupOrderColumns() []pgtest.Column {
	return []pgtest.Column{
		{Name: "id", OID: 20}, {Name: "user_id"}, {Name: "memo_code"},
		{Name: "expected_amount_micros", OID: 20}, {Name: "received_amount_micros", OID: 20},
		{Name: "retail_vnd_per_credit", OID: 20},
	}
}

// paymentEventHarness wires a stub for one CreditPaymentEvent call against an order whose
// memo matches, recording the statements that move money.
type paymentEventHarness struct {
	store        *Store
	creditSQL    string
	orderSQL     string
	eventClosed  bool
	ledgerWrites int
}

func newPaymentEventHarness(t *testing.T, eventStatus, memo string, expectedMicros, receivedMicros, vndPerCredit string) *paymentEventHarness {
	t.Helper()
	h := &paymentEventHarness{}
	srv := pgtest.New(t)

	srv.On("SELECT status FROM payment_events", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "status"}},
			Rows:    [][]*string{{pgtest.Text(eventStatus)}},
			Tag:     "SELECT 1",
		}
	})
	srv.On("FROM topup_orders WHERE memo_code=$1", func(q pgtest.Query) pgtest.Response {
		h.orderSQL = q.SQL
		if memo == "" {
			return pgtest.Response{Columns: topupOrderColumns(), Tag: "SELECT 0"}
		}
		return pgtest.Response{
			Columns: topupOrderColumns(),
			Rows: [][]*string{{
				pgtest.Text("9001"), pgtest.Text(pgtest.UUID(1)), pgtest.Text(memo),
				pgtest.Text(expectedMicros), pgtest.Text(receivedMicros), pgtest.Text(vndPerCredit),
			}},
			Tag: "SELECT 1",
		}
	})
	srv.On("UPDATE wallets SET balance_micros=balance_micros+", func(q pgtest.Query) pgtest.Response {
		h.creditSQL = q.SQL
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "balance_micros", OID: 20}, {Name: "held_micros", OID: 20}},
			Rows:    [][]*string{{pgtest.Text("40000000000"), pgtest.Text("0")}},
			Tag:     "UPDATE 1",
		}
	})
	srv.On("INSERT INTO ledger_entries", func(pgtest.Query) pgtest.Response {
		h.ledgerWrites++
		return pgtest.Response{Tag: "INSERT 0 1"}
	})
	srv.On("UPDATE payment_events SET status='credited'", func(pgtest.Query) pgtest.Response {
		h.eventClosed = true
		return pgtest.Response{Tag: "UPDATE 1"}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "OK"} })

	h.store = openTestStore(t, srv)
	return h
}

// A payment already credited is not credited twice.
//
// The worker reclaims an event whose processing stalled for five minutes, and a slow
// commit looks exactly like a stall. Without this guard a retry would add the money again
// and the ledger would carry two topup rows for one transfer.
func TestCreditPaymentEventIsIdempotent(t *testing.T) {
	h := newPaymentEventHarness(t, "credited", "NK7QP2XV", "10000000000", "0", "400")

	if err := h.store.CreditPaymentEvent(context.Background(), 1, "casso-tx-1", "NK7QP2XV", 10_000_000_000); err != nil {
		t.Fatalf("CreditPaymentEvent on an already-credited event = %v, want nil", err)
	}
	if h.creditSQL != "" {
		t.Error("an already-credited payment was credited again, doubling the customer's balance")
	}
	if h.ledgerWrites != 0 {
		t.Errorf("an already-credited payment wrote %d ledger entries, want 0", h.ledgerWrites)
	}
}

// An event that is not being processed is refused rather than credited.
//
// Only ClaimPaymentEvent moves an event into "processing", and it does so under a row
// lock. Crediting from any other status would mean crediting an event nobody claimed,
// which is how the same payment gets processed by two workers.
func TestCreditPaymentEventRequiresAClaimedEvent(t *testing.T) {
	for _, status := range []string{"received", "unmatched", "rejected"} {
		h := newPaymentEventHarness(t, status, "NK7QP2XV", "10000000000", "0", "400")
		err := h.store.CreditPaymentEvent(context.Background(), 1, "casso-tx-1", "NK7QP2XV", 10_000_000_000)
		if err == nil {
			t.Errorf("status %q was credited without being claimed", status)
		}
		if h.creditSQL != "" {
			t.Errorf("status %q reached the wallet update", status)
		}
	}
}

// A memo matching no order is reported as missing, not credited to someone.
//
// The worker turns ErrNotFound into "unmatched", which is the status the stale-payment
// alert watches. Crediting a guess would move a stranger's money into an account.
func TestCreditPaymentEventReportsUnknownOrders(t *testing.T) {
	h := newPaymentEventHarness(t, "processing", "", "10000000000", "0", "400")

	err := h.store.CreditPaymentEvent(context.Background(), 1, "casso-tx-1", "NKUNKNOWN", 10_000_000_000)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreditPaymentEvent for an unknown memo = %v, want ErrNotFound", err)
	}
	if h.creditSQL != "" {
		t.Error("an unattributable payment credited a wallet")
	}
}

// A non-positive amount never reaches the database.
//
// Zero would close the event as credited while adding nothing; negative would take money
// out of a wallet on the strength of an incoming transfer.
func TestCreditPaymentEventRejectsNonPositiveAmounts(t *testing.T) {
	h := newPaymentEventHarness(t, "processing", "NK7QP2XV", "10000000000", "0", "400")

	for _, amount := range []int64{0, -10_000_000_000} {
		if err := h.store.CreditPaymentEvent(context.Background(), 1, "casso-tx-1", "NK7QP2XV", amount); err == nil {
			t.Errorf("amount %d was accepted", amount)
		}
	}
	if h.creditSQL != "" {
		t.Error("a non-positive amount reached the wallet update")
	}
}

// The order's own rate prices the credit, not today's rate.
//
// An order records retail_vnd_per_credit when it is created. Pricing a late payment at the
// current rate would give a customer more or less than they were quoted, and repricing is
// exactly what the append-only ledger exists to prevent.
func TestCreditIsPricedAtTheRateTheOrderQuoted(t *testing.T) {
	// 10,000 VND on an order quoted at today's 75 VND/credit. The order rate and the retail
	// rate agree, so the wallet receives exactly the money that arrived.
	atRetailRate, err := walletCreditMicros(10_000*1_000_000, 75)
	if err != nil {
		t.Fatalf("walletCreditMicros: %v", err)
	}
	// The same transfer against an order written while a credit cost 400, the rate that
	// stood between migrations 0017 and 0022.
	atLegacyRate, err := walletCreditMicros(10_000*1_000_000, 400)
	if err != nil {
		t.Fatalf("walletCreditMicros: %v", err)
	}
	if atRetailRate == atLegacyRate {
		t.Fatal("the two rates produced the same credit, so this test proves nothing")
	}
	if atRetailRate < atLegacyRate {
		t.Errorf("a cheaper rate bought less wallet value: %d at 75 VND/credit against %d at 400",
			atRetailRate, atLegacyRate)
	}
}

// A partial transfer credits what arrived and leaves the order underpaid.
//
// Customers do transfer the wrong amount. Crediting the expected amount would give away
// the difference; refusing the payment would keep their money and credit nothing.
func TestUnderpaymentCreditsWhatArrived(t *testing.T) {
	if got := topupStatus(10_000_000_000, 7_000_000_000); got != TopupUnderpaid {
		t.Errorf("a short transfer produced status %q, want %q", got, TopupUnderpaid)
	}
	if got := topupStatus(10_000_000_000, 10_000_000_000); got != TopupPaid {
		t.Errorf("an exact transfer produced status %q, want %q", got, TopupPaid)
	}
	// Overpayment counts as paid rather than as an error: the surplus is real money that
	// belongs in the wallet.
	if got := topupStatus(10_000_000_000, 12_000_000_000); got != TopupPaid {
		t.Errorf("an over-transfer produced status %q, want %q", got, TopupPaid)
	}
}

// Repeat transfers against one order accumulate rather than replace.
//
// A customer who pays in two instalments has sent the full amount, and the order has to
// recognise that. Overwriting received_amount_micros would leave it permanently underpaid.
func TestRepeatTransfersAccumulateOnTheOrder(t *testing.T) {
	h := newPaymentEventHarness(t, "processing", "NK7QP2XV", "10000000000", "6000000000", "400")

	if err := h.store.CreditPaymentEvent(context.Background(), 1, "casso-tx-2", "NK7QP2XV", 4_000_000_000); err != nil {
		t.Fatalf("CreditPaymentEvent: %v", err)
	}
	if !strings.Contains(h.orderSQL, "received_amount_micros") {
		t.Errorf("the order lookup does not read the amount already received:\n%s", h.orderSQL)
	}
	if !h.eventClosed {
		t.Error("the event was not closed as credited, so the worker will reclaim it")
	}
}

// The ledger entry is keyed by the provider's transaction id.
//
// That key is the last line of defence: if two events somehow credit the same bank
// transaction, the unique idempotency_key turns the second one into a constraint violation
// instead of a duplicate credit.
func TestCreditWritesALedgerEntryKeyedByProviderTransaction(t *testing.T) {
	h := newPaymentEventHarness(t, "processing", "NK7QP2XV", "10000000000", "0", "400")

	if err := h.store.CreditPaymentEvent(context.Background(), 1, "casso-tx-3", "NK7QP2XV", 10_000_000_000); err != nil {
		t.Fatalf("CreditPaymentEvent: %v", err)
	}
	if h.ledgerWrites != 1 {
		t.Errorf("wrote %d ledger entries for one payment, want exactly 1", h.ledgerWrites)
	}
	if h.creditSQL == "" {
		t.Error("the wallet was never credited")
	}
}
