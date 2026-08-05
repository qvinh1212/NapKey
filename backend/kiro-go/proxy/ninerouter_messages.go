package proxy

// Serving /v1/messages from the 9Router upstream.
//
// The endpoint speaks Anthropic; the upstream speaks OpenAI. This translates in both
// directions and reshapes the SSE stream, so Claude Code and any Anthropic SDK keep
// working when the upstream is switched.
//
// Billing is unchanged from the chat path: token counts come from the upstream's
// usage block and credits are reported as zero, because the OpenAI protocol carries
// no credit meter and inventing one would put an unmeasured number in the ledger.

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"kiro-go/config"
	"kiro-go/logger"
)

// handleNineRouterMessages serves the Anthropic Messages API from 9Router.
func (h *Handler) handleNineRouterMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	client, err := getNineRouterClient()
	if err != nil {
		logger.Errorf("[9Router] refusing to serve messages: %v", err)
		h.sendClaudeError(w, http.StatusServiceUnavailable, "api_error", "upstream is not configured")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.sendClaudeError(w, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}

	var req ClaudeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.sendClaudeError(w, http.StatusBadRequest, "invalid_request_error", "Invalid JSON")
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		h.sendClaudeError(w, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if len(req.Messages) == 0 {
		h.sendClaudeError(w, http.StatusBadRequest, "invalid_request_error", "messages must not be empty")
		return
	}

	// The thinking suffix is a NapKey convention, not an upstream model id. Strip it
	// before forwarding or the upstream sees a model it does not have.
	//
	// Note the asymmetry with the chat path, which forwards the id unchanged: this
	// upstream does publish real "-thinking" variants, so the same public id yields
	// thinking output on /v1/chat/completions and plain output here. Neither is a
	// billing hole (both price against the public id the customer sent, and the
	// unknown-id fallback still charges the standard rate plus fee), so the behaviour
	// is left as-is rather than changed silently. Routing this endpoint to the
	// "-thinking" variant is a product decision, not a bug fix.
	thinkingCfg := config.GetThinkingConfig()
	actualModel, _ := ParseModelAndThinking(req.Model, thinkingCfg.Suffix)
	requestedModel := req.Model
	req.Model = actualModel

	converted := claudeToOpenAIRequest(&req)
	// Same rewrite as the chat path. requestedModel keeps the public id, which is what
	// the customer is told and billed against.
	converted.Model = nineRouterUpstreamModel(actualModel)
	payload, err := json.Marshal(converted)
	if err != nil {
		h.sendClaudeError(w, http.StatusInternalServerError, "api_error", "could not translate the request")
		return
	}

	apiKeyID := apiKeyIDFromContext(r.Context())
	billing := billingLeaseFromContext(r.Context())
	reqStart := time.Now()

	// Measured from the caller's own request, before translation, so it reflects what
	// they sent. Used only to report upstream prompt overhead, never to bill.
	estimatedInput := estimateClaudeRequestInputTokens(&req)

	// Same requirement as the chat path: without include_usage a streamed completion
	// reports no tokens and cannot be priced. The client never sees this frame here,
	// because the Anthropic protocol carries usage inside message_delta instead.
	payload, _ = injectStreamUsage(payload)

	resp, err := client.ChatCompletions(r.Context(), payload, req.Stream)
	if err != nil {
		logger.Warnf("[9Router] messages upstream call failed: %v", err)
		h.sendClaudeError(w, http.StatusBadGateway, "api_error", "upstream request failed")
		return
	}

	if resp.Stream != nil {
		h.streamNineRouterMessages(w, resp, requestedModel, apiKeyID, billing, reqStart, estimatedInput)
		return
	}

	if resp.Status >= 300 {
		// Pass the upstream status through, reshaped into an Anthropic error so the
		// client's error handling still works. Nothing is billed for a failed request.
		h.sendClaudeError(w, resp.Status, "api_error", upstreamErrorMessage(resp.Body))
		return
	}

	claudeResp, usage, err := openAIToClaudeResponse(resp.Body, requestedModel)
	if err != nil {
		logger.Warnf("[9Router] could not translate the upstream response: %v", err)
		h.sendClaudeError(w, http.StatusBadGateway, "api_error", "malformed upstream response")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(claudeResp)

	h.recordNineRouterUsage(usage, resp, requestedModel, apiKeyID, billing, reqStart, estimatedInput)
}

// streamNineRouterMessages relays a streamed completion as Anthropic SSE events.
//
// Frames are translated and flushed as they arrive so the customer sees tokens at
// upstream speed. Unlike the chat path this cannot be a byte copy: the two protocols
// structure a stream differently, so every frame is parsed and re-emitted.
func (h *Handler) streamNineRouterMessages(w http.ResponseWriter, resp *nineRouterResponse, model, apiKeyID string, billing *billingLease, reqStart time.Time, estimatedInput int) {
	defer resp.Stream.Close()

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.sendClaudeError(w, http.StatusInternalServerError, "api_error", "Streaming not supported")
		return
	}

	state := newClaudeStreamState()
	scanner := bufio.NewScanner(resp.Stream)
	// Tool call arguments can make a single frame large; the default 64KB limit is
	// not enough and an exceeded limit would silently truncate the stream.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		for _, ev := range state.translateOpenAIChunk([]byte(data), model) {
			h.sendSSE(w, flusher, ev.Event, ev.Data)
		}
	}
	if err := scanner.Err(); err != nil {
		logger.Warnf("[9Router] messages stream ended early: %v", err)
	}

	// Closing events are emitted even after a truncated stream, so the client sees a
	// terminated message rather than hanging on an open block.
	for _, ev := range state.finish() {
		h.sendSSE(w, flusher, ev.Event, ev.Data)
	}

	h.recordNineRouterUsage(state.usage, resp, model, apiKeyID, billing, reqStart, estimatedInput)
}

// upstreamErrorMessage pulls a readable message out of an upstream error body.
func upstreamErrorMessage(body []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		if msg := strings.TrimSpace(envelope.Error.Message); msg != "" {
			return msg
		}
		if code := strings.TrimSpace(envelope.Error.Code); code != "" {
			return "upstream error: " + code
		}
	}
	return "upstream request failed"
}
