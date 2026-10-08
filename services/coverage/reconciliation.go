package coverage

import (
	"encoding/json"
	"sort"
)

type PlanUnitKind string

const (
	PlanUnitFile              PlanUnitKind = "file"
	PlanUnitEntity            PlanUnitKind = "entity"
	PlanUnitKeywordOccurrence PlanUnitKind = "keyword_occurrence"
	PlanUnitChangeHunk        PlanUnitKind = "change_hunk"
)

type PlanUnit struct {
	ID          string          `json:"id"`
	Kind        PlanUnitKind    `json:"kind"`
	Path        string          `json:"path,omitempty"`
	DisplayName string          `json:"display_name,omitempty"`
	StartLine   int             `json:"start_line,omitempty"`
	EndLine     int             `json:"end_line,omitempty"`
	Evidence    json.RawMessage `json:"evidence,omitempty"`
}

type AssessmentRecord struct {
	PrimaryUnitID string `json:"primary_unit_id"`
	Status        string `json:"status"`
}

type PlanReconciliation struct {
	PlannedUnits   int      `json:"planned_units"`
	MatchedUnits   int      `json:"matched_units"`
	MissingUnits   []string `json:"missing_units,omitempty"`
	UnmatchedUnits []string `json:"unmatched_units,omitempty"`
}

func ReconcilePlanUnits(units []PlanUnit, assessments []AssessmentRecord) PlanReconciliation {
	assessmentIDs := make(map[string]int, len(assessments))
	plannedIDs := make(map[string]bool, len(units))
	for _, unit := range units {
		if unit.ID == "" {
			continue
		}
		plannedIDs[unit.ID] = true
	}

	for _, assessment := range assessments {
		if assessment.PrimaryUnitID == "" || !plannedIDs[assessment.PrimaryUnitID] {
			continue
		}
		assessmentIDs[assessment.PrimaryUnitID]++
	}

	result := PlanReconciliation{PlannedUnits: len(plannedIDs)}
	for _, unit := range units {
		if unit.ID == "" || !plannedIDs[unit.ID] {
			continue
		}
		if assessmentIDs[unit.ID] > 0 {
			result.MatchedUnits++
		} else {
			result.MissingUnits = append(result.MissingUnits, unit.ID)
		}
	}

	for _, assessment := range assessments {
		if assessment.PrimaryUnitID == "" {
			result.UnmatchedUnits = append(result.UnmatchedUnits, "")
			continue
		}
		if !plannedIDs[assessment.PrimaryUnitID] {
			result.UnmatchedUnits = append(result.UnmatchedUnits, assessment.PrimaryUnitID)
			continue
		}
		if count := assessmentIDs[assessment.PrimaryUnitID]; count > 1 {
			result.UnmatchedUnits = append(result.UnmatchedUnits, assessment.PrimaryUnitID)
			delete(assessmentIDs, assessment.PrimaryUnitID)
		}
	}

	sort.Strings(result.MissingUnits)
	sort.Strings(result.UnmatchedUnits)
	return result
}
