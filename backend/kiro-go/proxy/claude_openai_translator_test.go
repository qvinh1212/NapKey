package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

// The system prompt becomes a leading system message, which is where Chat
// Completions expects it.
func TestClaudeToOpenAIHoistsSystemPrompt(t *testing.T) {
	out := claudeToOpenAIRequest(&ClaudeRequest{
		Model:     "claude-sonnet-5",
		MaxTokens: 1024,
		System:    "be terse",
		Messages:  []ClaudeMessage{{Role: "user", Content: "hi"}},
	})

	if len(out.Messages) != 2 {
		t.Fatalf("got %d messages, want system + user", len(out.Messages))
	}
	if out.Messages[0].Role != "system" || out.Messages[0].Content != "be terse" {
		t.Errorf("first message = %+v, want the system prompt", out.Messages[0])
	}
	if out.MaxTokens != 1024 {
		t.Errorf("max_tokens = %d, want it carried over", out.MaxTokens)
	}
}

// Tool results move from a user content array to their own role:tool messages, and
// the id has to survive: OpenAI pairs a result with its call by tool_call_id, so a
// dropped id makes the upstream reject the conversation.
func TestClaudeToolResultsBecomeToolMessages(t *testing.T) {
	out := claudeToOpenAIRequest(&ClaudeRequest{
		Model: "claude-sonnet-5",
		Messages: []ClaudeMessage{{
			Role: "user",
			Content: []interface{}{
				map[string]interface{}{"type": "tool_result", "tool_use_id": "call_1", "content": "42"},
				map[string]interface{}{"type": "tool_result", "tool_use_id": "call_2", "content": "done"},
				map[string]interface{}{"type": "text", "text": "and now this"},
			},
		}},
	})

	if len(out.Messages) != 3 {
		t.Fatalf("got %d messages, want 2 tool results + 1 user text", len(out.Messages))
	}
	// Tool messages must precede new user text, which is what OpenAI requires.
	if out.Messages[0].Role != "tool" || out.Messages[0].ToolCallID != "call_1" {
		t.Errorf("message 0 = %+v, want the first tool result", out.Messages[0])
	}
	if out.Messages[1].ToolCallID != "call_2" {
		t.Errorf("message 1 lost its tool_call_id: %+v", out.Messages[1])
	}
	if out.Messages[2].Role != "user" || out.Messages[2].Content != "and now this" {
		t.Errorf("message 2 = %+v, want the trailing user text", out.Messages[2])
	}
}

// A tool_use block becomes an OpenAI tool call with arguments as a JSON string.
func TestClaudeToolUseBecomesToolCall(t *testing.T) {
	out := claudeToOpenAIRequest(&ClaudeRequest{
		Model: "claude-sonnet-5",
		Messages: []ClaudeMessage{{
			Role: "assistant",
			Content: []interface{}{
				map[string]interface{}{
					"type":  "tool_use",
					"id":    "call_9",
					"name":  "read_file",
					"input": map[string]interface{}{"path": "/tmp/x"},
				},
			},
		}},
	})

	if len(out.Messages) != 1 || len(out.Messages[0].ToolCalls) != 1 {
		t.Fatalf("expected one assistant message with one tool call, got %+v", out.Messages)
	}
	call := out.Messages[0].ToolCalls[0]
	if call.ID != "call_9" || call.Function.Name != "read_file" {
		t.Errorf("call = %+v", call)
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		t.Fatalf("arguments must be a JSON string: %v", err)
	}
	if args["path"] != "/tmp/x" {
		t.Errorf("arguments lost content: %v", args)
	}
}

// Thinking blocks have no Chat Completions representation. Dropping them is correct;
// folding them into visible text would present private reasoning as model output.
func TestThinkingBlocksAreDroppedNotLeaked(t *testing.T) {
	out := claudeToOpenAIRequest(&ClaudeRequest{
		Model: "claude-sonnet-5",
		Messages: []ClaudeMessage{{
			Role: "assistant",
			Content: []interface{}{
				map[string]interface{}{"type": "thinking", "thinking": "secret chain of thought"},
				map[string]interface{}{"type": "text", "text": "the answer is 4"},
			},
		}},
	})

	if len(out.Messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(out.Messages))
	}
	content, _ := out.Messages[0].Content.(string)
	if strings.Contains(content, "secret chain of thought") {
		t.Error("thinking content must not be forwarded as assistant text")
	}
	if content != "the answer is 4" {
		t.Errorf("content = %q, want only the visible text", content)
	}
}

// Anthropic server tools are executed by Anthropic and cannot be forwarded.
func TestServerToolsAreNotForwarded(t *testing.T) {
	out := claudeToOpenAIRequest(&ClaudeRequest{
		Model:    "claude-sonnet-5",
		Messages: []ClaudeMessage{{Role: "user", Content: "hi"}},
		Tools: []ClaudeTool{
			{Type: "web_search_20250305", Name: "web_search"},
			{Name: "read_file", Description: "reads", InputSchema: map[string]interface{}{"type": "object"}},
		},
	})

	if len(out.Tools) != 1 {
		t.Fatalf("got %d tools, want only the client tool", len(out.Tools))
	}
	if out.Tools[0].Function.Name != "read_file" {
		t.Errorf("wrong tool survived: %+v", out.Tools[0])
	}
}

// ---------- response direction ----------

func TestOpenAIResponseBecomesClaudeMessage(t *testing.T) {
	raw := []byte(`{"id":"chatcmpl-abc","model":"kiro/x","choices":[
		{"index":0,"message":{"role":"assistant","content":"hello there"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":100,"completion_tokens":5}}`)

	resp, usage, err := openAIToClaudeResponse(raw, "claude-sonnet-5")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if resp.Type != "message" || resp.Role != "assistant" {
		t.Errorf("envelope wrong: %+v", resp)
	}
	// The customer's model id is echoed back, not the upstream's internal one: that
	// is the id they can price against, and it does not leak the provider.
	if resp.Model != "claude-sonnet-5" {
		t.Errorf("model = %q, want the requested id", resp.Model)
	}
	if len(resp.Content) != 1 || resp.Content[0].Type != "text" || resp.Content[0].Text != "hello there" {
		t.Errorf("content = %+v", resp.Content)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("stop_reason = %q", resp.StopReason)
	}
	if usage == nil || usage.PromptTokens != 100 {
		t.Errorf("usage must be returned for billing: %+v", usage)
	}
}

// tool_calls must map to stop_reason tool_use. Claude Code branches on this to
// decide whether to run a tool, so end_turn here would break the agent loop.
func TestToolCallsProduceToolUseStopReason(t *testing.T) {
	raw := []byte(`{"id":"c1","choices":[{"index":0,"message":{"role":"assistant","content":null,
		"tool_calls":[{"id":"call_1","type":"function","function":{"name":"ls","arguments":"{\"dir\":\"/\"}"}}]},
		"finish_reason":"tool_calls"}]}`)

	resp, _, err := openAIToClaudeResponse(raw, "claude-opus-5")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if resp.StopReason != "tool_use" {
		t.Fatalf("stop_reason = %q, want tool_use or the agent loop stops", resp.StopReason)
	}
	if len(resp.Content) != 1 || resp.Content[0].Type != "tool_use" {
		t.Fatalf("content = %+v", resp.Content)
	}
	// Anthropic expects input as an object, not the OpenAI arguments string.
	input, ok := resp.Content[0].Input.(map[string]interface{})
	if !ok {
		t.Fatalf("input must be an object, got %T", resp.Content[0].Input)
	}
	if input["dir"] != "/" {
		t.Errorf("input = %v", input)
	}
}

func TestFinishReasonLengthBecomesMaxTokens(t *testing.T) {
	raw := []byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"trunc"},"finish_reason":"length"}]}`)
	resp, _, err := openAIToClaudeResponse(raw, "claude-sonnet-5")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != "max_tokens" {
		t.Errorf("stop_reason = %q, want max_tokens", resp.StopReason)
	}
}

// Cached prompt tokens are reported apart from fresh input, matching how the request
// is billed. Reporting the full prompt as input_tokens would show a number that
// double counts the cached part.
func TestClaudeUsageSeparatesCacheReads(t *testing.T) {
	got := claudeUsageFromOpenAI(&nineRouterUsage{
		PromptTokens:     1000,
		CompletionTokens: 40,
		PromptTokensDetails: &struct {
			CachedTokens int `json:"cached_tokens"`
		}{CachedTokens: 900},
	})
	if got.InputTokens != 100 {
		t.Errorf("input_tokens = %d, want 100 fresh", got.InputTokens)
	}
	if got.CacheReadInputTokens != 900 {
		t.Errorf("cache_read_input_tokens = %d, want 900", got.CacheReadInputTokens)
	}
	if got.InputTokens+got.CacheReadInputTokens != 1000 {
		t.Error("the parts must sum to the prompt total")
	}
}

// Unparseable tool arguments must not lose the whole turn.
func TestMalformedToolArgumentsDegradeToEmptyObject(t *testing.T) {
	raw := []byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"ok",
		"tool_calls":[{"id":"c","type":"function","function":{"name":"f","arguments":"{not json"}}]},
		"finish_reason":"tool_calls"}]}`)
	resp, _, err := openAIToClaudeResponse(raw, "claude-sonnet-5")
	if err != nil {
		t.Fatalf("a bad arguments string must not fail the response: %v", err)
	}
	if len(resp.Content) != 2 {
		t.Fatalf("expected text + tool_use, got %+v", resp.Content)
	}
	if _, ok := resp.Content[1].Input.(map[string]interface{}); !ok {
		t.Errorf("input = %v, want an empty object", resp.Content[1].Input)
	}
}

// ---------- streaming ----------

// A text stream must open, fill and close a block, then report usage.
func TestStreamTranslationEmitsBlockLifecycle(t *testing.T) {
	state := newClaudeStreamState()
	var events []claudeSSEEvent

	events = append(events, state.translateOpenAIChunk([]byte(
		`{"id":"chatcmpl-1","choices":[{"index":0,"delta":{"role":"assistant","content":"He"}}]}`), "claude-sonnet-5")...)
	events = append(events, state.translateOpenAIChunk([]byte(
		`{"id":"chatcmpl-1","choices":[{"index":0,"delta":{"content":"llo"},"finish_reason":"stop"}]}`), "claude-sonnet-5")...)
	events = append(events, state.translateOpenAIChunk([]byte(
		`{"id":"chatcmpl-1","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":2}}`), "claude-sonnet-5")...)
	events = append(events, state.finish()...)

	var names []string
	for _, e := range events {
		names = append(names, e.Event)
	}
	got := strings.Join(names, ",")
	want := "message_start,content_block_start,content_block_delta,content_block_delta,content_block_stop,message_delta,message_stop"
	if got != want {
		t.Errorf("event order:\n got  %s\n want %s", got, want)
	}

	// The closing message_delta carries the stop reason and the usage the client
	// reconciles against.
	final := events[len(events)-2]
	delta, _ := final.Data["delta"].(map[string]interface{})
	if delta["stop_reason"] != "end_turn" {
		t.Errorf("stop_reason = %v", delta["stop_reason"])
	}
	usage, _ := final.Data["usage"].(map[string]interface{})
	if usage["output_tokens"] != 2 {
		t.Errorf("output_tokens = %v, want 2", usage["output_tokens"])
	}
}

// A tool call mid-stream must close the open text block before opening its own:
// Anthropic requires one block to stop before the next starts, and overlapping
// blocks make a client's parser lose content.
func TestStreamClosesTextBeforeOpeningToolBlock(t *testing.T) {
	state := newClaudeStreamState()
	var events []claudeSSEEvent
	events = append(events, state.translateOpenAIChunk([]byte(
		`{"id":"c","choices":[{"index":0,"delta":{"content":"thinking..."}}]}`), "m")...)
	events = append(events, state.translateOpenAIChunk([]byte(
		`{"id":"c","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"t1","type":"function","function":{"name":"ls","arguments":"{}"}}]}}]}`), "m")...)

	var names []string
	for _, e := range events {
		names = append(names, e.Event)
	}
	got := strings.Join(names, ",")
	if !strings.Contains(got, "content_block_stop,content_block_start") {
		t.Errorf("text block must be closed before the tool block opens: %s", got)
	}

	// Indexes must differ, or the client overwrites one block with the other.
	var indexes []interface{}
	for _, e := range events {
		if e.Event == "content_block_start" {
			indexes = append(indexes, e.Data["index"])
		}
	}
	if len(indexes) != 2 || indexes[0] == indexes[1] {
		t.Errorf("block indexes must be distinct, got %v", indexes)
	}
}

// Tool input streams as partial JSON, and a streamed tool call must still finish
// with stop_reason tool_use.
func TestStreamToolCallReportsToolUse(t *testing.T) {
	state := newClaudeStreamState()
	state.translateOpenAIChunk([]byte(
		`{"id":"c","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"t1","type":"function","function":{"name":"ls","arguments":"{\"a\":"}}]}}]}`), "m")
	events := state.translateOpenAIChunk([]byte(
		`{"id":"c","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]},"finish_reason":"tool_calls"}]}`), "m")

	sawPartial := false
	for _, e := range events {
		if e.Event == "content_block_delta" {
			if d, _ := e.Data["delta"].(map[string]interface{}); d["type"] == "input_json_delta" {
				sawPartial = true
			}
		}
	}
	if !sawPartial {
		t.Error("tool arguments must stream as input_json_delta")
	}

	closing := state.finish()
	last := closing[len(closing)-2]
	delta, _ := last.Data["delta"].(map[string]interface{})
	if delta["stop_reason"] != "tool_use" {
		t.Errorf("stop_reason = %v, want tool_use", delta["stop_reason"])
	}
}

// A truncated stream must still terminate the message, or the client hangs waiting
// for a block that never closes.
func TestStreamFinishClosesOpenBlocksAfterTruncation(t *testing.T) {
	state := newClaudeStreamState()
	state.translateOpenAIChunk([]byte(`{"id":"c","choices":[{"index":0,"delta":{"content":"half"}}]}`), "m")

	closing := state.finish()
	var names []string
	for _, e := range closing {
		names = append(names, e.Event)
	}
	got := strings.Join(names, ",")
	if got != "content_block_stop,message_delta,message_stop" {
		t.Errorf("closing events = %s", got)
	}
}

// message_start must be emitted exactly once.
func TestStreamEmitsMessageStartOnce(t *testing.T) {
	state := newClaudeStreamState()
	count := 0
	for i := 0; i < 3; i++ {
		for _, e := range state.translateOpenAIChunk([]byte(`{"id":"c","choices":[{"index":0,"delta":{"content":"x"}}]}`), "m") {
			if e.Event == "message_start" {
				count++
			}
		}
	}
	if count != 1 {
		t.Errorf("message_start emitted %d times, want 1", count)
	}
}

// An upstream that returns tool calls but reports finish_reason "stop" must still
// produce tool_use. This is the case the hasToolCalls branch exists for: relying on
// finish_reason alone leaves Claude Code believing the turn ended, so the tool never
// runs and the agent loop silently stalls.
func TestToolCallsWithStopFinishReasonStillProduceToolUse(t *testing.T) {
	raw := []byte(`{"id":"c1","choices":[{"index":0,"message":{"role":"assistant","content":null,
		"tool_calls":[{"id":"call_1","type":"function","function":{"name":"ls","arguments":"{}"}}]},
		"finish_reason":"stop"}]}`)

	resp, _, err := openAIToClaudeResponse(raw, "claude-opus-5")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if resp.StopReason != "tool_use" {
		t.Fatalf("stop_reason = %q, want tool_use: a turn carrying tool calls is not finished", resp.StopReason)
	}
}

// Same gap on the streaming path.
func TestStreamToolCallWithStopFinishReasonReportsToolUse(t *testing.T) {
	state := newClaudeStreamState()
	state.translateOpenAIChunk([]byte(
		`{"id":"c","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"t1","type":"function","function":{"name":"ls","arguments":"{}"}}]}}]}`), "m")
	state.translateOpenAIChunk([]byte(
		`{"id":"c","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`), "m")

	closing := state.finish()
	delta, _ := closing[len(closing)-2].Data["delta"].(map[string]interface{})
	if delta["stop_reason"] != "tool_use" {
		t.Errorf("stop_reason = %v, want tool_use", delta["stop_reason"])
	}
}

// The direct mapping, so a regression in either branch is attributable.
func TestFinishReasonMapping(t *testing.T) {
	for _, tc := range []struct {
		reason   string
		hasTools bool
		want     string
	}{
		{"stop", false, "end_turn"},
		{"", false, "end_turn"},
		{"length", false, "max_tokens"},
		{"tool_calls", false, "tool_use"},
		{"function_call", false, "tool_use"},
		{"content_filter", false, "refusal"},
		{"unknown_future_value", false, "end_turn"},
		// Tool calls override whatever the upstream reported.
		{"stop", true, "tool_use"},
		{"length", true, "tool_use"},
	} {
		if got := openAIFinishReasonToClaude(tc.reason, tc.hasTools); got != tc.want {
			t.Errorf("(%q, hasTools=%v) = %q, want %q", tc.reason, tc.hasTools, got, tc.want)
		}
	}
}
