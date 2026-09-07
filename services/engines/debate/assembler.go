package debate

import (
	"code-shield/models"
	"code-shield/services/engines"
	"code-shield/services/engines/chunker"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// MaxPromptRuleBytes 领域提示词文件物理注入上限 (32KB)，防止模型上下文窗口溢出
const MaxPromptRuleBytes = 32 * 1024

// PromptAssembler 负责将三层提示词（L1元协议、L2领域规则、L3角色适配）动态编译装配为 LLM 提示词
type PromptAssembler struct{}

// loadTaskDomainPrompt 具备路径安全检查与防溢出截断的领域规则加载器
func (a *PromptAssembler) loadTaskDomainPrompt(promptPath string, allowedBaseDir string) string {
	if promptPath == "" {
		return ""
	}

	cleanPath := filepath.Clean(promptPath)
	if allowedBaseDir != "" {
		cleanBase := filepath.Clean(allowedBaseDir)
		if !strings.HasPrefix(cleanPath, cleanBase) {
			log.Printf("[PromptAssembler] Security Alert: prompt path %s escaped base dir %s", cleanPath, cleanBase)
			return ""
		}
	}

	bytes, err := os.ReadFile(cleanPath)
	if err != nil {
		log.Printf("[PromptAssembler] Warning: failed to read domain prompt %s: %v", cleanPath, err)
		return ""
	}

	content := string(bytes)
	if len(content) > MaxPromptRuleBytes {
		log.Printf("[PromptAssembler] Warning: prompt file %s exceeds %d bytes, truncating", cleanPath, MaxPromptRuleBytes)
		content = content[:MaxPromptRuleBytes] + "\n\n[... 规则文件过长，后续内容已被物理安全截断 ...]"
	}

	return strings.TrimSpace(content)
}

// getDefenseDimensionsForFamily 获取领域族群的默认抗辩维度模板 (基于 models.GetAllDomainFamilies SSOT)
func (a *PromptAssembler) getDefenseDimensionsForFamily(family string) string {
	families := models.GetAllDomainFamilies()
	var targetFamily *models.DomainFamilyInfo
	for i := range families {
		if families[i].Key == family {
			targetFamily = &families[i]
			break
		}
	}
	if targetFamily == nil {
		for i := range families {
			if families[i].Key == models.DomainFamilyComprehensive {
				targetFamily = &families[i]
				break
			}
		}
	}
	if targetFamily == nil || len(targetFamily.DefaultDimensions) == 0 {
		return "1. ContextMitigation: 上下文防护已充分\n2. ScopeExemption: 作用域豁免"
	}

	var sb strings.Builder
	for i, d := range targetFamily.DefaultDimensions {
		key := d.GetKey()
		name := d.GetDisplayName()
		if name != "" && name != key {
			sb.WriteString(fmt.Sprintf("%d. %s (%s): %s\n", i+1, key, name, d.Description))
		} else {
			sb.WriteString(fmt.Sprintf("%d. %s: %s\n", i+1, key, d.Description))
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// BuildHunterPrompt 组装猎手初筛 Prompt
func (a *PromptAssembler) BuildHunterPrompt(ctx *engines.EngineContext, bundle chunker.SemanticBundle) string {
	var sb strings.Builder

	taskName := ctx.TaskTypeName
	if taskName == "" {
		taskName = "代码质量与健壮性审查"
	}

	// 1. Role 与定位
	sb.WriteString(fmt.Sprintf("# Role\n你是一个专精于【%s】的资深静态代码分析专家与审计员。\n\n", taskName))

	// 2. 领域规则注入 (优先加载 analysis_prompt.md)
	baseTasksDir := ""
	if ctx.CodesPath != "" {
		baseTasksDir = models.AppConfig.GetAbsPath("tasks")
	}
	domainRules := a.loadTaskDomainPrompt(ctx.AnalysisPromptPath, baseTasksDir)
	if domainRules != "" {
		sb.WriteString("## 审计聚焦领域与判定标准 (Domain Specifications)\n")
		sb.WriteString(domainRules)
		sb.WriteString("\n\n")
	} else {
		sb.WriteString(fmt.Sprintf("## 审计目标\n请全面检视当前代码分片中关于【%s】的潜在漏洞、异常风险与质量违规。\n\n", taskName))
	}

	// 3. 分片上下文 (宏环境 + 头文件声明 + 负样本)
	if len(bundle.MacroContext) > 0 {
		sb.WriteString("## 构建宏环境定义 (Build Macros Context)\n")
		for k, v := range bundle.MacroContext {
			sb.WriteString(fmt.Sprintf("- `%s = %s`\n", k, v))
		}
		sb.WriteString("\n")
	}

	if bundle.HeaderOutline != "" {
		sb.WriteString("## 核心头文件大纲声明 (Header Outline)\n```cpp\n")
		sb.WriteString(bundle.HeaderOutline)
		sb.WriteString("\n```\n\n")
	}

	if len(bundle.NegativeRules) > 0 {
		sb.WriteString("## 历史负样本与例外规则 (False Positive / Negative Rules)\n")
		sb.WriteString("以下模式已在历史审计中确认安全或被研发标记为免扫，切勿针对它们误报：\n")
		for _, r := range bundle.NegativeRules {
			sb.WriteString(fmt.Sprintf("- %s\n", r))
		}
		sb.WriteString("\n")
	}

	// 4. 标准受控分类白名单约束 (SSOT)
	if len(ctx.AllowedCategories) > 0 {
		sb.WriteString("## 候选缺陷受控分类白名单 (Allowed Categories)\n")
		sb.WriteString("请将发现的问题严格归类为以下标准类别之一，严禁自造分类：\n")
		for _, cat := range ctx.AllowedCategories {
			sb.WriteString(fmt.Sprintf("- `%s`\n", cat))
		}
		sb.WriteString("\n")
	}

	// 5. 待检视文件列表
	sb.WriteString("## 待检视文件列表\n")
	workDir := ctx.WorkDir
	if workDir == "" {
		workDir = ctx.CodesPath
	}
	if workDir != "" {
		sb.WriteString(fmt.Sprintf("当前分析运行目录为代码仓根目录：%s，以下待检视文件路径均为相对于该根目录的相对路径。\n", workDir))
	}
	for _, f := range bundle.AllFiles {
		sb.WriteString(fmt.Sprintf("- `%s`\n", f))
	}

	// 6. 输出格式规范
	sb.WriteString("\n## 任务输出格式规范\n必须以合法纯 JSON 格式输出，不得包裹 Markdown 标记代码块。\n字段 trigger_condition 描述缺陷触发的前置诱因与机理：\n")
	categorySample := "标准缺陷分类"
	if len(ctx.AllowedCategories) > 0 {
		categorySample = ctx.AllowedCategories[0]
	}

	sb.WriteString(fmt.Sprintf(`{
  "candidates": [
    {
      "candidate_id": "H-001",
      "file_path": "src/example.cc",
      "line_range": "42-50",
      "trigger_line": "可疑代码触发语句",
      "scope_symbol": "ClassName::functionName",
      "category": "%s",
      "title": "缺陷简述",
      "code_snippet": "...",
      "trigger_condition": "当特定前置条件满足时触发异常或违规",
      "suspected_trigger": "疑似触发条件与入参"
    }
  ],
  "summary": "初筛发现的可疑线索概述"
}`, categorySample))

	return sb.String()
}

// BuildChallengerPrompt 组装辩护人对抗 Prompt
func (a *PromptAssembler) BuildChallengerPrompt(ctx *engines.EngineContext, bundle chunker.SemanticBundle, hunterOut *HunterOutput) (string, error) {
	var sb strings.Builder

	taskName := ctx.TaskTypeName
	if taskName == "" {
		taskName = "代码审查"
	}

	sb.WriteString(fmt.Sprintf("# Role\n你是一个严谨的代码审查辩护专家 (Code Review Challenger)，专精于【%s】领域。\n你的职责是基于源码上下文、编译宏、前置防御与业务事实，客观求证初筛缺陷是否存在误报或已被妥善防御。\n\n", taskName))

	// 候选缺陷清单序列化
	candBytes, err := json.MarshalIndent(hunterOut.Candidates, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal hunter candidates for challenger prompt: %w", err)
	}

	sb.WriteString("## 猎手提出的候选缺陷清单\n```json\n")
	sb.Write(candBytes)
	sb.WriteString("\n```\n\n")

	// 辩护维度层级优先策略：任务级专有 > 族群默认模板
	sb.WriteString("## 辩护维度\n请从以下角度进行辩护（若确实存在缺陷违规且无法反驳，则如实报告 CHALLENGE_FAILED）：\n")
	if len(ctx.DefenseDimensions) > 0 {
		for i, d := range ctx.DefenseDimensions {
			key := d.GetKey()
			name := d.GetDisplayName()
			if name != "" && name != key {
				sb.WriteString(fmt.Sprintf("%d. %s (%s): %s\n", i+1, key, name, d.Description))
			} else {
				sb.WriteString(fmt.Sprintf("%d. %s: %s\n", i+1, key, d.Description))
			}
		}
	} else {
		sb.WriteString(a.getDefenseDimensionsForFamily(ctx.DomainFamily))
	}
	sb.WriteString("\n\n")

	// 输出格式规范
	dimSample := "Guards"
	if len(ctx.DefenseDimensions) > 0 {
		dimSample = ctx.DefenseDimensions[0].GetKey()
	}

	sb.WriteString(fmt.Sprintf(`## 输出格式规范
必须以合法纯 JSON 格式输出：
{
  "defense_cases": [
    {
      "candidate_id": "H-001",
      "defense_verdict": "DEFENSE_SUCCESSFUL",
      "defense_arguments": [
        {"dimension": "%s", "finding": "基于源码事实证明已被安全防护或属于误报的具体原因"}
      ],
      "mitigating_factors": "免责/防护因素一句话总结",
      "counter_evidence_snippet": "反驳代码片段证据"
    }
  ],
  "summary": "成功辩护概述"
}`, dimSample))

	return sb.String(), nil
}

// BuildJudgePrompt 组装终审法官裁决 Prompt
func (a *PromptAssembler) BuildJudgePrompt(ctx *engines.EngineContext, bundle chunker.SemanticBundle, hunterOut *HunterOutput, challOut *ChallengerOutput) (string, error) {
	var sb strings.Builder

	taskName := ctx.TaskTypeName
	if taskName == "" {
		taskName = "代码缺陷终审"
	}

	sb.WriteString(fmt.Sprintf("# Role\n你是一个资深代码缺陷终审仲裁专家 (Code Audit Arbitrator)，负责【%s】任务。\n你需要综合初筛指控与辩护抗辩事实，依据真实源码上下文独立研判，做出确定性终审裁决。\n\n", taskName))

	// 强制分类白名单约束 (SSOT)
	if len(ctx.AllowedCategories) > 0 {
		sb.WriteString("## 强制分类白名单约束 (Strict Category Constraint)\n")
		sb.WriteString("裁决结论中的 `category` 字段**必须且仅能**从以下受控列表中选取，严禁自造分类：\n")
		for _, c := range ctx.AllowedCategories {
			sb.WriteString(fmt.Sprintf("- `%s`\n", c))
		}
		sb.WriteString("\n")
	}

	// 序列化双方论据 (显式错误处理)
	hBytes, err := json.MarshalIndent(hunterOut.Candidates, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal hunter candidates for judge prompt: %w", err)
	}
	cBytes, err := json.MarshalIndent(challOut.DefenseCases, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal challenger cases for judge prompt: %w", err)
	}

	sb.WriteString("## 控辩双方材料\n### 猎手初筛清单:\n```json\n")
	sb.Write(hBytes)
	sb.WriteString("\n```\n\n### 辩护人对抗意见:\n```json\n")
	sb.Write(cBytes)
	sb.WriteString("\n```\n\n")

	sb.WriteString("## 终审裁决规范 (Verdict Options)\n- CONFIRMED: 缺陷或违规确凿无误\n- REJECTED: 确系误报，予以驳回\n- CONDITIONAL: 条件性触发（如依赖特殊宏开启或极端非默认配置）\n\n")

	categorySample := "标准缺陷分类"
	if len(ctx.AllowedCategories) > 0 {
		categorySample = ctx.AllowedCategories[0]
	}

	sb.WriteString(fmt.Sprintf(`## 输出格式规范
必须以合法纯 JSON 格式输出：
{
  "final_verdicts": [
    {
      "candidate_id": "H-001",
      "verdict": "CONFIRMED",
      "severity_preliminary": "严重",
      "category": "%s",
      "file_path": "src/example.cc",
      "line_number": "42-50",
      "trigger_line": "触发语句",
      "scope_symbol": "ClassName::functionName",
      "title": "缺陷标题",
      "judgement_rationale": "【综合裁决】: 确认存在风险。辩护人提出的抗辩证据不充分，确存在触发路径。",
      "code_snippet": "...",
      "suggestion": "具体的代码级修复方案"
    }
  ]
}`, categorySample))

	return sb.String(), nil
}
