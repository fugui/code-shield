package reconciliation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
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

// ArbitrateResiduals 执行 R5 单文件残差集合对齐与熔断保护
// 仅当同文件内残差合计 <= 10 时触发；超时/错误或超过限制时安全降级
func ArbitrateResiduals(baseResiduals []InternalFinding, currentResiduals []InternalFinding) []ArbitrationResult {
	if len(baseResiduals) == 0 || len(currentResiduals) == 0 {
		return nil
	}

	// 1. 成本与复杂度熔断断路器：残差合计超过 10 条视为大规模重构，禁止触发
	total := len(baseResiduals) + len(currentResiduals)
	if total > 10 {
		return nil
	}

	// 2. 尝试基于局部物理拓扑与相似度进行启发式残差二部图对齐 (降级为 PROBABLE)
	var results []ArbitrationResult
	claimedBase := make(map[int]bool)

	for _, curr := range currentResiduals {
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
