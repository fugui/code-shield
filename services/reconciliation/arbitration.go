package reconciliation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"code-shield/services/invoker"
)

// ArbitrationResult R5 仲裁结果
type ArbitrationResult struct {
	BaseIndex      int
	CurrentIndex   int
	Relation       string
	Confidence     float64
	Reason         string
	SeverityRange  string
	SeverityTriage bool
}

// ClusterTemplateFamilies 执行 R6 跨文件模板族聚类
// 跨文件但具备相同函数名/作用域/分类、或触发语句强相似的缺陷，赋予相同 template_family_id，但不合并实体
func ClusterTemplateFamilies(items []SynthesisItem) []SynthesisItem {
	type famKey struct {
		NormScope string
		Category  string
	}
	groups := make(map[famKey][]int)

	for i, it := range items {
		normScope := NormalizeScopeSymbol(it.Payload.ScopeSymbol)
		cat := strings.TrimSpace(it.Payload.Category)
		if normScope != "" && cat != "" {
			k := famKey{NormScope: normScope, Category: cat}
			groups[k] = append(groups[k], i)
		}
	}

	result := make([]SynthesisItem, len(items))
	copy(result, items)

	for k, indices := range groups {
		// 只有在涉及 ≥ 2 个不同文件时，才成立为跨文件模板族
		files := make(map[string]bool)
		for _, idx := range indices {
			files[result[idx].FilePath] = true
		}

		if len(files) >= 2 {
			rawID := fmt.Sprintf("fam:%s|cat:%s", k.NormScope, k.Category)
			h := sha256.Sum256([]byte(rawID))
			famID := "fam-" + hex.EncodeToString(h[:])[:8]

			for _, idx := range indices {
				result[idx].TemplateFamilyID = famID
				// 若尚未设定 recon_relation，标注为 TEMPLATE
				if result[idx].ReconRelation == "" {
					result[idx].ReconRelation = RelationTemplate
				}
			}
		}
	}

	// 模式 2：类名相似聚类 (如 SFMCMCPManager 与 SFPSVIPMCPManager)
	type classKey struct {
		ClassSuffix string
		Category    string
	}
	classGroups := make(map[classKey][]int)
	for i, it := range result {
		baseName := strings.ToLower(filepath.Base(it.FilePath))
		cat := strings.TrimSpace(it.Payload.Category)
		if strings.Contains(baseName, "mcpmanager") {
			k := classKey{ClassSuffix: "mcpmanager", Category: cat}
			classGroups[k] = append(classGroups[k], i)
		} else if strings.Contains(baseName, ".proto") {
			k := classKey{ClassSuffix: "proto", Category: cat}
			classGroups[k] = append(classGroups[k], i)
		}
	}

	for k, indices := range classGroups {
		files := make(map[string]bool)
		for _, idx := range indices {
			files[result[idx].FilePath] = true
		}
		if len(files) >= 2 {
			rawID := fmt.Sprintf("fam-class:%s|cat:%s", k.ClassSuffix, k.Category)
			h := sha256.Sum256([]byte(rawID))
			famID := "fam-" + hex.EncodeToString(h[:])[:8]
			for _, idx := range indices {
				if result[idx].TemplateFamilyID == "" {
					result[idx].TemplateFamilyID = famID
					if result[idx].ReconRelation == "" {
						result[idx].ReconRelation = RelationTemplate
					}
				}
			}
		}
	}

	// 模式 3：跨文件相同核心物理 Token (CleanTrigger) 与同分类聚类
	type tokenKey struct {
		CleanToken string
		Category   string
	}
	tokenGroups := make(map[tokenKey][]int)
	for i, it := range result {
		clnToken := CleanSourceToken(it.Payload.TriggerLine)
		cat := strings.TrimSpace(it.Payload.Category)
		if len(clnToken) >= 8 && cat != "" {
			k := tokenKey{CleanToken: clnToken, Category: cat}
			tokenGroups[k] = append(tokenGroups[k], i)
		}
	}

	for k, indices := range tokenGroups {
		files := make(map[string]bool)
		for _, idx := range indices {
			files[result[idx].FilePath] = true
		}
		if len(files) >= 2 {
			rawID := fmt.Sprintf("fam-token:%s|cat:%s", k.CleanToken, k.Category)
			h := sha256.Sum256([]byte(rawID))
			famID := "fam-" + hex.EncodeToString(h[:])[:8]
			for _, idx := range indices {
				if result[idx].TemplateFamilyID == "" {
					result[idx].TemplateFamilyID = famID
					if result[idx].ReconRelation == "" {
						result[idx].ReconRelation = RelationTemplate
					}
				}
			}
		}
	}

	return result
}

type aiArbitrationResponse struct {
	IsSameDefect bool    `json:"is_same_defect"`
	Confidence   float64 `json:"confidence"`
	Reason       string  `json:"reason"`
	PrimaryUID   string  `json:"primary_uid"`
}

func cleanArbitrationJSON(raw []byte) []byte {
	s := strings.TrimSpace(string(raw))
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	if !strings.HasPrefix(s, "{") {
		if start := strings.Index(s, "{"); start != -1 {
			if end := strings.LastIndex(s, "}"); end > start {
				s = s[start : end+1]
			}
		}
	}
	return []byte(s)
}

func arbitratePairWithAI(inv invoker.AIInvoker, base InternalFinding, curr InternalFinding, repoRoot ...string) (*ArbitrationResult, error) {
	if inv == nil {
		return nil, fmt.Errorf("no AI invoker provided")
	}

	workDir := ""
	if len(repoRoot) > 0 && repoRoot[0] != "" {
		workDir = repoRoot[0]
	}

	prompt := fmt.Sprintf(`你是一名顶尖的代码静态分析与缺陷对账仲裁专家。
请判断以下两份针对文件 %q 的审查条目，是否在描述**完全相同的底层代码缺陷/逻辑隐患**（即便两者的分类名称、行号位置或严重级别不同）。

【历史存量缺陷 (Base)】
- UID: %s
- 位置: 行 %s (作用域: %s)
- 分类/严重度: %s / %s
- 标题: %s
- 描述: %s
- 代码片段:
%s

【本轮新报缺陷 (Current)】
- UID: %s
- 位置: 行 %s (作用域: %s)
- 分类/严重度: %s / %s
- 标题: %s
- 描述: %s
- 代码片段:
%s

【判决标准】
1. 若两个条目根因相同（例如：均指向同一递归解包路径缺少深度/环控制导致的栈溢出，仅一个抓在入口、一个抓在递归调用处），必须判定为 true。
2. 若属于同一函数中两个完全独立、互不相干的 bug（例如：一个是空指针，另一个是除以零），判定为 false。
3. 请严格输出如下 JSON 格式，禁止包含多余 Markdown 标记：
{
  "is_same_defect": true,
  "confidence": 0.95,
  "reason": "一句话阐明判定依据",
  "primary_uid": "%s"
}`,
		base.NormPath,
		base.ItemUID, base.Payload.LineNumber, base.NormScope, base.Category, base.Severity, base.Payload.Title, base.Payload.Detail, base.Payload.CodeSnippet,
		curr.ItemUID, curr.Payload.LineNumber, curr.NormScope, curr.Category, curr.Severity, curr.Payload.Title, curr.Payload.Detail, curr.Payload.CodeSnippet,
		curr.ItemUID,
	)

	tmpFile, err := os.CreateTemp("", "recon-r5-*.json")
	if err != nil {
		return nil, err
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// 根据用户明确指示，LLM 较慢，超时设置为 5 分钟 (300 秒)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	req := invoker.AIRequest{
		ParentContext:  ctx,
		WorkDir:        workDir,
		PromptMsg:      prompt,
		OutputPath:     tmpPath,
		TimeoutMin:     5,
		ResponseFormat: "json",
		WorkContext: &invoker.LLMWorkContext{
			Stage:   "对账仲裁: R5 AI 残差语义判决",
			SubTask: fmt.Sprintf("判决缺陷对齐 (%s)", base.NormPath),
		},
	}

	if err := inv.Invoke(req); err != nil {
		return nil, err
	}

	outBytes, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, err
	}

	cleaned := cleanArbitrationJSON(outBytes)
	var resp aiArbitrationResponse
	if err := json.Unmarshal(cleaned, &resp); err != nil {
		return nil, err
	}

	if resp.IsSameDefect && resp.Confidence >= 0.70 {
		sevRange, sevConflict := NormalizeSeverityRange(base.Severity, curr.Severity)
		reason := "R5 AI 语义残差仲裁确认: " + resp.Reason
		if resp.Reason == "" {
			reason = "R5 AI 语义残差仲裁确认 (同一底层缺陷)"
		}
		return &ArbitrationResult{
			BaseIndex:      base.OriginalIndex,
			CurrentIndex:   curr.OriginalIndex,
			Relation:       RelationSameSemantic,
			Confidence:     resp.Confidence,
			Reason:         reason,
			SeverityRange:  sevRange,
			SeverityTriage: sevConflict,
		}, nil
	}

	return nil, nil
}

// ArbitrateResiduals 执行 R5 单文件残差集合对齐与熔断保护
// 仅当同文件内残差合计 <= 10 时触发；超时/错误或超过限制时安全降级
func ArbitrateResiduals(baseResiduals []InternalFinding, currentResiduals []InternalFinding, inv invoker.AIInvoker, repoRootOpt ...string) []ArbitrationResult {
	if len(baseResiduals) == 0 || len(currentResiduals) == 0 {
		return nil
	}

	repoRoot := ""
	if len(repoRootOpt) > 0 {
		repoRoot = repoRootOpt[0]
	}

	// 1. 成本与复杂度熔断断路器：残差合计超过 10 条视为大规模重构，禁止触发
	total := len(baseResiduals) + len(currentResiduals)
	if total > 10 {
		return nil
	}

	var results []ArbitrationResult
	claimedBase := make(map[int]bool)
	claimedCurrent := make(map[int]bool)

	// 2. 若配置了 AI 驱动，优先尝试同文件残差对的 AI 语义仲裁
	if inv != nil {
		for _, curr := range currentResiduals {
			if claimedCurrent[curr.OriginalIndex] {
				continue
			}
			for _, base := range baseResiduals {
				if claimedBase[base.OriginalIndex] {
					continue
				}
				if base.NormPath != curr.NormPath {
					continue
				}

				// 仅当位于同一作用域，或者行距 <= 60 时，进入 AI 仲裁候选
				lineDiff := curr.StartLine - base.StartLine
				if lineDiff < 0 {
					lineDiff = -lineDiff
				}
				scopeMatch := (base.NormScope != "" && base.NormScope == curr.NormScope)
				if scopeMatch || lineDiff <= 60 {
					res, err := arbitratePairWithAI(inv, base, curr, repoRoot)
					if err == nil && res != nil {
						claimedBase[base.OriginalIndex] = true
						claimedCurrent[curr.OriginalIndex] = true
						results = append(results, *res)
						break
					}
				}
			}
		}
	}

	// 3. 启发式局部拓扑对齐兜底 (PROBABLE) - 针对未被 AI 认领的剩余残差
	for _, curr := range currentResiduals {
		if claimedCurrent[curr.OriginalIndex] {
			continue
		}
		bestBaseIdx := -1
		bestSim := 0.0

		for _, base := range baseResiduals {
			if claimedBase[base.OriginalIndex] {
				continue
			}
			if base.NormPath != curr.NormPath {
				continue
			}

			tokenSim := CalculateTokenJaccard(base.CleanTrigger, curr.CleanTrigger)
			isSameCat := strings.EqualFold(base.Category, curr.Category)
			if tokenSim > 0.4 || (isSameCat && tokenSim > 0.25) {
				if tokenSim > bestSim {
					bestSim = tokenSim
					bestBaseIdx = base.OriginalIndex
				}
			}
		}

		if bestBaseIdx != -1 && bestSim > 0.35 {
			claimedBase[bestBaseIdx] = true
			claimedCurrent[curr.OriginalIndex] = true
			results = append(results, ArbitrationResult{
				BaseIndex:    bestBaseIdx,
				CurrentIndex: curr.OriginalIndex,
				Relation:     RelationProbable,
				Confidence:   0.70,
				Reason:       "同文件残差集合对齐 (启发式局部拓扑匹配，建议人工复核)",
			})
		}
	}

	return results
}
