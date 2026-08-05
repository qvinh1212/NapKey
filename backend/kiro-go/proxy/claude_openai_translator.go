package proxy

// Translation between the Anthropic Messages API and the OpenAI Chat Completions
// API, for serving /v1/messages from the 9Router upstream.
//
// This exists because the two APIs describe the same conversation differently, and
// /v1/messages is the endpoint Claude Code speaks. Without it, switching the
// upstream to 9Router would leave the product's main client unserved.
//
// The mapping is not symmetric and a few Anthropic concepts have no OpenAI
// equivalent. Where that happens the choice is to drop the concept rather than
// approximate it, and to say so here:
//
//   - Extended thinking has no Chat Completions representation. A thinking block in
//     request history is dropped; the assistant's visible text is kept.
//   - Cache control is an Anthropic pricing feature. Chat Completions has no
//     equivalent request field, so cache_control hints are dropped. The response
//     side still reports cached_tokens when the upstream provides them, so cache
//     reads are billed correctly even though they cannot be requested here.
//   - Anthropic server tools (web_search_20250305 and friends) are executed by
//     Anthropic, not by the client. They cannot be forwarded and are dropped.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// claudeToOpenAIRequest converts an Anthropic Messages request.
//
// max_tokens is required by Anthropic and optional for OpenAI, so it carries over
// directly. The system prompt becomes a leading system message, which is where Chat
// Completions expects it.
func claudeToOpenAIRequest(req *ClaudeRequest) *OpenAIRequest {
	out := &OpenAIRequest{
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
	}

	if system := strings.TrimSpace(extractSystemPrompt(req.System)); system != "" {
		out.Messages = append(out.Messages, OpenAIMessage{Role: "system", Content: system})
	}

	for i := range req.Messages {
		out.Messages = append(out.Messages, claudeMessageToOpenAI(&req.Messages[i])...)
	}
	out.Tools = claudeToolsToOpenAI(req.Tools)
	return out
}

// claudeMessageToOpenAI converts one message, which can expand into several.
//
// The expansion is why this returns a slice: Anthropic puts tool results in a user
// message's content array, while OpenAI requires a separate message per result with
// role "tool". One Claude message carrying three tool results becomes three OpenAI
// messages, and any accompanying text becomes a fourth.
func claudeMessageToOpenAI(msg *ClaudeMessage) []OpenAIMessage {
	// The simple case: content is a plain string.
	if text, ok := msg.Content.(string); ok {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []OpenAIMessage{{Role: msg.Role, Content: text}}
	}

	blocks := contentBlocksAsMaps(msg.Content)
	if len(blocks) == 0 {
		return nil
	}

	var (
		out       []OpenAIMessage
		textParts []string
		imageURLs []map[string]interface{}
		toolCalls []ToolCall
		toolMsgs  []OpenAIMessage
	)

	for _, block := range blocks {
		switch blockType(block) {
		case "text":
			if s := blockString(block, "text"); s != "" {
				textParts = append(textParts, s)
			}
		case "thinking", "redacted_thinking":
			// No Chat Completions equivalent. Dropped rather than folded into the
			// visible text, which would make the model's private reasoning look like
			// something it said to the user.
		case "image":
			if url := claudeImageToDataURL(block); url != "" {
				imageURLs = append(imageURLs, map[string]interface{}{
					"type":      "image_url",
					"image_url": map[string]interface{}{"url": url},
				})
			}
		case "tool_use":
			if call, ok := claudeToolUseToCall(block); ok {
				toolCalls = append(toolCalls, call)
			}
		case "tool_result":
			// Each result becomes its own message. The id has to survive: OpenAI
			// matches a result to its call by tool_call_id, and a mismatch makes the
			// upstream reject the conversation.
			id := blockString(block, "tool_use_id")
			if id == "" {
				continue
			}
			toolMsgs = append(toolMsgs, OpenAIMessage{
				Role:       "tool",
				ToolCallID: id,
				Content:    claudeToolResultText(block["content"]),
			})
		}
	}

	// Tool results come first. OpenAI requires every tool message to directly follow
	// the assistant turn that requested it, before any new user text.
	out = append(out, toolMsgs...)

	joined := strings.TrimSpace(strings.Join(textParts, "\n\n"))
	switch {
	case len(imageURLs) > 0:
		// Multimodal content must use the array form. Text goes in as its own part.
		parts := make([]map[string]interface{}, 0, len(imageURLs)+1)
		if joined != "" {
			parts = append(parts, map[string]interface{}{"type": "text", "text": joined})
		}
		parts = append(parts, imageURLs...)
		out = append(out, OpenAIMessage{Role: msg.Role, Content: parts})
	case len(toolCalls) > 0:
		// An assistant turn that called tools. Content may be empty, which is legal
		// and common when the model only calls a tool.
		m := OpenAIMessage{Role: msg.Role, ToolCalls: toolCalls}
		if joined != "" {
			m.Content = joined
		}
		out = append(out, m)
	case joined != "":
		out = append(out, OpenAIMessage{Role: msg.Role, Content: joined})
	}
	return out
}

func blockType(block map[string]interface{}) string {
	t, _ := block["type"].(string)
	return t
}

func blockString(block map[string]interface{}, key string) string {
	s, _ := block[key].(string)
	return s
}

// claudeImageToDataURL rebuilds a data URL from an Anthropic image block.
func claudeImageToDataURL(block map[string]interface{}) string {
	source, ok := block["source"].(map[string]interface{})
	if !ok {
		return ""
	}
	switch source["type"] {
	case "base64":
		media := strings.TrimSpace(fmt.Sprint(source["media_type"]))
		data := strings.TrimSpace(fmt.Sprint(source["data"]))
		if media == "" || data == "" || data == "<nil>" {
			return ""
		}
		return "data:" + media + ";base64," + data
	case "url":
		url := strings.TrimSpace(fmt.Sprint(source["url"]))
		if url == "" || url == "<nil>" {
			return ""
		}
		return url
	}
	return ""
}

// claudeToolUseToCall converts a tool_use block into an OpenAI tool call.
//
// Arguments are a JSON string in the OpenAI shape and an object in the Anthropic
// one, so the input is marshalled. An input that cannot be marshalled yields no
// call: sending a malformed arguments string would have the upstream reject the
// whole request rather than just that tool.
func claudeToolUseToCall(block map[string]interface{}) (ToolCall, bool) {
	id := blockString(block, "id")
	name := blockString(block, "name")
	if id == "" || name == "" {
		return ToolCall{}, false
	}
	args := "{}"
	if raw, ok := block["input"]; ok && raw != nil {
		encoded, err := json.Marshal(raw)
		if err != nil {
			return ToolCall{}, false
		}
		args = string(encoded)
	}
	var call ToolCall
	call.ID = id
	call.Type = "function"
	call.Function.Name = name
	call.Function.Arguments = args
	return call, true
}

// claudeToolResultText flattens a tool result into text.
//
// Anthropic allows a result to be a string or an array of blocks, including images.
// Chat Completions tool messages carry text only, so image results are noted rather
// than silently vanishing: the model needs to know a result existed even if it
// cannot see it.
func claudeToolResultText(content interface{}) string {
	switch v := content.(type) {
	case nil:
		return ""
	case string:
		return v
	}
	blocks := contentBlocksAsMaps(content)
	if len(blocks) == 0 {
		// Some clients send a bare object or number. Marshalling keeps the value
		// rather than dropping the result entirely.
		if encoded, err := json.Marshal(content); err == nil {
			return string(encoded)
		}
		return ""
	}
	var parts []string
	for _, block := range blocks {
		switch blockType(block) {
		case "text":
			if s := blockString(block, "text"); s != "" {
				parts = append(parts, s)
			}
		case "image":
			parts = append(parts, "[image omitted: this upstream cannot receive images in tool results]")
		}
	}
	return strings.Join(parts, "\n")
}

// claudeToolsToOpenAI converts tool definitions.
//
// Anthropic server tools carry a Type and are executed by Anthropic itself. They
// cannot be forwarded to a different upstream, so they are dropped rather than
// passed through as a client tool the model would then try to call.
func claudeToolsToOpenAI(tools []ClaudeTool) []OpenAITool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]OpenAITool, 0, len(tools))
	for _, t := range tools {
		if t.Type != "" || strings.TrimSpace(t.Name) == "" {
			continue
		}
		var converted OpenAITool
		converted.Type = "function"
		converted.Function.Name = t.Name
		converted.Function.Description = t.Description
		converted.Function.Parameters = ensureObjectSchema(t.InputSchema)
		out = append(out, converted)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// openAIFinishReasonToClaude maps a finish reason to a stop reason.
//
// The distinction matters to clients: Claude Code branches on tool_use to decide
// whether to run a tool and continue the loop, so mapping it to end_turn would
// silently break agent workflows.
func openAIFinishReasonToClaude(reason string, hasToolCalls bool) string {
	if hasToolCalls {
		return "tool_use"
	}
	switch reason {
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "content_filter":
		return "refusal"
	case "stop", "":
		return "end_turn"
	default:
		return "end_turn"
	}
}
