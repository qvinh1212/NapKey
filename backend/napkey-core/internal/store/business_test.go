package store

import (
	"testing"
	"time"

	"napkey-core/internal/pgtest"
)

func TestGetBusinessSummaryUsesOneBoundedAggregateQuery(t *testing.T) {
	pg := pgtest.New(t)
	st, err := Open(t.Context(), pg.DSN(), 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	pg.On("business_accounts", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{
			Columns: []pgtest.Column{
				{Name: "new_users", OID: 20}, {Name: "verified_users", OID: 20},
				{Name: "activated_new_users", OID: 20}, {Name: "new_paying_users", OID: 20},
				{Name: "paying_customers", OID: 20}, {Name: "repeat_customers", OID: 20},
				{Name: "paid_orders", OID: 20},
				{Name: "cash_collected", OID: 20}, {Name: "wallet_liability", OID: 20},
			},
			Rows: [][]*string{{
				pgtest.Text("20"), pgtest.Text("15"), pgtest.Text("8"), pgtest.Text("5"),
				pgtest.Text("6"), pgtest.Text("2"), pgtest.Text("7"), pgtest.Text("420000000000"), pgtest.Text("180000000000"),
			}}, Tag: "SELECT 1",
		}
	})

	since := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	summary, err := st.GetBusinessSummary(t.Context(), since)
	if err != nil {
		t.Fatal(err)
	}
	if summary.NewUsers != 20 || summary.NewPayingUsers != 5 || summary.PayingCustomers != 6 || summary.CashCollectedMicros != 420000000000 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	query, ok := pg.FindQuery("business_accounts")
	if !ok || len(query.Params) != 1 {
		t.Fatalf("query params = %v, want one bound since value", query.Params)
	}
}
