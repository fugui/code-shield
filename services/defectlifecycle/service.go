package defectlifecycle

import (
	"errors"
	"fmt"
	"time"

	"code-shield/models"
	"code-shield/services/coverage"

	"gorm.io/gorm"
)

func PersistScanFacts(input ScanInput) (*ScanFactsResult, error) {
	if input.DB == nil {
		input.DB = models.DB
	}
	if input.DB == nil {
		return nil, errors.New("database is not initialized")
	}
	if input.Report.ID == 0 || input.Repo.ID == 0 || input.TaskType.ID == 0 {
		return nil, errors.New("scan facts require report, repository and task type")
	}
	if input.AlgorithmVersion == "" {
		input.AlgorithmVersion = AlgorithmVersion
	}

	findings := make([]models.AnalysisFinding, len(input.Findings))
	copy(findings, input.Findings)
	groups := BuildObservationGroups(input.RepoRoot, input.Report.ID, findings)
	findings = make([]models.AnalysisFinding, 0, len(input.Findings))
	for _, group := range groups {
		for _, item := range group.Findings {
			item.ID = 0
			item.TaskReportID = input.Report.ID
			item.TaskTypeID = input.TaskType.ID
			item.RepoID = input.Repo.ID
			item.AnchorConfidence = group.Identity.Confidence
			item.ObservationGroupUID = group.UID
			if item.CreatedAt.IsZero() {
				item.CreatedAt = time.Now()
			}
			findings = append(findings, item)
		}
	}

	coverageFact := input.Coverage
	if coverageFact == nil {
		coverageFact = &coverage.Coverage{ManifestMissing: true}
	}
	summary := coverageFact.Summary()
	entries := mergeChunkExecutionScopeEntries(input, buildScopeEntries(input))
	state := coverageState(summary, input.TaskType.GovernanceMode)
	hash := scopeHash(input, entries)
	committedAt := time.Now()
	var ledgerObservations []models.DefectObservation
	var ledgerEvents []models.DefectEvent
	var newDefectIDs []uint

	err := input.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("task_report_id = ?", input.Report.ID).Delete(&models.AnalysisFinding{}).Error; err != nil {
			return fmt.Errorf("replace raw findings: %w", err)
		}
		if err := tx.Where("report_id = ?", input.Report.ID).Delete(&models.ScanScopeEntry{}).Error; err != nil {
			return fmt.Errorf("replace scope entries: %w", err)
		}
		if len(findings) > 0 {
			if err := tx.CreateInBatches(findings, 100).Error; err != nil {
				return fmt.Errorf("persist raw findings: %w", err)
			}
		}
		if err := persistScope(tx, entries); err != nil {
			return fmt.Errorf("persist scope entries: %w", err)
		}

		for index := range groups {
			groups[index].Identity.RepoID = input.Repo.ID
			groups[index].Identity.TaskTypeID = input.TaskType.ID
		}
		if err := prepareLedgerCommit(tx, LedgerInput{
			Report: input.Report, Repo: input.Repo, TaskType: input.TaskType,
		}); err != nil {
			return err
		}
		var defects []models.Defect
		var aliases []models.DefectAlias
		if err := tx.Where("repo_id = ? AND task_type_id = ?", input.Repo.ID, input.TaskType.ID).Order("id").Find(&defects).Error; err != nil {
			return fmt.Errorf("load defects for matching: %w", err)
		}
		if err := tx.Where("repo_id = ? AND task_type_id = ?", input.Repo.ID, input.TaskType.ID).Order("id").Find(&aliases).Error; err != nil {
			return fmt.Errorf("load aliases for matching: %w", err)
		}
		ledgerGroups := ledgerObservationGroups(groups)
		matching := RunMatching(MatchingInput{
			Observations: ledgerGroups, Defects: defects, Aliases: aliases,
			Arbitrator: input.Arbitrator, RenameTargets: input.RenameTargets,
			Budget: RuntimeMatchBudget(), Thresholds: RuntimeMatchThresholds(),
		})
		matchedDefectIDs := assignedDefectIDs(matching.Observations)
		unmatchedDefects := make([]models.Defect, 0, len(defects))
		for _, defect := range defects {
			if !matchedDefectIDs[defect.ID] {
				unmatchedDefects = append(unmatchedDefects, defect)
			}
		}
		passDecisions := matchPassObservations(groups, unmatchedDefects, aliases, input)
		decisions := append(append([]ObservationDecision(nil), matching.Observations...), passDecisions...)
		ledger, ledgerErr := commitLedger(tx, LedgerInput{
			Report: input.Report, Repo: input.Repo, TaskType: input.TaskType,
			Observations: groups, Decisions: decisions, Scope: entries,
			Coverage: coverageFact, CoverageState: state, AlgorithmVersion: input.AlgorithmVersion,
			RenameTargets: input.RenameTargets,
			Lifecycle:     RuntimeLifecyclePolicy(),
			CommittedAt:   committedAt,
		})
		if ledgerErr != nil {
			return ledgerErr
		}
		ledgerObservations = ledger.Observations
		ledgerEvents = ledger.Events
		newDefectIDs = ledger.NewDefectIDs
		updates := map[string]interface{}{
			"coverage_complete":       summary.CoverageComplete,
			"coverage_degraded":       summary.CoverageDegraded,
			"coverage_not_applicable": summary.CoverageNotApplicable,
			"coverage_state":          state,
			"scope_hash":              hash,
			"worktree_clean":          coverageFact.WorktreeClean,
			"algorithm_version":       input.AlgorithmVersion,
		}
		if _, err := models.UpdateActiveTaskReport(tx, input.Report.ID, updates); err != nil {
			return fmt.Errorf("persist scan facts metadata: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &ScanFactsResult{
		Findings:      findings,
		Scope:         entries,
		ScopeHash:     hash,
		CoverageState: state,
		WorktreeClean: coverageFact.WorktreeClean,
		CommittedAt:   committedAt,
		Observations:  ledgerObservations,
		Events:        ledgerEvents,
		NewDefectIDs:  newDefectIDs,
		LedgerReady:   true,
	}, nil
}

func RunScanReconciliation(input ScanInput) (*ScanFactsResult, error) {
	return PersistScanFacts(input)
}

func matchPassObservations(groups []ObservationGroup, defects []models.Defect, aliases []models.DefectAlias, input ScanInput) []ObservationDecision {
	passGroups := passObservationGroups(groups)
	if len(passGroups) == 0 || len(defects) == 0 {
		return nil
	}

	matching := RunMatching(MatchingInput{
		Observations: passGroups, Defects: defects, Aliases: aliases,
		Arbitrator: input.Arbitrator, RenameTargets: input.RenameTargets,
		Budget: RuntimeMatchBudget(), Thresholds: RuntimeMatchThresholds(),
	})

	decisions := make([]ObservationDecision, 0, len(matching.Observations))
	for _, decision := range matching.Observations {
		if decision.DefectID != nil {
			decisions = append(decisions, decision)
		}
	}
	return decisions
}
