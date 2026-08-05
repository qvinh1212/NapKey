package proxy

// Request handling for the 9Router upstream.
//
// This path deliberately does very little to the payload. The client speaks the
// OpenAI protocol and so does 9Router, so the request is forwarded as received and
// the response is written back as received. Everything this file adds is the part
// NapKey owns either way: authentication already happened upstream of here, and
// what remains is measuring the request and reporting it for billing.

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"kiro-go/logger"
)

// handleNineRouterChat serves /v1/chat/completions from the 9Router upstream.
func (h *Handler) handleNineRouterChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	client, err := getNineRouterClient()
	if err != nil {
		// Misconfiguration, not an upstream outage. Serving from the account pool
		// instead would route traffic through an upstream the operator did not pick.
		logger.Errorf("[9Router] refusing to serve: %v", err)
		h.sendOpenAIError(w, http.StatusServiceUnavailable, "server_error", "upstream is not configured")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.sendOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}

	var req OpenAIRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.sendOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "Invalid JSON")
		return
	}
	if msg := validateOpenAIRequestShape(&req); msg != "" {
		h.sendOpenAIError(w, http.StatusBadRequest, "invalid_request_error", msg)
		return
	}

	apiKeyID := apiKeyIDFromContext(r.Context())
	billing := billingLeaseFromContext(r.Context())
	reqStart := time.Now()

	// Measured before the body is rewritten, so it reflects what the caller actually
	// sent. Used only to report upstream prompt overhead, never to bill.
	estimatedInput := estimateOpenAIRequestInputTokens(&req)

	// The public model id has to be rewritten to the upstream one, or 9Router answers
	// 404: it namespaces models by provider pool.
	body, err = rewriteRequestModel(body, nineRouterUpstreamModel(req.Model))
	if err != nil {
		h.sendOpenAIError(w, http.StatusInternalServerError, "server_error", "could not route the request")
		return
	}

	// Streaming providers only send the terminal usage frame when asked. Without it
	// there are no token counts to price the request with, so it would settle at zero
	// while the upstream still charges us.
	upstreamBody, clientWantsUsage := injectStreamUsage(body)

	resp, err := client.ChatCompletions(r.Context(), upstreamBody, req.Stream)
	if err != nil {
		logger.Warnf("[9Router] upstream call failed: %v", err)
		h.sendOpenAIError(w, http.StatusBadGateway, "server_error", "upstream request failed")
		return
	}

	if resp.Stream != nil {
		h.streamNineRouter(w, resp, req.Model, apiKeyID, billing, reqStart, clientWantsUsage, estimatedInput)
		return
	}

	// A non-2xx is written through so the client sees the upstream's own error, but
	// nothing is billed: the customer did not receive a completion.
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(resp.Status)
	w.Write(resp.Body)

	if resp.Status >= 300 {
		return
	}
	h.recordNineRouterUsage(resp.Usage, resp, req.Model, apiKeyID, billing, reqStart, estimatedInput)
}

// streamNineRouter relays an SSE stream to the client while capturing the trailing
// usage frame.
//
// The stream is forwarded chunk by chunk and flushed as it arrives, so the customer
// sees tokens at upstream speed. A copy is retained only to read the usage block at
// the end, which OpenAI-compatible streams place on the final frame.
func (h *Handler) streamNineRouter(w http.ResponseWriter, resp *nineRouterResponse, model, apiKeyID string, billing *billingLease, reqStart time.Time, clientWantsUsage bool, estimatedInput int) {
	defer resp.Stream.Close()

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.sendOpenAIError(w, http.StatusInternalServerError, "server_error", "Streaming not supported")
		return
	}

	// Read line by line rather than copying bytes, because the usage frame this
	// gateway asked for has to be recognised: it is forwarded only to clients that
	// requested it themselves, so a client that did not opt in sees exactly the
	// stream it would have received without our flag.
	var usage *nineRouterUsage
	scanner := bufio.NewScanner(resp.Stream)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if data := sseDataPayload(line); data != "" {
			if isUsageOnlyFrame([]byte(data)) {
				if parsed := parseNineRouterUsage([]byte(data)); parsed != nil {
					usage = parsed
				}
				if !clientWantsUsage {
					continue
				}
			} else if parsed := parseNineRouterUsage([]byte(data)); parsed != nil {
				// Some providers attach usage to the final content frame instead of
				// sending a separate one. Captured either way.
				usage = parsed
			}
		}
		if _, err := w.Write([]byte(line + "\n")); err != nil {
			// The client hung up. The caller's deferred cleanup releases the hold, so
			// nothing is charged for a response nobody received.
			logger.Debugf("[9Router] client disconnected mid-stream: %v", err)
			return
		}
		flusher.Flush()
	}
	if err := scanner.Err(); err != nil {
		logger.Warnf("[9Router] stream ended early: %v", err)
	}

	h.recordNineRouterUsage(usage, resp, model, apiKeyID, billing, reqStart, estimatedInput)
}

// recordNineRouterUsage reports one completed request for billing.
//
// Credits are reported as zero, always. 9Router speaks the OpenAI protocol, which
// carries no credit meter, so napkey-core prices this traffic from the token counts
// against model_prices. Synthesising a credit figure here would put a number
// nobody measured into the ledger, and it would be indistinguishable from a real
// meter reading once stored.
func (h *Handler) recordNineRouterUsage(usage *nineRouterUsage, resp *nineRouterResponse, model, apiKeyID string, billing *billingLease, reqStart time.Time, estimatedInput int) {
	if usage == nil {
		// Served but unmeasured. The control plane cannot price this, and inventing
		// token counts would bill a guess, so it is logged for the reconciliation
		// view instead of silently passing through as a free request.
		logger.Warnf("[9Router] no usage reported for model %s; request served but not billable", model)
		return
	}

	upstreamModel := resp.Model
	if upstreamModel == "" {
		upstreamModel = model
	}

	// prompt_tokens includes cached reads when the upstream separates them. Billing
	// splits the two because their rates differ, so the cached portion is subtracted
	// out rather than charged twice.
	cacheRead := 0
	if usage.PromptTokensDetails != nil {
		cacheRead = maxInt(usage.PromptTokensDetails.CachedTokens, 0)
	}
	freshInput := maxInt(usage.PromptTokens-cacheRead, 0)

	// Surface an upstream-injected prompt if there is one. Does not change the charge.
	reportPromptOverhead(model, estimatedInput, usage.PromptTokens)

	h.recordSuccessForApiKey(apiKeyID, usage.PromptTokens, usage.CompletionTokens, 0, usageDetail{
		Billing:             billing,
		Model:               model,
		AccountID:           resp.Provider,
		BillableInputTokens: freshInput,
		CacheReadTokens:     cacheRead,
		CacheWriteTokens:    0,
		LatencyMS:           time.Since(reqStart).Milliseconds(),
		// Token counts came from the upstream, not from this process's estimator.
		OutputEstimated: false,
	})
}

// nineRouterHealth reports whether the configured upstream answers.
func nineRouterHealth(ctx context.Context) error {
	client, err := getNineRouterClient()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+client.apiKey)
	if client.cfClientID != "" {
		req.Header.Set("CF-Access-Client-Id", client.cfClientID)
		req.Header.Set("CF-Access-Client-Secret", client.cfSecret)
	}
	resp, err := client.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return errNineRouterNoUsage
	}
	return nil
}
