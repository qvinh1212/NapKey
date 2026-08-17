package store

import (
	"context"
	"math"
	"os"
	"strings"
	"testing"

	"napkey-core/internal/pgtest"
	"napkey-core/internal/pricing"
)

func TestFirstTopupBonusMicrosIsDisabled(t *testing.T) {
	for _, tc := range []struct{name string; paid, purchased, want int64}{
		{"invalid", -1, 0, 0},
		{"below minimum receives nothing", 74_000 * pricing.MicrosPerVND, 74_000 * pricing.MicrosPerVND, 0},
		{"eligible first topup receives nothing", 75_000 * pricing.MicrosPerVND, 75_000 * pricing.MicrosPerVND, 0},
		{"large topup receives nothing", 375_000 * pricing.MicrosPerVND, 1_000_000_000, 0},
	} {
		t.Run(tc.name, func(t *testing.T) { if got:=firstTopupBonusMicros(tc.paid,tc.purchased); got!=tc.want { t.Fatalf("got %d, want %d",got,tc.want) } })
	}
}

func TestTopupSettlementDoesNotGrantNewPromotion(t *testing.T) {
	source, err := os.ReadFile("wallet.go")
	if err != nil { t.Fatalf("read wallet.go: %v", err) }
	if got := strings.Count(string(source), "grantFirstTopupBonusTx("); got != 1 {
		t.Fatalf("grantFirstTopupBonusTx references = %d, want only the legacy function definition", got)
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

// An order pays out the credits it was quoted, whatever the rate has since become.
//
// Each case below bought 1,000 credits: 45,000 VND when a credit cost 45, 60,000 when it
// cost 60, 75,000 at today's 75. All three therefore credit the same wallet value, which
// is what makes an old unpaid order safe to settle after a repricing. The 75 case is exact
// identity -- money in equals money out -- because the order rate and the retail rate
// agree; the expectations here move whenever pricing.RetailVNDPerCredit does, so a change
// that forgets this test is a change that quietly repriced settled quotes.
func TestWalletCreditValuePreservesLegacyPurchases(t *testing.T) {
	const thousandCredits = 1_000 * pricing.RetailMicrosPerCredit

	got, err := walletCreditMicros(45_000_000_000, 45)
	if err != nil {
		t.Fatalf("walletCreditMicros: %v", err)
	}
	if got != thousandCredits {
		t.Fatalf("credited micros = %d, want %d", got, thousandCredits)
	}

	got, err = walletCreditMicros(60_000_000_000, 60)
	if err != nil || got != thousandCredits {
		t.Fatalf("legacy 60 VND credit = %d, %v", got, err)
	}

	got, err = walletCreditMicros(75_000_000_000, 75)
	if err != nil || got != thousandCredits {
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
