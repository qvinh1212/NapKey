package payos

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSignPaymentRequestUsesPayOSCanonicalOrder(t *testing.T) {
	req := CreatePaymentRequest{
		OrderCode:   123456,
		Amount:      45_000,
		Description: "NAPKEY NKABC123",
		CancelURL:   "https://napkey.io.vn/vi/console/wallet?payment=cancelled",
		ReturnURL:   "https://napkey.io.vn/vi/console/wallet?payment=success",
	}
	const want = "71fe89cd73aaab34461ed9d3326632aac04ed2d7aa910bf3a4da6f9c7e555203"
	if got := SignPaymentRequest(req, "test-secret"); got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
}

func TestVerifyWebhookRejectsTampering(t *testing.T) {
	data := map[string]any{
		"amount":        json.Number("45000"),
		"orderCode":     json.Number("123456"),
		"paymentLinkId": "link-1",
		"reference":     "FT123",
	}
	signature, err := SignWebhookData(data, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyWebhookData(data, signature, "test-secret"); err != nil {
		t.Fatalf("valid webhook rejected: %v", err)
	}
	data["amount"] = json.Number("45001")
	if err := VerifyWebhookData(data, signature, "test-secret"); err == nil {
		t.Fatal("tampered webhook was accepted")
	}
}

func TestCreatePaymentAuthenticatesAndDecodesCheckout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v2/payment-requests" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("x-client-id") != "client-id" || r.Header.Get("x-api-key") != "api-key" {
			t.Fatal("PayOS credentials were not sent in headers")
		}
		var body CreatePaymentRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Signature == "" {
			t.Fatal("payment request did not include a signature")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"00","desc":"success","data":{"bin":"970436","accountNumber":"0123456789","accountName":"NAPKEY","amount":45000,"description":"NAPKEY NKABC123","orderCode":123456,"paymentLinkId":"link-1","status":"PENDING","checkoutUrl":"https://pay.payos.vn/web/link-1","qrCode":"qr-payload"}}`))
	}))
	defer server.Close()

	client := NewClient("client-id", "api-key", "checksum")
	client.endpoint = server.URL
	checkout, err := client.CreatePayment(context.Background(), CreatePaymentRequest{
		OrderCode: 123456, Amount: 45_000, Description: "NAPKEY NKABC123",
		CancelURL: "https://example.com/cancel", ReturnURL: "https://example.com/success",
	})
	if err != nil {
		t.Fatal(err)
	}
	if checkout.CheckoutURL != "https://pay.payos.vn/web/link-1" || checkout.QRCode != "qr-payload" {
		t.Fatalf("checkout = %+v", checkout)
	}
}

func TestCreatePaymentDoesNotLeakProviderErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"401","desc":"invalid api key: secret-value"}`))
	}))
	defer server.Close()
	client := NewClient("client-id", "api-key", "checksum")
	client.endpoint = server.URL
	_, err := client.CreatePayment(context.Background(), CreatePaymentRequest{OrderCode: 1, Amount: 45_000})
	if err == nil || err.Error() != "payos: payment service rejected the request" {
		t.Fatalf("error = %v", err)
	}
}

func TestCreatePaymentRejectsCheckoutOutsidePayOS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":"00","data":{"amount":45000,"orderCode":1,"checkoutUrl":"https://attacker.example/phish"}}`))
	}))
	defer server.Close()
	client := NewClient("client-id", "api-key", "checksum")
	client.endpoint = server.URL
	_, err := client.CreatePayment(context.Background(), CreatePaymentRequest{OrderCode: 1, Amount: 45_000})
	if err == nil || err.Error() != "payos: payment service returned an unsafe checkout URL" {
		t.Fatalf("error = %v", err)
	}
}
