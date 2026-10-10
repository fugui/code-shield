package entityreview

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines/assessment"
	"gorm.io/datatypes"
)

const (
	ProfileName      = "entityreview"
	ArtifactSchemaV2 = assessment.AssessmentsArtifactSchemaV2
)

type Profile struct{}

func Registration() assessment.ProfileRegistration {
	profile := Profile{}
	return assessment.ProfileRegistration{
		Descriptor:  profile.Descriptor(),
		Planner:     BundlePlanner{},
		Contract:    profile,
		Prompt:      profile,
		Normalize:   profile,
		Validate:    profile,
		Reconcile:   profile,
		MapFindings: profile,
		Outcome:     profile,
	}
}

func (Profile) Descriptor() assessment.Descriptor {
	return assessment.Descriptor{
		Name:           ProfileName,
		DisplayName:    "测试实体评估",
		UnitKind:       coverage.PlanUnitEntity,
		ArtifactSchema: ArtifactSchemaV2,
	}
}

func (Profile) Contract() assessment.OutputContract {
	return assessment.OutputContract{
		SchemaID:          ArtifactSchemaV2,
		TopLevel:          "assessments",
		RequiredFields:    []string{"schema", "assessments"},
		ForbiddenTopLevel: []string{"findings", "candidates"},
		AllowedOutcomes: []string{
			string(assessment.OutcomePass),
			string(assessment.OutcomeDefect),
			string(assessment.OutcomeNotTarget),
			string(assessment.OutcomeNeedsHuman),
		},
	}
}

func (Profile) Validate(plan assessment.PlanView, artifact assessment.AssessmentArtifact) (assessment.AssessmentResult, error) {
	result := assessment.AssessmentResult{Artifact: artifact}
	if artifact.Schema != ArtifactSchemaV2 {
		return result, fmt.Errorf("%w: expected schema %q, got %q", assessment.ErrContractRepair, ArtifactSchemaV2, artifact.Schema)
	}
	if plan.PromptRefs == nil {
		return result, fmt.Errorf("%w: prompt refs are required", assessment.ErrProfileRegistry)
	}
	for _, unit := range plan.Units {
		if unit.Kind != coverage.PlanUnitEntity {
			return result, fmt.Errorf("plan unit %q has kind %q, want %q", unit.ID, unit.Kind, coverage.PlanUnitEntity)
		}
	}

	seen := make(map[string]bool, len(plan.Units))
	assessed := make(map[string]int, len(artifact.Assessments))
	for _, unit := range plan.Units {
		seen[unit.ID] = true
	}
	for _, item := range artifact.Assessments {
		unitID, err := plan.PromptRefs.UnitIDForRef(item.UnitRef)
		if err != nil {
			result.Unknown = append(result.Unknown, item.UnitRef)
			continue
		}
		assessed[unitID]++
		if assessed[unitID] > 1 {
			result.Duplicate = append(result.Duplicate, unitID)
			continue
		}
		if err := validateAssessment(&item, plan.AllowedCategories); err != nil {
			result.Invalid = append(result.Invalid, assessment.FailedUnit{
				UnitRef:     item.UnitRef,
				UnitID:      unitID,
				DisplayName: displayNameForUnit(plan.Units, unitID),
				Errors:      []assessment.AssessmentIssue{assessment.NewAssessmentIssue(assessment.ErrorOutcomeInvalid, "outcome", "%v", err)},
			})
			continue
		}
		result.Valid = append(result.Valid, item)
	}
	for _, unit := range plan.Units {
		if assessed[unit.ID] == 0 {
			result.Missing = append(result.Missing, unit.ID)
		}
	}
	return result, nil
}

func (Profile) Reconcile(plan assessment.PlanView, result assessment.AssessmentResult) assessment.UnitReconciliation {
	records := make([]coverage.AssessmentRecord, 0, len(result.Valid))
	for _, item := range result.Valid {
		records = append(records, coverage.AssessmentRecord{PrimaryUnitID: item.PrimaryUnitID, Status: string(item.Outcome)})
	}
	return assessment.UnitReconciliation{
		PlanReconciliation: coverage.ReconcilePlanUnits(plan.Units, records),
		DuplicateUnits:     result.Duplicate,
	}
}

func (Profile) MapFindings(ctx assessment.AssessmentContext, bundle assessment.Bundle, result assessment.AssessmentResult) ([]models.AnalysisFinding, error) {
	units := make(map[string]coverage.PlanUnit, len(bundle.Units))
	for _, unit := range bundle.Units {
		units[unit.ID] = unit
	}
	findings := make([]models.AnalysisFinding, 0, len(result.Valid))
	for _, item := range result.Valid {
		unit, exists := units[item.PrimaryUnitID]
		if !exists {
			continue
		}
		rawArtifact, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("encode entity assessment %q: %w", item.PrimaryUnitID, err)
		}
		finding := baseFinding(ctx, item, unit, rawArtifact)
		displayName := displayNameForUnit(bundle.Units, item.PrimaryUnitID)
		switch item.Outcome {
		case assessment.OutcomePass:
			finding.Title = fmt.Sprintf("测试用例合格：%s", displayName)
			finding.Detail = item.Summary
		case assessment.OutcomeDefect:
			issue := worstIssue(item.Issues)
			finding.Title = fmt.Sprintf("%s：%s", issue.Category, displayName)
			finding.Severity = issue.Severity
			finding.Category = issue.Category
			finding.Detail = issueSummary(item.Issues)
			finding.Suggestion = issue.Suggestion
			finding.CodeSnippet = assessment.ExtractIssueSnippet(ctx, unit, issue.Code)
		case assessment.OutcomeNotTarget:
			finding.Title = fmt.Sprintf("非目标测试实体：%s", displayName)
			finding.Detail = item.Reason
		default:
			finding.Title = fmt.Sprintf("测试用例需人工评估：%s", displayName)
			finding.Severity = "建议"
			finding.Detail = item.Reason
			finding.CodeSnippet = assessment.ExtractIssueSnippet(ctx, unit, "")
		}
		findings = append(findings, finding)
	}
	return findings, nil
}

func (Profile) OutcomeForStatus(status string) (assessment.AssessmentOutcome, bool) {
	switch strings.ToLower(status) {
	case "valid", string(assessment.OutcomePass):
		return assessment.OutcomePass, true
	case "invalid", "issue", string(assessment.OutcomeDefect):
		return assessment.OutcomeDefect, true
	case string(assessment.OutcomeNotTarget):
		return assessment.OutcomeNotTarget, true
	case string(assessment.OutcomeNeedsHuman):
		return assessment.OutcomeNeedsHuman, true
	default:
		return "", false
	}
}

func (Profile) OutcomeForArtifact(artifact assessment.AssessmentArtifact) (assessment.AssessmentOutcome, bool) {
	if len(artifact.Assessments) == 0 {
		return "", false
	}
	switch artifact.Assessments[0].Outcome {
	case assessment.OutcomePass, assessment.OutcomeDefect, assessment.OutcomeNotTarget, assessment.OutcomeNeedsHuman:
		return artifact.Assessments[0].Outcome, true
	default:
		return "", false
	}
}

func baseFinding(ctx assessment.AssessmentContext, item assessment.UnitAssessment, unit coverage.PlanUnit, rawArtifact []byte) models.AnalysisFinding {
	reportID, taskTypeID, repoID := uint(0), uint(0), uint(0)
	if ctx.EngineContext != nil {
		reportID, taskTypeID, repoID = ctx.EngineContext.ReportID, ctx.EngineContext.TaskTypeID, ctx.EngineContext.RepoID
	}
	category, severity := "无问题", "合格"
	if item.Outcome == assessment.OutcomeNeedsHuman {
		severity = "建议"
	}
	return models.AnalysisFinding{
		TaskReportID:       reportID,
		TaskTypeID:         taskTypeID,
		RepoID:             repoID,
		PrimaryUnitID:      item.PrimaryUnitID,
		AssessmentStatus:   string(item.Outcome),
		AssessmentOutcome:  string(item.Outcome),
		AssessmentArtifact: datatypes.JSON(rawArtifact),
		FilePath:           unit.Path,
		LineNumber:         lineNumber(unit),
		Category:           category,
		CategoryStatus:     "LEGACY",
		Severity:           severity,
		CreatedAt:          time.Now(),
	}
}

func validateAssessment(item *assessment.UnitAssessment, allowedCategories []string) error {
	if err := item.Outcome.Validate(); err != nil {
		return err
	}
	switch item.Outcome {
	case assessment.OutcomePass, assessment.OutcomeNotTarget, assessment.OutcomeNeedsHuman:
		if len(item.Issues) > 0 {
			return fmt.Errorf("%s assessment cannot contain issues", item.Outcome)
		}
	case assessment.OutcomeDefect:
		if len(item.Issues) == 0 {
			return fmt.Errorf("defect assessment requires at least one issue")
		}
	}
	for i := range item.Issues {
		issue := &item.Issues[i]
		if len(allowedCategories) > 0 && !contains(allowedCategories, issue.Category) {
			return fmt.Errorf("%w: category %q is not allowed", assessment.ErrArtifactInvalid, issue.Category)
		}
	}
	return nil
}

func displayNameForUnit(units []coverage.PlanUnit, unitID string) string {
	for _, unit := range units {
		if unit.ID == unitID && unit.DisplayName != "" {
			return unit.DisplayName
		}
	}
	for _, unit := range units {
		if unit.ID == unitID && unit.Path != "" {
			if unit.StartLine <= 0 {
				return unit.Path
			}
			return fmt.Sprintf("%s:%d", unit.Path, unit.StartLine)
		}
	}
	return unitID
}

func lineNumber(unit coverage.PlanUnit) string {
	if unit.StartLine <= 0 {
		return ""
	}
	if unit.EndLine > unit.StartLine {
		return fmt.Sprintf("%d-%d", unit.StartLine, unit.EndLine)
	}
	return fmt.Sprintf("%d", unit.StartLine)
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func worstIssue(issues []assessment.AssessmentIssue) assessment.AssessmentIssue {
	if len(issues) == 0 {
		return assessment.AssessmentIssue{}
	}
	ranks := map[string]int{"致命": 4, "严重": 3, "一般": 2, "建议": 1}
	worst := issues[0]
	for _, issue := range issues[1:] {
		if ranks[issue.Severity] > ranks[worst.Severity] {
			worst = issue
		}
	}
	return worst
}

func issueSummary(issues []assessment.AssessmentIssue) string {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, fmt.Sprintf("[%s/%s] %s 修复建议：%s", issue.Severity, issue.Category, issue.Detail, issue.Suggestion))
	}
	return strings.Join(parts, "\n")
}
