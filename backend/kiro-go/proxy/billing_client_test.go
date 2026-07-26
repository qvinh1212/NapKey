package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBillingClientReserveAndRelease(t *testing.T) {
	var reserveAuth, releaseAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/wallet/reserve": reserveAuth = r.Header.Get("X-Internal-Token")
		case "/internal/wallet/release": releaseAuth = r.Header.Get("X-Internal-Token")
		default: t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := newBillingClient(server.URL, "secret")
	lease, err := client.Reserve(context.Background(), "key-1", "claude-sonnet-4.5", 100, 200)
	if err != nil { t.Fatalf("Reserve: %v", err) }
	if lease.RequestID == "" { t.Fatal("Reserve returned an empty request id") }
	if err := client.Release(context.Background(), lease.RequestID); err != nil { t.Fatalf("Release: %v", err) }
	if reserveAuth != "secret" || releaseAuth != "secret" { t.Fatal("internal token was not forwarded") }
}

func TestBillingClientMapsInsufficientBalance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusPaymentRequired) }))
	defer server.Close()
	_, err := newBillingClient(server.URL, "secret").Reserve(context.Background(), "key-1", "claude-sonnet-4.5", 100, 200)
	if err != errWalletInsufficient { t.Fatalf("Reserve error = %v, want errWalletInsufficient", err) }
}
