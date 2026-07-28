package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicErrorMessageRedactsUpstreamBrandAndHosts(t *testing.T) {
	message := publicErrorMessage("Kiro-Go failed at https://runtime.us-east-1.kiro.dev/ for KIRO profile")
	lower := strings.ToLower(message)
	if strings.Contains(lower, "kiro") {
		t.Fatalf("public message leaked upstream brand: %q", message)
	}
	if strings.Contains(lower, "runtime.us-east-1") {
		t.Fatalf("public message leaked upstream host: %q", message)
	}
	if message != "upstream failed at upstream service for upstream profile" {
		t.Fatalf("public message = %q", message)
	}
}

func TestPublicErrorMessageLeavesUnrelatedValidationTextAlone(t *testing.T) {
	const message = "model and messages are required"
	if got := publicErrorMessage(message); got != message {
		t.Fatalf("public message = %q, want %q", got, message)
	}
}

func TestServerErrorsNeverExposeRawUpstreamDetails(t *testing.T) {
	raw := "AWS CodeWhisperer arn:aws:codewhisperer:us-east-1:123456789012:profile/demo"
	if got := publicProtocolError(http.StatusBadGateway, raw); got != genericUpstreamError {
		t.Fatalf("server error = %q", got)
	}
}

func TestProtocolErrorWritersApplyBrandBoundary(t *testing.T) {
	h := &Handler{}
	for _, tc := range []struct {
		name  string
		write func(http.ResponseWriter)
	}{
		{name: "claude", write: func(w http.ResponseWriter) {
			h.sendClaudeError(w, http.StatusBadGateway, "api_error", "Kiro runtime failed")
		}},
		{name: "openai", write: func(w http.ResponseWriter) {
			h.sendOpenAIError(w, http.StatusBadGateway, "server_error", "Kiro runtime failed")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			tc.write(recorder)
			if strings.Contains(strings.ToLower(recorder.Body.String()), "kiro") {
				t.Fatalf("public response leaked upstream brand: %s", recorder.Body.String())
			}
		})
	}
}

func TestPublicModelAliasesAreOwnedByNapKey(t *testing.T) {
	model := buildModelInfo("auto", "napkey", true)
	encoded, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(encoded)), "kiro") {
		t.Fatalf("model metadata leaked upstream brand: %s", encoded)
	}
}

func TestAdminAssetsCanBeRestrictedToAnOperatorHost(t *testing.T) {
	h := &Handler{adminHost: normalizeAdminHost("https://ops.napkey.io.vn")}

	operatorRequest := httptest.NewRequest(http.MethodGet, "https://ops.napkey.io.vn/admin/", nil)
	if !h.adminHostAllowed(operatorRequest) {
		t.Fatal("operator hostname should be allowed")
	}
	publicRequest := httptest.NewRequest(http.MethodGet, "https://api.napkey.io.vn/admin/", nil)
	if h.adminHostAllowed(publicRequest) {
		t.Fatal("public API hostname should not expose admin assets")
	}
}

func TestAdminAssetsFailClosedWithoutAnOperatorHost(t *testing.T) {
	h := &Handler{}
	request := httptest.NewRequest(http.MethodGet, "https://api.napkey.io.vn/admin/", nil)
	if h.adminHostAllowed(request) {
		t.Fatal("missing ADMIN_HOST should disable browser admin assets")
	}
}

func TestInternalAdminAPIRoutingDoesNotRequireOperatorHost(t *testing.T) {
	for _, host := range []string{"kiro-go", "host.docker.internal"} {
		t.Run(host, func(t *testing.T) {
			h := &Handler{}
			request := httptest.NewRequest(http.MethodGet, "http://"+host+":8080/admin/api/accounts", nil)
			recorder := httptest.NewRecorder()

			h.ServeHTTP(recorder, request)
			if recorder.Code == http.StatusNotFound {
				t.Fatal("internal admin API must remain routable independently of the browser admin host")
			}
		})
	}
}

func TestPublicAPIHostDoesNotRouteAdminAPI(t *testing.T) {
	h := &Handler{adminHost: "ops.napkey.io.vn"}
	request := httptest.NewRequest(http.MethodGet, "https://api.napkey.io.vn/admin/api/accounts", nil)
	recorder := httptest.NewRecorder()

	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("public API host returned %d, want 404", recorder.Code)
	}
}

func TestOpenAIStreamFailureIsSanitizedAndTerminated(t *testing.T) {
	recorder := httptest.NewRecorder()
	sendOpenAIStreamFailure(recorder, recorder)
	body := recorder.Body.String()
	if !strings.Contains(body, genericUpstreamError) || !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("stream failure is incomplete: %q", body)
	}
}
