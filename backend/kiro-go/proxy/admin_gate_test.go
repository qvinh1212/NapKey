package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"kiro-go/config"
)

// An unloaded config must not admit an admin request.
//
// The gate authenticates by comparing the submitted password against the configured
// one. Before Load there is no configured password, and the natural stand-in for
// "none" is the empty string -- which a request that sends no password header at all
// compares equal to. That combination hands the admin panel, and every account
// credential it manages, to an unauthenticated caller.
//
// config.GetPassword reports availability separately so the gate can tell "no
// password configured" apart from "the password is empty", and this pins that an
// unavailable password is refused rather than matched.
func TestAdminGateRefusesWhenNoPasswordIsAvailable(t *testing.T) {
	t.Cleanup(config.UnloadForTest())

	handler := &Handler{}
	for _, tc := range []struct {
		name     string
		password string
	}{
		{"no password header", ""},
		{"empty password header", ""},
		{"guessed password", "changeme"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/api/not-a-real-endpoint", nil)
			if tc.password != "" {
				req.Header.Set("X-Admin-Password", tc.password)
			}
			rec := httptest.NewRecorder()

			handler.handleAdminAPI(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401: the admin panel was served without a configured password", rec.Code)
			}
		})
	}
}

// A loaded config still authenticates normally.
//
// The guard above must refuse an unavailable password without also refusing the
// operator who supplies the right one. Asserted against an unrouted admin path so the
// result reflects the gate rather than whatever a real endpoint does with a
// zero-value Handler: reaching the router at all means the password was accepted.
func TestAdminGateAcceptsTheConfiguredPassword(t *testing.T) {
	if err := config.Init(t.TempDir() + "/config.json"); err != nil {
		t.Fatalf("config.Init: %v", err)
	}
	config.SetPassword("correct-horse-battery-staple")

	req := httptest.NewRequest(http.MethodGet, "/admin/api/not-a-real-endpoint", nil)
	req.Header.Set("X-Admin-Password", "correct-horse-battery-staple")
	rec := httptest.NewRecorder()

	(&Handler{}).handleAdminAPI(rec, req)

	if rec.Code == http.StatusUnauthorized {
		t.Fatal("the configured admin password was rejected")
	}
}

// A wrong password is refused even when one is configured.
func TestAdminGateRefusesAWrongPassword(t *testing.T) {
	if err := config.Init(t.TempDir() + "/config.json"); err != nil {
		t.Fatalf("config.Init: %v", err)
	}
	config.SetPassword("correct-horse-battery-staple")

	req := httptest.NewRequest(http.MethodGet, "/admin/api/not-a-real-endpoint", nil)
	req.Header.Set("X-Admin-Password", "wrong")
	rec := httptest.NewRecorder()

	(&Handler{}).handleAdminAPI(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
