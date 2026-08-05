package config

import "testing"

// Readers reached before Init must return a default, not panic.
//
// cfg is nil until Load runs, and these are reachable before that: GetProxyURL is
// consulted on every outbound request through ResolveAccountProxyURL, and a test
// exercising that path panicked here rather than seeing the empty proxy that is the
// honest answer. A nil dereference in a getter takes down the request that touched
// it, so each of these has to answer for an unloaded config.
func TestReadersDoNotPanicBeforeLoad(t *testing.T) {
	unloadConfig(t)

	for name, read := range map[string]func(){
		"GetProxyURL":           func() { GetProxyURL() },
		"GetAccounts":           func() { GetAccounts() },
		"GetEnabledAccounts":    func() { GetEnabledAccounts() },
		"GetApiKey":             func() { GetApiKey() },
		"GetPort":               func() { GetPort() },
		"GetHost":               func() { GetHost() },
		"GetStats":              func() { GetStats() },
		"IsApiKeyRequired":      func() { IsApiKeyRequired() },
		"GetLogLevel":           func() { GetLogLevel() },
		"GetThinkingConfig":     func() { GetThinkingConfig() },
		"GetPreferredEndpoint":  func() { GetPreferredEndpoint() },
		"GetEndpointFallback":   func() { GetEndpointFallback() },
		"GetAllowOverUsage":     func() { GetAllowOverUsage() },
		"GetKiroClientConfig":   func() { GetKiroClientConfig() },
		"GetPromptFilterConfig": func() { GetPromptFilterConfig() },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("%s panicked on an unloaded config: %v", name, r)
				}
			}()
			read()
		})
	}
}

// Auth must not be reported as switched off merely because nothing is loaded yet.
//
// authenticate() returns early when this is false, so answering false for an unloaded
// config would serve the data plane to unauthenticated callers.
func TestApiKeyIsRequiredWhenConfigIsUnloaded(t *testing.T) {
	unloadConfig(t)

	if !IsApiKeyRequired() {
		t.Error("an unloaded config reported that no API key is required")
	}
}

// unloadConfig puts the package back into its pre-Init state for the duration of a
// test.
//
// cfg is process-wide, so a test that left one loaded would otherwise mask exactly
// the nil case being asserted here.
func unloadConfig(t *testing.T) {
	t.Helper()
	t.Cleanup(UnloadForTest())
}
