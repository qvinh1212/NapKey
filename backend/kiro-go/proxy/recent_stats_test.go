package proxy

import (
	"testing"
	"time"
)

func TestRecentRequestStatsUsesRollingWindow(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	h := &Handler{}
	for _, entry := range []RequestLog{
		{Time: now.Add(-20 * time.Minute).Unix(), Status: "error"},
		{Time: now.Add(-10 * time.Minute).Unix(), Status: "success"},
		{Time: now.Add(-time.Minute).Unix(), Status: "error"},
	} {
		h.appendRequestLog(entry)
	}
	total, failed := h.recentRequestStats(15*time.Minute, now)
	if total != 2 || failed != 1 {
		t.Fatalf("recent stats = %d/%d, want 2/1", total, failed)
	}
}

func TestRecentRequestStatsDoesNotLoseFailuresWhenDisplayLogRotates(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	h := &Handler{}
	for i := 0; i < 100; i++ {
		h.appendRequestLog(RequestLog{Time: now.Add(-10 * time.Minute).Unix(), Status: "error"})
	}
	for i := 0; i < requestLogsMaxSize; i++ {
		h.appendRequestLog(RequestLog{Time: now.Add(-time.Minute).Unix(), Status: "success"})
	}
	total, failed := h.recentRequestStats(15*time.Minute, now)
	if total != 600 || failed != 100 {
		t.Fatalf("recent stats = %d/%d, want 600/100", total, failed)
	}
}
