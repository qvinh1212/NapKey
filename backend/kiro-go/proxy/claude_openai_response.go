package proxy

// Response translation: OpenAI Chat Completions back to the Anthropic Messages
// shape, both for a single JSON body and for a streamed SSE conversation.

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

// openAIChoiceMessage is the assistant turn inside a completion response. Declared
// separately from OpenAIMessage because tool call arguments arrive as a JSON string
// here and have to be parsed back into an object for Anthropic.
type openAIChoiceMessage struct {
	Role      string     `json:"role"`
	Content   *string    `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Refusal   *string    `json:"refusal,omitempty"`
}

type openAICompletion struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int                 `json:"index"`
		Message      openAIChoiceMessage `json:"message"`
		FinishReason string              `json:"finish_reason"`
	} `json:"choices"`
	Usage *nineRouterUsage `json:"usage"`
}

// openAIToClaudeResponse converts a completion into an Anthropic response.
//
// Only the first choice is used. Anthropic has no representation for multiple
// candidates, and n>1 is not something this gateway requests.
func openAIToClaudeResponse(raw []byte, requestedModel string) (*ClaudeResponse, *nineRouterUsage, error) {
	var completion openAICompletion
	if err := json.Unmarshal(raw, &completion); err != nil {
		return nil, nil, err
	}

	id := completion.ID
	if id == "" {
		id = "msg_" + uuid.New().String()
	} else if !strings.HasPrefix(id, "msg_") {
		// Claude Code does not require the prefix, but keeping the Anthropic shape
		// avoids surprising clients that pattern-match on it.
		id = "msg_" + strings.TrimPrefix(id, "chatcmpl-")
	}

	// The model reported back is the one the customer asked for. Echoing the
	// upstream's internal id would leak which provider served the request and would
	// not match the id they can price against.
	model := requestedModel

	out := &ClaudeResponse{
		ID:      id,
		Type:    "message",
		Role:    "assistant",
		Model:   model,
		Content: []ClaudeContentBlock{},
	}

	if len(completion.Choices) == 0 {
		// A completion with no choices produced nothing. An empty text block keeps
		// the response well-formed for clients that index into content.
		out.Content = append(out.Content, ClaudeContentBlock{Type: "text", Text: ""})
		out.StopReason = "end_turn"
		return out, completion.Usage, nil
	}

	choice := completion.Choices[0]

	if choice.Message.Refusal != nil && strings.TrimSpace(*choice.Message.Refusal) != "" {
		out.Content = append(out.Content, ClaudeContentBlock{Type: "text", Text: *choice.Message.Refusal})
	} else if choice.Message.Content != nil && *choice.Message.Content != "" {
		out.Content = append(out.Content, ClaudeContentBlock{Type: "text", Text: *choice.Message.Content})
	}

	for _, call := range choice.Message.ToolCalls {
		block := ClaudeContentBlock{
			Type: "tool_use",
			ID:   call.ID,
			Name: call.Function.Name,
			// Anthropic expects an object. An unparseable arguments string becomes an
			// empty object rather than failing the response: the client can still see
			// which tool was called, and dropping the whole turn would lose the text
			// alongside it.
			Input: parseToolArguments(call.Function.Arguments),
		}
		out.Content = append(out.Content, block)
	}

	if len(out.Content) == 0 {
		out.Content = append(out.Content, ClaudeContentBlock{Type: "text", Text: ""})
	}

	out.StopReason = openAIFinishReasonToClaude(choice.FinishReason, len(choice.Message.ToolCalls) > 0)
	if completion.Usage != nil {
		out.Usage = claudeUsageFromOpenAI(completion.Usage)
	}
	return out, completion.Usage, nil
}

// parseToolArguments turns the OpenAI arguments string into an object.
func parseToolArguments(args string) interface{} {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" {
		return map[string]interface{}{}
	}
	var parsed interface{}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return map[string]interface{}{}
	}
	if parsed == nil {
		return map[string]interface{}{}
	}
	return parsed
}

// claudeUsageFromOpenAI maps token counts into the Anthropic usage block.
//
// prompt_tokens includes cached reads when the upstream separates them, while
// Anthropic reports input_tokens as fresh input with cache reads counted apart. The
// cached portion is therefore subtracted, or the client would see a total that
// double counts it against what they are billed.
func claudeUsageFromOpenAI(usage *nineRouterUsage) ClaudeUsage {
	cacheRead := 0
	if usage.PromptTokensDetails != nil {
		cacheRead = maxInt(usage.PromptTokensDetails.CachedTokens, 0)
	}
	return ClaudeUsage{
		InputTokens:          maxInt(usage.PromptTokens-cacheRead, 0),
		OutputTokens:         maxInt(usage.CompletionTokens, 0),
		CacheReadInputTokens: cacheRead,
	}
}

// ---------- streaming ----------

// openAIStreamChunk is one SSE frame of a streamed completion.
type openAIStreamChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *nineRouterUsage `json:"usage"`
}

// claudeStreamState tracks what has been emitted so a delta stream can be reshaped
// into Anthropic's block-structured events.
//
// The protocols differ in kind, not just in naming. OpenAI streams flat deltas;
// Anthropic streams explicit block lifecycles, where every piece of content is
// opened, filled and closed, and blocks are addressed by index. Turning one into the
// other requires remembering which block is currently open, which is what this holds.
type claudeStreamState struct {
	messageStarted bool
	textOpen       bool
	// nextIndex is the Anthropic content block index to use next. Text takes index 0
	// when present, and each tool call takes the following index.
	nextIndex int
	// toolIndexes maps an OpenAI tool call slot to the Anthropic block index that
	// was opened for it, because the two number their tools independently.
	toolIndexes map[int]int
	toolOpen    map[int]bool
	stopReason  string
	usage       *nineRouterUsage
	sawToolCall bool
}

func newClaudeStreamState() *claudeStreamState {
	return &claudeStreamState{
		toolIndexes: map[int]int{},
		toolOpen:    map[int]bool{},
		stopReason:  "end_turn",
	}
}

// claudeSSEEvent is one event to write to the client.
type claudeSSEEvent struct {
	Event string
	Data  map[string]interface{}
}

// translateOpenAIChunk converts one OpenAI frame into zero or more Anthropic events.
func (s *claudeStreamState) translateOpenAIChunk(raw []byte, model string) []claudeSSEEvent {
	var chunk openAIStreamChunk
	if err := json.Unmarshal(raw, &chunk); err != nil {
		return nil
	}

	var events []claudeSSEEvent

	if chunk.Usage != nil {
		// Usage can arrive on its own trailing frame with no choices.
		s.usage = chunk.Usage
	}

	if !s.messageStarted {
		s.messageStarted = true
		id := chunk.ID
		if id == "" {
			id = uuid.New().String()
		}
		events = append(events, claudeSSEEvent{
			Event: "message_start",
			Data: map[string]interface{}{
				"type": "message_start",
				"message": map[string]interface{}{
					"id":            "msg_" + strings.TrimPrefix(id, "chatcmpl-"),
					"type":          "message",
					"role":          "assistant",
					"model":         model,
					"content":       []interface{}{},
					"stop_reason":   nil,
					"stop_sequence": nil,
					"usage":         map[string]interface{}{"input_tokens": 0, "output_tokens": 0},
				},
			},
		})
	}

	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			if !s.textOpen {
				s.textOpen = true
				textIndex := s.nextIndex
				s.nextIndex++
				events = append(events, claudeSSEEvent{
					Event: "content_block_start",
					Data: map[string]interface{}{
						"type":          "content_block_start",
						"index":         textIndex,
						"content_block": map[string]interface{}{"type": "text", "text": ""},
					},
				})
				s.toolIndexes[-1] = textIndex
			}
			events = append(events, claudeSSEEvent{
				Event: "content_block_delta",
				Data: map[string]interface{}{
					"type":  "content_block_delta",
					"index": s.toolIndexes[-1],
					"delta": map[string]interface{}{"type": "text_delta", "text": choice.Delta.Content},
				},
			})
		}

		for _, call := range choice.Delta.ToolCalls {
			s.sawToolCall = true
			idx, known := s.toolIndexes[call.Index]
			if !known {
				// A new tool call. Text must be closed first: Anthropic requires the
				// current block to be stopped before the next one starts.
				if s.textOpen {
					events = append(events, claudeSSEEvent{
						Event: "content_block_stop",
						Data:  map[string]interface{}{"type": "content_block_stop", "index": s.toolIndexes[-1]},
					})
					s.textOpen = false
				}
				idx = s.nextIndex
				s.nextIndex++
				s.toolIndexes[call.Index] = idx
				s.toolOpen[call.Index] = true
				events = append(events, claudeSSEEvent{
					Event: "content_block_start",
					Data: map[string]interface{}{
						"type":  "content_block_start",
						"index": idx,
						"content_block": map[string]interface{}{
							"type":  "tool_use",
							"id":    call.ID,
							"name":  call.Function.Name,
							"input": map[string]interface{}{},
						},
					},
				})
			}
			if call.Function.Arguments != "" {
				// Anthropic streams tool input as partial JSON text, which is the same
				// thing OpenAI sends, so the fragment passes through unchanged.
				events = append(events, claudeSSEEvent{
					Event: "content_block_delta",
					Data: map[string]interface{}{
						"type":  "content_block_delta",
						"index": idx,
						"delta": map[string]interface{}{"type": "input_json_delta", "partial_json": call.Function.Arguments},
					},
				})
			}
		}

		if choice.FinishReason != "" {
			s.stopReason = openAIFinishReasonToClaude(choice.FinishReason, s.sawToolCall)
		}
	}

	return events
}

// finish emits the closing events for the conversation.
func (s *claudeStreamState) finish() []claudeSSEEvent {
	var events []claudeSSEEvent

	if s.textOpen {
		events = append(events, claudeSSEEvent{
			Event: "content_block_stop",
			Data:  map[string]interface{}{"type": "content_block_stop", "index": s.toolIndexes[-1]},
		})
		s.textOpen = false
	}
	for slot, open := range s.toolOpen {
		if !open {
			continue
		}
		events = append(events, claudeSSEEvent{
			Event: "content_block_stop",
			Data:  map[string]interface{}{"type": "content_block_stop", "index": s.toolIndexes[slot]},
		})
		s.toolOpen[slot] = false
	}

	usage := map[string]interface{}{"output_tokens": 0}
	if s.usage != nil {
		claudeUsage := claudeUsageFromOpenAI(s.usage)
		usage["input_tokens"] = claudeUsage.InputTokens
		usage["output_tokens"] = claudeUsage.OutputTokens
		if claudeUsage.CacheReadInputTokens > 0 {
			usage["cache_read_input_tokens"] = claudeUsage.CacheReadInputTokens
		}
	}

	events = append(events,
		claudeSSEEvent{
			Event: "message_delta",
			Data: map[string]interface{}{
				"type":  "message_delta",
				"delta": map[string]interface{}{"stop_reason": s.stopReason, "stop_sequence": nil},
				"usage": usage,
			},
		},
		claudeSSEEvent{
			Event: "message_stop",
			Data:  map[string]interface{}{"type": "message_stop"},
		},
	)
	return events
}
