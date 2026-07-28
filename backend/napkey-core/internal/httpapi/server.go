package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"napkey-core/internal/auth"
	"napkey-core/internal/config"
	"napkey-core/internal/kiro"
	"napkey-core/internal/logger"
	"napkey-core/internal/mail"
	"napkey-core/internal/payos"
	"napkey-core/internal/store"
)

// Cookie names.
const (
	sessionCookieName = "napkey_session"
	csrfCookieName    = "napkey_csrf"
	csrfHeaderName    = "X-CSRF-Token"
)

// Server holds the dependencies every handler needs.
type Server struct {
	cfg    *config.Config
	store  *store.Store
	kiro   *kiro.Client
	mailer mail.Sender
	payos  *payos.Client
	// trustProxy is on when running behind Traefik, so X-Forwarded-For is usable.
	trustProxy bool
	startedAt  time.Time
}

// New builds the server.
func New(cfg *config.Config, st *store.Store, kiroClient *kiro.Client, mailer mail.Sender) *Server {
	return &Server{
		cfg:    cfg,
		store:  st,
		kiro:   kiroClient,
		mailer: mailer,
		payos:  payos.NewClient(cfg.PayOSClientID, cfg.PayOSAPIKey, cfg.PayOSChecksumKey),
		// Coolify terminates TLS at Traefik and forwards, so the header is set by
		// infrastructure rather than the client.
		trustProxy: true,
		startedAt:  time.Now(),
	}
}

// Handler builds the route table.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public.
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /ready", s.handleReady)

	// Auth. These are the endpoints an attacker reaches without credentials, so
	// each one is rate limited inside the handler.
	mux.HandleFunc("POST /v1/auth/register", s.handleRegister)
	mux.HandleFunc("POST /v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /v1/auth/logout", s.handleLogout)
	mux.HandleFunc("POST /v1/auth/verify-email", s.handleVerifyEmail)
	mux.HandleFunc("POST /v1/auth/resend-verification", s.handleResendVerification)
	mux.HandleFunc("POST /v1/auth/forgot-password", s.handleForgotPassword)
	mux.HandleFunc("POST /v1/auth/reset-password", s.handleResetPassword)
	mux.HandleFunc("GET /v1/auth/session", s.requireSession(s.handleGetSession))

	// Console.
	mux.HandleFunc("POST /v1/me/password", s.requireSession(s.handleChangePassword))
	mux.HandleFunc("GET /v1/me/usage", s.requireVerified(s.handleUsageSummary))
	// Stage 3 usage views: the per-day and per-model breakdown, and the raw ledger
	// that makes a charge auditable from the customer's side.
	mux.HandleFunc("GET /v1/me/usage/detail", s.requireVerified(s.handleUsageDetail))
	mux.HandleFunc("GET /v1/me/usage/records", s.requireVerified(s.handleListUsageRecords))
	mux.HandleFunc("GET /v1/me/wallet", s.requireVerified(s.handleGetWallet))
	mux.HandleFunc("POST /v1/me/topups", s.requireVerified(s.handleCreateTopup))
	mux.HandleFunc("GET /v1/me/topups/{id}", s.requireVerified(s.handleGetTopup))
	mux.HandleFunc("GET /v1/keys", s.requireVerified(s.handleListKeys))
	mux.HandleFunc("POST /v1/keys", s.requireVerified(s.handleCreateKey))
	mux.HandleFunc("GET /v1/keys/{id}", s.requireVerified(s.handleGetKey))
	mux.HandleFunc("PATCH /v1/keys/{id}", s.requireVerified(s.handleUpdateKey))
	mux.HandleFunc("DELETE /v1/keys/{id}", s.requireVerified(s.handleRevokeKey))

	// Admin. Stage 2 grants admin by email allowlist; Stage 5 replaces this with
	// real roles and per-action permissions (DESIGN.md section 9).
	mux.HandleFunc("GET /v1/admin/users", s.requirePermission(permissionUsersRead, s.handleAdminListUsers))
	mux.HandleFunc("POST /v1/admin/users/{id}/status", s.requirePermission(permissionUsersWrite, s.handleAdminSetUserStatus))
	mux.HandleFunc("POST /v1/admin/users/{id}/quota", s.requirePermission(permissionKeysWrite, s.handleAdminSetQuota))
	mux.HandleFunc("GET /v1/admin/audit", s.requirePermission(permissionAuditRead, s.handleAdminAuditLog))
	mux.HandleFunc("GET /v1/admin/sync-drift", s.requirePermission(permissionOperationsRead, s.handleAdminSyncDrift))
	// The price book. Reads are open to any admin; a write opens a new price period
	// and is audited, because it changes what every subsequent request costs.
	mux.HandleFunc("GET /v1/admin/prices", s.requirePermission(permissionPricesRead, s.handleAdminListPrices))
	mux.HandleFunc("POST /v1/admin/prices", s.requirePermission(permissionPricesWrite, s.handleAdminSetPrice))
	mux.HandleFunc("GET /v1/admin/prices/quote", s.requirePermission(permissionPricesRead, s.handleAdminQuotePrice))
	// The pre-billing reconciliation view. This has to read clean before Stage 4
	// attaches money to any of these numbers.
	mux.HandleFunc("GET /v1/admin/usage-audit", s.requirePermission(permissionBillingRead, s.handleAdminUsageAudit))
	mux.HandleFunc("POST /v1/admin/keys/{id}/rebuild-counters", s.requirePermission(permissionKeysWrite, s.handleAdminRebuildCounters))
	mux.HandleFunc("GET /v1/admin/operations/summary", s.requirePermission(permissionOperationsRead, s.handleAdminOperationsSummary))
	mux.HandleFunc("GET /v1/admin/operations/alerts", s.requirePermission(permissionOperationsRead, s.handleAdminOperationsAlerts))
	mux.HandleFunc("POST /v1/admin/operations/reconcile-wallets", s.requirePermission(permissionBillingReconcile, s.handleAdminReconcileWallets))
	mux.HandleFunc("GET /v1/admin/users/{id}/roles", s.requirePermission(permissionUsersRead, s.handleAdminGetRoles))
	mux.HandleFunc("PUT /v1/admin/users/{id}/roles", s.requirePermission(permissionUsersWrite, s.handleAdminSetRoles))

	// Internal, called by kiro-go to report usage. Authenticated with the shared
	// admin password rather than a session.
	mux.HandleFunc("POST /internal/usage", s.requireInternalAuth(s.handleReportUsage))
	mux.HandleFunc("POST /internal/wallet/reserve", s.requireInternalAuth(s.handleReserveWallet))
	mux.HandleFunc("POST /internal/wallet/release", s.requireInternalAuth(s.handleReleaseWallet))

	// Public payment webhooks. Authentication is the provider HMAC, not a session.
	mux.HandleFunc("POST /webhooks/payos", s.handlePayOSWebhook)
	mux.HandleFunc("POST /webhooks/casso", s.handleCassoWebhook)

	return s.withCommonMiddleware(mux)
}

// withCommonMiddleware applies CORS, security headers, panic recovery, and logging.
func (s *Server) withCommonMiddleware(next http.Handler) http.Handler {
	return s.recoverPanic(s.securityHeaders(s.cors(s.logRequests(next))))
}

// recoverPanic keeps one bad request from taking the process down.
func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				// http.ErrAbortHandler is the documented way to abandon a response
				// and must not be reported as a crash.
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				logger.Errorf("panic serving %s %s: %v", r.Method, r.URL.Path, rec)
				writeError(w, http.StatusInternalServerError, codeInternal, "something went wrong on our side")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// This API only ever returns JSON, so a restrictive CSP costs nothing and
		// blocks rendering if a response is ever mistakenly served as HTML.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		if s.cfg.SecureCookies {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// cors allows the console origin only.
//
// A wildcard would be wrong here: these endpoints carry a session cookie, and
// credentialed requests plus a wildcard origin is how a third-party page reads a
// logged-in user's data.
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && s.isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, "+csrfHeaderName)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) isAllowedOrigin(origin string) bool {
	return strings.EqualFold(strings.TrimRight(origin, "/"), s.cfg.PublicBaseURL)
}

// statusRecorder captures the status code for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusRecorder) WriteHeader(status int) {
	if !w.wrote {
		w.status = status
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if !w.wrote {
		w.status = http.StatusOK
		w.wrote = true
	}
	return w.ResponseWriter.Write(b)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		// Only the path is logged. Query strings and bodies can hold tokens and
		// passwords, and an access log is the last place those should live.
		logger.Infof("%s %s %d %s %s",
			r.Method, r.URL.Path, rec.status,
			time.Since(start).Round(time.Millisecond), clientIP(r, s.trustProxy))
	})
}

// sessionContextKey keys the authenticated session in the request context.
type sessionContextKey struct{}

// sessionFromContext returns the authenticated session, if any.
func sessionFromContext(ctx context.Context) *store.SessionUser {
	v, _ := ctx.Value(sessionContextKey{}).(*store.SessionUser)
	return v
}

// requireSession authenticates a request and enforces CSRF on unsafe methods.
func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		su, ok := s.authenticate(w, r)
		if !ok {
			return
		}
		if !s.checkCSRF(w, r) {
			return
		}
		ctx := context.WithValue(r.Context(), sessionContextKey{}, su)
		next(w, r.WithContext(ctx))
	}
}

// requireVerified additionally demands a confirmed email address.
//
// Creating API keys is gated on verification so an unverified address cannot mint
// credentials, which is what makes throwaway-address abuse cheap.
func (s *Server) requireVerified(next http.HandlerFunc) http.HandlerFunc {
	return s.requireSession(func(w http.ResponseWriter, r *http.Request) {
		su := sessionFromContext(r.Context())
		if su == nil || !su.User.IsVerified() {
			writeError(w, http.StatusForbidden, codeEmailUnverified,
				"confirm your email address before managing API keys")
			return
		}
		next(w, r)
	})
}

// requirePermission authorizes database-backed roles. ADMIN_EMAILS remains a
// bootstrap owner path so the first operator can assign durable roles.
func (s *Server) requirePermission(permission string, next http.HandlerFunc) http.HandlerFunc {
	return s.requireVerified(func(w http.ResponseWriter, r *http.Request) {
		su := sessionFromContext(r.Context())
		if su == nil {
			writeError(w, http.StatusNotFound, codeNotFound, "not found")
			return
		}
		permissions, err := s.store.AdminPermissions(r.Context(), su.User.ID)
		if err != nil {
			writeStoreError(w, err, "loading admin permissions")
			return
		}
		if !s.cfg.IsAdmin(su.User.Email) && !containsPermission(permissions, permission) {
			// Deliberately 404, not 403: confirming that an admin route exists tells
			// an attacker where to aim.
			writeError(w, http.StatusNotFound, codeNotFound, "not found")
			return
		}
		next(w, r)
	})
}

// requireInternalAuth guards service-to-service endpoints with the shared admin
// password.
//
// This is a shared secret between two services on a private Docker network, not a
// user credential. It must never be reachable from the internet; Coolify should not
// publish this path.
func (s *Server) requireInternalAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provided := r.Header.Get("X-Internal-Token")
		if provided == "" || !auth.CompareCSRF(provided, s.cfg.KiroAdminPassword) {
			writeError(w, http.StatusUnauthorized, codeUnauthorized, "invalid internal token")
			return
		}
		next(w, r)
	}
}

// authenticate resolves the session cookie.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (*store.SessionUser, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "sign in to continue")
		return nil, false
	}
	token, err := auth.VerifyCookie(s.cfg.SessionSecret, cookie.Value)
	if err != nil {
		// A tampered cookie is rejected without touching the database.
		s.clearSessionCookie(w)
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "your session is no longer valid")
		return nil, false
	}
	su, err := s.store.LookupSession(r.Context(), auth.HashToken(token))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.clearSessionCookie(w)
			writeError(w, http.StatusUnauthorized, codeUnauthorized, "your session has expired")
			return nil, false
		}
		writeStoreError(w, err, "looking up session")
		return nil, false
	}
	// A suspension has to take effect on the next request, not at session expiry.
	if !su.User.IsActive() {
		s.clearSessionCookie(w)
		writeError(w, http.StatusForbidden, codeForbidden, "this account is suspended")
		return nil, false
	}
	// Throttled so a busy console does not write on every request.
	if err := s.store.TouchSession(r.Context(), su.Session.TokenHash, 5*time.Minute); err != nil {
		logger.Warnf("touching session failed: %v", err)
	}
	return su, true
}

// checkCSRF enforces the double-submit cookie pattern on state-changing methods.
//
// SameSite=Lax already blocks most cross-site POSTs, but it is a browser-side
// control and not uniform across clients. The token check is server-side and does
// not depend on the browser getting SameSite right.
func (s *Server) checkCSRF(w http.ResponseWriter, r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil || cookie.Value == "" {
		writeError(w, http.StatusForbidden, codeForbidden, "missing CSRF token")
		return false
	}
	if !auth.CompareCSRF(cookie.Value, r.Header.Get(csrfHeaderName)) {
		writeError(w, http.StatusForbidden, codeForbidden, "CSRF token mismatch")
		return false
	}
	return true
}

// setSessionCookies issues the session and CSRF cookies.
func (s *Server) setSessionCookies(w http.ResponseWriter, token string, expiresAt time.Time) error {
	csrf, err := auth.NewCSRFToken()
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:  sessionCookieName,
		Value: auth.SignCookie(s.cfg.SessionSecret, token),
		Path:  "/",
		// HttpOnly keeps the session out of reach of any script on the page, which
		// is what limits the damage of an XSS bug in the console.
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
	})
	// The CSRF cookie is readable by script on purpose: the frontend has to copy it
	// into a request header. It is not a secret, it is a same-origin proof.
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    csrf,
		Path:     "/",
		HttpOnly: false,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
	})
	return nil
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	for _, name := range []string{sessionCookieName, csrfCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			HttpOnly: name == sessionCookieName,
			Secure:   s.cfg.SecureCookies,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		})
	}
}
