package reconciliation

import (
	"code-shield/models"
	"path/filepath"
	"strings"
)

// InternalFinding 包含物理锚点与指纹特征的内部条目封装
type InternalFinding struct {
	OriginalIndex int
	Payload       models.AnalysisFinding
	NormPath      string
	NormScope     string
	CleanTrigger  string
	StartLine     int
	EndLine       int
	Fingerprint   string
	WeakFP        string
	Category      string
	Severity      string
	ItemUID       string
}

// PrepareInternalFindings 批量提取物理锚点与强指纹
func PrepareInternalFindings(reportID uint, findings []models.AnalysisFinding, repoID uint, taskTypeID uint, repoRoot string) []InternalFinding {
	result := make([]InternalFinding, len(findings))
	uidCounts := make(map[string]int)

	for i := range findings {
		f := findings[i]
		normPath := strings.ToLower(filepath.ToSlash(strings.TrimSpace(f.FilePath)))
		startLine, endLine := ParseLineNumberRange(f.LineNumber)
		cleanTrigger := CleanSourceToken(f.TriggerLine)
		normScope := NormalizeScopeSymbol(f.ScopeSymbol)
		if normScope == "" {
			normScope = filepath.Base(normPath)
		}

		if repoRoot != "" {
			if anchor, err := EnrichSourceAnchor(repoRoot, f.FilePath, f.LineNumber, f.TriggerLine); err == nil {
				startLine = anchor.StartLine
				endLine = anchor.EndLine
				cleanTrigger = anchor.PhysicalToken
				if anchor.NormalizedScope != "" {
					normScope = anchor.NormalizedScope
				}
			}
		}

		fp := CalculateDefectFingerprint(repoID, taskTypeID, f.FilePath, cleanTrigger, normScope)
		weakFP := CalculateWeakScopeFingerprint(repoID, taskTypeID, f.FilePath, normScope)

		baseUID := GenerateItemUID(reportID, fp, 0)
		uidCounts[baseUID]++
		finalUID := baseUID
		if count := uidCounts[baseUID]; count > 1 {
			finalUID = GenerateItemUID(reportID, fp, count-1)
		}

		result[i] = InternalFinding{
			OriginalIndex: i,
			Payload:       f,
			NormPath:      normPath,
			NormScope:     normScope,
			CleanTrigger:  cleanTrigger,
			StartLine:     startLine,
			EndLine:       endLine,
			Fingerprint:   fp,
			WeakFP:        weakFP,
			Category:      SanitizeCategory(f.Category),
			Severity:      f.Severity,
			ItemUID:       finalUID,
		}
	}
	return result
}

// MatchFunnelResult 单次漏斗匹配输出
type MatchFunnelResult struct {
	BaseIndex      int
	CurrentIndex   int
	MatchedTier    int
	Confidence     float64
	Relation       string
	Reason         string
	SeverityRange  string
	SeverityTriage bool
}

// RunDeterministicFunnel 执行 R1~R4 纯规则空间几何漏斗与 1:1 独占认领
func RunDeterministicFunnel(baseList []InternalFinding, currentList []InternalFinding) (map[int]*MatchFunnelResult, map[int]bool, map[int]bool) {
	matchedCurrentMap := make(map[int]*MatchFunnelResult)
	claimedBaseMap := make(map[int]bool)
	matchedBaseMap := make(map[int]bool)

	// 记录每个 Base 条目被哪些 Current 命中，便于识别 SAME_MULTI_VIEW
	baseToCurrents := make(map[int][]int)

	// ── Tier 1: 物理强指纹纳秒级精准匹配 ──
	baseFPMap := make(map[string]int)
	for i, a := range baseList {
		if _, ok := baseFPMap[a.Fingerprint]; !ok {
			baseFPMap[a.Fingerprint] = i
		}
	}

	for j, b := range currentList {
		if aIdx, exists := baseFPMap[b.Fingerprint]; exists {
			sevRange, sevConflict := NormalizeSeverityRange(baseList[aIdx].Severity, b.Severity)
			relation := RelationSame
			if claimedBaseMap[aIdx] {
				// 已被同锚点前序条目认领，此条为同物理锚点多视角上报
				relation = RelationSameMultiView
			} else {
				claimedBaseMap[aIdx] = true
				matchedBaseMap[aIdx] = true
			}

			res := &MatchFunnelResult{
				BaseIndex:      aIdx,
				CurrentIndex:   j,
				MatchedTier:    1,
				Confidence:     1.0,
				Relation:       relation,
				Reason:         "L1 物理强指纹精确匹配 (同一物理行与作用域)",
				SeverityRange:  sevRange,
				SeverityTriage: sevConflict,
			}
			matchedCurrentMap[j] = res
			baseToCurrents[aIdx] = append(baseToCurrents[aIdx], j)
		}
	}

	// ── Tier 2: 同文件 + 同作用域桶 + 物理 Token 相似 / 行距容错 ──
	type scopeKey struct {
		Path  string
		Scope string
	}
	scopeBuckets := make(map[scopeKey][]int)
	for i, a := range baseList {
		k := scopeKey{Path: a.NormPath, Scope: a.NormScope}
		scopeBuckets[k] = append(scopeBuckets[k], i)
		if strings.Contains(a.NormScope, " / ") {
			for _, part := range strings.Split(a.NormScope, " / ") {
				pNorm := NormalizeScopeSymbol(part)
				if pNorm != "" {
					scopeBuckets[scopeKey{Path: a.NormPath, Scope: pNorm}] = append(scopeBuckets[scopeKey{Path: a.NormPath, Scope: pNorm}], i)
				}
			}
		}
	}

	for j, b := range currentList {
		if matchedCurrentMap[j] != nil {
			continue
		}

		k := scopeKey{Path: b.NormPath, Scope: b.NormScope}
		cands := scopeBuckets[k]
		if len(cands) == 0 && strings.Contains(b.NormScope, " / ") {
			for _, part := range strings.Split(b.NormScope, " / ") {
				pNorm := NormalizeScopeSymbol(part)
				if pNorm != "" {
					cands = append(cands, scopeBuckets[scopeKey{Path: b.NormPath, Scope: pNorm}]...)
				}
			}
		}

		bestAIdx := -1
		bestSim := 0.0
		bestTierReason := ""

		for _, aIdx := range cands {
			a := baseList[aIdx]
			lineDiff := b.StartLine - a.StartLine
			if lineDiff < 0 {
				lineDiff = -lineDiff
			}
			tokenSim := CalculateTokenJaccard(b.CleanTrigger, a.CleanTrigger)
			isSameCat := strings.EqualFold(b.Category, a.Category)

			// 准则：(lineDiff <= 2 && tokenSim > 0.5) || tokenSim > 0.75 || (isSameCat && (lineDiff <= 15 || tokenSim > 0.65))
			if ((lineDiff <= 2 && tokenSim > 0.5) || tokenSim > 0.75) || (isSameCat && (lineDiff <= 15 || tokenSim > 0.65)) {
				// 优先选择未被认领的，或未认领中相似度最高的
				if !claimedBaseMap[aIdx] {
					if tokenSim >= bestSim {
						bestSim = tokenSim
						bestAIdx = aIdx
						bestTierReason = "同文件同作用域桶空间聚类命中 (行距与物理 Token 高度相符)"
					}
				} else if bestAIdx == -1 {
					// 备选同锚点多视角
					bestSim = tokenSim
					bestAIdx = aIdx
					bestTierReason = "同文件同作用域同物理锚点多视角上报"
				}
			}
		}

		if bestAIdx != -1 {
			sevRange, sevConflict := NormalizeSeverityRange(baseList[bestAIdx].Severity, b.Severity)
			relation := RelationSame
			if claimedBaseMap[bestAIdx] {
				relation = RelationSameMultiView
			} else {
				claimedBaseMap[bestAIdx] = true
				matchedBaseMap[bestAIdx] = true
			}

			matchedCurrentMap[j] = &MatchFunnelResult{
				BaseIndex:      bestAIdx,
				CurrentIndex:   j,
				MatchedTier:    2,
				Confidence:     0.95,
				Relation:       relation,
				Reason:         bestTierReason,
				SeverityRange:  sevRange,
				SeverityTriage: sevConflict,
			}
			baseToCurrents[bestAIdx] = append(baseToCurrents[bestAIdx], j)
		}
	}

	// ── Tier 3: 同文件 + 行区间重叠或窗口 <= 30 + 物理 Token 相似或同分类 (作用域/枚举符号漂移兜底) ──
	for j, b := range currentList {
		if matchedCurrentMap[j] != nil {
			continue
		}

		bestAIdx := -1
		bestSim := 0.0
		for aIdx, a := range baseList {
			if a.NormPath != b.NormPath {
				continue
			}
			if claimedBaseMap[aIdx] {
				continue
			}

			lineDiff := b.StartLine - a.StartLine
			if lineDiff < 0 {
				lineDiff = -lineDiff
			}
			lineOverlap := (b.StartLine <= a.EndLine && b.EndLine >= a.StartLine)
			tokenSim := CalculateTokenJaccard(b.CleanTrigger, a.CleanTrigger)
			isSameCat := strings.EqualFold(b.Category, a.Category)

			// 准则：(lineOverlap || lineDiff <= 30) && (tokenSim > 0.5 || (isSameCat && tokenSim > 0.35))
			if (lineOverlap || lineDiff <= 30) && (tokenSim > 0.5 || (isSameCat && tokenSim > 0.35)) {
				if tokenSim > bestSim {
					bestSim = tokenSim
					bestAIdx = aIdx
				}
			}
		}

		if bestAIdx != -1 {
			claimedBaseMap[bestAIdx] = true
			matchedBaseMap[bestAIdx] = true
			sevRange, sevConflict := NormalizeSeverityRange(baseList[bestAIdx].Severity, b.Severity)

			matchedCurrentMap[j] = &MatchFunnelResult{
				BaseIndex:      bestAIdx,
				CurrentIndex:   j,
				MatchedTier:    3,
				Confidence:     0.88,
				Relation:       RelationSame,
				Reason:         "同文件行区间重叠与物理 Token 相似 (作用域/枚举重定义漂移容错)",
				SeverityRange:  sevRange,
				SeverityTriage: sevConflict,
			}
			baseToCurrents[bestAIdx] = append(baseToCurrents[bestAIdx], j)
		}
	}

	// ── Tier 4: 同文件 + 物理 Token 强相似 (Jaccard > 0.85) 忽略 scope 漂移 ──
	for j, b := range currentList {
		if matchedCurrentMap[j] != nil {
			continue
		}

		bestAIdx := -1
		bestSim := 0.0
		for aIdx, a := range baseList {
			if a.NormPath != b.NormPath {
				continue
			}
			if claimedBaseMap[aIdx] {
				continue
			}

			tokenSim := CalculateTokenJaccard(b.CleanTrigger, a.CleanTrigger)
			if tokenSim >= 0.85 {
				if tokenSim > bestSim {
					bestSim = tokenSim
					bestAIdx = aIdx
				}
			}
		}

		if bestAIdx != -1 {
			claimedBaseMap[bestAIdx] = true
			matchedBaseMap[bestAIdx] = true
			sevRange, sevConflict := NormalizeSeverityRange(baseList[bestAIdx].Severity, b.Severity)

			matchedCurrentMap[j] = &MatchFunnelResult{
				BaseIndex:      bestAIdx,
				CurrentIndex:   j,
				MatchedTier:    4,
				Confidence:     0.85,
				Relation:       RelationSame,
				Reason:         "同文件核心触发行物理 Token 高度相符 (函数重构导致的 scope 漂移容错)",
				SeverityRange:  sevRange,
				SeverityTriage: sevConflict,
			}
			baseToCurrents[bestAIdx] = append(baseToCurrents[bestAIdx], j)
		}
	}

	return matchedCurrentMap, claimedBaseMap, matchedBaseMap
}
