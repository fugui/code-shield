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

	return a.stripLegacyOutputContracts(strings.TrimSpace(content), cleanPath)
}

// stripLegacyOutputContracts removes hand-written JSON/output sections from task
// prompt files during migration. Domain prompts must not own output contracts.
func (a *PromptAssembler) stripLegacyOutputContracts(content string, source string) string {
	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	stripped := false
	skipping := false
	skipLevel := 0

	isLegacyHeading := func(line string) bool {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			return false
		}
		title := strings.TrimLeft(trimmed, "#")
		title = strings.TrimSpace(title)
		return strings.Contains(title, "输出格式") ||
			strings.Contains(title, "JSON 格式") ||
			strings.Contains(title, "标准 JSON") ||
			strings.Contains(title, "输出通道")
	}

	headingLevel := func(line string) int {
		trimmed := strings.TrimSpace(line)
		level := 0
		for _, char := range trimmed {
			if char != '#' {
				break
			}
			level++
		}
		return level
	}

	for _, line := range lines {
		if !skipping && isLegacyHeading(line) {
			skipping = true
			skipLevel = headingLevel(line)
			stripped = true
			continue
		}
		if skipping {
			if strings.TrimSpace(line) != "" && strings.HasPrefix(strings.TrimSpace(line), "#") && headingLevel(line) <= skipLevel {
				skipping = false
				kept = append(kept, line)
			}
			continue
		}
		kept = append(kept, line)
	}

	if stripped {
		log.Printf("[PromptAssembler] Stripped legacy output contract from task prompt %s", source)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
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

// defenseDimensionKeys exposes the exact dimension enum used by the prompt and
// validator. Task-specific dimensions always override family defaults.
func (a *PromptAssembler) defenseDimensionKeys(ctx *engines.EngineContext) []string {
	if len(ctx.DefenseDimensions) > 0 {
		keys := make([]string, 0, len(ctx.DefenseDimensions))
		for _, dimension := range ctx.DefenseDimensions {
			if key := dimension.GetKey(); strings.TrimSpace(key) != "" {
				keys = append(keys, key)
			}
		}
		return keys
	}

	content := a.getDefenseDimensionsForFamily(ctx.DomainFamily)
	keys := make([]string, 0)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		dot := strings.Index(line, ".")
		if !strings.HasPrefix(line, "1.") && (dot <= 0 || dot > 4) {
			continue
		}
		rest := strings.TrimSpace(line[dot+1:])
		if colon := strings.IndexAny(rest, ":："); colon > 0 {
			rest = strings.TrimSpace(rest[:colon])
		}
		if rest != "" {
			keys = append(keys, rest)
		}
	}
	return keys
}

// BuildHunterPrompt 组装猎手初筛 Prompt
func (a *PromptAssembler) BuildHunterPrompt(ctx *engines.EngineContext, bundle chunker.SemanticBundle) string {
	var sb strings.Builder

	taskName := ctx.TaskTypeName
	if taskName == "" {
		taskName = "代码质量与健壮性审查"
	}

	mode := ctx.EngineMode
	if mode == "" {
		// PromptAssembler is used by DebateEngine. An empty mode means a caller
		// has not yet populated the new EngineContext field; default to debate.
		mode = "debate_full"
	}
	if _, profileErr := OutputProfileForEngine(mode); profileErr != nil {
		return ""
	}

	// 1. Role 与定位
	sb.WriteString(fmt.Sprintf("# Role\n你是一个专精于【%s】的资深静态代码分析专家与审计员。\n\n", taskName))

	sb.WriteString("## Invocation Scope\n")
	sb.WriteString("本次调用是一个 semantic bundle 的分片审查，不是全仓终审。\n")
	sb.WriteString("你必须只检查 Target Files 中列出的文件。\n")
	sb.WriteString("可以读取同仓相关文件以确认调用链、类型定义和宏含义。\n")
	sb.WriteString("不得为未列出的业务文件生成主候选。\n")
	sb.WriteString("不得声称已经完成整个仓库审查。\n\n")

	// 2. 领域规则注入 (优先加载 analysis_prompt.md)
	baseTasksDir := ""
	if ctx.CodesPath != "" {
		baseTasksDir = models.AppConfig.GetAbsPath("tasks")
	}
	domainRules := a.loadTaskDomainPromptContent(ctx.AnalysisPromptContent)
	if domainRules == "" && ctx.AnalysisPromptPath != "" {
		domainRules = a.loadTaskDomainPrompt(ctx.AnalysisPromptPath, baseTasksDir)
	}
	if domainRules != "" {
		sb.WriteString("## 审计聚焦领域与判定标准 (Domain Specifications)\n")
		sb.WriteString(domainRules)
		sb.WriteString("\n\n")
	} else {
		sb.WriteString(fmt.Sprintf("## 审计目标\n请全面检视当前代码分片中关于【%s】的潜在漏洞、异常风险与质量违规。\n\n", taskName))
	}

	// 3. 分片上下文 (宏环境 + 头文件声明 + 负样本)
	if len(bundle.MacroContext) > 0 {
		sb.WriteString("## Build Context\n")
		sb.WriteString("以下值来自构建系统提取，可能包含未展开变量；它们不是已确认的源码宏值。\n")
		sb.WriteString("只有当目标源码真实使用这些变量或宏，且影响可从上下文证明时，才能作为判定依据。\n")
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
		sb.WriteString("`category` 必须且只能使用以下值；禁止自造分类，禁止省略二级分类。\n")
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

	// 6. Output contract is rendered from code, not copied from a task file.
	contract, contractErr := ContractForStageWithTaxonomy(mode, "hunter", ctx.AllowedCategories, ctx.Taxonomy)
	if contractErr != nil {
		// The EngineContext mode is authoritative. A bad mode is a programming
		// error and must not silently fall back to another artifact schema.
		log.Printf("[PromptAssembler] Invalid engine mode %q: %v", ctx.EngineMode, contractErr)
	}
	sb.WriteString("\n")
	sb.WriteString(RenderOutputContract(contract))

	return sb.String()
}

// loadTaskDomainPromptContent applies the same cleanup and size limits to prompt
// content that was verified while loading the execution snapshot.
func (a *PromptAssembler) loadTaskDomainPromptContent(content string) string {
	if content == "" {
		return ""
	}
	if len(content) > MaxPromptRuleBytes {
		log.Printf("[PromptAssembler] Warning: prompt content exceeds %d bytes, truncating", MaxPromptRuleBytes)
		content = content[:MaxPromptRuleBytes] + "\n\n[... 规则文件过长，后续内容已被物理安全截断 ...]"
	}
	return a.stripLegacyOutputContracts(strings.TrimSpace(content), "execution snapshot")
}

// BuildChallengerPrompt 组装辩护人对抗 Prompt
func (a *PromptAssembler) BuildChallengerPrompt(
	ctx *engines.EngineContext,
	bundle chunker.SemanticBundle,
	hunterOut *HunterOutput,
	evidencePacks []JudgeCaseEvidencePack,
) (string, error) {
	var sb strings.Builder

	taskName := ctx.TaskTypeName
	if taskName == "" {
		taskName = "代码审查"
	}

	sb.WriteString(fmt.Sprintf("# Role\n你是一个严谨的代码审查辩护专家 (Code Review Challenger)，专精于【%s】领域。\n你的职责是基于源码上下文、编译宏、前置防御与业务事实，客观求证初筛缺陷是否存在误报或已被妥善防御。\n\n", taskName))

	sb.WriteString("## Invocation Scope\n")
	sb.WriteString("本次调用只辩护当前 semantic bundle 中列出的候选缺陷。\n")
	sb.WriteString("这是分片抗辩，不是全仓审查。\n")
	sb.WriteString("你可以读取 Target Files 及其直接相关文件，但输出证据必须来自当前候选的 Evidence Pack；不得修改业务源码。\n")
	sb.WriteString("不得为候选清单之外的业务文件生成 defense_case。\n\n")

	sb.WriteString("## Target Files\n")
	if ctx.CodesPath != "" {
		sb.WriteString(fmt.Sprintf("当前代码仓根目录：%s\n", ctx.CodesPath))
	}
	if len(bundle.AllFiles) == 0 {
		sb.WriteString("- （本次分片未显式提供目标文件列表）\n")
	}
	for _, file := range bundle.AllFiles {
		sb.WriteString(fmt.Sprintf("- `%s`\n", file))
	}
	sb.WriteString("\n")

	// 候选指控使用规范化视图，避免 attack_hypothesis / suspected_trigger 重复强化。
	candidateViews := NewJudgeCandidateViews(hunterOut.Candidates)
	claimBytes, err := json.MarshalIndent(candidateViews, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal candidate views for challenger prompt: %w", err)
	}

	sb.WriteString("## Candidate Claims (Unverified)\n```json\n")
	sb.Write(claimBytes)
	sb.WriteString("\n```\n\n")

	evidenceBytes, err := json.MarshalIndent(evidencePacks, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal evidence packs for challenger prompt: %w", err)
	}

	sb.WriteString("## Evidence Pack\n```json\n")
	sb.Write(evidenceBytes)
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Claims Are Not Facts\n")
	sb.WriteString("Hunter 的 Claim 是候选指控，不是已验证事实。\n")
	sb.WriteString("只有能定位到真实源码、调用链或构建上下文的证据才能作为成功抗辩依据。\n\n")

	sb.WriteString("## Defense Evidence Rules\n")
	sb.WriteString("1. 证据优先级：真实源码 > 调用链 / 生命周期证据 > 历史负样本 > Hunter 假设。\n")
	sb.WriteString("2. `DEFENSE_SUCCESSFUL` / `DEFENSE_PARTIAL` 的每个 defense_argument 必须携带 evidence。\n")
	sb.WriteString("3. evidence 的 path 必须在 Evidence Pack 的 allowed_evidence_paths 内；优先复制已有 evidence_id，snippet 必须复制真实源码，line_range 必须是单一区间。\n")
	sb.WriteString("4. 如果抗辩依据是“没有实际调用点”，必须引用 Evidence Pack 中的 probe_id 字段。\n")
	sb.WriteString("5. 无法找到真实源码证据时，必须返回 `CHALLENGE_FAILED`，不得编造反证。\n\n")

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
	mode := ctx.EngineMode
	if mode == "" {
		mode = "debate_full"
	}
	challengerContract := newChallengerContract(a.defenseDimensionKeys(ctx))
	sb.WriteString("\n")
	sb.WriteString(RenderOutputContract(challengerContract))

	expectedIDs := make([]string, 0, len(hunterOut.Candidates))
	for _, candidate := range hunterOut.Candidates {
		expectedIDs = append(expectedIDs, candidate.CandidateID)
	}
	if len(expectedIDs) > 0 {
		sb.WriteString(fmt.Sprintf("\n本批次必须为以下每个 candidate_id 各输出恰好一条 defense_case：%s。\n", strings.Join(expectedIDs, ", ")))
	}

	return sb.String(), nil
}

// BuildJudgePrompt 组装终审法官裁决 Prompt
func (a *PromptAssembler) BuildJudgePrompt(
	ctx *engines.EngineContext,
	bundle chunker.SemanticBundle,
	hunterOut *HunterOutput,
	challOut *ChallengerOutput,
	evidencePacks []JudgeCaseEvidencePack,
) (string, error) {
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

	sb.WriteString("## Invocation Scope\n")
	sb.WriteString("本次调用只裁决当前 semantic bundle 中列出的候选缺陷。\n")
	sb.WriteString("这是分片终审，不是全仓审查。\n")
	sb.WriteString("你可以读取 Target Files 及其直接相关文件，但输出证据必须来自当前候选的 Evidence Pack；不得修改业务源码。\n")
	sb.WriteString("不得为候选清单之外的业务文件生成 final_verdict。\n\n")

	sb.WriteString("## Target Files\n")
	if ctx.CodesPath != "" {
		sb.WriteString(fmt.Sprintf("当前代码仓根目录：%s\n", ctx.CodesPath))
	}
	if len(bundle.AllFiles) == 0 {
		sb.WriteString("- （本次分片未显式提供目标文件列表）\n")
	}
	for _, file := range bundle.AllFiles {
		sb.WriteString(fmt.Sprintf("- `%s`\n", file))
	}
	sb.WriteString("\n")

	evidenceBytes, err := json.MarshalIndent(evidencePacks, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal judge evidence packs: %w", err)
	}

	sb.WriteString("## Evidence Pack\n```json\n")
	sb.Write(evidenceBytes)
	sb.WriteString("\n```\n\n")

	sb.WriteString("## Claims Are Not Facts\n")
	sb.WriteString("Hunter 的 Claim 是候选指控，不是已验证事实。\n")
	sb.WriteString("Challenger 的 Defense 是抗辩叙事，也不是已验证事实。\n")
	sb.WriteString("只有能定位到真实源码、调用链或构建上下文的证据才能作为裁决基础。\n\n")

	sb.WriteString("## Evidence Rules\n")
	sb.WriteString("1. 证据优先级：真实源码 > 调用链 / 生命周期证据 > Challenger 反证 > Hunter 假设。\n")
	sb.WriteString("2. 优先引用 Evidence Pack 中已有 Evidence 的 `evidence_id`；服务端会使用该 ID 绑定的 path、line_range 和 snippet 复核证据。\n")
	sb.WriteString("3. Challenger 的每个辩护结论必须有 file:line 或真实代码片段支撑；否则视为未证明。\n")
	sb.WriteString("4. `CONFIRMED` 必须证明：缺陷点真实存在；存在可达触发路径；后果符合缺陷分类；没有有效前置防御。\n")
	sb.WriteString("5. `CONDITIONAL` 必须写清触发条件，例如宏、部署模式、调用线程、特殊输入或历史兼容行为。\n")
	sb.WriteString("6. `REJECTED` 必须说明误报原因，例如外层锁、调用约束、框架托管、协议保证、宏隔离或无实际调用点。\n")
	sb.WriteString("7. 无法证明触发路径时，禁止输出 `CONFIRMED`。\n")
	sb.WriteString("8. 不得为了保持输出完整而编造源码。\n\n")

	sb.WriteString("## Verdict Rules\n")
	sb.WriteString("- `CONFIRMED`: 有真实源码和可达触发路径，且没有有效防护。\n")
	sb.WriteString("- `CONDITIONAL`: 缺陷存在，但依赖特殊宏、部署模式、极端输入、历史兼容行为或非默认线程模型。\n")
	sb.WriteString("- `REJECTED`: 缺陷点不存在、不可达、已有有效防护，或属于历史豁免/负样本。\n\n")

	sb.WriteString("## Severity Rubric\n")
	sb.WriteString("- `致命`: 高概率导致进程崩溃、数据破坏、命令执行、权限突破或不可恢复状态。\n")
	sb.WriteString("- `严重`: 在正常或常见路径下会导致错误行为、资源耗尽、数据竞争或显著安全风险。\n")
	sb.WriteString("- `一般`: 在特定条件或维护路径下会引发缺陷、误用或可观测异常。\n")
	sb.WriteString("- `建议`: 不直接导致运行时缺陷，但会降低可维护性、可读性或演进安全。\n\n")

	mode := ctx.EngineMode
	if mode == "" {
		mode = "debate_full"
	}
	judgeContract, contractErr := ContractForStageWithTaxonomy(mode, "judge", ctx.AllowedCategories, ctx.Taxonomy)
	if contractErr != nil {
		log.Printf("[PromptAssembler] Failed to load judge contract: %v", contractErr)
	}
	sb.WriteString("\n")
	sb.WriteString(RenderOutputContract(judgeContract))

	expectedIDs := make([]string, 0, len(hunterOut.Candidates))
	for _, candidate := range hunterOut.Candidates {
		expectedIDs = append(expectedIDs, candidate.CandidateID)
	}
	if len(expectedIDs) > 0 {
		sb.WriteString(fmt.Sprintf("\n本批次必须为以下每个 candidate_id 各输出恰好一条 final_verdict：%s。\n", strings.Join(expectedIDs, ", ")))
	}

	return sb.String(), nil
}
