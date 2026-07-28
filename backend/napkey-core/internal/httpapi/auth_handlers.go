package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"napkey-core/internal/auth"
	"napkey-core/internal/logger"
	mailer "napkey-core/internal/mail"
	"napkey-core/internal/pricing"
	"napkey-core/internal/store"
)

// Rate limits for the unauthenticated endpoints. These are the doors an attacker
// can knock on without credentials, so each has a per-identifier budget.
const (
	// loginMaxAttempts is per email address in the window. Generous enough for a
	// human mistyping, tight enough that online guessing is not viable.
	loginMaxAttempts = 10
	loginWindow      = 15 * time.Minute
	// loginIPMaxAttempts is a second, wider limit per source address so one host
	// cannot spread guesses across many accounts.
	loginIPMaxAttempts = 50
	registerMaxPerIP   = 5
	registerWindow     = time.Hour
	// Email-sending endpoints are limited separately: each request costs an
	// outbound message, and an unbounded loop makes the service a spam relay
	// pointed at whatever address an attacker names.
	emailMaxPerAddress = 3
	emailWindow        = time.Hour
	resetMaxPerAddress = 3
	trialCredits       = int64(50)
	trialDuration      = 7 * 24 * time.Hour
)

// Rate limit scopes.
const (
	scopeLogin      = "login"
	scopeLoginIP    = "login_ip"
	scopeRegisterIP = "register_ip"
	scopeVerifyMail = "verify_mail"
	scopeResetMail  = "reset_mail"
)

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Locale   string `json:"locale,omitempty"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ip := clientIP(r, s.trustProxy)

	if !s.underLimit(r.Context(), w, scopeRegisterIP, ip, registerMaxPerIP, registerWindow,
		"too many accounts created from this address, try again later") {
		return
	}

	email := store.NormalizeEmail(req.Email)
	fields := map[string]string{}
	if err := validateEmail(email); err != nil {
		fields["email"] = err.Error()
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		fields["password"] = err.Error()
	}
	if len(fields) > 0 {
		writeFieldErrors(w, fields)
		return
	}

	if err := s.store.RecordAuthAttempt(r.Context(), scopeRegisterIP, ip); err != nil {
		logger.Warnf("recording register attempt failed: %v", err)
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		logger.Errorf("hashing password failed: %v", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "something went wrong on our side")
		return
	}

	user, err := s.store.CreateUser(r.Context(), email, hash)
	if err != nil {
		if errors.Is(err, store.ErrEmailTaken) {
			// Registration is the one place account existence cannot be fully
			// hidden, because the address genuinely cannot be reused. What is
			// avoided is confirming it to an unauthenticated caller: the response
			// is the same shape as success, and an email goes to the existing
			// address instead.
			s.sendExistingAccountNotice(r.Context(), email, req.Locale)
			writeJSON(w, http.StatusAccepted, map[string]any{
				"status":  "verification_sent",
				"message": "check your inbox to confirm your email address",
			})
			return
		}
		writeStoreError(w, err, "creating user")
		return
	}

	s.issueVerificationEmail(r.Context(), user, req.Locale)

	if err := s.store.WriteAudit(r.Context(), store.AuditEntry{
		ActorType:  "user",
		ActorID:    user.ID,
		Action:     "user.register",
		TargetType: "user",
		TargetID:   user.ID,
		IP:         ip,
	}); err != nil {
		logger.Warnf("writing audit log failed: %v", err)
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "verification_sent",
		"message": "check your inbox to confirm your email address",
	})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	email := store.NormalizeEmail(req.Email)
	ip := clientIP(r, s.trustProxy)

	if !s.underLimit(r.Context(), w, scopeLoginIP, ip, loginIPMaxAttempts, loginWindow,
		"too many sign-in attempts from this address, try again later") {
		return
	}
	if email != "" && !s.underLimit(r.Context(), w, scopeLogin, email, loginMaxAttempts, loginWindow,
		"too many sign-in attempts for this account, try again later") {
		return
	}

	if err := s.store.RecordAuthAttempt(r.Context(), scopeLoginIP, ip); err != nil {
		logger.Warnf("recording login attempt failed: %v", err)
	}
	if email != "" {
		if err := s.store.RecordAuthAttempt(r.Context(), scopeLogin, email); err != nil {
			logger.Warnf("recording login attempt failed: %v", err)
		}
	}

	user, err := s.store.GetUserByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Burn equivalent CPU so response time does not reveal whether the
			// address exists. Without this, login latency is an enumeration oracle.
			auth.DummyVerify(req.Password)
			writeError(w, http.StatusUnauthorized, codeUnauthorized, "email or password is incorrect")
			return
		}
		writeStoreError(w, err, "loading user for login")
		return
	}

	needsRehash, err := auth.VerifyPassword(req.Password, user.PasswordHash)
	if err != nil {
		if errors.Is(err, auth.ErrMismatch) || errors.Is(err, auth.ErrInvalidHash) {
			if errors.Is(err, auth.ErrInvalidHash) {
				logger.Errorf("user %s has a malformed password hash", user.ID)
			}
			// Same message for a wrong password and an unknown address.
			writeError(w, http.StatusUnauthorized, codeUnauthorized, "email or password is incorrect")
			return
		}
		logger.Errorf("verifying password failed: %v", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "something went wrong on our side")
		return
	}

	if !user.IsActive() {
		writeError(w, http.StatusForbidden, codeForbidden, "this account is suspended")
		return
	}

	// Upgrade the stored hash when the cost parameters have moved on. Doing it on
	// a successful login is the only moment the cleartext is available.
	if needsRehash {
		if newHash, hashErr := auth.HashPassword(req.Password); hashErr == nil {
			if err := s.store.UpdatePasswordHash(r.Context(), user.ID, newHash, nil); err != nil {
				logger.Warnf("rehashing password for user %s failed: %v", user.ID, err)
			}
		}
	}

	token, tokenHash, err := auth.NewSessionToken()
	if err != nil {
		logger.Errorf("generating session token failed: %v", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "something went wrong on our side")
		return
	}
	expiresAt := time.Now().Add(s.cfg.SessionTTL)
	if err := s.store.CreateSession(r.Context(), tokenHash, user.ID, expiresAt,
		r.UserAgent(), ip); err != nil {
		writeStoreError(w, err, "creating session")
		return
	}
	if err := s.setSessionCookies(w, token, expiresAt); err != nil {
		logger.Errorf("setting session cookies failed: %v", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "something went wrong on our side")
		return
	}

	// A successful sign-in clears the throttle so earlier typos do not linger.
	if err := s.store.ClearAuthAttempts(r.Context(), scopeLogin, email); err != nil {
		logger.Warnf("clearing login attempts failed: %v", err)
	}
	if err := s.store.WriteAudit(r.Context(), store.AuditEntry{
		ActorType: "user", ActorID: user.ID, Action: "user.login",
		TargetType: "user", TargetID: user.ID, IP: ip,
	}); err != nil {
		logger.Warnf("writing audit log failed: %v", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{"user": userView(user, s.cfg.IsAdmin(user.Email))})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Logout runs without requireSession so an already-invalid cookie still
	// results in the cookie being cleared rather than a 401.
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil && cookie.Value != "" {
		if token, verifyErr := auth.VerifyCookie(s.cfg.SessionSecret, cookie.Value); verifyErr == nil {
			if err := s.store.DeleteSession(r.Context(), auth.HashToken(token)); err != nil {
				logger.Warnf("deleting session failed: %v", err)
			}
		}
	}
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"status": "signed_out"})
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	su := sessionFromContext(r.Context())
	permissions, err := s.store.AdminPermissions(r.Context(), su.User.ID)
	if err != nil {
		writeStoreError(w, err, "loading session permissions")
		return
	}
	if s.cfg.IsAdmin(su.User.Email) {
		permissions = append([]string(nil), ownerPermissions...)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":      userView(&su.User, len(permissions) > 0),
		"permissions": permissions,
		"expiresAt": su.Session.ExpiresAt,
	})
}

type verifyEmailRequest struct {
	Token string `json:"token"`
}

func (s *Server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req verifyEmailRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "token is required")
		return
	}
	ip := clientIP(r, s.trustProxy)
	userID, trialGranted, err := s.store.VerifyEmailAndGrantTrial(
		r.Context(), auth.HashToken(req.Token), trialIPHash(s.cfg.TrialFingerprintSecret, ip),
		trialCredits*pricing.RetailMicrosPerCredit, time.Now().Add(trialDuration),
	)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Covers expired, already used, and never existed. Distinguishing them
			// would let someone probe which tokens are real.
			writeError(w, http.StatusBadRequest, codeInvalidRequest,
				"this verification link is invalid or has expired")
			return
		}
		writeStoreError(w, err, "consuming verification token")
		return
	}
	if err := s.store.WriteAudit(r.Context(), store.AuditEntry{
		ActorType: "user", ActorID: userID, Action: "user.email_verified",
		TargetType: "user", TargetID: userID, IP: ip,
	}); err != nil {
		logger.Warnf("writing audit log failed: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "verified",
		"trial": map[string]any{
			"granted": trialGranted,
			"credits": trialCredits,
			"expiresInDays": 7,
		},
	})
}

type resendVerificationRequest struct {
	Email  string `json:"email"`
	Locale string `json:"locale,omitempty"`
}

func (s *Server) handleResendVerification(w http.ResponseWriter, r *http.Request) {
	var req resendVerificationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	email := store.NormalizeEmail(req.Email)
	if email == "" {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "email is required")
		return
	}
	if !s.underLimit(r.Context(), w, scopeVerifyMail, email, emailMaxPerAddress, emailWindow,
		"a verification email was already sent recently, check your inbox") {
		return
	}
	if err := s.store.RecordAuthAttempt(r.Context(), scopeVerifyMail, email); err != nil {
		logger.Warnf("recording verification attempt failed: %v", err)
	}

	// The response never varies, so this cannot be used to test which addresses
	// are registered.
	defer writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "verification_sent",
		"message": "if that address needs verification, an email is on its way",
	})

	user, err := s.store.GetUserByEmail(r.Context(), email)
	if err != nil || user.IsVerified() {
		return
	}
	s.issueVerificationEmail(r.Context(), user, req.Locale)
}

type forgotPasswordRequest struct {
	Email  string `json:"email"`
	Locale string `json:"locale,omitempty"`
}

func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotPasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	email := store.NormalizeEmail(req.Email)
	if email == "" {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "email is required")
		return
	}
	if !s.underLimit(r.Context(), w, scopeResetMail, email, resetMaxPerAddress, emailWindow,
		"a reset email was already sent recently, check your inbox") {
		return
	}
	if err := s.store.RecordAuthAttempt(r.Context(), scopeResetMail, email); err != nil {
		logger.Warnf("recording reset attempt failed: %v", err)
	}

	defer writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "reset_sent",
		"message": "if that address has an account, a reset link is on its way",
	})

	user, err := s.store.GetUserByEmail(r.Context(), email)
	if err != nil {
		return
	}
	// Issuing a new link retires any earlier one, so a forwarded old email cannot
	// still be used.
	if err := s.store.InvalidateEmailTokens(r.Context(), user.ID, "reset_password"); err != nil {
		logger.Warnf("invalidating old reset tokens failed: %v", err)
	}
	token, tokenHash, err := auth.NewEmailToken()
	if err != nil {
		logger.Errorf("generating reset token failed: %v", err)
		return
	}
	// One hour, shorter than verification: a live reset link is a full account
	// takeover if the mailbox is compromised.
	if err := s.store.CreateEmailToken(r.Context(), tokenHash, user.ID, "reset_password",
		time.Now().Add(time.Hour)); err != nil {
		logger.Errorf("storing reset token failed: %v", err)
		return
	}
	msg := mailer.PasswordResetMessage(s.cfg.PublicBaseURL, req.Locale, user.Email, token)
	if err := s.mailer.Send(r.Context(), msg); err != nil {
		logger.Errorf("sending reset email to %s failed: %v", user.Email, err)
	}
}

type resetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		writeError(w, http.StatusBadRequest, codeInvalidRequest, "token is required")
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		writeFieldErrors(w, map[string]string{"password": err.Error()})
		return
	}
	userID, err := s.store.ConsumeEmailToken(r.Context(), auth.HashToken(req.Token), "reset_password")
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, codeInvalidRequest,
				"this reset link is invalid or has expired")
			return
		}
		writeStoreError(w, err, "consuming reset token")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		logger.Errorf("hashing password failed: %v", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "something went wrong on our side")
		return
	}
	// Passing nil drops every session: a reset is the response to a possible
	// compromise, so any session the attacker holds has to die with it.
	if err := s.store.UpdatePasswordHash(r.Context(), userID, hash, nil); err != nil {
		writeStoreError(w, err, "updating password")
		return
	}
	if err := s.store.WriteAudit(r.Context(), store.AuditEntry{
		ActorType: "user", ActorID: userID, Action: "user.password_reset",
		TargetType: "user", TargetID: userID, IP: clientIP(r, s.trustProxy),
	}); err != nil {
		logger.Warnf("writing audit log failed: %v", err)
	}
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"status": "password_updated"})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	su := sessionFromContext(r.Context())
	var req changePasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := auth.ValidatePassword(req.NewPassword); err != nil {
		writeFieldErrors(w, map[string]string{"newPassword": err.Error()})
		return
	}
	// Requiring the current password stops a stolen session from locking the real
	// owner out of their account.
	if _, err := auth.VerifyPassword(req.CurrentPassword, su.User.PasswordHash); err != nil {
		writeFieldErrors(w, map[string]string{"currentPassword": "current password is incorrect"})
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		logger.Errorf("hashing password failed: %v", err)
		writeError(w, http.StatusInternalServerError, codeInternal, "something went wrong on our side")
		return
	}
	// The current session survives; every other one is revoked.
	if err := s.store.UpdatePasswordHash(r.Context(), su.User.ID, hash, su.Session.TokenHash); err != nil {
		writeStoreError(w, err, "changing password")
		return
	}
	if err := s.store.WriteAudit(r.Context(), store.AuditEntry{
		ActorType: "user", ActorID: su.User.ID, Action: "user.password_changed",
		TargetType: "user", TargetID: su.User.ID, IP: clientIP(r, s.trustProxy),
	}); err != nil {
		logger.Warnf("writing audit log failed: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "password_updated"})
}

// issueVerificationEmail creates a token and sends the confirmation link.
func (s *Server) issueVerificationEmail(ctx context.Context, user *store.User, locale string) {
	if err := s.store.InvalidateEmailTokens(ctx, user.ID, "verify_email"); err != nil {
		logger.Warnf("invalidating old verification tokens failed: %v", err)
	}
	token, tokenHash, err := auth.NewEmailToken()
	if err != nil {
		logger.Errorf("generating verification token failed: %v", err)
		return
	}
	if err := s.store.CreateEmailToken(ctx, tokenHash, user.ID, "verify_email",
		time.Now().Add(s.cfg.EmailTokenTTL)); err != nil {
		logger.Errorf("storing verification token failed: %v", err)
		return
	}
	msg := mailer.VerificationMessage(s.cfg.PublicBaseURL, locale, user.Email, token)
	if err := s.mailer.Send(ctx, msg); err != nil {
		logger.Errorf("sending verification email to %s failed: %v", user.Email, err)
	}
}

// sendExistingAccountNotice tells the real owner that someone tried to register
// their address, without telling the requester anything.
func (s *Server) sendExistingAccountNotice(ctx context.Context, email, locale string) {
	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		return
	}
	// An unverified account gets a fresh verification link, since that is almost
	// always the real owner retrying.
	if !user.IsVerified() {
		s.issueVerificationEmail(ctx, user, locale)
	}
}

// underLimit checks a rate limit and writes a 429 when it is exceeded.
func (s *Server) underLimit(ctx context.Context, w http.ResponseWriter, scope, identifier string,
	max int, window time.Duration, message string) bool {
	if identifier == "" {
		return true
	}
	n, err := s.store.CountAuthAttempts(ctx, scope, identifier, window)
	if err != nil {
		// Fail open on a counting error rather than locking everyone out. The limit
		// is abuse control, not an authorization boundary, so availability wins;
		// the actual credential check still runs either way.
		logger.Errorf("counting auth attempts for scope %s failed: %v", scope, err)
		return true
	}
	if n >= max {
		w.Header().Set("Retry-After", strconv.Itoa(int(window.Seconds())))
		writeError(w, http.StatusTooManyRequests, codeRateLimited, message)
		return false
	}
	return true
}

// validateEmail checks the address shape and length.
func validateEmail(email string) error {
	if email == "" {
		return errors.New("email is required")
	}
	// The database column is citext with a 320-byte practical ceiling; refusing
	// early gives a clear message instead of a constraint violation.
	if len(email) > 254 {
		return errors.New("email address is too long")
	}
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return errors.New("that does not look like a valid email address")
	}
	// ParseAddress accepts display names; only a bare address is wanted.
	if addr.Address != email {
		return errors.New("enter the email address on its own")
	}
	if strings.Count(email, "@") != 1 {
		return errors.New("that does not look like a valid email address")
	}
	return nil
}

// userView is the user shape returned to the console. It deliberately omits the
// password hash.
func userView(u *store.User, isAdmin bool) map[string]any {
	return map[string]any{
		"id":              u.ID,
		"email":           u.Email,
		"emailVerified":   u.IsVerified(),
		"emailVerifiedAt": u.EmailVerifiedAt,
		"status":          u.Status,
		"isAdmin":         isAdmin,
		"createdAt":       u.CreatedAt,
	}
}
