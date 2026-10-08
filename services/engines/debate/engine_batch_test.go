package debate

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
)

func TestSplitCandidatesBalanced(t *testing.T) {
	candidates := make([]HunterCandidate, 22)
	for i := range candidates {
		candidates[i] = HunterCandidate{CandidateID: fmt.Sprintf("H-%03d", i+1)}
	}

	tests := []struct {
		name        string
		candidates  []HunterCandidate
		maxPerBatch int
		wantSizes   []int
		wantIDs     [][]string
	}{
		{
			name:        "nil candidates",
			maxPerBatch: defaultMaxCandidatesPerBatch,
			wantSizes:   nil,
			wantIDs:     nil,
		},
		{
			name:        "invalid max",
			candidates:  candidates[:1],
			maxPerBatch: 0,
			wantSizes:   nil,
			wantIDs:     nil,
		},
		{
			name:        "single full batch",
			candidates:  candidates[:5],
			maxPerBatch: defaultMaxCandidatesPerBatch,
			wantSizes:   []int{5},
			wantIDs:     [][]string{{"H-001", "H-002", "H-003", "H-004", "H-005"}},
		},
		{
			name:        "remainder is distributed",
			candidates:  candidates[:11],
			maxPerBatch: defaultMaxCandidatesPerBatch,
			wantSizes:   []int{4, 4, 3},
			wantIDs: [][]string{
				{"H-001", "H-002", "H-003", "H-004"},
				{"H-005", "H-006", "H-007", "H-008"},
				{"H-009", "H-010", "H-011"},
			},
		},
		{
			name:        "equal batches",
			candidates:  candidates[:12],
			maxPerBatch: defaultMaxCandidatesPerBatch,
			wantSizes:   []int{4, 4, 4},
			wantIDs: [][]string{
				{"H-001", "H-002", "H-003", "H-004"},
				{"H-005", "H-006", "H-007", "H-008"},
				{"H-009", "H-010", "H-011", "H-012"},
			},
		},
		{
			name:        "larger remainder is capped",
			candidates:  candidates,
			maxPerBatch: defaultMaxCandidatesPerBatch,
			wantSizes:   []int{5, 5, 4, 4, 4},
			wantIDs: [][]string{
				{"H-001", "H-002", "H-003", "H-004", "H-005"},
				{"H-006", "H-007", "H-008", "H-009", "H-010"},
				{"H-011", "H-012", "H-013", "H-014"},
				{"H-015", "H-016", "H-017", "H-018"},
				{"H-019", "H-020", "H-021", "H-022"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			batches := splitCandidatesBalanced(test.candidates, test.maxPerBatch)
			if len(batches) != len(test.wantSizes) {
				t.Fatalf("got %d batches, want %d", len(batches), len(test.wantSizes))
			}
			for i, batch := range batches {
				if len(batch) != test.wantSizes[i] {
					t.Fatalf("batch %d size = %d, want %d", i+1, len(batch), test.wantSizes[i])
				}
				ids := make([]string, 0, len(batch))
				for _, candidate := range batch {
					ids = append(ids, candidate.CandidateID)
				}
				if !reflect.DeepEqual(test.wantIDs[i], ids) {
					t.Fatalf("batch %d IDs mismatch\nwant: %v\ngot:  %v", i+1, test.wantIDs[i], ids)
				}
			}
		})
	}
}

func TestSplitCandidatesBalancedPreservesCoverage(t *testing.T) {
	candidates := make([]HunterCandidate, 123)
	for i := range candidates {
		candidates[i] = HunterCandidate{CandidateID: fmt.Sprintf("H-%03d", i+1)}
	}

	batches := splitCandidatesBalanced(candidates, defaultMaxCandidatesPerBatch)
	expected := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		expected = append(expected, candidate.CandidateID)
	}

	actual := make([]string, 0, len(candidates))
	for _, batch := range batches {
		if len(batch) > defaultMaxCandidatesPerBatch {
			t.Fatalf("batch size %d exceeds maximum %d", len(batch), defaultMaxCandidatesPerBatch)
		}
		for _, candidate := range batch {
			actual = append(actual, candidate.CandidateID)
		}
	}

	if !slices.Equal(expected, actual) {
		t.Fatalf("flattened candidate IDs changed\nexpected: %v\nactual: %v", expected, actual)
	}
}
