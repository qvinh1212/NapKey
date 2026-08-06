package store

import (
	"errors"
	"strings"
	"testing"

	"napkey-core/internal/pgtest"
)

// A failing background job has to become visible without anyone reading logs.
//
// Two queries failed on every tick from at least 2026-07-30 to 2026-08-06 and nobody
// knew, because a periodic job logs a warning and carries on -- correctly, since one
// bad sweep must not stop the process. The cost was real: the unmatched-payment alert
// never opened, and expired trial credit was never reclaimed from dormant wallets.
func TestBackgroundJobFailureOpensAnAlert(t *testing.T) {
	pg := pgtest.New(t)
	st, err := Open(t.Context(), pg.DSN(), 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	pg.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "INSERT 0 1"} })
	st.RecordBackgroundJobResult(t.Context(), "promotional-expiry", errors.New("boom"))

	query, ok := pg.FindQuery("operations_alerts")
	if !ok {
		t.Fatal("a failing job must write an alert")
	}
	if !strings.Contains(query.SQL, "INSERT INTO operations_alerts") {
		t.Errorf("expected an alert insert, got: %s", query.SQL)
	}
	// One open alert per job, however many ticks fail, or a broken job buries the
	// dashboard it is meant to appear on.
	if !strings.Contains(query.SQL, "ON CONFLICT (fingerprint) WHERE status = 'open'") {
		t.Error("repeated failures must update the open alert, not stack up new ones")
	}
}

// The alert clears itself, so a transient database blip does not need a human.
func TestBackgroundJobSuccessResolvesTheAlert(t *testing.T) {
	pg := pgtest.New(t)
	st, err := Open(t.Context(), pg.DSN(), 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	pg.OnAny(func(pgtest.Query) pgtest.Response { return pgtest.Response{Tag: "UPDATE 1"} })
	st.RecordBackgroundJobResult(t.Context(), "promotional-expiry", nil)

	query, ok := pg.FindQuery("operations_alerts")
	if !ok {
		t.Fatal("a successful run must resolve any open alert")
	}
	if !strings.Contains(query.SQL, "SET status = 'resolved'") {
		t.Errorf("expected a resolve, got: %s", query.SQL)
	}
}
