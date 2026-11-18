package defectlifecycle

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"code-shield/models"

	"gorm.io/gorm"
)

const (
	CompareBucketCommonPersisted    = "COMMON_PERSISTED"
	CompareBucketOnlyACodeChanged   = "ONLY_A_CODE_CHANGED"
	CompareBucketOnlyACodeUnchanged = "ONLY_A_CODE_UNCHANGED"
	CompareBucketOnlyANotCovered    = "ONLY_A_NOT_COVERED"
	CompareBucketOnlyBNew           = "ONLY_B_NEW"

	CompareDataSourceLedger           = "defect_observations"
	CompareDataSourceEntityAssessment = "entity_assessment_slots"
)

var (
	ErrCompareInvalidArgs      = errors.New("invalid report comparison parameters")
	ErrCompareReportNotFound   = errors.New("comparison report not found")
	ErrCompareRepoMismatch     = errors.New("cannot compare reports from different repositories")
	ErrCompareTaskTypeMismatch = errors.New("cannot compare reports from different task types")
)

// CompareReportMeta 承载基线或目标报告的基本上下文元信息。
type CompareReportMeta struct {
	ID               uint   `json:"id"`
	RepoID           uint   `json:"repo_id"`
	RepoName         string `json:"repo_name,omitempty"`
	TaskTypeID       uint   `json:"task_type_id"`
	TaskTypeName     string `json:"task_type_name,omitempty"`
	TaskDisplayName  string `json:"task_display_name,omitempty"`
	Branch           string `json:"branch,omitempty"`
	TargetScope      string `json:"target_scope,omitempty"`
	HeadCommit       string `json:"head_commit,omitempty"`
	CoverageState    string `json:"coverage_state"`
	CoverageComplete bool   `json:"coverage_complete"`
	GovernanceMode   string `json:"governance_mode"`
	CreatedAt        string `json:"created_at"`
}

// ReportCompareSummary 汇总五类互斥对比桶的计数。
type ReportCompareSummary struct {
	CommonPersisted    int `json:"common_persisted"`
	OnlyACodeChanged   int `json:"only_a_code_changed"`
	OnlyACodeUnchanged int `json:"only_a_code_unchanged"`
	OnlyANotCovered    int `json:"only_a_not_covered"`
	OnlyBNew           int `json:"only_b_new"`
	TotalA             int `json:"total_a"`
	TotalB             int `json:"total_b"`
}

// ReportCompareItem 描述单条缺陷或评估项在双报告比对中的归因明细。
type ReportCompareItem struct {
	Bucket         string `json:"bucket"`
	DefectID       *uint  `json:"defect_id,omitempty"`
	SlotKey        string `json:"slot_key,omitempty"`
	NormPath       string `json:"norm_path"`
	BaseNormPath   string `json:"base_norm_path,omitempty"`
	LineStart      *int   `json:"line_start,omitempty"`
	LineEnd        *int   `json:"line_end,omitempty"`
	BaseLineStart  *int   `json:"base_line_start,omitempty"`
	BaseLineEnd    *int   `json:"base_line_end,omitempty"`
	Severity       string `json:"severity"`
	Category       string `json:"category"`
	Title          string `json:"title"`
	ScopeSymbol    string `json:"scope_symbol,omitempty"`
	TargetEntity   string `json:"target_entity,omitempty"`
	DefectStatus   string `json:"defect_status,omitempty"`
	VerdictA       string `json:"verdict_a,omitempty"`
	VerdictB       string `json:"verdict_b,omitempty"`
	BaseBlobHash   string `json:"base_blob_hash,omitempty"`
	TargetBlobHash string `json:"target_blob_hash,omitempty"`
	ScopeOutcomeB  string `json:"scope_outcome_b,omitempty"`
}

// ReportCompareResult 为 SSOT 双报告对比 API 的统一返回载荷。
type ReportCompareResult struct {
	BaseReport         CompareReportMeta    `json:"base_report"`
	TargetReport       CompareReportMeta    `json:"target_report"`
	DataSource         string               `json:"data_source"`
	Summary            ReportCompareSummary `json:"summary"`
	CommonPersisted    []ReportCompareItem  `json:"common_persisted"`
	OnlyACodeChanged   []ReportCompareItem  `json:"only_a_code_changed"`
	OnlyACodeUnchanged []ReportCompareItem  `json:"only_a_code_unchanged"`
	OnlyANotCovered    []ReportCompareItem  `json:"only_a_not_covered"`
	OnlyBNew           []ReportCompareItem  `json:"only_b_new"`
}

// CompareReports 基于 SSOT 账本（defect_observations + merged_into_id 解析 + 全量覆盖清单）对比两份报告。
// 对于 entity_assessment 专项报告，自动降级为基于物理槽位的无账本比对。
func CompareReports(db *gorm.DB, baseReportID, targetReportID uint) (*ReportCompareResult, error) {
	if db == nil {
		db = models.DB
	}
	if db == nil {
		return nil, errors.New("database is not initialized")
	}
	if baseReportID == 0 || targetReportID == 0 {
		return nil, fmt.Errorf("%w: base_report_id and target_report_id must be positive", ErrCompareInvalidArgs)
	}

	var baseReport, targetReport models.TaskReport
	if err := db.Preload("Repo").Preload("TaskType").First(&baseReport, baseReportID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: base report %d", ErrCompareReportNotFound, baseReportID)
		}
		return nil, fmt.Errorf("load base report %d: %w", baseReportID, err)
	}
	if err := db.Preload("Repo").Preload("TaskType").First(&targetReport, targetReportID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: target report %d", ErrCompareReportNotFound, targetReportID)
		}
		return nil, fmt.Errorf("load target report %d: %w", targetReportID, err)
	}
	if baseReport.RepoID != targetReport.RepoID {
		return nil, fmt.Errorf("%w: %d vs %d", ErrCompareRepoMismatch, baseReport.RepoID, targetReport.RepoID)
	}
	if baseReport.TaskTypeID != targetReport.TaskTypeID {
		return nil, fmt.Errorf("%w: %d vs %d", ErrCompareTaskTypeMismatch, baseReport.TaskTypeID, targetReport.TaskTypeID)
	}

	result := &ReportCompareResult{
		BaseReport:         toCompareReportMeta(baseReport),
		TargetReport:       toCompareReportMeta(targetReport),
		CommonPersisted:    make([]ReportCompareItem, 0),
		OnlyACodeChanged:   make([]ReportCompareItem, 0),
		OnlyACodeUnchanged: make([]ReportCompareItem, 0),
		OnlyANotCovered:    make([]ReportCompareItem, 0),
		OnlyBNew:           make([]ReportCompareItem, 0),
	}

	scopeA := loadReportFullScopeMap(db, baseReport)
	scopeB := loadReportFullScopeMap(db, targetReport)

	isEntityMode := models.ResolveGovernanceMode(baseReport.TaskType.GovernanceMode) == models.GovernanceModeEntityAssessment ||
		models.ResolveGovernanceMode(targetReport.TaskType.GovernanceMode) == models.GovernanceModeEntityAssessment
	if isEntityMode {
		result.DataSource = CompareDataSourceEntityAssessment
		if err := compareByEntitySlots(db, baseReport, targetReport, scopeA, scopeB, result); err != nil {
			return nil, err
		}
		finalizeCompareSummary(result)
		return result, nil
	}

	result.DataSource = CompareDataSourceLedger
	if err := compareByLedgerObservations(db, baseReport, targetReport, scopeA, scopeB, result); err != nil {
		return nil, err
	}
	finalizeCompareSummary(result)
	return result, nil
}

func toCompareReportMeta(r models.TaskReport) CompareReportMeta {
	branch := r.Branch
	if branch == "" {
		branch = r.Repo.Branch
	}
	return CompareReportMeta{
		ID:               r.ID,
		RepoID:           r.RepoID,
		RepoName:         r.Repo.Name,
		TaskTypeID:       r.TaskTypeID,
		TaskTypeName:     r.TaskType.Name,
		TaskDisplayName:  r.TaskType.DisplayName,
		Branch:           branch,
		TargetScope:      r.TargetScope,
		HeadCommit:       r.HeadCommit,
		CoverageState:    r.CoverageState,
		CoverageComplete: r.CoverageComplete,
		GovernanceMode:   models.ResolveGovernanceMode(r.TaskType.GovernanceMode),
		CreatedAt:        r.CreatedAt.Format(time.RFC3339),
	}
}

func loadReportFullScopeMap(db *gorm.DB, report models.TaskReport) map[string]models.ScanScopeEntry {
	entries := make(map[string]models.ScanScopeEntry)
	if manifestPath := report.GetScopeManifestPath(); manifestPath != "" {
		if gzData, err := os.ReadFile(manifestPath); err == nil {
			if manifestEntries, decErr := decompressScopeManifest(gzData); decErr == nil {
				for _, entry := range manifestEntries {
					norm := NormalizePath("", entry.NormPath)
					if norm != "" {
						entry.NormPath = norm
						entries[norm] = entry
					}
				}
			}
		}
	}
	var dbEntries []models.ScanScopeEntry
	if err := db.Where("report_id = ?", report.ID).Find(&dbEntries).Error; err == nil {
		for _, entry := range dbEntries {
			norm := NormalizePath("", entry.NormPath)
			if norm != "" {
				entry.NormPath = norm
				entries[norm] = entry
			}
		}
	}
	return entries
}

type compareLedgerEntry struct {
	key  string
	item ReportCompareItem
}

func loadCanonicalReportObservations(db *gorm.DB, report models.TaskReport) (map[string]compareLedgerEntry, map[uint]models.Defect, error) {
	var observations []models.DefectObservation
	if err := db.Where("report_id = ?", report.ID).Order("id").Find(&observations).Error; err != nil {
		return nil, nil, fmt.Errorf("load observations for report %d: %w", report.ID, err)
	}

	var findings []models.AnalysisFinding
	if err := db.Select(
		"id", "title", "file_path", "line_number", "scope_symbol", "target_symbol",
		"observation_group_uid", "severity", "category",
	).Where("task_report_id = ?", report.ID).Order("id").Find(&findings).Error; err != nil {
		return nil, nil, fmt.Errorf("load findings for report %d: %w", report.ID, err)
	}
	findingByGroup := make(map[string]models.AnalysisFinding, len(findings))
	for _, f := range findings {
		if f.ObservationGroupUID == "" {
			continue
		}
		if _, exists := findingByGroup[f.ObservationGroupUID]; !exists {
			findingByGroup[f.ObservationGroupUID] = f
		}
	}

	seedDefectIDs := make([]uint, 0, len(observations))
	for _, obs := range observations {
		if obs.DefectID != nil && *obs.DefectID > 0 {
			seedDefectIDs = append(seedDefectIDs, *obs.DefectID)
		}
	}
	defectMap, err := loadResolvedDefectChain(db, seedDefectIDs)
	if err != nil {
		return nil, nil, err
	}

	result := make(map[string]compareLedgerEntry, len(observations))
	for _, obs := range observations {
		if obs.Verdict == VerdictCleared {
			continue
		}
		var key string
		var canonicalID *uint
		var resolvedDefect models.Defect

		if obs.DefectID != nil && *obs.DefectID > 0 {
			cid := canonicalDefectID(defectMap, *obs.DefectID)
			canonicalID = &cid
			resolvedDefect = defectMap[cid]
			key = fmt.Sprintf("defect:%d", cid)
		} else {
			key = "uid:" + obs.ObservationGroupUID
		}

		if existing, exists := result[key]; exists {
			// 同报告合并后如遇多个观测同归一个主缺陷，保留更明确的复现状态与较早元信息
			if existing.item.VerdictA == "" && obs.Verdict != "" {
				existing.item.VerdictA = obs.Verdict
				existing.item.VerdictB = obs.Verdict
				result[key] = existing
			}
			continue
		}

		f := findingByGroup[obs.ObservationGroupUID]
		normPath := NormalizePath("", resolvedDefect.NormPath)
		if normPath == "" {
			normPath = NormalizePath("", f.FilePath)
		}
		title := resolvedDefect.Title
		if f.Title != "" {
			title = f.Title
		}
		severity := resolvedDefect.Severity
		if f.Severity != "" {
			severity = f.Severity
		}
		category := resolvedDefect.Category
		if category == "" {
			category = resolvedDefect.DefectClassMajor
		}
		if f.Category != "" {
			category = f.Category
		}
		scopeSymbol := resolvedDefect.ScopeSymbol
		if f.ScopeSymbol != "" {
			scopeSymbol = NormalizeScopeSymbol(f.ScopeSymbol)
		}
		lineStart, lineEnd := resolvedDefect.LineStart, resolvedDefect.LineEnd
		if f.LineNumber != "" {
			ls, le := ParseLineNumberRange(f.LineNumber)
			if ls > 0 {
				lineStart = &ls
				lineEnd = &le
			}
		}

		result[key] = compareLedgerEntry{
			key: key,
			item: ReportCompareItem{
				DefectID:       canonicalID,
				SlotKey:        resolvedDefect.SlotKey,
				NormPath:       normPath,
				LineStart:      lineStart,
				LineEnd:        lineEnd,
				Severity:       severity,
				Category:       category,
				Title:          title,
				ScopeSymbol:    scopeSymbol,
				TargetEntity:   resolvedDefect.TargetEntity,
				DefectStatus:   resolvedDefect.Status,
				BaseBlobHash:   "",
				VerdictA:       obs.Verdict,
				VerdictB:       obs.Verdict,
				TargetBlobHash: "",
			},
		}
	}
	return result, defectMap, nil
}

func loadResolvedDefectChain(db *gorm.DB, seedIDs []uint) (map[uint]models.Defect, error) {
	defectsByID := make(map[uint]models.Defect)
	pending := make([]uint, 0, len(seedIDs))
	seenQuery := make(map[uint]bool)
	for _, id := range seedIDs {
		if id > 0 && !seenQuery[id] {
			seenQuery[id] = true
			pending = append(pending, id)
		}
	}
	for depth := 0; depth < 8 && len(pending) > 0; depth++ {
		var batch []models.Defect
		if err := db.Where("id IN ?", pending).Find(&batch).Error; err != nil {
			return nil, fmt.Errorf("load defects for compare: %w", err)
		}
		nextPending := make([]uint, 0)
		for _, d := range batch {
			defectsByID[d.ID] = d
			if d.Status == DefectStatusMerged && d.MergedIntoID != nil && *d.MergedIntoID > 0 {
				targetID := *d.MergedIntoID
				if !seenQuery[targetID] {
					seenQuery[targetID] = true
					nextPending = append(nextPending, targetID)
				}
			}
		}
		pending = nextPending
	}
	return defectsByID, nil
}

func canonicalDefectID(defectMap map[uint]models.Defect, id uint) uint {
	visited := make(map[uint]bool, 4)
	curr := id
	for depth := 0; depth < 8; depth++ {
		if visited[curr] {
			break
		}
		visited[curr] = true
		d, ok := defectMap[curr]
		if !ok || d.Status != DefectStatusMerged || d.MergedIntoID == nil || *d.MergedIntoID == 0 {
			break
		}
		curr = *d.MergedIntoID
	}
	return curr
}

func compareByLedgerObservations(
	db *gorm.DB,
	baseReport, targetReport models.TaskReport,
	scopeA, scopeB map[string]models.ScanScopeEntry,
	result *ReportCompareResult,
) error {
	entriesA, _, err := loadCanonicalReportObservations(db, baseReport)
	if err != nil {
		return err
	}
	entriesB, _, err := loadCanonicalReportObservations(db, targetReport)
	if err != nil {
		return err
	}

	for key, entryA := range entriesA {
		if entryB, ok := entriesB[key]; ok {
			merged := entryB.item
			merged.Bucket = CompareBucketCommonPersisted
			merged.BaseNormPath = entryA.item.NormPath
			merged.BaseLineStart = entryA.item.LineStart
			merged.BaseLineEnd = entryA.item.LineEnd
			merged.VerdictA = entryA.item.VerdictA
			merged.VerdictB = entryB.item.VerdictB
			populateCompareScopeFields(&merged, entryA.item.NormPath, scopeA, scopeB)
			result.CommonPersisted = append(result.CommonPersisted, merged)
			continue
		}

		item := entryA.item
		item.VerdictB = ""
		classifyOnlyInABucket(&item, scopeA, scopeB)
		appendItemToCompareResult(result, item)
	}

	for key, entryB := range entriesB {
		if _, ok := entriesA[key]; ok {
			continue
		}
		item := entryB.item
		item.VerdictA = ""
		item.Bucket = CompareBucketOnlyBNew
		populateCompareScopeFields(&item, item.NormPath, scopeA, scopeB)
		result.OnlyBNew = append(result.OnlyBNew, item)
	}
	return nil
}

func compareByEntitySlots(
	db *gorm.DB,
	baseReport, targetReport models.TaskReport,
	scopeA, scopeB map[string]models.ScanScopeEntry,
	result *ReportCompareResult,
) error {
	loadEntityEntries := func(report models.TaskReport) (map[string]ReportCompareItem, error) {
		var findings []models.AnalysisFinding
		if err := db.Where("task_report_id = ?", report.ID).Order("id").Find(&findings).Error; err != nil {
			return nil, fmt.Errorf("load findings for entity compare %d: %w", report.ID, err)
		}
		issues := make([]models.AnalysisFinding, 0, len(findings))
		for _, f := range findings {
			if !isPassAssessment(f) {
				issues = append(issues, f)
			}
		}
		identities := BuildIdentities("", report.RepoID, report.TaskTypeID, issues)
		items := make(map[string]ReportCompareItem, len(issues))
		slotSeq := make(map[string]int, len(issues))
		for i, f := range issues {
			id := identities[i]
			slotKey := id.SlotKey
			if slotKey == "" {
				slotKey = id.CoreSlotKey
			}
			if slotKey == "" {
				slotKey = fmt.Sprintf("finding:%d", f.ID)
			}
			// 对同报告内同槽位重复输出按本槽位出现序号做确定性去重后缀，避免受无关前序项增删扰动
			slotSeq[slotKey]++
			uniqueKey := slotKey
			if slotSeq[slotKey] > 1 {
				uniqueKey = fmt.Sprintf("%s#%d", slotKey, slotSeq[slotKey])
			}
			ls, le := ParseLineNumberRange(f.LineNumber)
			var lsPtr, lePtr *int
			if ls > 0 {
				lsPtr, lePtr = &ls, &le
			}
			items[uniqueKey] = ReportCompareItem{
				SlotKey:      slotKey,
				NormPath:     id.NormPath,
				LineStart:    lsPtr,
				LineEnd:      lePtr,
				Severity:     f.Severity,
				Category:     f.Category,
				Title:        f.Title,
				ScopeSymbol:  id.SymbolPath,
				TargetEntity: id.TargetEntity,
			}
		}
		return items, nil
	}

	itemsA, err := loadEntityEntries(baseReport)
	if err != nil {
		return err
	}
	itemsB, err := loadEntityEntries(targetReport)
	if err != nil {
		return err
	}

	for key, itemA := range itemsA {
		if itemB, ok := itemsB[key]; ok {
			itemB.Bucket = CompareBucketCommonPersisted
			itemB.BaseNormPath = itemA.NormPath
			itemB.BaseLineStart = itemA.LineStart
			itemB.BaseLineEnd = itemA.LineEnd
			populateCompareScopeFields(&itemB, itemA.NormPath, scopeA, scopeB)
			result.CommonPersisted = append(result.CommonPersisted, itemB)
			continue
		}
		classifyOnlyInABucket(&itemA, scopeA, scopeB)
		appendItemToCompareResult(result, itemA)
	}

	for key, itemB := range itemsB {
		if _, ok := itemsA[key]; ok {
			continue
		}
		itemB.Bucket = CompareBucketOnlyBNew
		populateCompareScopeFields(&itemB, itemB.NormPath, scopeA, scopeB)
		result.OnlyBNew = append(result.OnlyBNew, itemB)
	}
	return nil
}

func populateCompareScopeFields(item *ReportCompareItem, basePath string, scopeA, scopeB map[string]models.ScanScopeEntry) {
	if basePath == "" {
		basePath = item.NormPath
	}
	if sa, exists := scopeA[basePath]; exists && sa.BlobHash != "" {
		item.BaseBlobHash = sa.BlobHash
	} else if sa, exists := scopeA[item.NormPath]; exists && sa.BlobHash != "" {
		item.BaseBlobHash = sa.BlobHash
	}
	if sb, exists := scopeB[item.NormPath]; exists {
		item.TargetBlobHash = sb.BlobHash
		item.ScopeOutcomeB = sb.Outcome
	}
}

func classifyOnlyInABucket(item *ReportCompareItem, scopeA, scopeB map[string]models.ScanScopeEntry) {
	blobA := item.BaseBlobHash
	if sa, ok := scopeA[item.NormPath]; ok && sa.BlobHash != "" {
		blobA = sa.BlobHash
	}
	item.BaseBlobHash = blobA

	sb, hasB := scopeB[item.NormPath]
	if !hasB {
		item.Bucket = CompareBucketOnlyANotCovered
		return
	}
	item.TargetBlobHash = sb.BlobHash
	item.ScopeOutcomeB = sb.Outcome

	coveredInB := sb.Outcome == ScopeScanned || sb.Outcome == ScopeUnchangedSkipped
	if !coveredInB {
		item.Bucket = CompareBucketOnlyANotCovered
		return
	}

	codeChanged := (blobA != "" && sb.BlobHash != "" && blobA != sb.BlobHash) || sb.DiffTouched
	if codeChanged {
		item.Bucket = CompareBucketOnlyACodeChanged
		return
	}
	item.Bucket = CompareBucketOnlyACodeUnchanged
}

func appendItemToCompareResult(result *ReportCompareResult, item ReportCompareItem) {
	switch item.Bucket {
	case CompareBucketCommonPersisted:
		result.CommonPersisted = append(result.CommonPersisted, item)
	case CompareBucketOnlyACodeChanged:
		result.OnlyACodeChanged = append(result.OnlyACodeChanged, item)
	case CompareBucketOnlyACodeUnchanged:
		result.OnlyACodeUnchanged = append(result.OnlyACodeUnchanged, item)
	case CompareBucketOnlyANotCovered:
		result.OnlyANotCovered = append(result.OnlyANotCovered, item)
	case CompareBucketOnlyBNew:
		result.OnlyBNew = append(result.OnlyBNew, item)
	}
}

func sortCompareItems(items []ReportCompareItem) {
	sort.SliceStable(items, func(i, j int) bool {
		pi := strings.ToLower(items[i].NormPath)
		pj := strings.ToLower(items[j].NormPath)
		if pi != pj {
			return pi < pj
		}
		li, lj := derefInt(items[i].LineStart), derefInt(items[j].LineStart)
		if li != lj {
			return li < lj
		}
		var di, dj uint
		if items[i].DefectID != nil {
			di = *items[i].DefectID
		}
		if items[j].DefectID != nil {
			dj = *items[j].DefectID
		}
		if di != dj {
			return di < dj
		}
		return items[i].SlotKey < items[j].SlotKey
	})
}

func finalizeCompareSummary(result *ReportCompareResult) {
	sortCompareItems(result.CommonPersisted)
	sortCompareItems(result.OnlyACodeChanged)
	sortCompareItems(result.OnlyACodeUnchanged)
	sortCompareItems(result.OnlyANotCovered)
	sortCompareItems(result.OnlyBNew)

	onlyACount := len(result.OnlyACodeChanged) + len(result.OnlyACodeUnchanged) + len(result.OnlyANotCovered)
	result.Summary = ReportCompareSummary{
		CommonPersisted:    len(result.CommonPersisted),
		OnlyACodeChanged:   len(result.OnlyACodeChanged),
		OnlyACodeUnchanged: len(result.OnlyACodeUnchanged),
		OnlyANotCovered:    len(result.OnlyANotCovered),
		OnlyBNew:           len(result.OnlyBNew),
		TotalA:             len(result.CommonPersisted) + onlyACount,
		TotalB:             len(result.CommonPersisted) + len(result.OnlyBNew),
	}
}
