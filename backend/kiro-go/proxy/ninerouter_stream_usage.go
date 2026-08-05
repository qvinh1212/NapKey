package proxy

// Streaming usage accounting for the 9Router upstream.
//
// OpenAI-compatible providers only emit the terminal usage frame when the request
// asks for it with stream_options.include_usage. Without that flag a streamed
// completion arrives with no token counts at all, which this gateway would then be
// unable to price: the charge computes to zero, the wallet hold is released in full,
// and the customer is served for free while the upstream still bills us.
//
// So the flag is forced on for every streaming call. Because that changes what the
// client receives, the extra frame is withheld from clients that did not ask for it,
// keeping the response byte-identical to what they would have got otherwise.

import (
	"encoding/json"
	"strings"
)

// injectStreamUsage forces stream_options.include_usage on a streaming request and
// reports whether the caller had already asked for it.
//
// The payload is edited as a generic map rather than through OpenAIRequest, because
// round-tripping a typed struct would silently drop any field this gateway does not
// model: response_format, presence_penalty, logprobs, seed and anything else the
// client sent. Dropping a client's parameters while adding our own would be a worse
// bug than the one this fixes.
func injectStreamUsage(payload []byte) (out []byte, clientOptedIn bool) {
	var body map[string]interface{}
	if err := json.Unmarshal(payload, &body); err != nil {
		// Not an object we can edit. Forward it unchanged and let the upstream reject
		// it; rewriting a malformed body would only obscure the client's error.
		return payload, false
	}
	if streaming, _ := body["stream"].(bool); !streaming {
		return payload, false
	}

	options, _ := body["stream_options"].(map[string]interface{})
	if options == nil {
		options = map[string]interface{}{}
	} else {
		clientOptedIn, _ = options["include_usage"].(bool)
	}
	if clientOptedIn {
		// Already asked for. Nothing to add, and the frame belongs to the client.
		return payload, true
	}

	options["include_usage"] = true
	body["stream_options"] = options
	edited, err := json.Marshal(body)
	if err != nil {
		return payload, false
	}
	return edited, false
}

// isUsageOnlyFrame reports whether a stream frame carries usage and no content.
//
// This is the frame include_usage adds: usage present, choices empty. It is the one
// frame withheld from clients that did not opt in, and identifying it by shape
// rather than by position matters because it is not always last.
func isUsageOnlyFrame(raw []byte) bool {
	var frame struct {
		Choices []json.RawMessage `json:"choices"`
		Usage   *json.RawMessage  `json:"usage"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		return false
	}
	return frame.Usage != nil && len(frame.Choices) == 0
}

// sseDataPayload extracts the JSON from an SSE data line, or "" if the line is not
// one. Comments, event names and the [DONE] sentinel all return "".
func sseDataPayload(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "data:") {
		return ""
	}
	data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if data == "" || data == "[DONE]" {
		return ""
	}
	return data
}

// rewriteRequestModel replaces the model field on a raw request body.
//
// Edited as a generic map rather than through OpenAIRequest, because round-tripping a
// typed struct would silently drop any field this gateway does not model: seed,
// response_format, logprobs, presence_penalty and anything a client sends that we
// have not enumerated. Losing a customer's parameters while rewriting our own would
// be a worse bug than the routing one this fixes.
func rewriteRequestModel(payload []byte, upstreamModel string) ([]byte, error) {
	if upstreamModel == "" {
		return payload, nil
	}
	var body map[string]interface{}
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, err
	}
	body["model"] = upstreamModel
	return json.Marshal(body)
}
