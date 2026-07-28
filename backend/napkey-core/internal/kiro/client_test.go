package kiro

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeDataPlane stands in for kiro-go's admin API.
type fakeDataPlane struct {
	*httptest.Server
	password string
	// requests records what arrived, so tests can assert on the wire contract
	// between the two services rather than just the return value.
	requests []recordedRequest
}

type recordedRequest struct {
	Method string
	Path   string
	Auth   string
	Body   map[string]any
}

func newFakeDataPlane(t *testing.T, password string, handler func(w http.ResponseWriter, r *http.Request, body map[string]any)) *fakeDataPlane {
	t.Helper()
	dp := &fakeDataPlane{password: password}
	dp.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		dp.requests = append(dp.requests, recordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Auth:   r.Header.Get("X-Admin-Password"),
			Body:   body,
		})
		// kiro-go gates /admin/api/* on this header.
		if r.Header.Get("X-Admin-Password") != dp.password {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		handler(w, r, body)
	}))
	t.Cleanup(dp.Close)
	return dp
}

func TestCreateKeySendsCleartextAndReturnsRemoteID(t *testing.T) {
	dp := newFakeDataPlane(t, "admin-pw", func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"id":      "remote-key-1",
			"key":     body["key"],
		})
	})
	client := New(dp.URL, "admin-pw")

	enabled := true
	remoteID, err := client.CreateKey(context.Background(), CreateKeyRequest{
		Name:        "napkey:local-1 (a@napkey.vn)",
		Key:         "nk_live_abc",
		Enabled:     &enabled,
		TokenLimit:  1000,
		CreditLimit: 2.5,
	})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	if remoteID != "remote-key-1" {
		t.Errorf("remoteID = %q, want remote-key-1", remoteID)
	}

	if len(dp.requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(dp.requests))
	}
	req := dp.requests[0]
	if req.Method != http.MethodPost || req.Path != "/admin/api/api-keys" {
		t.Errorf("unexpected request: %s %s", req.Method, req.Path)
	}
	// napkey-core generates the key so it can hash it before storing; the data
	// plane must receive that exact value or the two would disagree.
	if req.Body["key"] != "nk_live_abc" {
		t.Errorf("key sent = %v, want nk_live_abc", req.Body["key"])
	}
}

func TestOperationsStatusReadsPoolAndUsageReportingHealth(t *testing.T) {
	dp := newFakeDataPlane(t, "admin-pw", func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		if r.URL.Path != "/admin/api/status" {
			t.Fatalf("path = %q, want operations status", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"version": "1.2.3", "accounts": 4, "available": 3, "recentRequests": 25, "recentFailures": 2,
			"usageReporting": map[string]any{"enabled": 1, "healthy": 1, "sent": 20, "dropped": 0, "pending": 2},
		})
	})
	status, err := New(dp.URL, "admin-pw").OperationsStatus(context.Background())
	if err != nil {
		t.Fatalf("OperationsStatus: %v", err)
	}
	if status.Available != 3 || status.RecentRequests != 25 || status.RecentFailures != 2 || status.UsageReporting.Healthy != 1 || status.UsageReporting.Pending != 2 {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestCreateKeyTreatsDuplicateAsSuccess(t *testing.T) {
	// kiro-go rejects a duplicate key value. The desired state is already true, so
	// retrying forever would be wrong.
	dp := newFakeDataPlane(t, "pw", func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "api key already exists"})
	})
	client := New(dp.URL, "pw")

	if _, err := client.CreateKey(context.Background(), CreateKeyRequest{Key: "nk_live_dup"}); err != nil {
		t.Fatalf("a duplicate key should be treated as success, got: %v", err)
	}
}

func TestUnauthorizedIsDistinguishable(t *testing.T) {
	dp := newFakeDataPlane(t, "correct-pw", func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		json.NewEncoder(w).Encode(map[string]any{"apiKeys": []any{}})
	})
	client := New(dp.URL, "wrong-pw")

	err := client.Health(context.Background())
	if !errors.Is(err, ErrUnauthorized) {
		// Startup turns this specific error into a hard failure, since every key
		// creation would break otherwise.
		t.Fatalf("expected ErrUnauthorized, got: %v", err)
	}
}

func TestDeleteKeyTreatsMissingAsSuccess(t *testing.T) {
	dp := newFakeDataPlane(t, "pw", func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "API key not found"})
	})
	client := New(dp.URL, "pw")

	// The goal is that the key no longer authenticates. Already gone satisfies it.
	if err := client.DeleteKey(context.Background(), "remote-1"); err != nil {
		t.Fatalf("deleting an already-missing key should succeed, got: %v", err)
	}
}

func TestDeleteKeyRequiresRemoteID(t *testing.T) {
	client := New("http://127.0.0.1:1", "pw")
	if err := client.DeleteKey(context.Background(), ""); err == nil {
		t.Error("expected an error when no remote id is supplied")
	}
	if err := client.UpdateKey(context.Background(), "", UpdateKeyRequest{}); err == nil {
		t.Error("expected an error when no remote id is supplied")
	}
}

func TestUpdateKeySendsOnlyProvidedFields(t *testing.T) {
	dp := newFakeDataPlane(t, "pw", func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	})
	client := New(dp.URL, "pw")

	enabled := false
	if err := client.UpdateKey(context.Background(), "remote-1", UpdateKeyRequest{Enabled: &enabled}); err != nil {
		t.Fatalf("UpdateKey: %v", err)
	}
	req := dp.requests[0]
	if req.Method != http.MethodPut || req.Path != "/admin/api/api-keys/remote-1" {
		t.Errorf("unexpected request: %s %s", req.Method, req.Path)
	}
	// Omitted fields must stay absent so kiro-go's patch semantics leave them
	// alone rather than resetting them to a zero value.
	if _, present := req.Body["name"]; present {
		t.Error("name should not be sent when it was not set")
	}
	if v, present := req.Body["enabled"]; !present || v != false {
		t.Errorf("enabled = %v (present=%v), want false", v, present)
	}
}

func TestListKeysParsesResponse(t *testing.T) {
	dp := newFakeDataPlane(t, "pw", func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		json.NewEncoder(w).Encode(map[string]any{
			"apiKeys": []map[string]any{
				{"id": "r1", "keyMasked": "nk_live_ab****cd12", "enabled": true, "tokensUsed": 42},
				{"id": "r2", "keyMasked": "nk_test_cd****ef34", "enabled": false},
			},
		})
	})
	client := New(dp.URL, "pw")

	keys, err := client.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("got %d keys, want 2", len(keys))
	}
	if keys[0].ID != "r1" || !keys[0].Enabled || keys[0].TokensUsed != 42 {
		t.Errorf("first key parsed wrong: %+v", keys[0])
	}
}

func TestServerErrorIsReported(t *testing.T) {
	dp := newFakeDataPlane(t, "pw", func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("<html>internal server error</html>"))
	})
	client := New(dp.URL, "pw")

	_, err := client.CreateKey(context.Background(), CreateKeyRequest{Key: "nk_live_x"})
	if err == nil {
		t.Fatal("expected a 500 to produce an error")
	}
	// A key creation must never look successful when the data plane failed.
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention the status code, got: %v", err)
	}
}

func TestContextCancellationIsRespected(t *testing.T) {
	dp := newFakeDataPlane(t, "pw", func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		json.NewEncoder(w).Encode(map[string]any{"apiKeys": []any{}})
	})
	client := New(dp.URL, "pw")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.ListKeys(ctx); err == nil {
		t.Error("expected a canceled context to fail the request")
	}
}

func TestUnreachableDataPlaneFails(t *testing.T) {
	// Port 1 is not listening, standing in for a data plane that is down.
	client := New("http://127.0.0.1:1", "pw")
	if err := client.Health(context.Background()); err == nil {
		t.Error("expected an unreachable data plane to produce an error")
	}
}
