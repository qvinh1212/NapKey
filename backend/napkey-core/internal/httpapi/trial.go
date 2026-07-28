package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"net"
)

const trialFingerprintDomain = "napkey/trial-ip/v1\x00"

// normalizeTrialIP groups IPv6 privacy addresses by /64 while retaining the
// exact IPv4 address. This prevents trivial IPv6 address rotation without
// storing the source address itself.
func normalizeTrialIP(raw string) string {
	ip := net.ParseIP(raw)
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	ip = ip.To16()
	if ip == nil {
		return ""
	}
	mask := net.CIDRMask(64, 128)
	return (&net.IPNet{IP: ip.Mask(mask), Mask: mask}).String()
}

// trialIPHash creates a stable, non-reversible database identifier. The
// existing session secret is domain-separated so the hash cannot be reused as
// a session primitive even though both are HMAC based.
func trialIPHash(secret []byte, rawIP string) []byte {
	normalized := normalizeTrialIP(rawIP)
	if normalized == "" || len(secret) == 0 {
		return nil
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(trialFingerprintDomain))
	_, _ = mac.Write([]byte(normalized))
	return mac.Sum(nil)
}
