// Package httpapi is the REST surface consumed by the Next.js console.
package httpapi

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"

	"napkey-core/internal/logger"
	"napkey-core/internal/store"
)

// errorResponse is the single error shape every endpoint returns, so the frontend
// has one thing to parse.
type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		// Fields carries per-field validation messages for forms.
		Fields map[string]string `json:"fields,omitempty"`
	} `json:"error"`
}

// Error codes the console branches on.
const (
	codeInvalidRequest  = "invalid_request"
	codeUnauthorized    = "unauthorized"
	codeForbidden       = "forbidden"
	codeNotFound        = "not_found"
	codeConflict        = "conflict"
	codeRateLimited     = "rate_limited"
	codeInternal        = "internal_error"
	codeEmailUnverified = "email_unverified"
	codeUpstreamFailure = "upstream_failure"
)

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Nothing this API returns should be cached: it is all per-session data.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		// The status line is already sent, so this can only be logged.
		logger.Warnf("encoding response body failed: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var resp errorResponse
	resp.Error.Code = code
	resp.Error.Message = message
	writeJSON(w, status, resp)
}

func writeFieldErrors(w http.ResponseWriter, fields map[string]string) {
	var resp errorResponse
	resp.Error.Code = codeInvalidRequest
	resp.Error.Message = "one or more fields are invalid"
	resp.Error.Fields = fields
	writeJSON(w, http.StatusBadRequest, resp)
}

// writeStoreError maps a store error onto a status code.
//
// Internal errors are logged with detail and reported without it: a database
// message can name tables and constraints, which is free reconnaissance.
func writeStoreError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "not found")
	case errors.Is(err, store.ErrEmailTaken):
		writeError(w, http.StatusConflict, codeConflict, "email is already registered")
	case errors.Is(err, store.ErrKeyLimit):
		writeError(w, http.StatusConflict, codeConflict, "you have reached the maximum number of API keys")
	case errors.Is(err, store.ErrUserSuspended):
		writeError(w, http.StatusForbidden, codeForbidden, "this account is suspended")
	default:
		logger.Errorf("%s: %v", action, err)
		writeError(w, http.StatusInternalServerError, codeInternal, "something went wrong on our side")
	}
}

// maxBodyBytes caps request bodies. Every endpoint here takes a small JSON object,
// so a generous cap still refuses anything that could be a memory attack.
const maxBodyBytes = 64 << 10

// decodeJSON reads and strictly decodes a JSON body.
//
// DisallowUnknownFields is deliberate: a typo'd field name in a client should be a
// visible error rather than a silently ignored setting. That matters most for
// fields like enabled, where being ignored means the opposite of what was asked.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if ct := r.Header.Get("Content-Type"); ct != "" {
		mediaType := strings.TrimSpace(strings.Split(ct, ";")[0])
		if !strings.EqualFold(mediaType, "application/json") {
			writeError(w, http.StatusUnsupportedMediaType, codeInvalidRequest,
				"Content-Type must be application/json")
			return false
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, codeInvalidRequest, "request body is too large")
			return false
		}
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "request body is not valid JSON: "+err.Error())
		return false
	}
	// A second object in the body would mean the client sent something other than
	// what was parsed.
	if dec.More() {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "request body must contain a single JSON object")
		return false
	}
	return true
}

// clientIP extracts the caller's address.
//
// X-Forwarded-For is only trusted when trustProxy is set, which is correct behind
// Coolify's Traefik. Trusting it unconditionally would let anyone spoof the
// identifier that rate limiting keys on, making the limit useless.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// Leftmost entry is the original client; the rest are proxies.
			first := strings.TrimSpace(strings.Split(xff, ",")[0])
			if first != "" && net.ParseIP(first) != nil {
				return first
			}
		}
		if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" && net.ParseIP(real) != nil {
			return real
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// parsePagination reads limit and offset query parameters with safe defaults.
func parsePagination(r *http.Request, defLimit, maxLimit int) (limit, offset int) {
	limit = defLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}
