package defectlifecycle

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"code-shield/models"
	"code-shield/services/coverage"

	"gorm.io/gorm"
)

var (
	ErrDefectVersionConflict = errors.New("defect row_version conflict")
	ErrSupersededReport      = errors.New("report is superseded by a newer committed report")
)

func isVersionConflict(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrDefectVersionConflict) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "changed during ledger commit") ||
		strings.Contains(msg, "changed while withholding lifecycle advancement") ||
		strings.Contains(msg, "row_version")
}

func markReportSuperseded(db *gorm.DB, reportID uint, findings []models.AnalysisFinding, scope []models.ScanScopeEntry, hash, state string, worktreeClean bool) (*ScanFactsResult, error) {
	now := time.Now()
	updates := map[string]interface{}{
		"ledger_committed_at": &now,
		"scope_hash":          hash,
		"coverage_state":      state,
		"worktree_clean":      worktreeClean,
	}
	_ = db.Model(&models.TaskReport{}).Where("id = ?", reportID).Updates(updates).Error
	return &ScanFactsResult{
		Findings:      findings,
		Scope:         scope,
		ScopeHash:     hash,
		CoverageState: state,
		WorktreeClean: worktreeClean,
		CommittedAt:   now,
		LedgerReady:   false,
	}, nil
}

// PersistScanFacts 重构为三阶段解耦流水线：
// Phase 1: 只读快照加载（无写锁）
// Phase 2: 纯内存无锁计算（零 DB 交互）
// Phase 3: 极简短写事务（<15ms 提交）
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
	if input.Report.RepoID == 0 {
		input.Report.RepoID = input.Repo.ID
	}
	if input.Report.TaskTypeID == 0 {
		input.Report.TaskTypeID = input.TaskType.ID
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
	for index := range groups {
		groups[index].Identity.RepoID = input.Repo.ID
		groups[index].Identity.TaskTypeID = input.TaskType.ID
	}

	coverageFact := input.Coverage
	if coverageFact == nil {
		coverageFact = &coverage.Coverage{ManifestMissing: true}
	}
	summary := coverageFact.Summary()
	fullScopeEntries := mergeChunkExecutionScopeEntries(input, buildScopeEntries(input))
	state := coverageState(summary, input.TaskType.GovernanceMode)
	hash := scopeHash(input, fullScopeEntries)

	// 基于全量计划文件集构建内存覆盖判定器（对账专用，与稀疏落库解耦）
	evaluator := NewMemoryScopeEvaluator(fullScopeEntries)

	// 计算稀疏入库切片与 Manifest 压缩数据
	sparseEntries, manifestBytes := filterSparseScope(input, fullScopeEntries)

	// 同步预写 Manifest 到临时文件（事务前完成，不使用裸协程）
	tempManifestPath, err := writeTempManifest(&input.Report, manifestBytes)
	if err != nil {
		return nil, fmt.Errorf("write temp manifest: %w", err)
	}
	defer func() {
		if tempManifestPath != "" {
			if _, statErr := os.Stat(tempManifestPath); statErr == nil {
				_ = os.Remove(tempManifestPath)
			}
		}
	}()

	const maxRetries = 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		// ── Phase 1: 无锁快照读 ──
		var defects []models.Defect
		var aliases []models.DefectAlias
		if err := input.DB.Where("repo_id = ? AND task_type_id = ?", input.Repo.ID, input.TaskType.ID).Order("id").Find(&defects).Error; err != nil {
			return nil, fmt.Errorf("load defects for matching (attempt %d): %w", attempt, err)
		}
		if err := input.DB.Where("repo_id = ? AND task_type_id = ?", input.Repo.ID, input.TaskType.ID).Order("id").Find(&aliases).Error; err != nil {
			return nil, fmt.Errorf("load aliases for matching (attempt %d): %w", attempt, err)
		}

		// ── Phase 2: 纯内存匹配计算（零 DB 锁占用） ──
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

		// ── Phase 3: 极简短事务提交（<15ms） ──
		committedAt := time.Now()
		var ledgerObservations []models.DefectObservation
		var ledgerEvents []models.DefectEvent
		var newDefectIDs []uint

		txErr := input.DB.Transaction(func(tx *gorm.DB) error {
			// 1. 获取同仓短事务排他锁（毫秒级串行保护，防止并发交叉死锁）
			if tx.Dialector.Name() == "postgres" {
				lockKey := fmt.Sprintf("%d:%d", input.Repo.ID, input.TaskType.ID)
				if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
					return fmt.Errorf("acquire short ledger lock: %w", err)
				}
			}

			// 2. 乱序时间线校验：防旧报告覆盖新报告
			if err := validateLatestLedgerReport(tx, input.Report); err != nil {
				return err
			}

			// 3. 写入本轮 Findings 与稀疏 ScanScopeEntry
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
			if len(sparseEntries) > 0 {
				if err := persistScope(tx, sparseEntries); err != nil {
					return fmt.Errorf("persist scope entries: %w", err)
				}
			}

			// 4. 清理上一次本报告可能的未结观测并提交台账流转
			if err := prepareLedgerCommit(tx, LedgerInput{
				Report: input.Report, Repo: input.Repo, TaskType: input.TaskType,
			}); err != nil {
				return err
			}

			ledger, ledgerErr := commitLedger(tx, LedgerInput{
				Report: input.Report, Repo: input.Repo, TaskType: input.TaskType,
				Observations: groups, Decisions: decisions, Scope: sparseEntries,
				Evaluator: evaluator,
				Coverage:  coverageFact, CoverageState: state, AlgorithmVersion: input.AlgorithmVersion,
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

			// 5. 回写报告汇总指标
			updates := map[string]interface{}{
				"coverage_complete":       summary.CoverageComplete,
				"coverage_degraded":       summary.CoverageDegraded,
				"coverage_not_applicable": summary.CoverageNotApplicable,
				"coverage_state":          state,
				"scope_hash":              hash,
				"worktree_clean":          coverageFact.WorktreeClean,
				"algorithm_version":       input.AlgorithmVersion,
				"ledger_committed_at":     &committedAt,
			}
			if _, err := models.UpdateActiveTaskReport(tx, input.Report.ID, updates); err != nil {
				return fmt.Errorf("persist scan facts metadata: %w", err)
			}
			return nil
		})

		if txErr == nil {
			// 事务提交成功，原子 Rename 将 Manifest 临时文件转正为正式路径
			finalPath := manifestFinalPath(&input.Report)
			if finalPath != "" {
				if renameErr := os.Rename(tempManifestPath, finalPath); renameErr != nil {
					log.Printf("[Warn] Finalize manifest failed for report %d: %v (non-critical)", input.Report.ID, renameErr)
				}
			}
			return &ScanFactsResult{
				Findings:      findings,
				Scope:         sparseEntries,
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

		// 遇到滞后旧报告：跳过台账提交，降级返回
		if errors.Is(txErr, ErrSupersededReport) {
			log.Printf("[Ledger] Report %d is superseded by a newer completed scan. Skipping ledger commit.", input.Report.ID)
			return markReportSuperseded(input.DB, input.Report.ID, findings, sparseEntries, hash, state, coverageFact.WorktreeClean)
		}

		// 遇到乐观锁版本冲突：退避重试（重新加载快照）
		if isVersionConflict(txErr) {
			lastErr = txErr
			backoff := time.Duration(attempt*50) * time.Millisecond
			time.Sleep(backoff)
			continue
		}

		return nil, txErr
	}

	return nil, fmt.Errorf("commit scan facts conflict after %d retries: %w", maxRetries, lastErr)
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
