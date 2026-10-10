package changereview

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"code-shield/models"
	"code-shield/services/coverage"
	"code-shield/services/engines/assessment"
	"gorm.io/datatypes"
)

const (
	ProfileName      = "changereview"
	ArtifactSchemaV2 = assessment.AssessmentsArtifactSchemaV2
	DomainVerdictKey = "change_verdict"

	DomainRegression      = "REGRESSION"
	DomainExposedExisting = "EXPOSED_EXISTING"
	DomainConditional     = "CONDITIONAL"
)

type Profile struct{}

func (Profile) Descriptor() assessment.Descriptor {
	return assessment.Descriptor{
		Name:           ProfileName,
		DisplayName:    "变更回归检视",
		UnitKind:       coverage.PlanUnitChangeHunk,
		ArtifactSchema: ArtifactSchemaV2,
	}
}

func (Profile) Contract() assessment.OutputContract {
	return assessment.OutputContract{
		SchemaID:          ArtifactSchemaV2,
		TopLevel:          "assessments",
		RequiredFields:    []string{"schema", "assessments"},
		ForbiddenTopLevel: []string{"findings", "candidates", "final_verdicts"},
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
		if unit.Kind != coverage.PlanUnitChangeHunk {
			return result, fmt.Errorf("plan unit %q has kind %q, want %q", unit.ID, unit.Kind, coverage.PlanUnitChangeHunk)
		}
	}
	assessed := make(map[string]int, len(plan.Units))
	for _, item := range artifact.Assessments {
		unitID, err := plan.PromptRefs.UnitIDForRef(item.UnitRef)
		if err != nil {
			result.Unknown = append(result.Unknown, item.UnitRef)
			continue
		}
		unit := unitByID(plan.Units, unitID)
		if err := validateAssessment(&item, plan.AllowedCategories, unit); err != nil {
			result.Invalid = append(result.Invalid, assessment.FailedUnit{
				UnitRef:     item.UnitRef,
				UnitID:      unitID,
				DisplayName: displayNameForUnit(plan.Units, unitID),
				Errors:      []assessment.AssessmentIssue{assessment.NewAssessmentIssue(assessment.ErrorOutcomeInvalid, "outcome", "%v", err)},
			})
			continue
		}
		assessed[unitID]++
		if assessed[unitID] > 1 {
			result.Duplicate = append(result.Duplicate, unitID)
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
		switch item.Outcome {
		case assessment.OutcomePass, assessment.OutcomeNotTarget:
			continue
		}
		unit, exists := units[item.PrimaryUnitID]
		if !exists {
			continue
		}
		rawArtifact, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("encode change assessment %q: %w", item.PrimaryUnitID, err)
		}
		finding := baseFinding(ctx, item, unit, rawArtifact)
		displayName := displayNameForUnit(bundle.Units, item.PrimaryUnitID)
		switch item.Outcome {
		case assessment.OutcomeDefect:
			issue := worstIssue(item.Issues)
			finding.Title = fmt.Sprintf("%s：%s", issue.Category, displayName)
			finding.Severity = issue.Severity
			finding.Category = issue.Category
			finding.Detail = item.Summary
			finding.Suggestion = issue.Suggestion
			finding.CodeSnippet = assessment.ExtractIssueSnippet(ctx, unit, issue.Code)
		default:
			finding.Title = fmt.Sprintf("变更需人工评估：%s", displayName)
			finding.Severity = "建议"
			finding.Detail = item.Summary
			finding.CodeSnippet = assessment.ExtractIssueSnippet(ctx, unit, "")
		}
		findings = append(findings, finding)
	}
	return findings, nil
}

func (Profile) OutcomeForStatus(status string) (assessment.AssessmentOutcome, bool) {
	switch strings.ToUpper(status) {
	case "SAFE", string(assessment.OutcomePass):
		return assessment.OutcomePass, true
	case "REGRESSION", "EXPOSED_EXISTING", string(assessment.OutcomeDefect):
		return assessment.OutcomeDefect, true
	case "NOT_CHANGE_RELATED", string(assessment.OutcomeNotTarget):
		return assessment.OutcomeNotTarget, true
	case "CONDITIONAL", "NEEDS_HUMAN", string(assessment.OutcomeNeedsHuman):
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

func unitByID(units []coverage.PlanUnit, unitID string) coverage.PlanUnit {
	for _, unit := range units {
		if unit.ID == unitID {
			return unit
		}
	}
	return coverage.PlanUnit{}
}

func displayNameForUnit(units []coverage.PlanUnit, unitID string) string {
	if unit := unitByID(units, unitID); unit.DisplayName != "" {
		return unit.DisplayName
	}
	if unit := unitByID(units, unitID); unit.Path != "" {
		if unit.StartLine <= 0 {
			return unit.Path
		}
		if unit.EndLine > unit.StartLine {
			return fmt.Sprintf("%s:%d-%d", unit.Path, unit.StartLine, unit.EndLine)
		}
		return fmt.Sprintf("%s:%d", unit.Path, unit.StartLine)
	}
	return unitID
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
	finding := models.AnalysisFinding{
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
	if verdict, ok := domainString(&item, DomainVerdictKey); ok {
		finding.JudgeVerdict = verdict
	}
	return finding
}

func validateAssessment(item *assessment.UnitAssessment, allowedCategories []string, unit coverage.PlanUnit) error {
	if err := item.Outcome.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(item.Summary) == "" {
		return fmt.Errorf("summary is required")
	}
	if unit.Path != "" {
		if err := validateRelativeSourcePath(unit.Path); err != nil {
			return err
		}
	}
	verdict, hasVerdict := domainString(item, DomainVerdictKey)
	if item.Outcome == assessment.OutcomeDefect {
		if len(item.Issues) == 0 {
			return fmt.Errorf("defect assessment requires at least one issue")
		}
		if !hasVerdict {
			return fmt.Errorf("defect assessment requires domain.%s", DomainVerdictKey)
		}
		switch verdict {
		case DomainRegression:
			commit, ok := domainString(item, "introduced_by_commit")
			if !ok || strings.TrimSpace(commit) == "" {
				return fmt.Errorf("%s requires domain.introduced_by_commit", DomainRegression)
			}
		case DomainExposedExisting, DomainConditional:
		default:
			return fmt.Errorf("domain.%s %q is not allowed", DomainVerdictKey, verdict)
		}
	} else if hasVerdict {
		switch verdict {
		case DomainRegression, DomainExposedExisting, DomainConditional:
		default:
			return fmt.Errorf("domain.%s %q is not allowed", DomainVerdictKey, verdict)
		}
	}
	switch item.Outcome {
	case assessment.OutcomeNotTarget:
		if strings.TrimSpace(item.Reason) == "" {
			return fmt.Errorf("not_target assessment requires reason")
		}
	case assessment.OutcomePass:
		if len(item.Issues) > 0 {
			return fmt.Errorf("%s assessment cannot contain issues", item.Outcome)
		}
	}
	return validateIssues(item.Issues, allowedCategories)
}

func domainString(item *assessment.UnitAssessment, key string) (string, bool) {
	raw, exists := item.Domain[key]
	if !exists {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return strings.TrimSpace(value), strings.TrimSpace(value) != ""
}

func validateRelativeSourcePath(rawPath string) error {
	cleanPath := strings.TrimSpace(rawPath)
	if cleanPath == "" {
		return fmt.Errorf("file_path is empty")
	}
	if filepath.IsAbs(cleanPath) || strings.Contains(cleanPath, "\\") {
		return fmt.Errorf("file_path must be a POSIX relative path, got %q", cleanPath)
	}
	cleaned := filepath.Clean(cleanPath)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("file_path escapes the repository root: %q", cleanPath)
	}
	return nil
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

func validateIssues(issues []assessment.AssessmentIssue, allowedCategories []string) error {
	for i := range issues {
		issue := &issues[i]
		if issue.Category == "" {
			return fmt.Errorf("issues[%d].category is empty", i)
		}
		if len(allowedCategories) > 0 && !contains(allowedCategories, issue.Category) {
			return fmt.Errorf("issues[%d].category %q is not allowed", i, issue.Category)
		}
		if issue.Severity == "" {
			issue.Severity = "一般"
		}
		if issue.Detail == "" {
			return fmt.Errorf("issues[%d].detail is empty", i)
		}
	}
	return nil
}
