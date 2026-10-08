package debate

import (
	"code-shield/models"
	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
)

// HunterCandidate 猎手初筛出的候选缺陷
type HunterCandidate struct {
	CandidateID          string   `json:"candidate_id"`                     // 候选编号如 H-001
	FilePath             string   `json:"file_path"`                        // 问题文件相对路径
	LineRange            string   `json:"line_range"`                       // 行号范围如 "1915-1919"
	AdditionalLineRanges []string `json:"additional_line_ranges,omitempty"` // 非主锚点的关联位置
	TriggerLine          string   `json:"trigger_line"`                     // 核心引发风险的关键单一语句 (用于抗漂移强指纹计算)
	ScopeSymbol          string   `json:"scope_symbol"`                     // AST 作用域符号（函数名/类名签名）
	CodeSnippet          string   `json:"code_snippet"`                     // 原始代码片段
	CWECategory          string   `json:"cwe_category,omitempty"`           // 兼容历史 CWE 字段
	Category             string   `json:"category,omitempty"`               // 标准受控分类字段
	CategoryCode         string   `json:"category_code,omitempty"`          // 受控短码主标识
	ReviewRequired       bool     `json:"-"`
	Title                string   `json:"title,omitempty"`             // 简明缺陷标题
	TriggerCondition     string   `json:"trigger_condition,omitempty"` // 触发诱因与成因（中性健壮性术语）
	AttackHypothesis     string   `json:"attack_hypothesis,omitempty"` // 兼容历史字段：攻击路径假设与漏洞成因
	SuspectedTrigger     string   `json:"suspected_trigger,omitempty"` // 疑似触发条件
}

// NormalizeAlignFields 双向对齐触发机理与攻击假设字段，确保新老版本平滑兼容
func (h *HunterCandidate) NormalizeAlignFields() {
	if h.TriggerCondition == "" && h.AttackHypothesis != "" {
		h.TriggerCondition = h.AttackHypothesis
	}
	if h.AttackHypothesis == "" && h.TriggerCondition != "" {
		h.AttackHypothesis = h.TriggerCondition
	}
	if h.SuspectedTrigger == "" && h.TriggerCondition != "" {
		h.SuspectedTrigger = h.TriggerCondition
	}
}

// GetCategory 返回候选缺陷的有效分类
func (h *HunterCandidate) GetCategory() string {
	if h.Category != "" {
		return h.Category
	}
	return h.CWECategory
}

// HunterOutput 猎手阶段产物
type HunterOutput struct {
	Candidates      []HunterCandidate             `json:"candidates"`
	CategoryMetrics engines.CategoryHunterMetrics `json:"-"`
}

// DefenseArgument 辩护维度的单条论据
type DefenseArgument struct {
	Dimension string             `json:"dimension"` // Guards, MacroIsolation, StandardUB, Architecture
	Finding   string             `json:"finding"`   // 辩护事实与代码证据
	Evidence  []JudgeEvidenceRef `json:"evidence"`
}

// ChallengerDefenseCase 辩护人对单条候选缺陷的对抗意见
type ChallengerDefenseCase struct {
	CandidateID            string            `json:"candidate_id"`
	DefenseVerdict         string            `json:"defense_verdict"` // DEFENSE_SUCCESSFUL, DEFENSE_PARTIAL, CHALLENGE_FAILED
	DefenseArguments       []DefenseArgument `json:"defense_arguments"`
	MitigatingFactors      string            `json:"mitigating_factors"`
	CounterEvidenceSnippet string            `json:"counter_evidence_snippet"`
}

// ChallengerOutput 辩护阶段产物
type ChallengerOutput struct {
	DefenseCases []ChallengerDefenseCase `json:"defense_cases"`
}

// JudgeFinalVerdict 终审法官最终裁决
type JudgeFinalVerdict struct {
	CandidateID             string             `json:"candidate_id"`
	Verdict                 string             `json:"verdict"`              // CONFIRMED, REJECTED, CONDITIONAL
	SeverityPreliminary     string             `json:"severity_preliminary"` // preliminary severity
	Category                string             `json:"category"`
	CategoryCode            string             `json:"category_code,omitempty"`
	ClassificationRationale string             `json:"classification_rationale,omitempty"`
	ReviewRequired          bool               `json:"-"`
	FilePath                string             `json:"file_path"`
	LineRange               string             `json:"line_range"`
	LineNumber              string             `json:"line_number"`
	TriggerLine             string             `json:"trigger_line"` // 核心触发行
	ScopeSymbol             string             `json:"scope_symbol"` // 作用域符号
	Title                   string             `json:"title"`
	JudgementRationale      string             `json:"judgement_rationale"` // 仲裁裁决依据与证据法理
	CodeSnippet             string             `json:"code_snippet"`
	Suggestion              string             `json:"suggestion"` // 修复建议
	Evidence                []JudgeEvidenceRef `json:"evidence"`
	EvidenceDegraded        bool               `json:"evidence_degraded,omitempty"`
}

// JudgeOutput 法官阶段产物
type JudgeOutput struct {
	FinalVerdicts   []JudgeFinalVerdict          `json:"final_verdicts"`
	CategoryMetrics engines.CategoryJudgeMetrics `json:"-"`
}

// DebateTicket 异步流水线中流转的辩论工单
type DebateTicket struct {
	ReportID   uint
	ChunkIndex int
	Bundle     chunker.SemanticBundle
	HunterOut  *HunterOutput
	DoneChan   chan DebateTicketResult
}

// DebateTicketResult 工单执行结果
type DebateTicketResult struct {
	Findings   []models.AnalysisFinding
	DebateLogs []models.TaskDebateLog
	Error      error
}
