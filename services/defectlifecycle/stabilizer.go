package defectlifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"code-shield/models"
	"code-shield/services/coverage"
)

// StabilizeFindingsInput 端侧定桩输入
type StabilizeFindingsInput struct {
	RepoRoot string                   // 本地源码绝对路径 (仅 Runner 持有)
	Findings []models.AnalysisFinding // AI 原始检出结果
}

// StabilizedFindingDTO 端侧定桩输出 DTO
type StabilizedFindingDTO struct {
	OriginalIndex    int              `json:"original_index"`
	FindingID        string           `json:"finding_id"`
	NormPath         string           `json:"norm_path"`          // 归一化相对路径
	ScopeSymbol      string           `json:"scope_symbol"`       // 规范化函数符号
	DefectClassMajor string           `json:"defect_class_major"` // 受控缺陷主类 (如 NPD)
	TargetEntity     string           `json:"target_entity"`      // 核心变量或合成受体 (如 ptr 或 SINK:exec)
	StmtAnchorHash   string           `json:"stmt_anchor_hash"`   // 核心语句 Token 哈希 (绝对静止二级判歧键)
	CoreSlotKey      string           `json:"core_slot_key"`      // 一级主槽位哈希 (不含 StmtAnchorHash)
	SlotKey          string           `json:"slot_key"`           // 二级精准判歧槽位哈希 (含 StmtAnchorHash)
	ScopeBodyHash    string           `json:"scope_body_hash"`    // 所在作用域代码块 SHA-256（防相邻函数污染）
	SourceAnchored   bool             `json:"source_anchored"`    // 是否成功读取并锚定物理源码文件
	Confidence       AnchorConfidence `json:"confidence"`         // HIGH / MEDIUM / LOW

	// ── 自包含上下文快照（中央无需挂载代码仓即可纯内存判定与 AI 终审）──
	TriggerLine string `json:"trigger_line"` // 校准后的单行触发语句
	StmtWindow  string `json:"stmt_window"`  // 重构后的多行逻辑语句窗口
	CleanToken  string `json:"clean_token"`  // 物理 Token
	CodeSnippet string `json:"code_snippet"` // 局部窗口代码切片 [L-5, L+5]
	LineStart   int    `json:"line_start"`   // 物理起始行
	LineEnd     int    `json:"line_end"`     // 物理结束行
	Title       string `json:"title"`
	Category    string `json:"category"`
	Severity    string `json:"severity"`
	Suggestion  string `json:"suggestion"`
	Detail      string `json:"detail"`
}

// IngestionStabilizer 端侧定桩组件标准契约接口
type IngestionStabilizer interface {
	Stabilize(input StabilizeFindingsInput) ([]StabilizedFindingDTO, error)
}

// DefaultIngestionStabilizer 端侧定桩默认纯 Go 实现
type DefaultIngestionStabilizer struct{}

// NewIngestionStabilizer 创建端侧物理定桩器
func NewIngestionStabilizer() IngestionStabilizer {
	return &DefaultIngestionStabilizer{}
}

// ComputeStmtAnchorHash 计算绝对静止的语句锚点哈希 (SHA-256 前 16 位十六进制)
func ComputeStmtAnchorHash(cleanToken string) string {
	if cleanToken == "" {
		return ""
	}
	h := sha256.Sum256([]byte(cleanToken))
	return hex.EncodeToString(h[:8]) // 8 字节 = 16 字符
}

// ComputeCoreSlotKey 计算一级核心主槽位哈希
func ComputeCoreSlotKey(normPath, scopeSymbol, defectClassMajor, targetEntity string) string {
	seed := strings.ToLower(normPath) + "|" + scopeSymbol + "|" + defectClassMajor + "|" + targetEntity
	h := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(h[:16]) // 16 字节 = 32 字符
}

// ComputePreciseSlotKey 计算二级精准判歧槽位哈希
func ComputePreciseSlotKey(coreSlotKey, stmtAnchorHash string) string {
	if stmtAnchorHash == "" {
		return coreSlotKey
	}
	seed := coreSlotKey + "|" + stmtAnchorHash
	h := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(h[:16])
}

// Stabilize 执行物理定桩与降噪清洗
func (s *DefaultIngestionStabilizer) Stabilize(input StabilizeFindingsInput) ([]StabilizedFindingDTO, error) {
	fileLinesCache := make(map[string][]string)

	loadLines := func(relPath string) []string {
		if lines, ok := fileLinesCache[relPath]; ok {
			return lines
		}
		if input.RepoRoot == "" {
			return nil
		}
		fullPath := filepath.Join(input.RepoRoot, relPath)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			fileLinesCache[relPath] = nil
			return nil
		}
		rawLines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
		fileLinesCache[relPath] = rawLines
		return rawLines
	}

	result := make([]StabilizedFindingDTO, len(input.Findings))

	for i, f := range input.Findings {
		relPath := coverage.NormalizeRepoPath(input.RepoRoot, f.FilePath)
		normPath := NormalizePath(input.RepoRoot, f.FilePath)

		lines := loadLines(relPath)

		// 1. 双轨行号校准
		rawLineStart, rawLineEnd := ParseLineNumberRange(f.LineNumber)
		calibratedLine := CalibratePhysicalLine(lines, rawLineStart, f.TriggerLine)
		if calibratedLine <= 0 {
			calibratedLine = 1
		}
		if len(lines) > 0 && calibratedLine > len(lines) {
			calibratedLine = len(lines)
		}

		lineStart := calibratedLine
		lineEnd := calibratedLine
		if rawLineEnd > rawLineStart {
			lineEnd = lineStart + (rawLineEnd - rawLineStart)
		}
		if len(lines) > 0 && lineEnd > len(lines) {
			lineEnd = len(lines)
		}

		// 2. 作用域与作用域代码块哈希提取（单次读盘缓存完成，消除重复 I/O）
		scopeSymbol := strings.TrimSpace(NormalizeScopeSymbol(f.ScopeSymbol))
		scopeBodyHash := ""
		if len(lines) > 0 && calibratedLine > 0 {
			detectedScope, scopeBody := ExtractScopeAndBodyFromLines(relPath, lines, calibratedLine)
			if scopeSymbol == "" {
				scopeSymbol = NormalizeScopeSymbol(detectedScope)
			}
			if scopeBody != "" {
				h := sha256.Sum256([]byte(CleanSourceToken(scopeBody)))
				scopeBodyHash = hex.EncodeToString(h[:])
			}
		}

		// 3. 字典归约受控缺陷大类
		defectClassMajor := ReduceControlledDefectClass(f.Category, f.Title)

		// 4. 多行物理语句窗口拼接与纯 Go 词法受体抽取
		var stmtWindow string
		if len(lines) > 0 {
			stmtWindow = ReconstructStatementWindow(lines, calibratedLine)
		} else {
			stmtWindow = strings.TrimSpace(f.TriggerLine)
			if stmtWindow == "" {
				stmtWindow = firstCodeStatement(f)
			}
		}

		targetEntity, conf := ExtractPhysicalTargetEntity(stmtWindow, f.TargetSymbol, defectClassMajor)
		if targetEntity == "" && strings.TrimSpace(f.CodeSnippet) != "" {
			if snippetEnt, snippetConf := ExtractPhysicalTargetEntity(f.CodeSnippet, f.TargetSymbol, defectClassMajor); snippetEnt != "" {
				targetEntity = snippetEnt
				if conf == ConfidenceLow {
					conf = snippetConf
				}
			}
		}

		// 5. 局部代码切片抓取 [calibratedLine-5, calibratedLine+5]
		var snippet string
		if len(lines) > 0 {
			start := calibratedLine - 5
			if start < 1 {
				start = 1
			}
			end := calibratedLine + 5
			if end > len(lines) {
				end = len(lines)
			}
			if start > end {
				start = end
			}
			if start >= 1 && end >= start {
				snippet = strings.Join(lines[start-1:end], "\n")
			} else {
				snippet = f.CodeSnippet
			}
		} else {
			snippet = f.CodeSnippet
		}

		// 6. 清洗 Token 与语句绝对静止锚点哈希
		cleanToken := CleanSourceToken(stmtWindow)
		if cleanToken == "" {
			cleanToken = CleanSourceToken(f.TriggerLine)
		}
		stmtAnchorHash := ComputeStmtAnchorHash(cleanToken)

		// 7. 计算两级槽位
		coreSlotKey := ComputeCoreSlotKey(normPath, scopeSymbol, defectClassMajor, targetEntity)
		preciseSlotKey := ComputePreciseSlotKey(coreSlotKey, stmtAnchorHash)

		findingIDStr := ""
		if f.ID > 0 {
			findingIDStr = strconv.FormatUint(uint64(f.ID), 10)
		}

		triggerLine := f.TriggerLine
		if triggerLine == "" {
			triggerLine = firstCodeStatement(f)
		}

		result[i] = StabilizedFindingDTO{
			OriginalIndex:    i,
			FindingID:        findingIDStr,
			NormPath:         normPath,
			ScopeSymbol:      scopeSymbol,
			DefectClassMajor: defectClassMajor,
			TargetEntity:     targetEntity,
			StmtAnchorHash:   stmtAnchorHash,
			CoreSlotKey:      coreSlotKey,
			SlotKey:          preciseSlotKey,
			ScopeBodyHash:    scopeBodyHash,
			SourceAnchored:   len(lines) > 0,
			Confidence:       conf,
			TriggerLine:      triggerLine,
			StmtWindow:       stmtWindow,
			CleanToken:       cleanToken,
			CodeSnippet:      snippet,
			LineStart:        lineStart,
			LineEnd:          lineEnd,
			Title:            f.Title,
			Category:         f.Category,
			Severity:         f.Severity,
			Suggestion:       f.Suggestion,
			Detail:           f.Detail,
		}
	}

	return result, nil
}
