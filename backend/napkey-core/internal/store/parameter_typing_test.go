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

// No ledger insert may bind one placeholder to both user_id and ref_id.
//
// ledger_entries.user_id is uuid and ref_id is text. A placeholder carries exactly one
// type, so reusing $1 for both makes Postgres deduce a conflict and the statement fails
// at execution -- "inconsistent types deduced for parameter $1". A cast does not rescue
// it either: ::text merely flips the error to "text versus uuid", which is what a first
// attempt at this fix actually did on production.
//
// Checked structurally rather than by matching one known-good string, because the
// string-matching version of this test passed while the bug was still live.
func TestLedgerInsertsBindUserIDAndRefIDSeparately(t *testing.T) {
	source := readStoreSource(t, "wallet.go")
	for _, values := range ledgerInsertValueLists(source) {
		params := strings.Split(values, ",")
		if len(params) < 7 {
			t.Fatalf("unexpected ledger insert shape: %s", values)
		}
		userID, refID := strings.TrimSpace(params[0]), strings.TrimSpace(params[6])
		base := strings.SplitN(refID, "::", 2)[0]
		if base == userID {
			t.Errorf("ledger insert binds %s to both user_id and ref_id: %s\n"+
				"user_id is uuid and ref_id is text; one placeholder cannot be both",
				userID, values)
		}
	}
}

// ledgerInsertValueLists returns the VALUES(...) list of every ledger_entries insert.
func ledgerInsertValueLists(source string) []string {
	var out []string
	for _, chunk := range strings.Split(source, "INSERT INTO ledger_entries")[1:] {
		start := strings.Index(chunk, "VALUES(")
		if start < 0 {
			continue
		}
		rest := chunk[start+len("VALUES("):]
		depth, end := 0, -1
		for i, r := range rest {
			switch r {
			case '(':
				depth++
			case ')':
				if depth == 0 {
					end = i
				} else {
					depth--
				}
			}
			if end >= 0 {
				break
			}
		}
		if end > 0 {
			out = append(out, rest[:end])
		}
	}
	return out
}
