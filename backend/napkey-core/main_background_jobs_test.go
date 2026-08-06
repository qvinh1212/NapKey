package main

import (
	"os"
	"strings"
	"testing"
)

// Every sweep that moves or reclaims money reports its result, so a failure reaches
// the operations dashboard instead of only a log line.
//
// Two queries failed on every tick from at least 2026-07-30 to 2026-08-06 with nothing
// to show for it but a WARN, and were found only while reading logs during an
// unrelated deploy. Expired trial credit went unreclaimed the whole time. Adding a
// sweep without an alert reopens that hole, so the list is asserted here rather than
// left to a reviewer noticing.
func TestMoneyMovingSweepsReportTheirResult(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("reading main.go: %v", err)
	}
	for _, job := range []string{"operations-alerts", "promotional-expiry", "expired-holds"} {
		want := `RecordBackgroundJobResult(` 
		if !strings.Contains(string(source), want) || !strings.Contains(string(source), `"`+job+`"`) {
			t.Errorf("sweep %q must call RecordBackgroundJobResult so a failure is visible", job)
		}
	}
}
