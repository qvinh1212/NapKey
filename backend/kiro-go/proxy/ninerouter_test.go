package proxy

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Absent usage must be distinguishable from zero usage.
//
// Billing treats them differently: a missing block is a reporting gap that must not
// be priced, while a genuine zero is a request that consumed nothing. Collapsing
// them into the same value is how served traffic becomes free.
func TestParseNineRouterUsageDistinguishesAbsentFromZero(t *testing.T) {
	if got := parseNineRouterUsage([]byte(`{"id":"x","choices":[]}`)); got != nil {
		t.Errorf("a response with no usage block must return nil, got %+v", got)
	}
	got := parseNineRouterUsage([]byte(`{"usage":{"prompt_tokens":0,"completion_tokens":0}}`))
	if got == nil {
		t.Fatal("an explicit zero usage block must not be reported as absent")
	}
	if got.PromptTokens != 0 || got.CompletionTokens != 0 {
		t.Errorf("unexpected values: %+v", got)
	}
}

func TestParseNineRouterUsageReadsTokenCounts(t *testing.T) {
	got := parseNineRouterUsage([]byte(`{"usage":{"prompt_tokens":1200,"completion_tokens":340,"total_tokens":1540}}`))
	if got == nil {
		t.Fatal("usage should have parsed")
	}
	if got.PromptTokens != 1200 || got.CompletionTokens != 340 {
		t.Errorf("got %+v", got)
	}
}

// Cached prompt tokens are reported separately so billing can price them apart.
func TestParseNineRouterUsageReadsCachedTokens(t *testing.T) {
	got := parseNineRouterUsage([]byte(
		`{"usage":{"prompt_tokens":1000,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":800}}}`))
	if got == nil || got.PromptTokensDetails == nil {
		t.Fatal("cached token details should have parsed")
	}
	if got.PromptTokensDetails.CachedTokens != 800 {
		t.Errorf("cached = %d, want 800", got.PromptTokensDetails.CachedTokens)
	}
	// Fresh input is the part that is not a cache read. Charging the full prompt
	// would bill the cached portion at the fresh rate.
	fresh := got.PromptTokens - got.PromptTokensDetails.CachedTokens
	if fresh != 200 {
		t.Errorf("fresh input = %d, want 200", fresh)
	}
}

// Streaming requests must ask for the usage frame.
//
// This is the bug that makes every streamed completion free: OpenAI-compatible
// providers omit token counts unless stream_options.include_usage is set, and
// without them the charge computes to zero while the upstream still bills us.
func TestInjectStreamUsageForcesIncludeUsage(t *testing.T) {
	out, clientOptedIn := injectStreamUsage([]byte(`{"model":"m","stream":true,"messages":[]}`))
	if clientOptedIn {
		t.Error("the client did not ask for usage, so the frame is ours to withhold")
	}

	var body map[string]interface{}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	options, ok := body["stream_options"].(map[string]interface{})
	if !ok {
		t.Fatal("stream_options was not added")
	}
	if options["include_usage"] != true {
		t.Errorf("include_usage = %v, want true", options["include_usage"])
	}
}

// A non-streaming request carries usage already and must not be rewritten.
func TestInjectStreamUsageLeavesNonStreamingAlone(t *testing.T) {
	in := []byte(`{"model":"m","stream":false,"messages":[]}`)
	out, _ := injectStreamUsage(in)
	if string(out) != string(in) {
		t.Errorf("non-streaming body was modified:\n got  %s\n want %s", out, in)
	}
}

// A client that asked for usage itself keeps the frame.
func TestInjectStreamUsageRespectsClientOptIn(t *testing.T) {
	out, clientOptedIn := injectStreamUsage(
		[]byte(`{"model":"m","stream":true,"stream_options":{"include_usage":true}}`))
	if !clientOptedIn {
		t.Error("the client opted in, so the usage frame must be forwarded to them")
	}
	var body map[string]interface{}
	json.Unmarshal(out, &body)
	options, _ := body["stream_options"].(map[string]interface{})
	if options["include_usage"] != true {
		t.Error("the client's own opt-in was lost")
	}
}

// Unknown client parameters must survive the edit. Round-tripping through a typed
// struct would drop anything this gateway does not model, which would be a worse
// bug than the one include_usage fixes.
func TestInjectStreamUsagePreservesUnknownFields(t *testing.T) {
	out, _ := injectStreamUsage([]byte(
		`{"model":"m","stream":true,"seed":42,"response_format":{"type":"json_object"},"logprobs":true}`))

	var body map[string]interface{}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	if body["seed"] != float64(42) {
		t.Errorf("seed was lost: %v", body["seed"])
	}
	if body["logprobs"] != true {
		t.Errorf("logprobs was lost: %v", body["logprobs"])
	}
	if _, ok := body["response_format"].(map[string]interface{}); !ok {
		t.Errorf("response_format was lost: %v", body["response_format"])
	}
}

// A malformed body is forwarded unchanged rather than rewritten, so the client sees
// the upstream's own validation error.
func TestInjectStreamUsageIgnoresMalformedBody(t *testing.T) {
	in := []byte(`{not json`)
	out, optedIn := injectStreamUsage(in)
	if string(out) != string(in) || optedIn {
		t.Errorf("malformed body should pass through untouched, got %s", out)
	}
}

// The usage-only frame is identified by shape: usage present, no choices.
func TestIsUsageOnlyFrame(t *testing.T) {
	if !isUsageOnlyFrame([]byte(`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":1}}`)) {
		t.Error("a frame with usage and no choices is the usage-only frame")
	}
	if isUsageOnlyFrame([]byte(`{"choices":[{"delta":{"content":"x"}}]}`)) {
		t.Error("a content frame is not the usage-only frame")
	}
	// A provider that attaches usage to the last content frame: that frame carries
	// content the client needs, so it must not be withheld.
	if isUsageOnlyFrame([]byte(`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5}}`)) {
		t.Error("a frame with choices must be forwarded even when it carries usage")
	}
}

func TestSSEDataPayload(t *testing.T) {
	if got := sseDataPayload(`data: {"a":1}`); got != `{"a":1}` {
		t.Errorf("got %q", got)
	}
	for _, line := range []string{"", "event: message", "data: [DONE]", "data:", ": comment"} {
		if got := sseDataPayload(line); got != "" {
			t.Errorf("%q should yield no payload, got %q", line, got)
		}
	}
}

// Timeout parsing is clamped: an absurd value must not disable the timeout or pin a
// connection for an unbounded time.
func TestNineRouterTimeoutIsClamped(t *testing.T) {
	t.Setenv(envNineRouterTimeout, "")
	if got := nineRouterTimeout(); got != nineRouterDefaultTimeout {
		t.Errorf("unset -> %v, want the default %v", got, nineRouterDefaultTimeout)
	}
	t.Setenv(envNineRouterTimeout, "1")
	if got := nineRouterTimeout(); got < 5*time.Second {
		t.Errorf("1ms -> %v, want at least 5s", got)
	}
	t.Setenv(envNineRouterTimeout, "99999999")
	if got := nineRouterTimeout(); got > 10*time.Minute {
		t.Errorf("huge -> %v, want at most 10m", got)
	}
	t.Setenv(envNineRouterTimeout, "not-a-number")
	if got := nineRouterTimeout(); got != nineRouterDefaultTimeout {
		t.Errorf("garbage -> %v, want the default", got)
	}
}

// Credentials must never reach a log line.
func TestRedactURLHidesCredentials(t *testing.T) {
	got := redactURL("https://user:secret@gateway.internal:20242/v1")
	if strings.Contains(got, "secret") {
		t.Errorf("credentials leaked: %q", got)
	}
	if !strings.Contains(got, "gateway.internal") {
		t.Errorf("host should stay readable for diagnostics: %q", got)
	}
}
