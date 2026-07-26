package proxy

import (
	"sync"
	"time"
)

const rateLimitWindow = time.Minute

type rateLimitResult struct {
	Allowed           bool
	RequestLimit      int
	TokenLimit        int
	RequestsRemaining int
	TokensRemaining   int
	ResetAt           time.Time
}

type rateLimitWindowState struct {
	startedAt time.Time
	requests  int
	tokens    int
}

// rateLimiter keeps per-key minute windows in memory. A process restart resets the
// current window, while durable limits continue to come from the key configuration.
type rateLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	windows map[string]rateLimitWindowState
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{now: time.Now, windows: make(map[string]rateLimitWindowState)}
}

func (l *rateLimiter) allow(keyID string, rpmLimit, tpmLimit, tokens int) rateLimitResult {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	state := l.windows[keyID]
	if state.startedAt.IsZero() || !now.Before(state.startedAt.Add(rateLimitWindow)) {
		state = rateLimitWindowState{startedAt: now}
	}

	result := rateLimitResult{
		Allowed:      true,
		RequestLimit: rpmLimit,
		TokenLimit:   tpmLimit,
		ResetAt:      state.startedAt.Add(rateLimitWindow),
	}
	if rpmLimit > 0 && state.requests+1 > rpmLimit {
		result.Allowed = false
	}
	if tpmLimit > 0 && state.tokens+tokens > tpmLimit {
		result.Allowed = false
	}
	if result.Allowed {
		state.requests++
		state.tokens += tokens
		l.windows[keyID] = state
	}
	if rpmLimit > 0 {
		result.RequestsRemaining = max(0, rpmLimit-state.requests)
	}
	if tpmLimit > 0 {
		result.TokensRemaining = max(0, tpmLimit-state.tokens)
	}
	return result
}
