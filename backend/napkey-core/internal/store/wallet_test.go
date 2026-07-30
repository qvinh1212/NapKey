package store

import (
	"context"
	"math"
	"strings"
	"testing"

	"napkey-core/internal/pgtest"
	"napkey-core/internal/pricing"
)

func TestFirstTopupBonusMicros(t *testing.T) {
	for _, tc := range []struct{name string; paid, purchased, want int64}{
		{"invalid", -1, 0, 0},
		{"below minimum receives nothing", 74_000 * pricing.MicrosPerVND, 74_000 * pricing.MicrosPerVND, 0},
		{"minimum matches purchase", 75_000 * pricing.MicrosPerVND, 75_000 * pricing.MicrosPerVND, FirstTopupBonusCapMicros},
		{"large topup remains capped", 375_000 * pricing.MicrosPerVND, FirstTopupBonusCapMicros*5, FirstTopupBonusCapMicros},
	} {
		t.Run(tc.name, func(t *testing.T) { if got:=firstTopupBonusMicros(tc.paid,tc.purchased); got!=tc.want { t.Fatalf("got %d, want %d",got,tc.want) } })
	}
}

func TestValidateTopupAmount(t *testing.T) {
	if err := ValidateTopupAmount(9_999_000_000); err == nil {
		t.Fatal("top-up below 10,000 VND must be rejected")
	}
	if err := ValidateTopupAmount(10_000_000_000); err != nil {
		t.Fatalf("minimum top-up should be accepted: %v", err)
	}
	if err := ValidateTopupAmount(10_001_000_000); err == nil {
		t.Fatal("top-up outside the 1,000 VND step must be rejected")
	}
	if err := ValidateTopupAmount(11_000_000_000); err != nil {
		t.Fatalf("top-up on the 1,000 VND step should be accepted: %v", err)
	}
}

func TestNormalizeTopupStatus(t *testing.T) {
	if got := topupStatus(50_000_000_000, 40_000_000_000); got != TopupUnderpaid {
		t.Fatalf("status = %q, want underpaid", got)
	}
	if got := topupStatus(50_000_000_000, 60_000_000_000); got != TopupPaid {
		t.Fatalf("status = %q, want paid", got)
	}
}

func TestWalletCreditValuePreservesLegacyPurchases(t *testing.T) {
	got, err := walletCreditMicros(45_000_000_000, 45)
	if err != nil {
		t.Fatalf("walletCreditMicros: %v", err)
	}
	if got != 75_000_000_000 {
		t.Fatalf("credited micros = %d, want 75000000000", got)
	}

	got, err = walletCreditMicros(60_000_000_000, 60)
	if err != nil || got != 75_000_000_000 {
		t.Fatalf("legacy 60 VND credit = %d, %v", got, err)
	}

	got, err = walletCreditMicros(75_000_000_000, 75)
	if err != nil || got != 75_000_000_000 {
		t.Fatalf("current-rate credit = %d, %v", got, err)
	}
	if _, err := walletCreditMicros(math.MaxInt64, 1); err == nil {
		t.Fatal("overflowing credit conversion should fail")
	}
}

func TestGetWalletQualifiesPromotionalColumnsDuringExpiry(t *testing.T) {
	srv := pgtest.New(t)
	srv.On("SELECT user_id, balance_micros", func(pgtest.Query) pgtest.Response {
		created := "2026-01-01 00:00:00+00"
		return pgtest.Response{
			Columns: []pgtest.Column{
				{Name: "user_id"}, {Name: "balance_micros", OID: 20},
				{Name: "held_micros", OID: 20}, {Name: "promotional_micros", OID: 20},
				{Name: "promotional_expires_at", OID: 1184}, {Name: "currency"},
				{Name: "updated_at", OID: 1184},
			},
			Rows: [][]*string{{
				pgtest.Text(pgtest.UUID(1)), pgtest.Text("3000000000"), pgtest.Text("0"),
				pgtest.Text("3000000000"), pgtest.Text(created), pgtest.Text("VND"), pgtest.Text(created),
			}},
			Tag: "SELECT 1",
		}
	})
	st := openTestStore(t, srv)

	if _, err := st.GetWallet(context.Background(), pgtest.UUID(1)); err != nil {
		t.Fatalf("GetWallet: %v", err)
	}
	q, ok := srv.FindQuery("UPDATE wallets w")
	if !ok {
		t.Fatal("promotional expiry query was not executed")
	}
	for _, fragment := range []string{
		"w.balance_micros-c.removable",
		"w.promotional_micros-c.removable",
		"ELSE w.promotional_expires_at",
	} {
		if !strings.Contains(q.SQL, fragment) {
			t.Errorf("promotional expiry query is missing qualified reference %q", fragment)
		}
	}
}
