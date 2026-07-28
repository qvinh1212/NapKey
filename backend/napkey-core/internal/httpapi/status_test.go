package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicStatusIsAnonymousAndRedacted(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodGet, "/v1/status", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := decode(t, w)
	if body["status"] == nil || body["components"] == nil || body["checkedAt"] == nil {
		t.Fatalf("incomplete public status: %+v", body)
	}
	for _, forbidden := range []string{"accounts", "available", "errorRatePercent", "issues"} {
		if _, exists := body[forbidden]; exists {
			t.Fatalf("public status exposed %q: %+v", forbidden, body)
		}
	}
}

func TestCanceledCallerCannotPoisonPublicStatusRefresh(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/v1/status", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	calls := 0
	for _, call := range h.planeLog {
		if call.Path == "/admin/api/status" {
			calls++
		}
	}
	if calls != 1 {
		t.Fatalf("data-plane status calls = %d, want 1 despite canceled caller", calls)
	}
}

func TestPublicStatusCachesDependencyChecks(t *testing.T) {
	h := newHarness(t)
	for range 2 {
		if w := h.do(http.MethodGet, "/v1/status", nil); w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	}
	calls := 0
	for _, call := range h.planeLog {
		if call.Path == "/admin/api/status" {
			calls++
		}
	}
	if calls != 1 {
		t.Fatalf("data-plane status calls = %d, want 1", calls)
	}
}
