package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"code-shield/models"
)

// TaskSynthesisConfig 声明在 meta.json 中的 Tier 4 综合报告自适应配置
type TaskSynthesisConfig struct {
	ReportType               string `json:"report_type"`               // "incremental" 或 "static" (默认)
	OverviewGuidelines       string `json:"overview_guidelines"`       // 概要部分的业务引导
	RecommendationGuidelines string `json:"recommendation_guidelines"` // 总结与改进建议的领域引导
}

const staticSynthesisTemplate = `# {{TASK_DISPLAY_NAME}} 综合评估报告生成指令

你将收到一份 JSON 格式的评估数据（基于初筛与终审结果）。请基于这些发现，生成一份面向研发团队与技术管理者的专业综合 Markdown 评估报告。

## 输入说明

输入为 JSON 格式的评估数据对象（Schema: ` + "`code-shield.tier4-synthesis-input.v1`" + `），包含以下关键部分：
- ` + "`report`" + `: 代码仓与检视基本元信息（` + "`repo_name`" + `, ` + "`task_type`" + `, ` + "`head_commit`" + ` 等）
- ` + "`statistics`" + `: 统计数据（包含各类严重度统计 ` + "`fatal`" + `, ` + "`critical`" + `, ` + "`minor`" + `, ` + "`suggestion`" + ` 以及合格用例数 ` + "`pass`" + `）
- ` + "`detailed_findings`" + `: 重点缺陷列表，每个元素包含 ` + "`severity`" + `, ` + "`category`" + `, ` + "`file_path`" + `, ` + "`line_number`" + `, ` + "`code_snippet`" + `, ` + "`title`" + `, ` + "`detail`" + `, ` + "`suggestion`" + `
- ` + "`summary_findings`" + `: 其余缺陷简要索引列表（包含 ` + "`file_path`" + `, ` + "`line_number`" + `, ` + "`title`" + `, ` + "`severity`" + `）

## 输出格式要求

请使用简体中文，以 Markdown 格式输出。报告必须严格由"一、检视结果概要"、"二、问题清单"、"三、总结与建议"三部分组成：

### 一、检视结果概要
- 介绍本次审计的目标和评估策略，说明代码仓基本信息（代码仓名称、最新 Commit ID 等）；
- 概述代码整体质量与缺陷分布情况；若存在合格用例统计（` + "`statistics.pass`" + `），明确指出评估通过的数量；
{{OVERVIEW_GUIDELINES}}

> ⚠️ **概要部分的最后一行，格式约束（机器解析硬性约束，不得违反）**：
> - 必须严格使用 ` + "`致命：N，严重：N，一般：N，建议：N`" + ` 的格式，关键字和标点均不得更改
> - 冒号使用中文冒号 ` + "`：`" + `，分隔符使用中文逗号 ` + "`，`" + `
> - N 为非负整数，无问题填 ` + "`0`" + `

### 二、问题清单
- **完整呈现要求**：必须完整呈现 ` + "`detailed_findings`" + ` 中的所有重点缺陷条目，严禁自行省略、合并或以“因篇幅原因略去”代替；
- **零缺陷安全退避**：若无缺陷（统计均为 0），本章节明确写明“本次评估未发现相关缺陷，代码状态良好、符合质量规范”，无需列出具体条目；
- 按照严重程度从高到低（致命 -> 严重 -> 一般 -> 建议）对问题进行整理排版；
- 每个详细问题条目必须包含以下清晰结构：
  - **严重程度**：（致命 | 严重 | 一般 | 建议）
  - **代码位置**：` + "`file_path:line_number`" + `（必须原样保留真实文件路径）
  - **缺陷类型**：问题所属分类（` + "`category`" + `）
  - **现状描述**：详细描述代码存在的具体缺陷，结合代码片段分析；
  - **改进建议**：给出具体的重构或修复方案，提供聚焦于核心逻辑的精炼修复代码（避免过于冗长）。
- 若数据中存在 ` + "`summary_findings`" + `，请在重点问题后以简要 Markdown 表格列出其余缺陷索引。

### 三、总结与建议
- 对该代码仓在当前维度的整体防护水平与代码质量进行评价；
{{RECOMMENDATION_GUIDELINES}}
`

const incrementalSynthesisTemplate = `# {{TASK_DISPLAY_NAME}} 综合报告生成指令（增量变更检视）

你将收到一份 JSON 格式的代码检视发现清单（基于对代码仓库最近提交变更的分析）。请基于这些发现与输入的修改信息，生成一份专业的增量变更代码审查 Markdown 报告，指导开发团队进行缺陷修复、排除合入风险。

## 输入说明

输入为 JSON 格式的评估数据对象（Schema: ` + "`code-shield.tier4-synthesis-input.v1`" + `），包含以下关键部分：
- ` + "`report`" + `: 代码仓与检视基本元信息（` + "`repo_name`" + `, ` + "`task_type`" + `, ` + "`head_commit`" + ` 等）
- ` + "`statistics`" + `: 统计数据（包含各类严重度统计 ` + "`fatal`" + `, ` + "`critical`" + `, ` + "`minor`" + `, ` + "`suggestion`" + ` 等）
- ` + "`detailed_findings`" + `: 重点变更缺陷列表，每个元素包含 ` + "`severity`" + `, ` + "`category`" + `, ` + "`file_path`" + `, ` + "`line_number`" + `, ` + "`code_snippet`" + `, ` + "`title`" + `, ` + "`detail`" + `, ` + "`suggestion`" + `
- ` + "`summary_findings`" + `: 其余缺陷简要索引列表（包含 ` + "`file_path`" + `, ` + "`line_number`" + `, ` + "`title`" + `, ` + "`severity`" + `）

## 输出格式要求

请使用简体中文，以 Markdown 格式输出。报告必须严格由"一、变更与检视结果概要"、"二、变更缺陷与风险清单"、"三、合并与改进建议"三部分组成：

### 一、变更与检视结果概要
- **基本信息**：审查的 Commit 区间、主要修改文件范围；
- **主要变更内容**：总结本次提交或合并中，主要新增、修改、去除了哪些功能/特性；
- **【特别说明】对外接口变更**：如果本次提交修改了对外公共接口（API、SDK函数签名、暴露的 RPC 方法），**必须以醒目的高亮形式（如 Blockquote 或 Warning Alert）明确指出来**；
{{OVERVIEW_GUIDELINES}}

> ⚠️ **概要部分的最后一行，格式约束（机器解析硬性约束，不得违反）**：
> - 必须严格使用 ` + "`致命：N，严重：N，一般：N，建议：N`" + ` 的格式，关键字和标点均不得更改
> - 冒号使用中文冒号 ` + "`：`" + `，分隔符使用中文逗号 ` + "`，`" + `
> - N 为非负整数，无问题填 ` + "`0`" + `

### 二、变更缺陷与风险清单
- **完整呈现要求**：必须完整呈现 ` + "`detailed_findings`" + ` 中的所有重点变更缺陷条目，严禁自行省略或合并；
- **零缺陷安全退避**：若无缺陷（统计均为 0），本章节明确写明“本次增量变更未发现缺陷与合并风险，代码符合合入标准”，无需列出条目；
- 按照严重程度从高到低（致命 -> 严重 -> 一般 -> 建议）对问题进行整理排版；
- 每个详细问题条目必须包含以下清晰结构：
  - **严重程度**：（致命 | 严重 | 一般 | 建议）
  - **代码位置**：` + "`file_path:line_number`" + `（必须原样保留真实文件路径）
  - **缺陷类型**：问题所属分类（` + "`category`" + `）
  - **现状描述**：详细描述变更代码存在的具体缺陷，结合 diff/代码片段深入分析；
  - **改进建议**：给出具体的重构或修复方案，提供可直接实施的修改代码。
- 若数据中存在 ` + "`summary_findings`" + `，请在重点问题后以简要 Markdown 表格列出其余缺陷索引。

### 三、合并与改进建议
- 对本次变更的整体质量、合并风险与架构合理性进行客观评价；
{{RECOMMENDATION_GUIDELINES}}
`

// LoadTaskSynthesisConfig 从 meta.json 中加载 synthesis_config
func LoadTaskSynthesisConfig(taskDir string) TaskSynthesisConfig {
	metaPath := filepath.Join(taskDir, "meta.json")
	bytes, err := os.ReadFile(metaPath)
	if err != nil {
		return TaskSynthesisConfig{ReportType: "static"}
	}
	var raw struct {
		SynthesisConfig TaskSynthesisConfig `json:"synthesis_config"`
	}
	if err := json.Unmarshal(bytes, &raw); err != nil {
		return TaskSynthesisConfig{ReportType: "static"}
	}
	if raw.SynthesisConfig.ReportType == "" {
		raw.SynthesisConfig.ReportType = "static"
	}
	return raw.SynthesisConfig
}

// BuildSynthesisPrompt 根据 TaskType 及其 meta.json 配置动态装配最终的 Tier 4 综合报告 Prompt
func BuildSynthesisPrompt(taskType *models.TaskType) string {
	taskDir := ""
	if taskType != nil {
		taskDir = models.AppConfig.GetAbsPath(taskType.TaskDir())
	}
	return BuildSynthesisPromptWithDir(taskType, taskDir)
}

// BuildSynthesisPromptWithDir 支持显式指定 taskDir 进行装配
func BuildSynthesisPromptWithDir(taskType *models.TaskType, taskDir string) string {
	cfg := LoadTaskSynthesisConfig(taskDir)

	displayName := "代码检视"
	if taskType != nil && taskType.DisplayName != "" {
		displayName = taskType.DisplayName
	}

	var template string
	if strings.EqualFold(cfg.ReportType, "incremental") {
		template = incrementalSynthesisTemplate
	} else {
		template = staticSynthesisTemplate
	}

	overview := ""
	if cfg.OverviewGuidelines != "" {
		overview = fmt.Sprintf("- %s", strings.TrimSpace(cfg.OverviewGuidelines))
	} else {
		overview = "- 总结潜在风险分布与代码健壮性水平。"
	}

	recommendation := ""
	if cfg.RecommendationGuidelines != "" {
		recommendation = fmt.Sprintf("- %s", strings.TrimSpace(cfg.RecommendationGuidelines))
	} else {
		recommendation = "- 提供针对性的工程化预防与代码规范重构指导。"
	}

	result := template
	result = strings.ReplaceAll(result, "{{TASK_DISPLAY_NAME}}", displayName)
	result = strings.ReplaceAll(result, "{{OVERVIEW_GUIDELINES}}", overview)
	result = strings.ReplaceAll(result, "{{RECOMMENDATION_GUIDELINES}}", recommendation)

	return result
}
