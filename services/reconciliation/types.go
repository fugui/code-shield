package reconciliation

import (
	"code-shield/models"
	"fmt"
	"strings"
	"time"
)

// 对账关系常量 (Relation)
const (
	RelationSame          = "SAME"               // 相同缺陷 (高置信)
	RelationSameMultiView = "SAME_MULTI_VIEW"    // 同锚点多视角上报
	RelationProbable      = "PROBABLE"           // 疑似相同 (需人工确认)
	RelationSplitFrom     = "SPLIT_FROM"         // 一拆多
	RelationMergedInto    = "MERGED_INTO"        // 多并一
	RelationTemplate      = "TEMPLATE"           // 模板族聚簇 (跨文件同模式，不合并实体)
	RelationVanished      = "VANISHED"           // 消失 (A 侧未复现)
	RelationNew           = "NEW"                // 本次新增
	RelationResolvedByChg = "RESOLVED_BY_CHANGE" // 变更焦点模式下顺带修复
)

// 缺陷生命周期状态 (LifecycleStatus)
const (
	LifecycleActive           = "ACTIVE"            // 存活活动状态
	LifecycleCoverageGap      = "COVERAGE_GAP"      // 覆盖缺口 (未复现但代码未动)
	LifecycleDormantArchived  = "DORMANT_ARCHIVED"  // 退火休眠归档
	LifecycleObsoleteDeleted  = "OBSOLETE_DELETED"  // 物理文件已删除归档
	LifecycleExemptedArchived = "EXEMPTED_ARCHIVED" // 人工豁免归档
	LifecycleVerifiedPending  = "VERIFIED_PENDING"  // 预关闭观察期
	LifecycleResolved         = "RESOLVED"          // 已确认修复
)

// 缺陷差异增量状态 (DiffStatus)
const (
	DiffStatusNew         = "NEW"
	DiffStatusExisted     = "EXISTED"
	DiffStatusExistedGap  = "EXISTED(未复现)"
	DiffStatusReopened    = "REOPENED"
	DiffStatusResurrected = "RESURRECTED"
	DiffStatusResolved    = "RESOLVED"
	DiffStatusDormant     = "DORMANT"
	DiffStatusObsolete    = "OBSOLETE"
)

// 归档原因常量 (ArchiveReason)
const (
	ArchiveReasonObsoleteDeleted = "OBSOLETE_DELETED" // 物理文件删除
	ArchiveReasonDormantAnnealed = "DORMANT_ANNEALED" // 连续未复现退火休眠
	ArchiveReasonExemptedManual  = "EXEMPTED_MANUAL"  // 人工豁免
)

// LineRange 行号区间
type LineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// ProofOfFixData 修复证据链
type ProofOfFixData struct {
	CommitHash string `json:"commit_hash,omitempty"`
	HunkRange  string `json:"hunk_range,omitempty"`
	Evidence   string `json:"evidence,omitempty"`
}

// SynthesisMeta 台账元数据
type SynthesisMeta struct {
	ReportID             uint      `json:"report_id"`
	BaseReportID         uint      `json:"base_report_id,omitempty"`
	RepoID               uint      `json:"repo_id"`
	TaskTypeID           uint      `json:"task_type_id"`
	TaskName             string    `json:"task_name,omitempty"`
	GovernanceMode       string    `json:"governance_mode,omitempty"` // full_ledger / change_focus
	CommitHash           string    `json:"commit_hash,omitempty"`
	RepoUnchanged        bool      `json:"repo_unchanged"`
	ActiveCount          int       `json:"active_count"`
	ArchivedCount        int       `json:"archived_count"`
	ChangedFiles         []string  `json:"changed_files,omitempty"`
	NewIntroducedCount   int       `json:"new_introduced_count,omitempty"`
	ResolvedHistoryCount int       `json:"resolved_history_count,omitempty"`
	ReconciledAt         time.Time `json:"reconciled_at"`
}

// SynthesisItem 台账活动工作集条目 (items[])
type SynthesisItem struct {
	Fingerprint             string                 `json:"fingerprint"`
	ItemUID                 string                 `json:"item_uid"`
	DiffStatus              string                 `json:"diff_status"`
	LifecycleStatus         string                 `json:"lifecycle_status"`
	FilePath                string                 `json:"file_path"`
	LineNumber              string                 `json:"line_number"`
	RoundsSeen              []uint                 `json:"rounds_seen"`
	FirstSeenReport         uint                   `json:"first_seen_report"`
	LastSeenReport          uint                   `json:"last_seen_report"`
	ConsecutiveMissedRounds int                    `json:"consecutive_missed_rounds"`
	MissedCount             int                    `json:"missed_count,omitempty"`
	CoverageGap             bool                   `json:"coverage_gap"`
	TemplateFamilyID        string                 `json:"template_family_id,omitempty"`
	ReconRelation           string                 `json:"recon_relation,omitempty"`
	ReconBaseUID            string                 `json:"recon_base_uid,omitempty"`
	SeverityRange           string                 `json:"severity_range,omitempty"`
	SeverityTriage          bool                   `json:"severity_triage,omitempty"`
	ProofOfFix              *ProofOfFixData        `json:"proof_of_fix,omitempty"`
	Note                    string                 `json:"note,omitempty"`
	Payload                 models.AnalysisFinding `json:"payload"`
}

// ArchivedItem 台账冷归档条目 (archived_items[])
type ArchivedItem struct {
	Fingerprint             string                 `json:"fingerprint"`
	ItemUID                 string                 `json:"item_uid"`
	DiffStatus              string                 `json:"diff_status"`
	LifecycleStatus         string                 `json:"lifecycle_status"`
	FilePath                string                 `json:"file_path"`
	LineNumber              string                 `json:"line_number"`
	RoundsSeen              []uint                 `json:"rounds_seen"`
	LastSeenReport          uint                   `json:"last_seen_report"`
	ConsecutiveMissedRounds int                    `json:"consecutive_missed_rounds"`
	ArchiveReason           string                 `json:"archive_reason"`
	ArchivedAt              time.Time              `json:"archived_at"`
	Note                    string                 `json:"note,omitempty"`
	Payload                 models.AnalysisFinding `json:"payload"`
}

// SynthesisLedger 完整问题台账 SSOT 结构
type SynthesisLedger struct {
	Meta          SynthesisMeta   `json:"meta"`
	Items         []SynthesisItem `json:"items"`
	ArchivedItems []ArchivedItem  `json:"archived_items"`
}

// ReconSummary 对账差异汇总
type ReconSummary struct {
	BaseCount            int `json:"base_count"`
	CurrentCount         int `json:"current_count"`
	Existed              int `json:"existed"`
	New                  int `json:"new"`
	VanishedCoverageGap  int `json:"vanished_coverage_gap"`
	VanishedFixCandidate int `json:"vanished_fix_candidate"`
	MultiViewMerged      int `json:"multi_view_merged"`
	TemplateFamily       int `json:"template_family"`
	ActiveWorkingCount   int `json:"active_working_count"`
	DormantArchivedCount int `json:"dormant_archived_count"`
	ObsoleteDeletedCount int `json:"obsolete_deleted_count"`
}

// ReconDiffLink 对账差异明细链接 (recon-*.json 用)
type ReconDiffLink struct {
	BaseFP          string   `json:"base_fp,omitempty"`
	CurrentFP       string   `json:"current_fp,omitempty"`
	BaseItemUID     string   `json:"base_item_uid,omitempty"`
	CurrentItemUID  string   `json:"current_item_uid,omitempty"`
	CurrentItemUIDs []string `json:"current_item_uids,omitempty"` // 用于 1:N 多视角
	Relation        string   `json:"relation"`
	MatchedTier     int      `json:"matched_tier"`
	Confidence      float64  `json:"confidence"`
	File            string   `json:"file"`
	Reason          string   `json:"reason"`
	SeverityRange   string   `json:"severity_range,omitempty"`
	SeverityTriage  bool     `json:"severity_triage,omitempty"`
}

// ReconDiffPayload recon-{B}-vs-{A}.json 完整结构
type ReconDiffPayload struct {
	BaseReportID    uint            `json:"base_report_id"`
	CurrentReportID uint            `json:"current_report_id"`
	RepoUnchanged   bool            `json:"repo_unchanged"`
	Summary         ReconSummary    `json:"summary"`
	Links           []ReconDiffLink `json:"links"`
}

// ReconcileRequest 对账执行请求
type ReconcileRequest struct {
	RepoID            uint
	TaskTypeID        uint
	TaskName          string
	CurrentReportID   uint
	BaseReportID      uint
	CurrentFindings   []models.AnalysisFinding
	BaseSynthesisJSON []byte
	RepoRoot          string
	BaseCommit        string
	HeadCommit        string
	RepoUnchanged     bool
	GovernanceMode    string // full_ledger / change_focus
	ChangedFiles      []string
	HunkRanges        map[string][]LineRange
	ScannedFiles      []string
}

// ReconcileResult 对账执行返回结果
type ReconcileResult struct {
	Ledger           SynthesisLedger
	DiffPayload      ReconDiffPayload
	Reconciliation   models.ScanReconciliation
	Links            []models.ReconciliationLink
	ResolvedByChange []models.AnalysisFinding
}

// GenerateItemUID 确定性短哈希业务别名生成算法
// 规范：F{reportID}-{fingerprint[:8]}，碰撞时追加 -{seq}
func GenerateItemUID(reportID uint, fingerprint string, seq int) string {
	shortFP := fingerprint
	if len(shortFP) > 8 {
		shortFP = shortFP[:8]
	}
	if shortFP == "" {
		shortFP = "unknown"
	}
	if seq > 0 {
		return fmt.Sprintf("F%d-%s-%d", reportID, shortFP, seq)
	}
	return fmt.Sprintf("F%d-%s", reportID, shortFP)
}

// NormalizeSeverityRange 计算两轮跨轮严重度极值区间与冲突判定
func NormalizeSeverityRange(sev1, sev2 string) (rangeJSON string, isConflict bool) {
	s1 := strings.TrimSpace(sev1)
	s2 := strings.TrimSpace(sev2)
	if s1 == "" && s2 == "" {
		return "", false
	}
	if s1 == "" {
		return fmt.Sprintf("[\"%s\"]", s2), false
	}
	if s2 == "" || s1 == s2 {
		return fmt.Sprintf("[\"%s\"]", s1), false
	}

	w1 := severityWeight(s1)
	w2 := severityWeight(s2)

	// 若权重差距 >= 2 (如建议 w=1 vs 严重 w=3 或 致命 w=4)，视为严重冲突，触发人工 triage
	diff := w1 - w2
	if diff < 0 {
		diff = -diff
	}
	isConflict = (diff >= 2)

	if w1 <= w2 {
		return fmt.Sprintf("[\"%s\",\"%s\"]", s1, s2), isConflict
	}
	return fmt.Sprintf("[\"%s\",\"%s\"]", s2, s1), isConflict
}

func severityWeight(s string) int {
	norm := strings.ToLower(s)
	switch {
	case strings.Contains(norm, "致命") || strings.Contains(norm, "fatal") || strings.Contains(norm, "blocker"):
		return 4
	case strings.Contains(norm, "严重") || strings.Contains(norm, "critical") || strings.Contains(norm, "high") || strings.Contains(norm, "error"):
		return 3
	case strings.Contains(norm, "一般") || strings.Contains(norm, "major") || strings.Contains(norm, "medium") || strings.Contains(norm, "warning"):
		return 2
	case strings.Contains(norm, "建议") || strings.Contains(norm, "suggestion") || strings.Contains(norm, "low") || strings.Contains(norm, "提示") || strings.Contains(norm, "hint") || strings.Contains(norm, "info"):
		return 1
	default:
		return 1
	}
}
