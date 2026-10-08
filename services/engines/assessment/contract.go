package assessment

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	AssessmentsArtifactSchemaV2 = "code-shield.unit-assessments.v2"
	RepairRequestSchemaV2       = "code-shield.unit-assessment-repair-request.v2"
	RepairResponseSchemaV2      = "code-shield.unit-assessment-repair-response.v2"
)

type AssessmentOutcome string

const (
	OutcomePass       AssessmentOutcome = "pass"
	OutcomeDefect     AssessmentOutcome = "defect"
	OutcomeNotTarget  AssessmentOutcome = "not_target"
	OutcomeNeedsHuman AssessmentOutcome = "needs_human"
)

func (outcome AssessmentOutcome) Validate() error {
	switch outcome {
	case OutcomePass, OutcomeDefect, OutcomeNotTarget, OutcomeNeedsHuman:
		return nil
	default:
		return fmt.Errorf("%w: invalid assessment outcome %q", ErrArtifactInvalid, outcome)
	}
}

type AssessmentIssue struct {
	Code       string          `json:"code,omitempty"`
	Field      string          `json:"field,omitempty"`
	Category   string          `json:"category"`
	Severity   string          `json:"severity"`
	Detail     string          `json:"detail"`
	Suggestion string          `json:"suggestion,omitempty"`
	Evidence   map[string]any  `json:"evidence,omitempty"`
	Raw        json.RawMessage `json:"-"`
}

type UnitAssessment struct {
	UnitRef       string                     `json:"unit_ref"`
	PrimaryUnitID string                     `json:"primary_unit_id,omitempty"`
	Outcome       AssessmentOutcome          `json:"outcome"`
	Summary       string                     `json:"summary,omitempty"`
	Reason        string                     `json:"reason,omitempty"`
	Evidence      map[string]any             `json:"evidence,omitempty"`
	Domain        map[string]json.RawMessage `json:"domain,omitempty"`
	Issues        []AssessmentIssue          `json:"issues,omitempty"`
}

type AssessmentArtifact struct {
	Schema      string           `json:"schema"`
	Assessments []UnitAssessment `json:"assessments"`
}

type PartialResult struct {
	Valid   []UnitAssessment `json:"valid"`
	Invalid []FailedUnit     `json:"invalid"`
	Missing []string         `json:"missing"`
	Unknown []string         `json:"unknown"`
}

type FailedUnit struct {
	UnitRef     string            `json:"unit_ref"`
	UnitID      string            `json:"unit_id,omitempty"`
	DisplayName string            `json:"display_name,omitempty"`
	Errors      []AssessmentIssue `json:"validation_errors"`
}

var (
	ErrArtifactInvalid = errors.New("assessment artifact invalid")
	ErrContractRepair  = errors.New("assessment artifact requires repair")
)

const (
	ErrorJSONInvalid             = "JSON_INVALID"
	ErrorTopLevelMissing         = "TOP_LEVEL_MISSING"
	ErrorContractSchemaMismatch  = "CONTRACT_SCHEMA_MISMATCH"
	ErrorUnitRefUnknown          = "UNIT_REF_UNKNOWN"
	ErrorUnitRefMissing          = "UNIT_REF_MISSING"
	ErrorUnitRefDuplicate        = "UNIT_REF_DUPLICATE"
	ErrorOutcomeInvalid          = "OUTCOME_INVALID"
	ErrorIssueCategoryNotAllowed = "ISSUE_CATEGORY_NOT_ALLOWED"
	ErrorIssueDetailMissing      = "ISSUE_DETAIL_MISSING"
	ErrorProfileFacetNotFound    = "PROFILE_FACET_NOT_FOUND"
	ErrorHistoryOutcomeUnknown   = "HISTORY_OUTCOME_UNKNOWN"
)

func NewAssessmentIssue(code, field, format string, arguments ...any) AssessmentIssue {
	return AssessmentIssue{Code: code, Field: field, Detail: fmt.Sprintf(format, arguments...)}
}

func (result PartialResult) Label() string {
	var parts []string
	parts = append(parts, fmt.Sprintf("valid=%d invalid=%d missing=%d unknown=%d",
		len(result.Valid), len(result.Invalid), len(result.Missing), len(result.Unknown)))
	return strings.Join(parts, " ")
}
