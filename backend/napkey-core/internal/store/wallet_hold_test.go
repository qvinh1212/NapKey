package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"napkey-core/internal/pgtest"
)

// The wallet hold lifecycle: reserve, then either settle or release. These four functions
// are the only things standing between a served request and an unpaid one, and none of
// them had a test.
//
// pgtest replays wire responses rather than running Postgres, so what is verified here is
// the decision each function makes and the SQL it issues -- not that the database enforces
// the constraints those statements rely on. Where a guarantee lives in a WHERE clause, the
// test asserts the clause is present, and says so.

// holdRow is a wallet_holds row in the column order ReserveWallet selects.
func holdRow(requestID string, amountMicros string) []*string {
	return []*string{
		pgtest.Text("11111111-1111-4111-8111-111111111111"),
		pgtest.Text(requestID),
		pgtest.Text(amountMicros),
		pgtest.Text("2026-08-06 12:15:00+00"),
	}
}

func holdColumns() []pgtest.Column {
	return []pgtest.Column{
		{Name: "id"}, {Name: "request_id"},
		{Name: "amount_micros", OID: 20}, {Name: "expires_at", OID: 1184},
	}
}

// A hold already on file is returned as-is, without reserving a second time.
//
// The data plane retries a reserve when a response is lost, and the request id is the
// idempotency key. Reserving again on retry would hold the amount twice against one
// request and the duplicate would only come back at expiry, fifteen minutes later.
func TestReserveWalletIsIdempotentOnRequestID(t *testing.T) {
	srv := pgtest.New(t)
	var reserved bool
	srv.On("SELECT id,request_id,amount_micros,expires_at FROM wallet_holds", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: holdColumns(), Rows: [][]*string{holdRow("req-1", "5000000")}, Tag: "SELECT 1"}
	})
	srv.On("UPDATE wallets SET held_micros=held_micros+", func(pgtest.Query) pgtest.Response {
		reserved = true
		return pgtest.Response{Tag: "UPDATE 1"}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "OK"} })

	st := openTestStore(t, srv)
	hold, err := st.ReserveWallet(context.Background(), pgtest.UUID(1), pgtest.UUID(2), "req-1", 5_000_000)
	if err != nil {
		t.Fatalf("ReserveWallet: %v", err)
	}
	if reserved {
		t.Error("an existing hold was reserved again, so the amount is held twice for one request")
	}
	if hold.AmountMicros != 5_000_000 {
		t.Errorf("returned amount = %d, want the amount already on file (5000000)", hold.AmountMicros)
	}
}

// A wallet that cannot cover the hold reports insufficient funds, not a database error.
//
// The reserve UPDATE carries its own guard -- balance_micros-held_micros >= $2 -- so a
// wallet without the funds matches no row. That has to surface as ErrInsufficientFunds,
// because the API turns it into a 402 the customer can act on; anything else becomes a
// 500 and looks like an outage.
func TestReserveWalletReportsInsufficientFunds(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("SELECT id,request_id,amount_micros,expires_at FROM wallet_holds", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: holdColumns(), Tag: "SELECT 0"}
	})
	srv.On("UPDATE wallets SET held_micros=held_micros+", func(pgtest.Query) pgtest.Response {
		// No row matched: the guard in the statement rejected the reservation.
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "balance_micros", OID: 20}, {Name: "held_micros", OID: 20}},
			Tag:     "UPDATE 0",
		}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "OK"} })

	st := openTestStore(t, srv)
	_, err := st.ReserveWallet(context.Background(), pgtest.UUID(1), pgtest.UUID(2), "req-2", 5_000_000)
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("ReserveWallet on an underfunded wallet = %v, want ErrInsufficientFunds", err)
	}
}

// The reserve statement guards on available balance, not total balance.
//
// Reserving against balance_micros alone would let concurrent requests each pass the check
// and collectively hold more than the wallet contains. The guarantee is the arithmetic in
// the WHERE clause, so this asserts the clause rather than the outcome.
func TestReserveWalletGuardsOnUnheldBalance(t *testing.T) {
	srv := pgtest.New(t)
	var reserveSQL string
	srv.On("SELECT id,request_id,amount_micros,expires_at FROM wallet_holds", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: holdColumns(), Tag: "SELECT 0"}
	})
	srv.On("UPDATE wallets SET held_micros=held_micros+", func(q pgtest.Query) pgtest.Response {
		reserveSQL = q.SQL
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "balance_micros", OID: 20}, {Name: "held_micros", OID: 20}},
			Rows:    [][]*string{{pgtest.Text("100000000"), pgtest.Text("5000000")}},
			Tag:     "UPDATE 1",
		}
	})
	srv.On("INSERT INTO wallet_holds", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: holdColumns(), Rows: [][]*string{holdRow("req-3", "5000000")}, Tag: "INSERT 0 1"}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "OK"} })

	st := openTestStore(t, srv)
	if _, err := st.ReserveWallet(context.Background(), pgtest.UUID(1), pgtest.UUID(2), "req-3", 5_000_000); err != nil {
		t.Fatalf("ReserveWallet: %v", err)
	}
	if !strings.Contains(reserveSQL, "balance_micros-held_micros >=") {
		t.Errorf("the reserve guard does not subtract existing holds, so concurrent requests can over-reserve:\n%s", reserveSQL)
	}
}

// A non-positive hold is refused before any statement runs.
//
// Zero would reserve nothing and settle for whatever the request turned out to cost;
// negative would credit the wallet.
func TestReserveWalletRejectsNonPositiveAmounts(t *testing.T) {
	srv := pgtest.New(t)
	srv.OnAny(func(q pgtest.Query) pgtest.Response {
		if strings.Contains(q.SQL, "wallet") {
			t.Errorf("a non-positive hold reached the database: %s", q.SQL)
		}
		return pgtest.Response{Tag: "OK"}
	})
	st := openTestStore(t, srv)

	for _, amount := range []int64{0, -1_000_000} {
		if _, err := st.ReserveWallet(context.Background(), pgtest.UUID(1), pgtest.UUID(2), "req-4", amount); err == nil {
			t.Errorf("amount %d was accepted", amount)
		}
	}
}

// Releasing a hold that is not open is a no-op, not an error.
//
// Release runs on the failure path of a request whose outcome may already have been
// settled. Treating an already-closed hold as an error would turn a routine race into an
// alert, and worse, retrying it could release funds a second time.
func TestReleaseWalletIgnoresAlreadyClosedHolds(t *testing.T) {
	srv := pgtest.New(t)
	var released bool
	srv.On("SELECT id,user_id,amount_micros,status FROM wallet_holds", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{
				{Name: "id"}, {Name: "user_id"}, {Name: "amount_micros", OID: 20}, {Name: "status"},
			},
			Rows: [][]*string{{
				pgtest.Text(pgtest.UUID(3)), pgtest.Text(pgtest.UUID(1)),
				pgtest.Text("5000000"), pgtest.Text("settled"),
			}},
			Tag: "SELECT 1",
		}
	})
	srv.On("UPDATE wallets SET held_micros=held_micros-", func(pgtest.Query) pgtest.Response {
		released = true
		return pgtest.Response{Tag: "UPDATE 1"}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "OK"} })

	st := openTestStore(t, srv)
	if err := st.ReleaseWallet(context.Background(), "req-5"); err != nil {
		t.Fatalf("ReleaseWallet on a settled hold = %v, want nil", err)
	}
	if released {
		t.Error("a settled hold was released again, returning funds that were already charged")
	}
}

// Releasing an unknown request is reported, not silently accepted.
//
// The caller turns this into a "not_found" status rather than claiming a release happened.
func TestReleaseWalletReportsMissingHolds(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("SELECT id,user_id,amount_micros,status FROM wallet_holds", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{
				{Name: "id"}, {Name: "user_id"}, {Name: "amount_micros", OID: 20}, {Name: "status"},
			},
			Tag: "SELECT 0",
		}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "OK"} })

	st := openTestStore(t, srv)
	if err := st.ReleaseWallet(context.Background(), "req-missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ReleaseWallet on an unknown request = %v, want ErrNotFound", err)
	}
}

// A negative settlement is refused before any statement runs.
//
// Settling below zero would credit the wallet for serving a request.
func TestSettleWalletRejectsNegativeAmounts(t *testing.T) {
	srv := pgtest.New(t)
	srv.OnAny(func(q pgtest.Query) pgtest.Response {
		if strings.Contains(q.SQL, "wallet_holds") {
			t.Errorf("a negative settlement reached the database: %s", q.SQL)
		}
		return pgtest.Response{Tag: "OK"}
	})
	st := openTestStore(t, srv)
	if err := st.SettleWallet(context.Background(), "req-6", -1); err == nil {
		t.Error("a negative settlement was accepted")
	}
}

// Settling a request with no open hold is an error on the direct path.
//
// SettleWallet is called when the caller believes a hold exists. RecordUsage takes the
// same code path with allowMissing set, because usage can arrive for a request whose hold
// already expired; the distinction is what keeps that tolerance out of the direct path.
func TestSettleWalletRequiresAnOpenHold(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("SELECT id,user_id,amount_micros FROM wallet_holds", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "id"}, {Name: "user_id"}, {Name: "amount_micros", OID: 20}},
			Tag:     "SELECT 0",
		}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "OK"} })

	st := openTestStore(t, srv)
	if err := st.SettleWallet(context.Background(), "req-7", 1_000_000); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SettleWallet without an open hold = %v, want ErrNotFound", err)
	}
}

// Settlement releases the amount that was held, and charges what the request cost.
//
// These are different numbers -- the hold is an estimate -- so the statement has to carry
// both. Deducting the held amount instead of the actual cost would bill the estimate, and
// releasing the actual cost instead of the held amount would leave the difference held
// forever.
func TestSettleWalletChargesActualAndReleasesReserved(t *testing.T) {
	srv := pgtest.New(t)
	var settleSQL string
	srv.On("SELECT id,user_id,amount_micros FROM wallet_holds", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "id"}, {Name: "user_id"}, {Name: "amount_micros", OID: 20}},
			Rows: [][]*string{{
				pgtest.Text(pgtest.UUID(3)), pgtest.Text(pgtest.UUID(1)), pgtest.Text("9000000"),
			}},
			Tag: "SELECT 1",
		}
	})
	srv.On("SELECT promotional_micros FROM wallets", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "promotional_micros", OID: 20}},
			Rows:    [][]*string{{pgtest.Text("0")}},
			Tag:     "SELECT 1",
		}
	})
	srv.On("UPDATE wallets SET balance_micros=balance_micros-", func(q pgtest.Query) pgtest.Response {
		settleSQL = q.SQL
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "balance_micros", OID: 20}, {Name: "held_micros", OID: 20}},
			Rows:    [][]*string{{pgtest.Text("91000000"), pgtest.Text("0")}},
			Tag:     "UPDATE 1",
		}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "OK"} })

	st := openTestStore(t, srv)
	if err := st.SettleWallet(context.Background(), "req-8", 4_000_000); err != nil {
		t.Fatalf("SettleWallet: %v", err)
	}
	if !strings.Contains(settleSQL, "balance_micros=balance_micros-$2") {
		t.Errorf("settlement does not charge the actual cost:\n%s", settleSQL)
	}
	if !strings.Contains(settleSQL, "held_micros=held_micros-$3") {
		t.Errorf("settlement does not release the reserved amount:\n%s", settleSQL)
	}
}

// Settlement guards on both balance and held amount.
//
// This clause is why an under-reserved request cannot be charged: with balance below the
// cost the UPDATE matches nothing, the charge is lost and the hold stays open until it
// expires. That is the failure walletHoldTokens exists to prevent, and this asserts the
// guard is still what makes it a failure rather than a silent overdraft.
func TestSettleWalletRefusesToOverdraw(t *testing.T) {
	srv := pgtest.New(t)
	var settleSQL string
	srv.On("SELECT id,user_id,amount_micros FROM wallet_holds", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "id"}, {Name: "user_id"}, {Name: "amount_micros", OID: 20}},
			Rows: [][]*string{{
				pgtest.Text(pgtest.UUID(3)), pgtest.Text(pgtest.UUID(1)), pgtest.Text("1000000"),
			}},
			Tag: "SELECT 1",
		}
	})
	srv.On("SELECT promotional_micros FROM wallets", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "promotional_micros", OID: 20}},
			Rows:    [][]*string{{pgtest.Text("0")}},
			Tag:     "SELECT 1",
		}
	})
	srv.On("UPDATE wallets SET balance_micros=balance_micros-", func(q pgtest.Query) pgtest.Response {
		settleSQL = q.SQL
		return pgtest.Response{
			Columns: []pgtest.Column{{Name: "balance_micros", OID: 20}, {Name: "held_micros", OID: 20}},
			Tag:     "UPDATE 0",
		}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "OK"} })

	st := openTestStore(t, srv)
	err := st.SettleWallet(context.Background(), "req-9", 50_000_000)
	if err == nil {
		t.Error("settling more than the wallet holds succeeded, so the balance can go negative")
	}
	if !strings.Contains(settleSQL, "balance_micros >= $2") || !strings.Contains(settleSQL, "held_micros >= $3") {
		t.Errorf("settlement is missing a guard, so it could overdraw a wallet:\n%s", settleSQL)
	}
}

// Expired holds are swept one at a time, skipping rows another sweeper holds.
//
// The sweep is what returns funds after a data-plane crash. Without SKIP LOCKED two
// instances would serialise behind each other and a backlog would never drain; without the
// expires_at filter it would release holds still in flight.
func TestReleaseExpiredHoldsSkipsLockedRowsAndFiltersByExpiry(t *testing.T) {
	srv := pgtest.New(t)
	var sweepSQL string
	srv.On("FROM wallet_holds WHERE status='open'", func(q pgtest.Query) pgtest.Response {
		sweepSQL = q.SQL
		return pgtest.Response{
			Columns: []pgtest.Column{
				{Name: "id"}, {Name: "user_id"}, {Name: "request_id"}, {Name: "amount_micros", OID: 20},
			},
			Tag: "SELECT 0",
		}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "OK"} })

	st := openTestStore(t, srv)
	released, err := st.ReleaseExpiredHolds(context.Background(), 10)
	if err != nil {
		t.Fatalf("ReleaseExpiredHolds: %v", err)
	}
	if released != 0 {
		t.Errorf("released = %d on an empty sweep, want 0", released)
	}
	if !strings.Contains(sweepSQL, "SKIP LOCKED") {
		t.Errorf("the sweep does not skip locked rows, so concurrent sweepers block:\n%s", sweepSQL)
	}
	if !strings.Contains(sweepSQL, "expires_at<now()") {
		t.Errorf("the sweep does not filter on expiry, so it could release live holds:\n%s", sweepSQL)
	}
}

// An empty sweep stops rather than looping to the limit.
//
// The sweeper runs on a ticker. Continuing to query after the first empty result would
// issue the full limit in queries every tick on an idle system.
func TestReleaseExpiredHoldsStopsWhenNothingIsExpired(t *testing.T) {
	srv := pgtest.New(t)
	queries := 0
	srv.On("FROM wallet_holds WHERE status='open'", func(pgtest.Query) pgtest.Response {
		queries++
		return pgtest.Response{
			Columns: []pgtest.Column{
				{Name: "id"}, {Name: "user_id"}, {Name: "request_id"}, {Name: "amount_micros", OID: 20},
			},
			Tag: "SELECT 0",
		}
	})
	srv.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "OK"} })

	st := openTestStore(t, srv)
	if _, err := st.ReleaseExpiredHolds(context.Background(), 500); err != nil {
		t.Fatalf("ReleaseExpiredHolds: %v", err)
	}
	if queries != 1 {
		t.Errorf("an empty sweep issued %d queries, want 1", queries)
	}
}
