package gendb

import (
	"strings"
	"testing"
)

// These guard generated SQL at the admission boundary, not only scan callers.
// Behavioral database cases live in tests/integration; those require an isolated DB.
func TestConcurrencyAdmissionSQLContracts(t *testing.T) {
	cases := []struct {
		name, query string
		required    []string
	}{
		{"due scan excludes blocked queue schedules", listDueSchedules, []string{"NOT EXISTS", "schedule_occurrences", "overlap_policy"}},
		{"pending scan excludes active and earlier pending", listAdmissiblePendingOccurrences, []string{"NOT EXISTS", "'queued'", "'running'", "earlier"}},
		{"delivery claim checks due time and attempts", cASClaimDelivery, []string{"next_attempt_at", "CURRENT_TIMESTAMP(3)", "attempt < max_attempts", "attempt = ?"}},
		{"delivery finish fences generation", cASFinishDelivery, []string{"attempt = ?"}},
		{"delivery retry fences generation", requeueDelivery, []string{"attempt = ?"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			for _, fragment := range tt.required {
				if !strings.Contains(tt.query, fragment) {
					t.Errorf("missing %q in %s", fragment, tt.query)
				}
			}
		})
	}
}
