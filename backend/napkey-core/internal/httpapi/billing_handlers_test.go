package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"napkey-core/internal/pgtest"
	"napkey-core/internal/payos"
)

func TestPayOSWebhookRejectsSignedPayloadWithoutCompleteTransactionIdentity(t *testing.T) {
	h := newHarness(t)
	h.server.cfg.PayOSChecksumKey = "checksum-secret"
	data := map[string]any{
		"orderCode": json.Number("123456"),
		"amount": json.Number("60000"),
		"paymentLinkId": "link-1",
	}
	signature, err := payos.SignWebhookData(data, "checksum-secret")
	if err != nil { t.Fatal(err) }
	w := h.do(http.MethodPost, "/webhooks/payos", map[string]any{
		"code": "00", "success": true, "signature": signature, "data": data,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
	if _, ok := h.pg.FindQuery("INSERT INTO payment_events"); ok {
		t.Fatal("incomplete PayOS identity reached the payment ledger")
	}
}

func TestPayOSTransactionIDRequiresBothStableFields(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		ok   bool
	}{
		{"complete", map[string]any{"paymentLinkId": "link-1", "reference": "FT123"}, true},
		{"missing link", map[string]any{"reference": "FT123"}, false},
		{"missing reference", map[string]any{"paymentLinkId": "link-1"}, false},
		{"blank link", map[string]any{"paymentLinkId": " ", "reference": "FT123"}, false},
		{"blank reference", map[string]any{"paymentLinkId": "link-1", "reference": " "}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := payOSTransactionID(tc.data)
			if ok != tc.ok { t.Fatalf("ok = %v, want %v", ok, tc.ok) }
			if tc.ok && got != "link-1:FT123" { t.Fatalf("id = %q", got) }
		})
	}
}

func TestTopupHistoryIsScopedToAuthenticatedUser(t *testing.T) {
	h := newHarness(t)
	const userID = "00000000-0000-4000-8000-000000000042"
	h.sessionFor(userID, "billing@napkey.vn", "active", true)
	h.pg.On("UPDATE topup_orders SET status='expired'", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Tag: "UPDATE 0"}
	})
	h.pg.On("FROM topup_orders WHERE user_id=$1", func(pgtest.Query) pgtest.Response {
		return pgtest.Response{Columns: []pgtest.Column{
			{Name: "id"}, {Name: "user_id"}, {Name: "memo_code"}, {Name: "provider"},
			{Name: "provider_order_code", OID: 20}, {Name: "provider_payment_link_id"},
			{Name: "checkout_url"}, {Name: "qr_code"}, {Name: "bank_account_number"},
			{Name: "status"}, {Name: "expected_amount_micros", OID: 20},
			{Name: "received_amount_micros", OID: 20}, {Name: "retail_vnd_per_credit", OID: 20},
			{Name: "expires_at", OID: 1184}, {Name: "paid_at", OID: 1184},
			{Name: "created_at", OID: 1184},
		}, Tag: "SELECT 0"}
	})

	w := h.do(http.MethodGet, "/v1/me/topups", nil, h.authed("token"))
	if w.Code != http.StatusOK { t.Fatalf("status = %d; body: %s", w.Code, w.Body.String()) }
	query, ok := h.pg.FindQuery("FROM topup_orders WHERE user_id=$1")
	if !ok { t.Fatal("top-up history query did not run") }
	if len(query.Params) != 2 || query.Params[0] != userID || query.Params[1] != "50" {
		t.Fatalf("params = %v, want authenticated user and bounded limit", query.Params)
	}
}
