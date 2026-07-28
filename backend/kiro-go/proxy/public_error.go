package proxy

import "regexp"

const genericUpstreamError = "Upstream service is temporarily unavailable"

var (
	publicUpstreamHost = regexp.MustCompile(`(?i)https?://[^\s"'<>]*kiro\.dev[^\s"'<>]*`)
	publicUpstreamName = regexp.MustCompile(`(?i)\bkiro(?:[-_ ]?(?:go|cli|ide))?\b`)
)

// publicErrorMessage keeps provider-specific names and hosts behind the proxy boundary.
func publicErrorMessage(message string) string {
	message = publicUpstreamHost.ReplaceAllString(message, "upstream service")
	return publicUpstreamName.ReplaceAllString(message, "upstream")
}

func publicProtocolError(status int, message string) string {
	if status >= 500 {
		return genericUpstreamError
	}
	return publicErrorMessage(message)
}
