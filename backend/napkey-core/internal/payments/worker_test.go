package payments

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"napkey-core/internal/casso"
	"napkey-core/internal/pricing"
)

// The worker turns a bank transaction into a wallet credit. Everything it decides before
// touching the database is tested here: which payments are money, which are attributable,
// and how much a payment is worth.
//
// Getting any of these wrong loses a customer's money or credits it to the wrong account,
// and unlike a serving bug it is not visible in a response -- the customer simply does not
// receive what they paid for.

func envelope(t *testing.T, id int64, amount int64, description string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"data": map[string]any{
			"id":          id,
			"reference":   fmt.Sprintf("ref-%d", id),
			"description": description,
			"amount":      amount,
		},
	})
	if err != nil {
		t.Fatalf("building envelope: %v", err)
	}
	return raw
}

// An outgoing or zero-value transaction is not a payment.
//
// Casso reports both directions on an account. Crediting a withdrawal would hand a
// customer money they did not send, and the amount is the only thing distinguishing them.
func TestOnlyIncomingTransactionsAreMoney(t *testing.T) {
	for _, amount := range []int64{-50_000, 0} {
		tx, err := casso.ParseTransaction(envelope(t, 1, amount, "NK7QP2XV"))
		if err != nil {
			t.Fatalf("parsing: %v", err)
		}
		if tx.AmountVND > 0 {
			t.Errorf("amount %d was treated as an incoming payment", amount)
		}
	}

	tx, err := casso.ParseTransaction(envelope(t, 2, 50_000, "NK7QP2XV"))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if tx.AmountVND != 50_000 {
		t.Errorf("incoming amount = %d, want 50000", tx.AmountVND)
	}
}

// A payment with no recoverable memo is unmatched, not rejected.
//
// The distinction decides whether anyone looks at it again. A transfer whose memo the
// bank mangled is still a real customer's money; rejecting it outright would close the
// event and lose the payment silently, while "unmatched" is what
// CountStaleUnmatchedPayments alerts on.
func TestMemoIsRequiredToAttributeAPayment(t *testing.T) {
	withoutMemo := []string{
		"CHUYEN TIEN",
		"",
		"THANH TOAN DON HANG 12345",
		// Eight characters starting NK but not valid Crockford: I, L, O and U are
		// excluded from the alphabet precisely because they read as 1, 0 and V.
		"NKILOU12",
	}
	for _, description := range withoutMemo {
		if got := casso.ExtractMemoCode(description); got != "" {
			t.Errorf("description %q yielded memo %q, expected none", description, got)
		}
	}
}

// A memo survives what banks do to it.
//
// Vietnamese bank descriptions arrive folded, padded and wrapped in the bank's own
// wording. If the memo is only recognised in its pristine form, ordinary transfers land
// as unmatched and every one needs manual reconciliation.
func TestMemoSurvivesBankFormatting(t *testing.T) {
	tx, err := casso.ParseTransaction(envelope(t, 3, 50_000, "NK7QP2XV"))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	want := casso.ExtractMemoCode(tx.Description)
	if want == "" {
		t.Fatal("the baseline description yielded no memo; the rest of this test is meaningless")
	}

	variants := []string{
		"nk7qp2xv",
		"CT DEN:NK7QP2XV",
		"  NK7QP2XV  ",
		"TRANSFER NK7QP2XV FROM 0123456789",
	}
	for _, description := range variants {
		if got := casso.ExtractMemoCode(description); got != want {
			t.Errorf("description %q yielded memo %q, want %q", description, got, want)
		}
	}
}

// The credited amount comes from the transaction, not from the order.
//
// A customer can transfer more or less than they were quoted. Crediting the expected
// amount instead of the received one would either give away credit or keep money without
// giving credit, and both are only discoverable by hand.
func TestCreditFollowsTheAmountActuallyReceived(t *testing.T) {
	tx, err := casso.ParseTransaction(envelope(t, 4, 37_000, "NK7QP2XV"))
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	micros, err := pricing.MicrosFromVND(tx.AmountVND)
	if err != nil {
		t.Fatalf("converting: %v", err)
	}
	if want := int64(37_000) * pricing.MicrosPerVND; micros != want {
		t.Errorf("credited %d micros for a 37,000 VND transfer, want %d", micros, want)
	}
}

// An amount outside the supported range is rejected rather than truncated.
//
// pricing works in micro-VND on int64, so an absurd amount overflows. Silently wrapping
// it would write a negative or tiny credit into the ledger, which reconciles to nonsense.
func TestAbsurdAmountsAreRejectedNotWrapped(t *testing.T) {
	if _, err := pricing.MicrosFromVND(1 << 62); err == nil {
		t.Error("an amount that overflows micro-VND was accepted")
	}
}

// A malformed payload is an error, not an empty transaction.
//
// The worker rejects what it cannot parse. If parsing quietly returned a zero value, the
// event would be treated as a non-payment and closed, losing a real transfer whose
// envelope simply arrived in an unexpected shape.
func TestMalformedPayloadIsAnError(t *testing.T) {
	cases := map[string]string{
		"not json":       "{{{",
		"missing id":     `{"data":{"amount":50000,"description":"NK7QP2XV"}}`,
		"amount is text": `{"data":{"id":"tx-5","amount":"a lot","description":"NK7QP2XV"}}`,
	}
	for name, payload := range cases {
		if _, err := casso.ParseTransaction([]byte(payload)); err == nil {
			t.Errorf("%s: parsed without error", name)
		} else if strings.TrimSpace(err.Error()) == "" {
			t.Errorf("%s: error carries no message", name)
		}
	}
}
