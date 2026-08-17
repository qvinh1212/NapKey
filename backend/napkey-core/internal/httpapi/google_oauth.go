package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"napkey-core/internal/auth"
	"napkey-core/internal/logger"
	"napkey-core/internal/pricing"
	"napkey-core/internal/store"
)

const (
	googleStateCookie    = "napkey_google_state"
	googleVerifierCookie = "napkey_google_verifier"
	googleFlowTTL        = 10 * time.Minute
)

func (s *Server) googleRedirectURI() string {
	return s.cfg.PublicBaseURL + "/api/v1/auth/google/callback"
}

func safeOAuthLocale(value string) string {
	if value == "en" {
		return "en"
	}
	return "vi"
}

func (s *Server) handleGoogleStart(w http.ResponseWriter, r *http.Request) {
	locale := safeOAuthLocale(r.URL.Query().Get("locale"))
	// A browser reaches this endpoint by navigating, not by fetch, so every failure
	// path has to end in a redirect. Returning JSON here would show the raw error
	// body in the address bar instead of the sign-in page.
	if s.cfg.GoogleClientID == "" || s.cfg.GoogleClientSecret == "" {
		s.redirectOAuthError(w, r, locale, "unconfigured")
		return
	}
	ip := clientIP(r, s.trustProxy)
	if !s.underOAuthLimit(r.Context(), ip) {
		s.redirectOAuthError(w, r, locale, "rate_limited")
		return
	}
	if err := s.store.RecordAuthAttempt(r.Context(), scopeGoogleIP, ip); err != nil {
		logger.Warnf("recording Google start attempt failed: %v", err)
	}
	state, err := auth.NewCSRFToken()
	if err != nil {
		s.redirectOAuthError(w, r, locale, "internal")
		return
	}
	verifier, err := auth.NewCSRFToken()
	if err != nil {
		s.redirectOAuthError(w, r, locale, "internal")
		return
	}
	expires := time.Now().Add(googleFlowTTL)
	s.setOAuthCookie(w, googleStateCookie, auth.SignCookie(s.cfg.SessionSecret, state+":"+locale), expires)
	s.setOAuthCookie(w, googleVerifierCookie, auth.SignCookie(s.cfg.SessionSecret, verifier), expires)

	challenge := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"client_id":             {s.cfg.GoogleClientID},
		"redirect_uri":          {s.googleRedirectURI()},
		"response_type":         {"code"},
		"scope":                 {"openid email profile"},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
		"prompt":                {"select_account"},
	}
	http.Redirect(w, r, "https://accounts.google.com/o/oauth2/v2/auth?"+q.Encode(), http.StatusFound)
}

// underOAuthLimit bounds redirect starts per source address. It fails open for the
// same reason the other auth limits do: this is abuse control, and the callback
// still verifies state and PKCE regardless.
func (s *Server) underOAuthLimit(ctx context.Context, ip string) bool {
	if ip == "" {
		return true
	}
	n, err := s.store.CountAuthAttempts(ctx, scopeGoogleIP, ip, googleStartWindow)
	if err != nil {
		logger.Errorf("counting Google start attempts failed: %v", err)
		return true
	}
	return n < googleStartMaxPerIP
}

func (s *Server) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, stateErr := r.Cookie(googleStateCookie)
	verifierCookie, verifierErr := r.Cookie(googleVerifierCookie)
	s.clearOAuthCookies(w)
	if stateErr != nil || verifierErr != nil {
		s.redirectOAuthError(w, r, "vi", "expired")
		return
	}
	signedState, err := auth.VerifyCookie(s.cfg.SessionSecret, stateCookie.Value)
	if err != nil {
		s.redirectOAuthError(w, r, "vi", "invalid_state")
		return
	}
	expectedState, locale, ok := strings.Cut(signedState, ":")
	locale = safeOAuthLocale(locale)
	if !ok || !auth.CompareCSRF(expectedState, r.URL.Query().Get("state")) {
		s.redirectOAuthError(w, r, locale, "invalid_state")
		return
	}
	if r.URL.Query().Get("error") != "" || strings.TrimSpace(r.URL.Query().Get("code")) == "" {
		s.redirectOAuthError(w, r, locale, "cancelled")
		return
	}
	verifier, err := auth.VerifyCookie(s.cfg.SessionSecret, verifierCookie.Value)
	if err != nil {
		s.redirectOAuthError(w, r, locale, "invalid_state")
		return
	}
	accessToken, err := s.exchangeGoogleCode(r, r.URL.Query().Get("code"), verifier)
	if err != nil {
		logger.Warnf("Google OAuth code exchange failed: %v", err)
		s.redirectOAuthError(w, r, locale, "provider")
		return
	}
	profile, err := s.fetchGoogleProfile(r, accessToken)
	if err != nil || profile.Subject == "" || profile.Email == "" || !profile.EmailVerified {
		logger.Warnf("Google OAuth profile validation failed: %v", err)
		s.redirectOAuthError(w, r, locale, "profile")
		return
	}

	randomPassword, _, err := auth.NewSessionToken()
	if err != nil {
		s.redirectOAuthError(w, r, locale, "internal")
		return
	}
	passwordHash, err := auth.HashPassword(randomPassword)
	if err != nil {
		s.redirectOAuthError(w, r, locale, "internal")
		return
	}
	user, created, err := s.store.FindOrCreateOAuthUser(r.Context(), "google", profile.Subject, profile.Email, passwordHash)
	if err != nil {
		if errors.Is(err, store.ErrOAuthConflict) {
			s.redirectOAuthError(w, r, locale, "account_conflict")
			return
		}
		logger.Errorf("resolving Google user failed: %v", err)
		s.redirectOAuthError(w, r, locale, "internal")
		return
	}
	if !user.IsActive() {
		s.redirectOAuthError(w, r, locale, "suspended")
		return
	}
	if _, err := s.store.GrantTrialForUser(r.Context(), user.ID,
		trialIPHash(s.cfg.TrialFingerprintSecret, clientIP(r, s.trustProxy)),
		trialVND*pricing.MicrosPerVND, time.Now().Add(trialDuration)); err != nil {
		logger.Errorf("granting Google signup trial failed: %v", err)
		s.redirectOAuthError(w, r, locale, "internal")
		return
	}

	token, tokenHash, err := auth.NewSessionToken()
	if err != nil {
		s.redirectOAuthError(w, r, locale, "internal")
		return
	}
	expiresAt := time.Now().Add(s.cfg.SessionTTL)
	if err := s.store.CreateSession(r.Context(), tokenHash, user.ID, expiresAt, r.UserAgent(), clientIP(r, s.trustProxy)); err != nil {
		logger.Errorf("creating Google session failed: %v", err)
		s.redirectOAuthError(w, r, locale, "internal")
		return
	}
	if err := s.setSessionCookies(w, token, expiresAt); err != nil {
		s.redirectOAuthError(w, r, locale, "internal")
		return
	}
	action := "user.login_google"
	if created {
		action = "user.register_google"
	}
	if err := s.store.WriteAudit(r.Context(), store.AuditEntry{
		ActorType: "user", ActorID: user.ID, Action: action,
		TargetType: "user", TargetID: user.ID, IP: clientIP(r, s.trustProxy),
	}); err != nil {
		logger.Warnf("writing Google login audit failed: %v", err)
	}
	http.Redirect(w, r, "/"+locale+"/console", http.StatusSeeOther)
}

func (s *Server) exchangeGoogleCode(r *http.Request, code, verifier string) (string, error) {
	form := url.Values{
		"code": {code}, "client_id": {s.cfg.GoogleClientID},
		"client_secret": {s.cfg.GoogleClientSecret}, "redirect_uri": {s.googleRedirectURI()},
		"grant_type": {"authorization_code"}, "code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.googleTokenURL, strings.NewReader(form.Encode()))
	if err != nil { return "", err }
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := s.oauthHTTP.Do(req)
	if err != nil { return "", err }
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil { return "", err }
	if res.StatusCode != http.StatusOK { return "", errors.New("token endpoint rejected the code") }
	var payload struct { AccessToken string `json:"access_token"` }
	if err := json.Unmarshal(body, &payload); err != nil || payload.AccessToken == "" { return "", errors.New("token response is invalid") }
	return payload.AccessToken, nil
}

type googleProfile struct {
	Subject string `json:"sub"`
	Email string `json:"email"`
	EmailVerified bool `json:"email_verified"`
}

func (s *Server) fetchGoogleProfile(r *http.Request, accessToken string) (*googleProfile, error) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, s.googleUserInfoURL, nil)
	if err != nil { return nil, err }
	req.Header.Set("Authorization", "Bearer "+accessToken)
	res, err := s.oauthHTTP.Do(req)
	if err != nil { return nil, err }
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK { return nil, errors.New("userinfo endpoint rejected the token") }
	var profile googleProfile
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&profile); err != nil { return nil, err }
	profile.Email = store.NormalizeEmail(profile.Email)
	return &profile, nil
}

func (s *Server) setOAuthCookie(w http.ResponseWriter, name, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/api/v1/auth/google/callback",
		HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
		Expires: expires, MaxAge: int(time.Until(expires).Seconds())})
}

func (s *Server) clearOAuthCookies(w http.ResponseWriter) {
	for _, name := range []string{googleStateCookie, googleVerifierCookie} {
		http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/api/v1/auth/google/callback",
			HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
			Expires: time.Unix(1, 0), MaxAge: -1})
	}
}

func (s *Server) redirectOAuthError(w http.ResponseWriter, r *http.Request, locale, code string) {
	http.Redirect(w, r, "/"+safeOAuthLocale(locale)+"/signin?oauth_error="+url.QueryEscape(code), http.StatusSeeOther)
}
