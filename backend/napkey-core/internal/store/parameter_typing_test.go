package store

import (
	"os"
	"strings"
	"testing"
)

// Postgres has to deduce a type for every $n placeholder, and two queries gave it no
// way to do so. Both parsed fine, passed every test, and failed only against a real
// server -- they ran on a background timer, so they logged a warning and the process
// carried on:
//
//	refreshing operations alerts failed: could not determine data type of parameter $5
//	expiring promotional credits failed: inconsistent types deduced for parameter $1
//
// The first hides an alert about unmatched payments. The second means expired trial
// credit is never removed from a dormant wallet, so the liability stays on the books.
// Neither is visible without reading logs, which is why they survived to production.
//
// pgtest replays canned responses and does not type-check, so no round-trip test can
// catch this class. Asserting on the SQL text is the check that would have.
func TestQueriesGivePostgresATypeForEveryParameter(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		fragment string
		reason   string
	}{
		{
			name:     "operations alert count",
			file:     "operations.go",
			fragment: "jsonb_build_object('count', $5::bigint)",
			reason:   "jsonb_build_object accepts any type, so $5 has no context to be deduced from",
		},
		{
			name:     "promotional expiry ledger ref",
			file:     "wallet.go",
			fragment: "'promotional_expiry',$1::text,",
			reason:   "$1 is a uuid in user_id and text in ref_id; without a cast Postgres deduces both and conflicts",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := readStoreSource(t, tc.file)
			if !strings.Contains(source, tc.fragment) {
				t.Errorf("%s must contain %q: %s", tc.file, tc.fragment, tc.reason)
			}
		})
	}
}

func readStoreSource(t *testing.T, name string) string {
	t.Helper()
	source, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return string(source)
}
