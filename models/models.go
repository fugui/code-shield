package models

import (
	commonModels "code-common/backend/models"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type User = commonModels.User
type Department = commonModels.Department
type SysAuditLog = commonModels.SysAuditLog
type AuditLevel = commonModels.AuditLevel

const (
	AuditLevelP0 = commonModels.AuditLevelP0
	AuditLevelP1 = commonModels.AuditLevelP1
	AuditLevelP2 = commonModels.AuditLevelP2
)

// ── GovernanceMode 枚举常量 ──
const (
	GovernanceModeEntityAssessment = "entity_assessment" // 全量实体评估模式
	GovernanceModeFullLedger       = "full_ledger"       // 全量基线台账模式
	GovernanceModeChangeFocus      = "change_focus"      // 变更增量焦点模式
)

// ResolveGovernanceMode returns the normalized governance mode.
func ResolveGovernanceMode(raw string) string {
	switch raw {
	case GovernanceModeChangeFocus:
		return GovernanceModeChangeFocus
	case GovernanceModeEntityAssessment:
		return GovernanceModeEntityAssessment
	case GovernanceModeFullLedger, "":
		return GovernanceModeFullLedger
	default:
		return GovernanceModeFullLedger
	}
}

type Repository struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	DepartmentID   uint           `json:"department_id"`
	Department     Department     `gorm:"foreignKey:DepartmentID" json:"department"`
	Name           string         `gorm:"uniqueIndex;not null" json:"name"`
	ProjectID      string         `gorm:"default:''" json:"project_id"`
	URL            string         `gorm:"not null" json:"url"`
	HTTPURL        string         `gorm:"default:''" json:"http_url"`
	OwnerID        uint           `json:"owner_id"`
	Owner          User           `gorm:"foreignKey:OwnerID" json:"owner"`
	Branch         string         `gorm:"default:master" json:"branch"`
	ServiceGroup   string         `gorm:"size:30" json:"service_group"`
	RelatedMembers datatypes.JSON `json:"related_members"` // Optional related members (receives CC emails)
	IsActive       bool           `gorm:"default:true" json:"is_active"`
	LastCommitHash string         `json:"last_commit_hash"`
	ReportCount    int64          `gorm:"-" json:"report_count"`
	CreatedAt      time.Time      `json:"created_at"`
}

// BeforeSave prevents blank repository URLs from entering the persistent queue.
// URL is nullable in historical schemas, so both NULL and whitespace-only values
// must be rejected before GORM issues the write.
func (r *Repository) BeforeSave(tx *gorm.DB) error {
	r.URL = strings.TrimSpace(r.URL)
	if r.URL == "" {
		return fmt.Errorf("repository %q has an empty URL", r.Name)
	}
	return nil
}

// RunParams 定义任务执行时的运行时动态过滤参数。
// ScheduleConfig 或 API 触发时可设置此结构覆盖默认扫描范围，nil 字段表示不覆盖。
// ── 任务领域族群 (DomainFamily) 枚举常量 ──
const (
	DomainFamilyMemoryCrash          = "memory_crash"
	DomainFamilyArchitectureGov      = "architecture_governance"
	DomainFamilyNumericalDeterminism = "numerical_determinism"
	DomainFamilyTestEngineering      = "test_engineering"
	DomainFamilySecurityInjection    = "security_injection"
	DomainFamilyComprehensive        = "comprehensive_evolution"
)

// DefenseDimension 描述任务特化或领域通用的抗辩证据维度
type DefenseDimension struct {
	Key         string `json:"key,omitempty"`       // 维度英文唯一标识，如 "Guards"
	Name        string `json:"name,omitempty"`      // 维度中文名称，如 "前置防御事实"
	Dimension   string `json:"dimension,omitempty"` // 兼容历史字段别名
	Description string `json:"description"`         // 详细阐述
}

// GetKey 返回有效维度英文标识
func (d *DefenseDimension) GetKey() string {
	if d.Key != "" {
		return d.Key
	}
	if d.Dimension != "" {
		return d.Dimension
	}
	return d.Name
}

// GetDisplayName 返回用于呈现的友好名称
func (d *DefenseDimension) GetDisplayName() string {
	if d.Name != "" {
		return d.Name
	}
	if d.Dimension != "" {
		return d.Dimension
	}
	return d.Key
}

// TargetSemantics describes the object evaluated by an assessment profile.
type TargetSemantics struct {
	Singular       string `json:"singular,omitempty"`
	Plural         string `json:"plural,omitempty"`
	NotTargetLabel string `json:"not_target_label,omitempty"`
}

// DisplaySemantics describes user-facing labels for assessment outcomes.
type DisplaySemantics struct {
	DomainLabel    string `json:"domain_label,omitempty"`
	TargetLabel    string `json:"target_label,omitempty"`
	NotTargetLabel string `json:"not_target_label,omitempty"`
}

// DomainFamilyInfo 描述领域族群的元信息与推荐模式
type DomainFamilyInfo struct {
	Key               string             `json:"key"`
	Name              string             `json:"name"`
	Description       string             `json:"description"`
	RecommendedMode   string             `json:"recommended_mode"`
	DefaultDimensions []DefenseDimension `json:"default_dimensions"`
}

// GetAllDomainFamilies 获取系统支持的所有领域族群元信息字典 (SSOT)
func GetAllDomainFamilies() []DomainFamilyInfo {
	return []DomainFamilyInfo{
		{
			Key:             DomainFamilyMemoryCrash,
			Name:            "内存与底层崩溃防御",
			Description:     "C/C++ 底层内存安全、空指针、野指针、资源释放生命周期等高危缺陷",
			RecommendedMode: "debate_full",
			DefaultDimensions: []DefenseDimension{
				{Key: "Guards", Name: "前置防御事实", Description: "前置判空、非零校验、下标上界断言或参数前置防御等事实"},
				{Key: "MacroIsolation", Name: "宏条件编译隔离", Description: "代码受非默认宏开关隔离，默认生产构建下不可达"},
				{Key: "AsyncSafe", Name: "异步与RAII回收", Description: "符合异步信号安全、异常捕获与 RAII 生命周期自动回收保障"},
			},
		},
		{
			Key:             DomainFamilyArchitectureGov,
			Name:            "架构与设计规范治理",
			Description:     "全局架构设计约束、并发模型、线程管控、组件依赖规范治理",
			RecommendedMode: "chunked_fast",
			DefaultDimensions: []DefenseDimension{
				{Key: "ScopeExemption", Name: "非生产作用域豁免", Description: "属于单测桩代码、本地离线排障脚本或辅助工具，无生产暴露"},
				{Key: "PoolManaged", Name: "底层池化托管", Description: "已接入系统级统一线程池或协程池生命周期托管"},
				{Key: "ConfigControlled", Name: "动态配置开关", Description: "具备动态配置开关管控，非硬编码无节制创建"},
			},
		},
		{
			Key:             DomainFamilyNumericalDeterminism,
			Name:            "数值计算与确定性",
			Description:     "浮点数精度比较、容器迭代遍历顺序确定性、跨平台一致性",
			RecommendedMode: "chunked_fast",
			DefaultDimensions: []DefenseDimension{
				{Key: "EpsilonTolerance", Name: "显式容差设计", Description: "业务逻辑本就依赖特定 Epsilon 容差或已在外层保证数值边界"},
				{Key: "NonDeterministicTolerant", Name: "无序不敏感", Description: "算法业务上对遍历顺序不敏感，集合元素具有可交换性"},
				{Key: "HardwareBound", Name: "特定平台加速契约", Description: "针对特定编译器或硬件体系结构的特定加速契约"},
			},
		},
		{
			Key:             DomainFamilyTestEngineering,
			Name:            "测试工程与有效性",
			Description:     "测试用例断言有效性、空测试、Mock 规范、脆弱测试质量审计",
			RecommendedMode: "chunked_fast",
			DefaultDimensions: []DefenseDimension{
				{Key: "HelperAssertion", Name: "辅助校验函数封装", Description: "断言逻辑封装在专用辅助/校验函数中，并非空断言或无断言"},
				{Key: "ExpectedNoThrow", Name: "无异常即成功契约", Description: "用例旨在验证操作不抛出异常或安全兜底，无需显式 ASSERT"},
				{Key: "StubContract", Name: "存根调用契约覆盖", Description: "Mock 或 Stub 的调用期望本身构成隐式逻辑检验"},
			},
		},
		{
			Key:             DomainFamilySecurityInjection,
			Name:            "应用安全与注入防御",
			Description:     "Web/API 安全、SQL/命令注入、XSS、敏感凭据泄漏与越权访问",
			RecommendedMode: "debate_full",
			DefaultDimensions: []DefenseDimension{
				{Key: "WAFSanitized", Name: "前置网关过滤", Description: "流量已通过统一 WAF 网关或前置框架参数类型安全绑定与白名单校验"},
				{Key: "ParametrizedQuery", Name: "原生预编译绑定", Description: "ORM 或底层驱动自动执行预编译参数绑定，非纯文本拼接"},
				{Key: "ConstantTrusted", Name: "内部可信常量源", Description: "输入源为内部枚举常量或配置文件，非不受信外部用户输入"},
			},
		},
		{
			Key:             DomainFamilyComprehensive,
			Name:            "综合演进与深度检视",
			Description:     "多维度综合深度架构审查、存量与增量代码变更演化安全防护",
			RecommendedMode: "debate_full",
			DefaultDimensions: []DefenseDimension{
				{Key: "RegressionGuarded", Name: "回归防护充分", Description: "变更已有充分的伴随测试或上下游防护，不构成实际回归隐患"},
				{Key: "ContextDefense", Name: "全局上下文防御", Description: "全局架构或外层已具备拦截校验机制，单点无需过度防御"},
				{Key: "HistoricalLegacy", Name: "历史兼容与协议约束", Description: "代码遵循特定历史硬件/网络协议契约约束，非架构缺陷"},
			},
		},
	}
}

type RunParams struct {
	TargetScope *string `json:"target_scope,omitempty"` // nil = 不覆盖，使用 TaskType 默认 ("all", "business", "test")
}

// TaskType 任务类型定义（管理员可配置）
type TaskType struct {
	ID                   uint           `gorm:"primaryKey" json:"id"`
	Name                 string         `gorm:"uniqueIndex;not null" json:"name"`     // 唯一标识: "code_review", "memory_leak"
	DisplayName          string         `gorm:"not null" json:"display_name"`         // 中文名: "代码检视"
	Description          string         `json:"description"`                          // 任务说明
	EngineMode           string         `gorm:"default:single" json:"engine_mode"`    // 执行引擎模式: single, chunked
	EngineConfig         datatypes.JSON `json:"engine_config"`                        // 引擎配置 {"max_files": 50, "depth": 2}
	ScanProfileHash      string         `gorm:"-" json:"scan_profile_hash,omitempty"` // Derived canonical profile hash; never persisted.
	AssessmentConfig     datatypes.JSON `json:"assessment_config"`
	AssessmentConfigHash string         `gorm:"size:72;not null;default:''" json:"assessment_config_hash"`
	TargetScope          string         `gorm:"default:'business'" json:"target_scope"` // 处理范围: all (全部), business (仅业务), test (仅测试)
	NotifyTemplate       string         `json:"notify_template"`                        // 邮件主题模板
	NotifyThreshold      int            `gorm:"default:0" json:"notify_threshold"`      // score >= 此值才通知
	NotifyCc             datatypes.JSON `json:"notify_cc"`                              // 通知抄送邮箱列表 ["a@x.com","b@x.com"]
	Timeout              int            `gorm:"default:30" json:"timeout"`              // AI 执行超时（分钟）

	// ── 智能体协同与异构调度扩展 (阶段二) ──
	DebateEnabled     bool           `gorm:"default:true" json:"debate_enabled"`                             // 是否启用三方对抗辩论流
	DomainFamily      string         `gorm:"size:50;default:'comprehensive_evolution'" json:"domain_family"` // 所属领域族群
	DefenseDimensions datatypes.JSON `gorm:"type:jsonb" json:"defense_dimensions"`                           // 专属自定义抗辩维度列表

	// ── 标准化领域与展示语义（Phase 5）──
	DomainLabel      string         `gorm:"size:100;not null;default:''" json:"domain_label"`
	TargetSemantics  datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"target_semantics"`
	DisplaySemantics datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"display_semantics"`

	// ── 专项分析元数据扩展 ──
	IsCampaign     bool           `gorm:"default:false;index" json:"is_campaign"`   // 是否启用为专项分析
	CampaignPath   string         `gorm:"size:100;default:''" json:"campaign_path"` // 路由别名 (空则默认同 Name)
	GovernanceMode string         `gorm:"size:50;default:'full_ledger'" json:"governance_mode"`
	CampaignIcon   string         `gorm:"type:text" json:"campaign_icon"` // SVG 图标路径或图标类名
	CampaignConfig datatypes.JSON `json:"campaign_config"`                // 高级配置，结构定义见 CampaignConfigSchema
	Categories     datatypes.JSON `gorm:"type:jsonb" json:"categories"`   // 受控标准分类白名单 (SSOT)
	Taxonomy       datatypes.JSON `gorm:"type:jsonb" json:"taxonomy"`

	TaxonomySchemaVersion int    `gorm:"not null;default:0" json:"taxonomy_schema_version"`
	TaxonomyHash          string `gorm:"size:72;not null;default:''" json:"taxonomy_hash"`

	IsActive          bool      `gorm:"default:true" json:"is_active"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	CurrentRevisionID *uint     `gorm:"index" json:"current_revision_id"`
}

type TaskTypeRevision struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	TaskTypeID uint   `gorm:"not null;uniqueIndex:uq_task_type_revision_aggregate,priority:1" json:"task_type_id"`
	Revision   int    `gorm:"not null" json:"revision"`
	EngineMode string `gorm:"size:40;not null;default:''" json:"engine_mode"`

	EngineConfig     datatypes.JSON `json:"engine_config"`
	EngineConfigHash string         `gorm:"size:72;not null;default:''" json:"engine_config_hash"`

	AssessmentConfig     datatypes.JSON `json:"assessment_config"`
	AssessmentConfigHash string         `gorm:"size:72;not null;default:''" json:"assessment_config_hash"`

	PromptContent     string `gorm:"type:text" json:"prompt_content"`
	PromptContentHash string `gorm:"size:72;not null;default:''" json:"prompt_content_hash"`

	Categories         datatypes.JSON `gorm:"type:jsonb" json:"categories"`
	CategorySchemaHash string         `gorm:"size:72;not null;default:''" json:"category_schema_hash"`

	Taxonomy              datatypes.JSON `gorm:"type:jsonb" json:"taxonomy"`
	TaxonomySchemaVersion int            `gorm:"not null;default:0" json:"taxonomy_schema_version"`
	TaxonomyHash          string         `gorm:"size:72;not null;default:''" json:"taxonomy_hash"`

	DomainFamily      string         `gorm:"size:50;not null;default:''" json:"domain_family"`
	DefenseDimensions datatypes.JSON `gorm:"type:jsonb" json:"defense_dimensions"`
	GovernanceMode    string         `gorm:"size:50;not null;default:''" json:"governance_mode"`

	DomainLabel      string         `gorm:"size:100;not null;default:''" json:"domain_label"`
	TargetSemantics  datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"target_semantics"`
	DisplaySemantics datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"display_semantics"`

	PostprocessContent string `gorm:"type:text" json:"postprocess_content"`
	PostprocessHash    string `gorm:"size:72;not null;default:''" json:"postprocess_hash"`

	AggregateHash string    `gorm:"size:72;not null;uniqueIndex:uq_task_type_revision_aggregate,priority:2" json:"aggregate_hash"`
	CreatedAt     time.Time `json:"created_at"`
}

const AssessmentConfigSchemaV1 = "code-shield.assessment-config.v1"

func HashAssessmentConfig(raw datatypes.JSON) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		sum := sha256.Sum256(raw)
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		sum := sha256.Sum256(raw)
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(normalized)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type AssessmentConfigEnvelope struct {
	Version int    `json:"version"`
	Profile string `json:"profile"`
	Schema  string `json:"schema"`
	Domain  string `json:"domain"`
}

func ParseAssessmentConfig(raw datatypes.JSON) (AssessmentConfigEnvelope, error) {
	envelope := AssessmentConfigEnvelope{}
	if len(raw) == 0 {
		return envelope, nil
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return AssessmentConfigEnvelope{}, fmt.Errorf("invalid assessment_config: %w", err)
	}
	if envelope.Version == 0 && envelope.Profile == "" && envelope.Schema == "" && envelope.Domain == "" {
		return envelope, nil
	}
	if envelope.Version != 1 {
		return AssessmentConfigEnvelope{}, fmt.Errorf("assessment_config.version must be 1")
	}
	if envelope.Profile == "" {
		return AssessmentConfigEnvelope{}, fmt.Errorf("assessment_config.profile is required")
	}
	if envelope.Schema != AssessmentConfigSchemaV1 {
		return AssessmentConfigEnvelope{}, fmt.Errorf("assessment_config.schema must be %q", AssessmentConfigSchemaV1)
	}
	if envelope.Domain == "" {
		return AssessmentConfigEnvelope{}, fmt.Errorf("assessment_config.domain is required")
	}
	return envelope, nil
}

type ExecutionContextSnapshot struct {
	EngineMode            string           `json:"engine_mode"`
	ScanProfile           json.RawMessage  `json:"scan_profile"`
	ScanProfileHash       string           `json:"scan_profile_hash"`
	AssessmentConfig      json.RawMessage  `json:"assessment_config"`
	AssessmentConfigHash  string           `json:"assessment_config_hash"`
	PromptContent         string           `json:"prompt_content"`
	PromptContentHash     string           `json:"prompt_content_hash"`
	Categories            []string         `json:"categories"`
	CategorySchemaHash    string           `json:"category_schema_hash"`
	Taxonomy              CategoryTaxonomy `json:"taxonomy,omitempty"`
	TaxonomySchemaVersion int              `json:"taxonomy_schema_version"`
	TaxonomyHash          string           `json:"taxonomy_hash"`
	DomainFamily          string           `json:"domain_family"`
	DomainLabel           string           `json:"domain_label"`
	DefenseDimensions     json.RawMessage  `json:"defense_dimensions"`
	TargetSemantics       json.RawMessage  `json:"target_semantics,omitempty"`
	DisplaySemantics      json.RawMessage  `json:"display_semantics,omitempty"`
	GovernanceMode        string           `json:"governance_mode"`
	PostprocessContent    string           `json:"postprocess_content,omitempty"`
	PostprocessHash       string           `json:"postprocess_hash,omitempty"`
	TaskTypeRevisionID    string           `json:"task_type_revision_id,omitempty"`
	EngineConfig          json.RawMessage  `json:"engine_config,omitempty"`
	EngineConfigHash      string           `json:"engine_config_hash,omitempty"`
}

func (snapshot ExecutionContextSnapshot) GetDefenseDimensions() []DefenseDimension {
	if len(snapshot.DefenseDimensions) == 0 {
		return nil
	}
	var dimensions []DefenseDimension
	if err := json.Unmarshal(snapshot.DefenseDimensions, &dimensions); err != nil {
		return nil
	}
	return dimensions
}

func (snapshot ExecutionContextSnapshot) GetTargetSemantics() TargetSemantics {
	var semantics TargetSemantics
	_ = json.Unmarshal(snapshot.TargetSemantics, &semantics)
	return semantics
}

func (snapshot ExecutionContextSnapshot) GetDisplaySemantics() DisplaySemantics {
	var semantics DisplaySemantics
	_ = json.Unmarshal(snapshot.DisplaySemantics, &semantics)
	return semantics
}

// GetDomainFamily 获取有效的领域族群，缺省安全回退
func (t *TaskType) GetDomainFamily() string {
	if t.DomainFamily != "" {
		return t.DomainFamily
	}
	return DomainFamilyComprehensive
}

func (t *TaskType) GetDomainLabel() string {
	if t.DomainLabel != "" {
		return t.DomainLabel
	}
	for _, family := range GetAllDomainFamilies() {
		if family.Key == t.GetDomainFamily() {
			return family.Name
		}
	}
	return "通用代码评估"
}

func (t *TaskType) GetTargetSemantics() TargetSemantics {
	var semantics TargetSemantics
	_ = json.Unmarshal(t.TargetSemantics, &semantics)
	return semantics
}

func (t *TaskType) GetDisplaySemantics() DisplaySemantics {
	var semantics DisplaySemantics
	_ = json.Unmarshal(t.DisplaySemantics, &semantics)
	if semantics.DomainLabel == "" {
		semantics.DomainLabel = t.GetDomainLabel()
	}
	return semantics
}

// GetDefenseDimensions 解析任务专有抗辩维度列表
func (t *TaskType) GetDefenseDimensions() []DefenseDimension {
	if len(t.DefenseDimensions) == 0 {
		return nil
	}
	var dims []DefenseDimension
	if err := json.Unmarshal(t.DefenseDimensions, &dims); err == nil {
		return dims
	}
	return nil
}

// GetAllowedCategories 返回该任务类型配置的标准受控分类白名单
func (t *TaskType) GetAllowedCategories() []string {
	if taxonomy := t.GetCategoryTaxonomy(); taxonomy != nil {
		labels := make([]string, 0, len(taxonomy.Categories))
		for _, category := range taxonomy.Categories {
			labels = append(labels, category.Label)
		}
		return labels
	}
	if len(t.Categories) == 0 {
		return nil
	}
	var cats []string
	if err := json.Unmarshal(t.Categories, &cats); err == nil {
		return cats
	}
	return nil
}

// GetCategoryTaxonomy parses the controlled category snapshot for this task
// type. Legacy string-array tasks intentionally return nil.
func (t *TaskType) GetCategoryTaxonomy() *CategoryTaxonomy {
	if len(t.Taxonomy) == 0 {
		return nil
	}
	var taxonomy CategoryTaxonomy
	if err := json.Unmarshal(t.Taxonomy, &taxonomy); err != nil {
		return nil
	}
	return &taxonomy
}

type CategoryAlias struct {
	Label string `json:"label"`
}

type CategoryDefinition struct {
	Code            string          `json:"code"`
	Label           string          `json:"label"`
	Definition      string          `json:"definition,omitempty"`
	DecisionRules   []string        `json:"decision_rules,omitempty"`
	PositiveHints   []string        `json:"positive_hints,omitempty"`
	NegativeHints   []string        `json:"negative_hints,omitempty"`
	DefaultSeverity string          `json:"default_severity,omitempty"`
	EscalateWhen    []string        `json:"escalate_when,omitempty"`
	DowngradeWhen   []string        `json:"downgrade_when,omitempty"`
	Priority        int             `json:"priority,omitempty"`
	Status          string          `json:"status"`
	Aliases         []CategoryAlias `json:"aliases,omitempty"`
}

type DeprecatedAlias struct {
	Label      string `json:"label"`
	TargetCode string `json:"target_code"`
	Reason     string `json:"reason,omitempty"`
}

type CategoryTaxonomy struct {
	SchemaVersion     int                  `json:"schema_version"`
	Hash              string               `json:"hash,omitempty"`
	Categories        []CategoryDefinition `json:"categories"`
	DeprecatedAliases []DeprecatedAlias    `json:"deprecated_aliases,omitempty"`
}

// CampaignConfigSchema 高级专项配置 Schema
type CampaignConfigSchema struct {
	Version           int               `json:"version"`              // Schema 版本号，当前为 1
	SeverityFilter    []string          `json:"severity_filter"`      // 需要展示的严重等级列表 (空=全部展示)
	NotifyOnNewDefect bool              `json:"notify_on_new_defect"` // 新缺陷入库时是否触发通知
	CustomLabels      map[string]string `json:"custom_labels"`        // 看板自定义标签 (如 {"metric": "合格率"})
}

// 模型写入校验 Hook
func (t *TaskType) BeforeCreate(tx *gorm.DB) error { return t.validate() }
func (t *TaskType) BeforeUpdate(tx *gorm.DB) error { return t.validate() }

func (t *TaskType) validate() error {
	t.AssessmentConfigHash = HashAssessmentConfig(t.AssessmentConfig)
	if _, err := ParseAssessmentConfig(t.AssessmentConfig); err != nil {
		return err
	}
	if len(t.Taxonomy) > 0 {
		taxonomy := t.GetCategoryTaxonomy()
		if taxonomy == nil {
			return fmt.Errorf("invalid taxonomy JSON")
		}
		if err := taxonomy.validate(); err != nil {
			return fmt.Errorf("invalid taxonomy: %w", err)
		}
		if t.TaxonomySchemaVersion != 0 && t.TaxonomySchemaVersion != taxonomy.SchemaVersion {
			return fmt.Errorf("taxonomy schema_version %d does not match taxonomy %d", t.TaxonomySchemaVersion, taxonomy.SchemaVersion)
		}
		t.TaxonomySchemaVersion = taxonomy.SchemaVersion
		t.TaxonomyHash = HashCategoryTaxonomy(taxonomy)
	}
	if t.IsCampaign {
		if t.GovernanceMode == "" {
			t.GovernanceMode = GovernanceModeFullLedger
		}
		switch t.GovernanceMode {
		case GovernanceModeEntityAssessment, GovernanceModeFullLedger, GovernanceModeChangeFocus:
			// valid
		default:
			return fmt.Errorf("invalid governance_mode: %q, must be one of: %q, %q, %q",
				t.GovernanceMode, GovernanceModeEntityAssessment, GovernanceModeFullLedger, GovernanceModeChangeFocus)
		}
		if len(t.CampaignConfig) > 0 {
			var cfg CampaignConfigSchema
			if err := json.Unmarshal(t.CampaignConfig, &cfg); err != nil {
				return fmt.Errorf("invalid campaign_config JSON: %w", err)
			}
		}
	}
	return nil
}

func HashCategoryTaxonomy(taxonomy *CategoryTaxonomy) string {
	if taxonomy == nil {
		return ""
	}
	canonical := *taxonomy
	canonical.Hash = ""
	normalized, err := json.Marshal(canonical)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(normalized)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (t *CategoryTaxonomy) validate() error {
	codes := make(map[string]struct{}, len(t.Categories))
	labels := make(map[string]struct{}, len(t.Categories))
	deprecatedAliasLabels := make(map[string]struct{}, len(t.DeprecatedAliases))
	inlineAliasLabels := make(map[string]struct{}, len(t.Categories))
	if t.SchemaVersion < 1 || t.SchemaVersion > 3 {
		return fmt.Errorf("unsupported taxonomy schema_version %d", t.SchemaVersion)
	}
	for _, category := range t.Categories {
		code := strings.TrimSpace(category.Code)
		label := strings.TrimSpace(category.Label)
		if code == "" || label == "" {
			return fmt.Errorf("category code and label are required")
		}
		if _, exists := codes[code]; exists {
			return fmt.Errorf("duplicate category code %q", code)
		}
		if _, exists := labels[label]; exists {
			return fmt.Errorf("duplicate category label %q", label)
		}
		codes[code] = struct{}{}
		labels[label] = struct{}{}

		if category.DefaultSeverity != "" {
			switch category.DefaultSeverity {
			case "致命", "严重", "一般", "建议":
			default:
				return fmt.Errorf("category %q default_severity %q is invalid", code, category.DefaultSeverity)
			}
		}
		for _, group := range [][]string{category.EscalateWhen, category.DowngradeWhen} {
			for _, reason := range group {
				if strings.TrimSpace(reason) == "" {
					return fmt.Errorf("category %q severity policy contains an empty reason", code)
				}
			}
		}

		if t.SchemaVersion < 3 {
			continue
		}
		if strings.TrimSpace(category.Definition) == "" {
			return fmt.Errorf("category %q definition is required for taxonomy v3", code)
		}
		if len(category.DecisionRules) == 0 {
			return fmt.Errorf("category %q decision_rules are required for taxonomy v3", code)
		}
		for index, rule := range category.DecisionRules {
			if strings.TrimSpace(rule) == "" {
				return fmt.Errorf("category %q decision_rules[%d] is empty", code, index)
			}
		}
		if len(category.PositiveHints) == 0 || len(category.NegativeHints) == 0 {
			return fmt.Errorf("category %q positive and negative hints are required for taxonomy v3", code)
		}
		for _, hint := range append(append([]string(nil), category.PositiveHints...), category.NegativeHints...) {
			if strings.TrimSpace(hint) == "" {
				return fmt.Errorf("category %q hints cannot contain empty entries", code)
			}
		}
		if category.Priority == 0 {
			return fmt.Errorf("category %q priority is required for taxonomy v3", code)
		}
		switch strings.TrimSpace(category.Status) {
		case "active", "deprecated", "superseded":
		default:
			return fmt.Errorf("category %q status %q is invalid for taxonomy v3", code, category.Status)
		}
	}
	for _, alias := range t.DeprecatedAliases {
		if strings.TrimSpace(alias.Label) == "" || strings.TrimSpace(alias.TargetCode) == "" {
			return fmt.Errorf("deprecated alias label and target_code are required")
		}
		if _, exists := codes[alias.TargetCode]; !exists {
			return fmt.Errorf("deprecated alias target_code %q is unknown", alias.TargetCode)
		}
		aliasKey := strings.ToLower(strings.TrimSpace(alias.Label))
		if _, exists := deprecatedAliasLabels[aliasKey]; exists {
			return fmt.Errorf("duplicate deprecated alias label %q", alias.Label)
		}
		if _, exists := labels[aliasKey]; exists {
			return fmt.Errorf("deprecated alias label %q conflicts with a category label", alias.Label)
		}
		if _, exists := inlineAliasLabels[aliasKey]; exists {
			return fmt.Errorf("deprecated alias label %q conflicts with an inline alias", alias.Label)
		}
		deprecatedAliasLabels[aliasKey] = struct{}{}
	}
	for _, category := range t.Categories {
		for _, alias := range category.Aliases {
			if strings.TrimSpace(alias.Label) == "" {
				return fmt.Errorf("category %q inline alias label is empty", category.Code)
			}
			aliasKey := strings.ToLower(strings.TrimSpace(alias.Label))
			if _, exists := labels[aliasKey]; exists {
				return fmt.Errorf("category %q inline alias label %q conflicts with a category label", category.Code, alias.Label)
			}
			if _, exists := deprecatedAliasLabels[aliasKey]; exists {
				return fmt.Errorf("category %q inline alias label %q conflicts with a deprecated alias", category.Code, alias.Label)
			}
			if _, exists := inlineAliasLabels[aliasKey]; exists {
				return fmt.Errorf("duplicate inline alias label %q", alias.Label)
			}
			inlineAliasLabels[aliasKey] = struct{}{}
		}
	}
	return nil
}

// TaskDir 返回任务类型的文件目录（约定: tasks/<name-with-hyphens>/）
func (t *TaskType) TaskDir() string {
	return filepath.Join("tasks", strings.ReplaceAll(t.Name, "_", "-"))
}

// AnalysisPromptFile 分析阶段提示词文件路径（约定固定）
func (t *TaskType) AnalysisPromptFile() string {
	return filepath.Join(t.TaskDir(), "analysis_prompt.md")
}

// SynthesisPromptFile 综合报告阶段提示词文件路径（约定固定）
func (t *TaskType) SynthesisPromptFile() string {
	return filepath.Join(t.TaskDir(), "synthesis_prompt.md")
}

// PostprocessScript 后置结果解析脚本路径（约定固定）
func (t *TaskType) PostprocessScript() string {
	return filepath.Join(t.TaskDir(), "postprocess")
}

// TaskReport 通用任务报告
type TaskReport struct {
	ID               uint           `gorm:"primaryKey" json:"id"`
	RepoID           uint           `gorm:"index" json:"repo_id"`
	Repo             Repository     `gorm:"foreignKey:RepoID" json:"repo"`
	TaskTypeID       uint           `gorm:"index:idx_task_reports_type_status_created,priority:1;index" json:"task_type_id"`
	TaskType         TaskType       `gorm:"foreignKey:TaskTypeID" json:"task_type"`
	ParentID         uint           `gorm:"default:0;index" json:"parent_id"` // 0 if it is a parent or independent task
	ChunkName        string         `gorm:"default:''" json:"chunk_name"`     // Name of the directory or file group
	TotalChunks      int            `gorm:"default:0" json:"total_chunks"`
	ProcessedChunks  int            `gorm:"default:0" json:"processed_chunks"`
	SuccessChunks    int            `gorm:"default:0" json:"success_chunks"`
	Status           string         `gorm:"default:pending;index:idx_task_reports_type_status_created,priority:2;index" json:"status"` // pending, queued, cloning, pre_processing, analyzing, post_processing, success, failed, skipped
	CloneStatus      string         `gorm:"default:pending" json:"clone_status"`
	AISummary        string         `json:"ai_summary"`
	ReportPath       string         `json:"report_path"`
	Score            int            `gorm:"default:0" json:"score"`
	Metrics          datatypes.JSON `json:"metrics"` // {"blocking":0,"critical":3,...}
	BaseCommit       string         `json:"base_commit"`
	HeadCommit       string         `json:"head_commit"`
	ChangeCutoffAt   *time.Time     `json:"change_cutoff_at"`
	BaseCommitTime   *time.Time     `json:"base_commit_time"`
	DiffManifestHash string         `gorm:"size:72" json:"diff_manifest_hash"`

	// ── 报告执行快照（ADR-010）──
	EngineMode       string         `gorm:"size:40;not null;default:''" json:"engine_mode"`
	ScanProfile      datatypes.JSON `json:"scan_profile"`
	ScanProfileHash  string         `gorm:"size:72;not null;default:''" json:"scan_profile_hash"`
	PromptVersion    string         `gorm:"size:64;not null;default:''" json:"prompt_version"`
	EngineConfigHash string         `gorm:"size:72;not null;default:''" json:"engine_config_hash"`
	PlannerVersion   string         `gorm:"size:40;not null;default:''" json:"planner_version"`

	// ── 专项评估插件执行快照（Phase 1）──
	AssessmentConfig         datatypes.JSON `json:"assessment_config"`
	AssessmentConfigHash     string         `gorm:"size:72;not null;default:''" json:"assessment_config_hash"`
	PromptContent            string         `gorm:"type:text" json:"prompt_content"`
	PromptContentHash        string         `gorm:"size:72;not null;default:''" json:"prompt_content_hash"`
	Categories               datatypes.JSON `gorm:"type:jsonb" json:"categories"`
	CategorySchemaHash       string         `gorm:"size:72;not null;default:''" json:"category_schema_hash"`
	TaxonomySchemaVersion    int            `gorm:"not null;default:0" json:"taxonomy_schema_version"`
	TaxonomyHash             string         `gorm:"size:72;not null;default:''" json:"taxonomy_hash"`
	DomainFamily             string         `gorm:"size:50;not null;default:''" json:"domain_family"`
	DefenseDimensions        datatypes.JSON `gorm:"type:jsonb" json:"defense_dimensions"`
	GovernanceMode           string         `gorm:"size:50;not null;default:''" json:"governance_mode"`
	DomainLabel              string         `gorm:"size:100;not null;default:''" json:"domain_label"`
	TargetSemantics          datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"target_semantics"`
	DisplaySemantics         datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"display_semantics"`
	PostprocessContent       string         `gorm:"type:text" json:"postprocess_content"`
	PostprocessHash          string         `gorm:"size:72;not null;default:''" json:"postprocess_hash"`
	TaskTypeRevisionID       *uint          `gorm:"index" json:"task_type_revision_id"`
	ExecutionSnapshot        datatypes.JSON `json:"execution_snapshot"`
	ExecutionSnapshotVersion int            `gorm:"not null;default:0" json:"execution_snapshot_version"`
	ExecutionSnapshotState   string         `gorm:"size:32;not null;default:''" json:"execution_snapshot_state"`

	// ── 扫描覆盖守卫与生命周期基线资格 ──
	CoverageComplete       bool       `gorm:"default:false" json:"coverage_complete"`
	CoverageDegraded       bool       `gorm:"default:false" json:"coverage_degraded"`
	CoverageNotApplicable  bool       `gorm:"default:false" json:"coverage_not_applicable"`
	CoverageState          string     `gorm:"size:24;not null;default:'UNKNOWN'" json:"coverage_state"`
	ScopeHash              string     `gorm:"size:64;not null;default:''" json:"scope_hash"`
	WorktreeClean          bool       `gorm:"not null;default:false" json:"worktree_clean"`
	AlgorithmVersion       string     `gorm:"size:32;not null;default:''" json:"algorithm_version"`
	LedgerCommittedAt      *time.Time `json:"ledger_committed_at"`
	LedgerAlgorithmVersion string     `gorm:"size:32;not null;default:''" json:"ledger_algorithm_version"`

	// ── Token 消耗统计 ──
	Tier1Tokens int64 `gorm:"default:0" json:"tier1_tokens"`
	Tier2Tokens int64 `gorm:"default:0" json:"tier2_tokens"`

	CreatedAt time.Time `gorm:"index:idx_task_reports_type_status_created,priority:3;index" json:"created_at"`
}

// GetAbsReportPath 返回报告文件的绝对路径（如果存储的是相对路径，则使用 server.data_dir / storage.root 拼接）
func (r *TaskReport) GetAbsReportPath() string {
	if r.ReportPath == "" {
		return ""
	}
	if filepath.IsAbs(r.ReportPath) {
		// 1. 如果原始绝对路径文件存在，直接使用
		if _, err := os.Stat(r.ReportPath); err == nil {
			return r.ReportPath
		}
		// 2. 否则，如果路径中包含 "reports/"，截取相对路径并与当前 AppConfig.GetDataDir() 拼接做兼容性容错
		if idx := strings.Index(r.ReportPath, "reports/"); idx != -1 {
			relPath := r.ReportPath[idx:]
			absPath := filepath.Join(AppConfig.GetDataDir(), relPath)
			if _, err := os.Stat(absPath); err == nil {
				return absPath
			}
		}
		return r.ReportPath
	}
	return filepath.Join(AppConfig.GetDataDir(), r.ReportPath)
}

// GetReportDir 返回任务专属的报告存储目录
func (r *TaskReport) GetReportDir() string {
	absReport := r.GetAbsReportPath()
	if absReport != "" {
		return filepath.Dir(absReport)
	}
	return filepath.Join(AppConfig.GetDataDir(), "reports")
}

// GetSynthesisJSONPath 返回 Synthesis Findings JSON 文件路径（内建历史命名兼容与全路径通配回退）
func (r *TaskReport) GetSynthesisJSONPath() string {
	dir := r.GetReportDir()
	// 1. 标准命名
	p1 := filepath.Join(dir, "findings.json")
	if _, err := os.Stat(p1); err == nil {
		return p1
	}
	// 2. 规范命名 report-{id}-synthesis-{safeRepo}.json (带物理存在性检查)
	safeRepo := strings.ReplaceAll(r.Repo.Name, "/", "-")
	if safeRepo != "" {
		p2 := filepath.Join(dir, fmt.Sprintf("report-%d-synthesis-%s.json", r.ID, safeRepo))
		if _, err := os.Stat(p2); err == nil {
			return p2
		}
	}
	// 3. 通配容错 report-{id}-synthesis-*.json (杜绝因 Repo.Name 未预加载导致的文件丢失)
	pattern := filepath.Join(dir, fmt.Sprintf("report-%d-synthesis-*.json", r.ID))
	if matches, err := filepath.Glob(pattern); err == nil && len(matches) > 0 {
		return matches[0]
	}
	// 4. 历史候选路径回退
	candidates := []string{
		filepath.Join(dir, fmt.Sprintf("report-%d-synthesis.json", r.ID)),
		filepath.Join(dir, "synthesis.json"),
		filepath.Join(dir, fmt.Sprintf("report-%d-raw-findings.json", r.ID)),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	if safeRepo != "" {
		return filepath.Join(dir, fmt.Sprintf("report-%d-synthesis-%s.json", r.ID, safeRepo))
	}
	return filepath.Join(dir, fmt.Sprintf("report-%d-synthesis.json", r.ID))
}

// GetSummaryJSONPath 返回 Summary Diagnostics JSON 文件路径（带规范化通配与候选回退）
func (r *TaskReport) GetSummaryJSONPath() string {
	dir := r.GetReportDir()
	// 1. 标准命名
	p1 := filepath.Join(dir, "diagnostics.json")
	if _, err := os.Stat(p1); err == nil {
		return p1
	}
	// 2. 规范命名 report-{id}-summary-{safeRepo}.json (带物理存在性检查)
	safeRepo := strings.ReplaceAll(r.Repo.Name, "/", "-")
	if safeRepo != "" {
		p2 := filepath.Join(dir, fmt.Sprintf("report-%d-summary-%s.json", r.ID, safeRepo))
		if _, err := os.Stat(p2); err == nil {
			return p2
		}
	}
	// 3. 通配容错 report-{id}-summary-*.json
	pattern := filepath.Join(dir, fmt.Sprintf("report-%d-summary-*.json", r.ID))
	if matches, err := filepath.Glob(pattern); err == nil && len(matches) > 0 {
		return matches[0]
	}
	// 4. 历史候选路径回退
	candidates := []string{
		filepath.Join(dir, fmt.Sprintf("report-%d-summary.json", r.ID)),
		filepath.Join(dir, "summary.json"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	if safeRepo != "" {
		return filepath.Join(dir, fmt.Sprintf("report-%d-summary-%s.json", r.ID, safeRepo))
	}
	return filepath.Join(dir, fmt.Sprintf("report-%d-summary.json", r.ID))
}

// GetScopeManifestPath 返回全量覆盖明细压缩清单路径（08号设计 §3.2）
func (r *TaskReport) GetScopeManifestPath() string {
	if AppConfig.Retention.ScopeManifestDir != "" {
		return filepath.Join(AppConfig.Retention.ScopeManifestDir, fmt.Sprintf("scope_manifest-%d.json.gz", r.ID))
	}
	dir := r.GetReportDir()
	return filepath.Join(dir, fmt.Sprintf("scope_manifest-%d.json.gz", r.ID))
}

// GetExecutionLogPath 返回任务 AI 执行输出日志路径
func (r *TaskReport) GetExecutionLogPath() string {
	dir := r.GetReportDir()
	p1 := filepath.Join(dir, "execution.log")
	if _, err := os.Stat(p1); err == nil {
		return p1
	}
	absReport := r.GetAbsReportPath()
	if absReport != "" {
		p2 := absReport + ".output.txt"
		if _, err := os.Stat(p2); err == nil {
			return p2
		}
	}
	return p1
}

// AnalysisFinding 记录 AI 分析阶段输出的结构化问题
type AnalysisFinding struct {
	ID                      uint   `gorm:"primaryKey" json:"id"`
	TaskReportID            uint   `gorm:"index" json:"task_report_id"`                                                         // 关联到 TaskReport
	TaskTypeID              uint   `gorm:"index;index:idx_analysis_findings_type_category_code,priority:1" json:"task_type_id"` // 哪个任务类型触发的
	RepoID                  uint   `gorm:"index" json:"repo_id"`                                                                // 来自哪个代码仓
	PrimaryUnitID           string `gorm:"size:512;not null;default:'';index" json:"primary_unit_id,omitempty"`
	Severity                string `gorm:"not null;index" json:"severity"` // 严重程度（致命/严重/一般/建议）
	Category                string `gorm:"index" json:"category"`          // 问题分类（multithreading, memory_leak, library...）
	CategoryDetail          string `gorm:"type:text" json:"category_detail,omitempty"`
	CategoryCode            string `gorm:"size:128;not null;default:'';index:idx_analysis_findings_type_category_code,priority:2" json:"category_code"`
	CategorySource          string `gorm:"size:32;not null;default:''" json:"category_source"`
	CategoryStatus          string `gorm:"size:32;not null;default:''" json:"category_status"`
	ClassificationRationale string `gorm:"type:text" json:"classification_rationale,omitempty"`
	TaxonomyHash            string `gorm:"size:72;not null;default:''" json:"taxonomy_hash"`
	FilePath                string `json:"file_path"`                     // 问题所在文件
	LineNumber              string `json:"line_number"`                   // 行号（支持范围如 "100-125" 或多行 "41,42"）
	CodeSnippet             string `gorm:"type:text" json:"code_snippet"` // 问题发生处的原始代码片段
	Title                   string `gorm:"not null" json:"title"`         // 问题标题
	Detail                  string `gorm:"type:text" json:"detail"`       // 详细描述
	Suggestion              string `gorm:"type:text" json:"suggestion"`   // 修复建议
	// ── 智能体辩论与物理定位证据 ──
	TriggerLine           string         `gorm:"type:text" json:"trigger_line"`
	ScopeSymbol           string         `gorm:"size:256" json:"scope_symbol"`
	HunterClaim           string         `gorm:"type:text" json:"hunter_claim"`
	ChallengerArg         string         `gorm:"type:text" json:"challenger_arg"`
	JudgeVerdict          string         `gorm:"type:text" json:"judge_verdict"`
	CalibrationRule       string         `gorm:"size:128" json:"calibration_rule"`
	TaxonomyGoverned      bool           `gorm:"default:false" json:"taxonomy_governed"`
	ReviewRequired        bool           `gorm:"default:false" json:"review_required"`
	ObservationGroupUID   string         `gorm:"size:64;not null;default:'';index:idx_analysis_findings_report_group" json:"observation_group_uid"`
	AnchorConfidence      string         `gorm:"size:16;not null;default:'LOW'" json:"anchor_confidence"`
	AssessmentStatus      string         `gorm:"size:32;not null;default:''" json:"assessment_status,omitempty"`
	AssessmentOutcome     string         `gorm:"size:32;not null;default:'';index" json:"assessment_outcome,omitempty"`
	AssessmentArtifact    datatypes.JSON `json:"assessment_artifact,omitempty"`
	ArtifactSchemaID      string         `gorm:"size:128;not null;default:''" json:"artifact_schema_id,omitempty"`
	ArtifactSchemaHash    string         `gorm:"size:72;not null;default:''" json:"artifact_schema_hash,omitempty"`
	ArtifactRepairMetrics datatypes.JSON `json:"artifact_repair_metrics,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

type ScanScopeEntry struct {
	ID            uint           `gorm:"primaryKey" json:"id"`
	ReportID      uint           `gorm:"not null;uniqueIndex:uq_scan_scope_report_path" json:"report_id"`
	RepoID        uint           `gorm:"not null;index:idx_scan_scope_repo_task_path" json:"repo_id"`
	TaskTypeID    uint           `gorm:"not null;index:idx_scan_scope_repo_task_path" json:"task_type_id"`
	NormPath      string         `gorm:"size:512;not null;uniqueIndex:uq_scan_scope_report_path" json:"norm_path"`
	BlobHash      string         `gorm:"size:64;not null;default:''" json:"blob_hash"`
	Outcome       string         `gorm:"size:32;not null;index:idx_scan_scope_report_outcome" json:"outcome"`
	ChunkName     string         `gorm:"size:256;not null;default:''" json:"chunk_name"`
	ChunkUID      string         `gorm:"size:64;not null;default:'';index" json:"chunk_uid"`
	PrimaryUnitID string         `gorm:"size:512;not null;default:''" json:"primary_unit_id"`
	Stage         string         `gorm:"size:32;not null;default:''" json:"stage"`
	ErrorClass    string         `gorm:"size:32;not null;default:''" json:"error_class"`
	FailReason    string         `gorm:"type:text;not null;default:''" json:"fail_reason"`
	DiffTouched   bool           `gorm:"not null;default:false" json:"diff_touched"`
	HunkRanges    datatypes.JSON `gorm:"not null;default:'[]'" json:"hunk_ranges"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type ArtifactRepairAudit struct {
	ID                     uint           `gorm:"primaryKey" json:"id"`
	ReportID               uint           `gorm:"not null;index:idx_artifact_repair_audits_report_bundle,priority:1" json:"report_id"`
	RepoID                 uint           `gorm:"not null;index" json:"repo_id"`
	TaskTypeID             uint           `gorm:"not null;index" json:"task_type_id"`
	BundleID               string         `gorm:"size:256;not null;default:'';index:idx_artifact_repair_audits_report_bundle,priority:2" json:"bundle_id"`
	Driver                 string         `gorm:"size:64;not null;default:''" json:"driver"`
	ResourceID             string         `gorm:"size:128;not null;default:''" json:"resource_id"`
	ResponseFormatMode     string         `gorm:"size:32;not null;default:''" json:"response_format_mode"`
	SchemaID               string         `gorm:"size:128;not null;default:''" json:"schema_id"`
	SchemaHash             string         `gorm:"size:72;not null;default:''" json:"schema_hash"`
	OriginalArtifactHash   string         `gorm:"size:72;not null;default:''" json:"original_artifact_hash"`
	FinalStatus            string         `gorm:"size:32;not null;default:'failed'" json:"final_status"`
	RepairTokens           int64          `gorm:"not null;default:0" json:"repair_tokens"`
	LocalRepairs           int            `gorm:"not null;default:0" json:"local_repairs"`
	SyntaxRepairs          int            `gorm:"not null;default:0" json:"syntax_repairs"`
	LLMRepairs             int            `gorm:"not null;default:0" json:"llm_repairs"`
	LLMRepairAttempts      int            `gorm:"not null;default:0" json:"llm_repair_attempts"`
	LLMRepairSuccesses     int            `gorm:"not null;default:0" json:"llm_repair_successes"`
	RepairDrifted          bool           `gorm:"not null;default:false" json:"repair_drifted"`
	RepairDriftUnchecked   bool           `gorm:"not null;default:false" json:"repair_drift_unchecked"`
	RepairDriftUnitRef     string         `gorm:"size:128;not null;default:''" json:"repair_drift_unit_ref"`
	BaselineSignatureKnown bool           `gorm:"not null;default:false" json:"baseline_signature_known"`
	RepairedSignatureKnown bool           `gorm:"not null;default:false" json:"repaired_signature_known"`
	BaselineSignatureHash  string         `gorm:"size:72;not null;default:''" json:"baseline_signature_hash"`
	RepairedSignatureHash  string         `gorm:"size:72;not null;default:''" json:"repaired_signature_hash"`
	UnverifiedRepair       bool           `gorm:"not null;default:false" json:"unverified_repair"`
	RepairOutcome          string         `gorm:"size:48;not null;default:''" json:"repair_outcome"`
	SchemaRepairIssues     datatypes.JSON `json:"schema_repair_issues,omitempty"`
	RepairAttempts         datatypes.JSON `json:"repair_attempts,omitempty"`
	CreatedAt              time.Time      `json:"created_at"`
	UpdatedAt              time.Time      `json:"updated_at"`
}

type TaskChunkExecution struct {
	ID            uint           `gorm:"primaryKey" json:"id"`
	ReportID      uint           `gorm:"not null;index;uniqueIndex:uq_task_chunk_executions_attempt" json:"report_id"`
	RepoID        uint           `gorm:"not null;index" json:"repo_id"`
	TaskTypeID    uint           `gorm:"not null;index" json:"task_type_id"`
	ChunkUID      string         `gorm:"size:64;not null;uniqueIndex:uq_task_chunk_executions_attempt;index" json:"chunk_uid"`
	ChunkName     string         `gorm:"size:256;not null;default:''" json:"chunk_name"`
	PrimaryUnitID string         `gorm:"size:512;not null;default:''" json:"primary_unit_id"`
	FilePath      string         `gorm:"size:512;not null;default:''" json:"file_path"`
	Stage         string         `gorm:"size:32;not null;default:'';uniqueIndex:uq_task_chunk_executions_attempt" json:"stage"`
	Attempt       int            `gorm:"not null;default:1;uniqueIndex:uq_task_chunk_executions_attempt" json:"attempt"`
	AttemptKind   string         `gorm:"size:32;not null;default:'invocation'" json:"attempt_kind"`
	IsFinal       bool           `gorm:"not null;default:false;index" json:"is_final"`
	Status        string         `gorm:"size:24;not null;default:'running'" json:"status"`
	ErrorClass    string         `gorm:"size:32;not null;default:'none'" json:"error_class"`
	ErrorMessage  string         `gorm:"type:text;not null;default:''" json:"error_message"`
	Driver        string         `gorm:"size:64;not null;default:''" json:"driver"`
	Backend       string         `gorm:"size:64;not null;default:''" json:"backend"`
	ResourceID    string         `gorm:"size:128;not null;default:''" json:"resource_id"`
	ModelName     string         `gorm:"size:255;not null;default:''" json:"model_name"`
	SessionID     string         `gorm:"size:128;not null;default:''" json:"session_id"`
	PromptHash    string         `gorm:"size:72;not null;default:''" json:"prompt_hash"`
	ArtifactHash  string         `gorm:"size:72;not null;default:''" json:"artifact_hash"`
	ArtifactPath  string         `gorm:"size:512;not null;default:''" json:"artifact_path"`
	QueueWaitMs   int64          `gorm:"not null;default:0" json:"queue_wait_ms"`
	DurationMs    int64          `gorm:"not null;default:0" json:"duration_ms"`
	TokenUsage    datatypes.JSON `gorm:"not null;default:'{}'" json:"token_usage"`
	StartedAt     time.Time      `json:"started_at"`
	FinishedAt    *time.Time     `json:"finished_at"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type Defect struct {
	ID                   uint   `gorm:"primaryKey" json:"id"`
	RepoID               uint   `gorm:"not null;uniqueIndex:uq_defects_current_identity" json:"repo_id"`
	TaskTypeID           uint   `gorm:"not null;uniqueIndex:uq_defects_current_identity" json:"task_type_id"`
	Status               string `gorm:"size:32;not null;default:'ACTIVE'" json:"status"`
	StatusReason         string `gorm:"type:text;not null;default:''" json:"status_reason"`
	CanonicalFingerprint string `gorm:"size:64;not null;uniqueIndex:uq_defects_current_identity" json:"canonical_fingerprint"`
	IdentityKind         string `gorm:"size:16;not null" json:"identity_kind"`
	NormPath             string `gorm:"size:512;not null" json:"norm_path"`
	ScopeKey             string `gorm:"size:64;not null" json:"scope_key"`
	SymbolPath           string `gorm:"size:512;not null;default:''" json:"symbol_path"`
	StmtShape            string `gorm:"size:64;not null" json:"stmt_shape"`
	CleanToken           string `gorm:"type:text;not null;default:''" json:"clean_token"`
	PrevShape            string `gorm:"size:64;not null;default:''" json:"prev_shape"`
	NextShape            string `gorm:"size:64;not null;default:''" json:"next_shape"`
	OccurrenceIndex      int    `gorm:"not null;default:0" json:"occurrence_index"`
	DefectClassMajor     string `gorm:"size:64;not null" json:"defect_class_major"`
	Severity             string `gorm:"size:32;not null;default:''" json:"severity"`
	LineStart            *int   `json:"line_start"`
	LineEnd              *int   `json:"line_end"`

	// 自包含核心文本元数据：TTL 清理后仍可完整渲染缺陷信息（08号设计 §6.2）
	Title         string `gorm:"size:500;not null;default:''" json:"title"`
	Category      string `gorm:"size:255;not null;default:''" json:"category"`
	CodeSnippet   string `gorm:"type:text;not null;default:''" json:"code_snippet"`
	Suggestion    string `gorm:"type:text;not null;default:''" json:"suggestion"`
	DetailSummary string `gorm:"type:text;not null;default:''" json:"detail_summary"`

	BlobHash            string         `gorm:"size:64;not null;default:''" json:"blob_hash"`
	ScopeBodyHash       string         `gorm:"size:64;not null;default:''" json:"scope_body_hash"`
	FirstReportID       uint           `gorm:"not null" json:"first_report_id"`
	LastSeenReportID    uint           `gorm:"not null" json:"last_seen_report_id"`
	LastMatchedReportID uint           `gorm:"not null;default:0" json:"last_matched_report_id"`
	MissedCount         int            `gorm:"not null;default:0" json:"missed_count"`
	DormantRounds       int            `gorm:"not null;default:0" json:"dormant_rounds"`
	HumanLocked         bool           `gorm:"not null;default:false" json:"human_locked"`
	HumanDecision       string         `gorm:"size:32;not null;default:''" json:"human_decision"`
	AssigneeID          *uint          `gorm:"index" json:"assignee_id"`
	AssignedAt          *time.Time     `json:"assigned_at"`
	ResolvedReportID    *uint          `json:"resolved_report_id"`
	ResolvedCommit      string         `gorm:"size:64;not null;default:''" json:"resolved_commit"`
	MergedIntoID        *uint          `gorm:"index" json:"merged_into_id"`
	RowVersion          int            `gorm:"not null;default:1" json:"row_version"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
	DeletedAt           gorm.DeletedAt `gorm:"index" json:"deleted_at"`
}

type DefectAlias struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	DefectID      uint      `gorm:"not null;index" json:"defect_id"`
	RepoID        uint      `gorm:"not null;index:idx_alias_lookup" json:"repo_id"`
	TaskTypeID    uint      `gorm:"not null" json:"task_type_id"`
	AliasType     string    `gorm:"size:16;not null;index:idx_alias_lookup" json:"alias_type"`
	AliasValue    string    `gorm:"size:512;not null;index:idx_alias_lookup" json:"alias_value"`
	AliasClass    string    `gorm:"size:16;not null" json:"alias_class"`
	FirstReportID uint      `gorm:"not null" json:"first_report_id"`
	LastReportID  uint      `gorm:"not null" json:"last_report_id"`
	HitCount      int       `gorm:"not null;default:1" json:"hit_count"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type DefectObservation struct {
	ID                  uint           `gorm:"primaryKey" json:"id"`
	ReportID            uint           `gorm:"not null;uniqueIndex:uq_defect_observation_group" json:"report_id"`
	RepoID              uint           `gorm:"not null" json:"repo_id"`
	TaskTypeID          uint           `gorm:"not null" json:"task_type_id"`
	ObservationGroupUID string         `gorm:"size:64;not null;uniqueIndex:uq_defect_observation_group" json:"observation_group_uid"`
	DefectID            *uint          `gorm:"index" json:"defect_id"`
	Verdict             string         `gorm:"size:32;not null" json:"verdict"`
	MatchTier           string         `gorm:"size:24;not null" json:"match_tier"`
	Confidence          float64        `gorm:"type:numeric(5,4);not null;default:0" json:"confidence"`
	ScoreDetail         datatypes.JSON `gorm:"not null;default:'{}'" json:"score_detail"`
	Reason              string         `gorm:"type:text;not null;default:''" json:"reason"`
	CreatedAt           time.Time      `json:"created_at"`
}

type DefectEvent struct {
	ID         uint           `gorm:"primaryKey" json:"id"`
	DefectID   uint           `gorm:"not null;index:idx_defect_events_defect" json:"defect_id"`
	ReportID   *uint          `gorm:"index" json:"report_id"`
	EventType  string         `gorm:"size:32;not null" json:"event_type"`
	FromStatus string         `gorm:"size:32;not null;default:''" json:"from_status"`
	ToStatus   string         `gorm:"size:32;not null;default:''" json:"to_status"`
	ActorType  string         `gorm:"size:16;not null;default:'SYSTEM'" json:"actor_type"`
	ActorID    *uint          `json:"actor_id"`
	Reason     string         `gorm:"type:text;not null;default:''" json:"reason"`
	Evidence   datatypes.JSON `gorm:"not null;default:'{}'" json:"evidence"`
	CreatedAt  time.Time      `gorm:"index:idx_defect_events_defect" json:"created_at"`
}

const (
	StatusPending        = "pending"
	StatusQueued         = "queued"
	StatusRunning        = "running"
	StatusCloning        = "cloning"
	StatusPreProcessing  = "pre_processing"
	StatusAnalyzing      = "analyzing"
	StatusSynthesis      = "synthesis"
	StatusPostProcessing = "post_processing"
	StatusMerging        = "merging"
	StatusSuccess        = "success"
	StatusDegraded       = "degraded"
	StatusFailed         = "failed"
	StatusSkipped        = "skipped"
)

// TerminalTaskStatuses returns task states that must not be recovered or re-run by startup cleanup.
func TerminalTaskStatuses() []string {
	return []string{StatusSuccess, StatusDegraded, StatusFailed, StatusSkipped}
}

// BusyTaskReportStatuses returns states that represent work already in progress.
func BusyTaskReportStatuses() []string {
	return []string{
		StatusRunning,
		StatusCloning,
		StatusPreProcessing,
		StatusAnalyzing,
		StatusSynthesis,
		StatusPostProcessing,
		StatusMerging,
	}
}

func IsTerminalTaskStatus(status string) bool {
	for _, terminalStatus := range TerminalTaskStatuses() {
		if status == terminalStatus {
			return true
		}
	}
	return false
}

func IsBusyTaskReportStatus(status string) bool {
	for _, busyStatus := range BusyTaskReportStatuses() {
		if status == busyStatus {
			return true
		}
	}
	return false
}

// ErrTaskReportImmutable marks an update rejected because the report has reached a terminal state.
var ErrTaskReportImmutable = errors.New("task report is immutable after terminal state")

// UpdateActiveTaskReport applies updates only while a task report still represents a scan instance.
// Once it reaches a terminal state it becomes an immutable historical report.
func UpdateActiveTaskReport(db *gorm.DB, reportID uint, updates map[string]interface{}) (int64, error) {
	if db == nil {
		return 0, ErrTaskReportImmutable
	}
	if reportID == 0 || len(updates) == 0 {
		return 0, nil
	}

	var affected int64
	err := db.Transaction(func(tx *gorm.DB) error {
		var current struct {
			Status string
		}
		if err := tx.Model(&TaskReport{}).Select("status").
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", reportID).
			Take(&current).Error; err != nil {
			return err
		}
		if IsTerminalTaskStatus(current.Status) {
			return ErrTaskReportImmutable
		}

		result := tx.Model(&TaskReport{}).
			Where("id = ? AND status NOT IN ?", reportID, TerminalTaskStatuses()).
			Updates(updates)
		affected = result.RowsAffected
		return result.Error
	})
	return affected, err
}

// KeyIssue 核心问题追踪
type KeyIssue struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	RepoID       uint       `json:"repo_id"`
	TaskReportID uint       `json:"task_report_id"`
	Repo         Repository `gorm:"foreignKey:RepoID" json:"repo"`
	TaskReport   TaskReport `gorm:"foreignKey:TaskReportID" json:"task_report"`
	IssueType    string     `gorm:"not null" json:"issue_type"` // multithreading, lock, memory_leak, library
	Title        string     `gorm:"not null" json:"title"`
	FilePath     string     `json:"file_path"`
	LineNumber   string     `json:"line_number"`
	Status       string     `gorm:"default:open" json:"status"` // open, in_progress, resolved
	AssigneeID   *uint      `json:"assignee_id"`
	Assignee     *User      `gorm:"foreignKey:AssigneeID" json:"assignee,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

type SystemConfig struct {
	ID               uint       `gorm:"primaryKey" json:"id"` // Always 1
	AutoNotify       bool       `gorm:"default:false" json:"auto_notify"`
	ConcurrencyScale float64    `gorm:"default:1.0" json:"concurrency_scale"`
	ScaleExpiresAt   *time.Time `json:"scale_expires_at"`
	QueuePaused      bool       `gorm:"default:false" json:"queue_paused"`
}

// SystemDynamicConfig 数据库持久化动态配置表 (按 Category 独立存储结构化 JSON，单库天然唯一)
type SystemDynamicConfig struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	Category  string         `gorm:"size:50;not null;uniqueIndex" json:"category"` // "llm", "scanner", "governance", "notification"
	Data      datatypes.JSON `gorm:"type:jsonb;not null" json:"data"`              // 对应的结构化 JSON 数据
	Version   int            `gorm:"default:1" json:"version"`                     // 乐观锁版本号
	UpdatedBy string         `gorm:"size:100" json:"updated_by"`                   // 最后修改人
	CreatedAt time.Time      `json:"created_at"`                                   // 首次 Seed 导入时间
	UpdatedAt time.Time      `json:"updated_at"`
}

type ScheduleConfig struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	Name         string         `gorm:"not null;default:''" json:"name"`
	CronExpr     string         `gorm:"not null;default:''" json:"cron_expr"`
	TaskTypeID   uint           `json:"task_type_id"`
	TaskType     TaskType       `gorm:"foreignKey:TaskTypeID" json:"task_type"`
	TargetMode   string         `gorm:"not null;default:'all'" json:"target_mode"` // "all", "service_group", "team", "specific"
	TargetValues datatypes.JSON `json:"target_values"`                             // JSON array
	AutoNotify   bool           `gorm:"default:true" json:"auto_notify"`
	IsActive     bool           `gorm:"default:true" json:"is_active"`
	CreatedAt    time.Time      `json:"created_at"`
	RunParams    datatypes.JSON `json:"run_params"` // 运行参数覆盖 {"ai_backend":"claude","target_scope":"business"}
	UpdatedAt    time.Time      `json:"updated_at"`
}

// TaskTriggerLog 记录面向人类的操作审计触发日志
type TaskTriggerLog struct {
	ID            uint            `gorm:"primaryKey" json:"id"`
	TriggerBatch  string          `gorm:"uniqueIndex;size:64;not null" json:"trigger_batch"` // 批次号 e.g. "TRG-20260731-XXXXXX"
	TriggerType   string          `gorm:"size:30;not null;index" json:"trigger_type"`        // "manual_single", "manual_batch", "cron_auto", "cron_manual"
	OperatorID    *uint           `gorm:"index" json:"operator_id"`                          // 操作人 ID (系统触发为 nil)
	Operator      *User           `gorm:"foreignKey:OperatorID" json:"operator,omitempty"`   // 关联 User
	OperatorName  string          `gorm:"size:100" json:"operator_name"`                     // 操作人姓名/Email 冗余快照
	TaskTypeID    uint            `gorm:"index" json:"task_type_id"`
	TaskType      TaskType        `gorm:"foreignKey:TaskTypeID" json:"task_type"`
	TargetMode    string          `gorm:"size:30" json:"target_mode"`     // "single", "all", "service_group", "team", "missing_days", "specific"
	TargetSummary string          `gorm:"size:255" json:"target_summary"` // 目标摘要：“代码仓 repo-a”, “过去 7 天未扫代码仓”
	FilterParams  datatypes.JSON  `json:"filter_params"`                  // 筛选参数Json
	ScheduleID    *uint           `gorm:"index" json:"schedule_id"`       // 如果关联定时任务策略
	Schedule      *ScheduleConfig `gorm:"foreignKey:ScheduleID" json:"schedule,omitempty"`
	TotalRepos    int             `gorm:"default:0" json:"total_repos"`   // 本次触发涉及的代码仓总数
	SuccessCount  int             `gorm:"default:0" json:"success_count"` // 成功排队数
	SkipCount     int             `gorm:"default:0" json:"skip_count"`    // 跳过数
	ClientIP      string          `gorm:"size:50" json:"client_ip"`       // 操作客户端 IP
	Remark        string          `gorm:"type:text" json:"remark"`        // 审计备注
	CreatedAt     time.Time       `gorm:"index" json:"created_at"`
}

type TaskExecutionLog struct {
	ID             uint            `gorm:"primaryKey" json:"id"`
	ScheduleID     *uint           `gorm:"index" json:"schedule_id"`
	Schedule       *ScheduleConfig `gorm:"foreignKey:ScheduleID" json:"schedule"`
	TriggerLogID   *uint           `gorm:"index" json:"trigger_log_id"`
	TriggerLog     *TaskTriggerLog `gorm:"foreignKey:TriggerLogID" json:"trigger_log,omitempty"`
	RepoID         uint            `gorm:"index" json:"repo_id"`
	Repo           Repository      `gorm:"foreignKey:RepoID" json:"repo"`
	TaskReportID   *uint           `gorm:"index" json:"task_report_id"`
	TaskReport     *TaskReport     `gorm:"foreignKey:TaskReportID" json:"task_report"`
	TaskTypeID     uint            `gorm:"index" json:"task_type_id"`
	TaskType       TaskType        `gorm:"foreignKey:TaskTypeID" json:"task_type"`
	TriggerType    string          `gorm:"not null" json:"trigger_type"`                                  // "cron", "manual", "webhook"
	Status         string          `gorm:"default:pending;index" json:"status"`                           // "pending", "running", "success", "failed", "skipped"
	StatusPriority int             `gorm:"default:2;index:idx_status_priority_id" json:"status_priority"` // 1: running/analyzing, 2: pending, 3: completed/failed, 4: other
	IsResume       bool            `gorm:"default:false" json:"is_resume"`                                // 是否为分片失败恢复任务（worker 走 ResumeFailedChunks）
	ErrorMessage   string          `json:"error_message"`
	StartTime      time.Time       `json:"start_time"`
	EndTime        *time.Time      `json:"end_time"`
	CreatedAt      time.Time       `gorm:"index" json:"created_at"`
}

// GetStatusPriority 返回状态排序优先级 (1=进行中, 2=待处理, 3=已结束, 4=其它)
func GetStatusPriority(status string) int {
	switch status {
	case "cloning", "pre_processing", "analyzing", "synthesis", "post_processing", "merging", "running":
		return 1
	case "pending":
		return 2
	case "success", "failed", "skipped":
		return 3
	default:
		return 4
	}
}

// CampaignFinding 统一专项分析缺陷与实体评估记录模型（替代原 7 张独立分表）
type CampaignFinding struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	TaskTypeID   uint       `gorm:"uniqueIndex:idx_camp_finding_uniq,priority:1;index:idx_camp_finding_repo_status,priority:2;index:idx_camp_finding_task_status_repo_severity,priority:1;index;not null" json:"task_type_id"`
	TaskType     TaskType   `gorm:"foreignKey:TaskTypeID" json:"task_type"`
	RepoID       uint       `gorm:"uniqueIndex:idx_camp_finding_uniq,priority:2;index:idx_camp_finding_repo_status,priority:1;index:idx_camp_finding_task_status_repo_severity,priority:3;index;not null" json:"repo_id"`
	Repo         Repository `gorm:"foreignKey:RepoID" json:"repo"`
	TaskReportID uint       `gorm:"index" json:"task_report_id"`

	// 缺陷/实体本身属性
	FilePath    string `gorm:"uniqueIndex:idx_camp_finding_uniq,priority:3;size:500;not null" json:"file_path"`
	LineNumber  string `gorm:"size:255" json:"line_number"`
	Title       string `gorm:"uniqueIndex:idx_camp_finding_uniq,priority:4;size:500;not null" json:"title"` // 普通专项存缺陷标题，UT存测试用例名称
	Detail      string `gorm:"type:text" json:"detail"`
	Severity    string `gorm:"size:100;not null;index:idx_camp_finding_task_status_repo_severity,priority:4" json:"severity"` // 致命/严重/一般/建议/合格
	Category    string `gorm:"size:255;index" json:"category"`
	CodeSnippet string `gorm:"type:text" json:"code_snippet"`
	Suggestion  string `gorm:"type:text" json:"suggestion"`

	// 治理状态与审计跟踪
	Status     string         `gorm:"default:'open';size:50;index:idx_camp_finding_repo_status,priority:3;index:idx_camp_finding_task_status_repo_severity,priority:2;index" json:"status"` // open, analyzing, resolved, closed, invalid
	AssigneeID *uint          `json:"assignee_id"`
	Assignee   *User          `gorm:"foreignKey:AssigneeID" json:"assignee,omitempty"`
	StatusLog  datatypes.JSON `json:"status_log"` // [{"status":"open","time":"...","user":"xxx","reason":"..."}]
	Feedback   string         `gorm:"type:text" json:"feedback"`
	CreatedAt  time.Time      `gorm:"index" json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// ── 智能体辩论结论常量 (DebateVerdict) ──
const (
	DebateVerdictConfirmed   = "CONFIRMED"   // 确认存在
	DebateVerdictRejected    = "REJECTED"    // 判定误报
	DebateVerdictConditional = "CONDITIONAL" // 条件触发
)

// TaskDebateLog 智能体三方对抗辩论轨迹表 (支持 TTL 自动清理与合规审计)
type TaskDebateLog struct {
	ID               uint           `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskReportID     uint           `gorm:"index;not null" json:"task_report_id"`
	ChunkName        string         `gorm:"size:256;not null" json:"chunk_name"`
	CandidateID      string         `gorm:"size:64;not null" json:"candidate_id"`
	TriggerLine      string         `gorm:"type:text" json:"trigger_line"`
	HunterOutput     datatypes.JSON `gorm:"type:jsonb" json:"hunter_output"`
	ChallengerOutput datatypes.JSON `gorm:"type:jsonb" json:"challenger_output"`
	JudgeOutput      datatypes.JSON `gorm:"type:jsonb;not null" json:"judge_output"`
	Verdict          string         `gorm:"size:32;not null;index" json:"verdict"` // CONFIRMED, REJECTED, CONDITIONAL
	DurationMs       int            `json:"duration_ms"`
	TokenUsage       datatypes.JSON `gorm:"type:jsonb" json:"token_usage"` // {"hunter": 1200, "challenger": 800, "judge": 950}
	CreatedAt        time.Time      `gorm:"index" json:"created_at"`
}

// RepoFeedbackRule 代码仓人机反馈例外规则库表 (负样本沉淀知识库)
type RepoFeedbackRule struct {
	ID         uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	RepoID     uint       `gorm:"index:idx_fb_repo_task;not null" json:"repo_id"`
	Repo       Repository `gorm:"foreignKey:RepoID" json:"repo"`
	TaskTypeID uint       `gorm:"index:idx_fb_repo_task;not null" json:"task_type_id"`
	TaskType   TaskType   `gorm:"foreignKey:TaskTypeID" json:"task_type"`
	ScopeType  string     `gorm:"size:32;default:'FILE'" json:"scope_type"`    // FILE (单文件), REPO (全仓), GLOBAL (全局)
	Pattern    string     `gorm:"size:512;not null" json:"pattern"`            // 文件路径正则或符号通配符
	RuleAction string     `gorm:"size:32;default:'IGNORE'" json:"rule_action"` // IGNORE (忽略), DOWNGRADE (降级)
	Reason     string     `gorm:"type:text;not null" json:"reason"`            // 豁免理由
	CreatedBy  string     `gorm:"size:64" json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// CategoryAliasUsage records resolver alias hits. It is deliberately outside
// CategoryTaxonomy so audit volume can never alter a frozen taxonomy hash.
type CategoryAliasUsage struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	TaskTypeID   uint      `gorm:"index" json:"task_type_id"`
	TaxonomyHash string    `gorm:"size:72;not null;default:'';index:idx_category_alias_usage,priority:1" json:"taxonomy_hash"`
	AliasKind    string    `gorm:"size:16;not null;default:'';index:idx_category_alias_usage,priority:2" json:"alias_kind"`
	AliasLabel   string    `gorm:"size:256;not null;default:'';index:idx_category_alias_usage,priority:3" json:"alias_label"`
	TargetCode   string    `gorm:"size:128;not null;default:''" json:"target_code"`
	HitCount     int64     `gorm:"not null;default:0" json:"hit_count"`
	LastReportID uint      `json:"last_report_id"`
	FirstSeenAt  time.Time `json:"first_seen_at"`
	LastSeenAt   time.Time `gorm:"index" json:"last_seen_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CategoryRegressionSample stores the persisted trend view of semantic
// regression samples. The JSONL corpus remains the source-controlled seed.
type CategoryRegressionSample struct {
	ID                uint           `gorm:"primaryKey" json:"id"`
	SampleKey         string         `gorm:"size:128;not null;uniqueIndex" json:"sample_key"`
	TaskType          string         `gorm:"size:128;not null;default:'';index" json:"task_type"`
	PairKey           string         `gorm:"size:128;not null;default:'';index" json:"pair_key"`
	CandidateFacts    datatypes.JSON `gorm:"type:jsonb" json:"candidate_facts"`
	ExpectedCode      string         `gorm:"size:128;not null;default:''" json:"expected_code"`
	ActualCode        string         `gorm:"size:128;not null;default:''" json:"actual_code"`
	Outcome           string         `gorm:"size:32;not null;default:'';index" json:"outcome"`
	Status            string         `gorm:"size:32;not null;default:'ACTIVE';index" json:"status"`
	TaxonomyHash      string         `gorm:"size:72;not null;default:'';index" json:"taxonomy_hash"`
	PromptHash        string         `gorm:"size:72;not null;default:'';index" json:"prompt_hash"`
	ModelBackend      string         `gorm:"size:128;not null;default:''" json:"model_backend"`
	FailureCount      int            `gorm:"not null;default:0" json:"failure_count"`
	LastFailureReason string         `gorm:"type:text" json:"last_failure_reason"`
	LastRunAt         *time.Time     `gorm:"index" json:"last_run_at"`
	SupersededBy      string         `gorm:"size:128;not null;default:''" json:"superseded_by"`
	SupersededReason  string         `gorm:"type:text" json:"superseded_reason"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

// CategoryRegressionReview supports one initial reviewer and second review for
// high-conflict or Judge override cases. DISPUTED records enter taxonomy review.
type CategoryRegressionReview struct {
	ID            uint                     `gorm:"primaryKey" json:"id"`
	SampleID      uint                     `gorm:"index:idx_category_regression_review_sample_stage,priority:1" json:"sample_id"`
	Sample        CategoryRegressionSample `gorm:"foreignKey:SampleID" json:"sample"`
	Stage         string                   `gorm:"size:16;not null;default:'';index:idx_category_regression_review_sample_stage,priority:2" json:"stage"`
	Reviewer      string                   `gorm:"size:128;not null;default:'';index" json:"reviewer"`
	Outcome       string                   `gorm:"size:32;not null;default:''" json:"outcome"`
	JudgeOverride bool                     `gorm:"not null;default:false" json:"judge_override"`
	DisputeReason string                   `gorm:"type:text" json:"dispute_reason"`
	CreatedAt     time.Time                `json:"created_at"`
	UpdatedAt     time.Time                `json:"updated_at"`
}

// CategoryConfusionMatrix is a monthly (or arbitrary period) trend snapshot.
type CategoryConfusionMatrix struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	TaxonomyHash string         `gorm:"size:72;not null;default:'';index" json:"taxonomy_hash"`
	PromptHash   string         `gorm:"size:72;not null;default:'';index" json:"prompt_hash"`
	ModelBackend string         `gorm:"size:128;not null;default:'';index" json:"model_backend"`
	PeriodStart  time.Time      `gorm:"not null;index" json:"period_start"`
	PeriodEnd    time.Time      `gorm:"not null;index" json:"period_end"`
	Matrix       datatypes.JSON `gorm:"type:jsonb" json:"matrix"`
	SampleCount  int            `gorm:"not null;default:0" json:"sample_count"`
	ErrorCount   int            `gorm:"not null;default:0" json:"error_count"`
	CreatedAt    time.Time      `gorm:"index" json:"created_at"`
}
