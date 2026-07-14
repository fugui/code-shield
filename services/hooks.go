package services

import (
	"code-shield/models"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/datatypes"
)

// TaskHook is a callback function run when a task finishes successfully
type TaskHook func(ctx *taskContext, findings []models.AnalysisFinding) error

var (
	taskHooksMu sync.RWMutex
	taskHooks   = make(map[string][]TaskHook)
)

// RegisterTaskHook registers a postprocess hook for a specific task type name
func RegisterTaskHook(taskTypeName string, hook TaskHook) {
	taskHooksMu.Lock()
	defer taskHooksMu.Unlock()
	taskHooks[taskTypeName] = append(taskHooks[taskTypeName], hook)
}

// executeHooks runs all hooks registered for the current task type
func (ctx *taskContext) executeHooks(findings []models.AnalysisFinding) {
	taskHooksMu.RLock()
	hooks, ok := taskHooks[ctx.taskType.Name]
	taskHooksMu.RUnlock()
	if !ok {
		return
	}
	log.Printf("[TaskHooks] Running %d hooks for task type %q (Report ID: %d)", len(hooks), ctx.taskType.Name, ctx.report.ID)
	for i, hook := range hooks {
		if err := hook(ctx, findings); err != nil {
			log.Printf("[TaskHooks] Hook %d for %q failed: %v", i, ctx.taskType.Name, err)
		}
	}
}

func init() {
	// Register hook for test case effectiveness
	RegisterTaskHook("ut_effectiveness", handleUTEffectivenessHook)
	// Register hook for coredump risk analysis
	RegisterTaskHook("coredump_risk", handleCampaignHook[models.CoredumpFinding])
	// Register hook for python float comparison scan
	RegisterTaskHook("float_comparison", handleCampaignHook[models.FloatFinding])
	// Register hook for thread creation analysis
	RegisterTaskHook("thread_create", handleCampaignHook[models.ThreadFinding])
	// Register hook for cjson memory leak scan
	RegisterTaskHook("cjson_scan", handleCampaignHook[models.CjsonFinding])
}

func handleUTEffectivenessHook(ctx *taskContext, findings []models.AnalysisFinding) error {
	log.Printf("[TaskHooks] Processing ut_effectiveness hook for Repo ID: %d, findings count: %d", ctx.repo.ID, len(findings))

	var allOldFindings []models.TestCaseFinding
	if err := models.DB.Where("repo_id = ?", ctx.repo.ID).Find(&allOldFindings).Error; err != nil {
		log.Printf("[TaskHooks] Failed to load old TestCaseFinding: %v", err)
	}

	matchedOldIDs := make(map[uint]bool)

	for _, f := range findings {
		var matchedFinding *models.TestCaseFinding
		targetStatus := "open"
		if f.Severity == "合格" {
			targetStatus = "closed"
		}

		for i := range allOldFindings {
			oldF := &allOldFindings[i]
			if oldF.FilePath == f.FilePath && oldF.TestCaseName == f.Title {
				matchedFinding = oldF
				break
			}
		}

		if matchedFinding == nil {
			statusLog := []map[string]interface{}{
				{
					"status": targetStatus,
					"time":   time.Now().Format("2006-01-02 15:04:05"),
					"user":   "system",
					"reason": "Initial scan discovery",
				},
			}
			logBytes, _ := json.Marshal(statusLog)

			tf := models.TestCaseFinding{
				RepoID:       ctx.repo.ID,
				TaskReportID: ctx.report.ID,
				FilePath:     f.FilePath,
				LineNumber:   f.LineNumber,
				TestCaseName: f.Title,
				Detail:       f.Detail,
				Severity:     f.Severity,
				Category:     f.Category,
				CodeSnippet:  f.CodeSnippet,
				Suggestion:   f.Suggestion,
				Status:       targetStatus,
				StatusLog:    logBytes,
			}
			if err := models.DB.Create(&tf).Error; err != nil {
				log.Printf("[TaskHooks] Failed to create TestCaseFinding record: %v", err)
			}
		} else {
			matchedOldIDs[matchedFinding.ID] = true
			updatedStatus := matchedFinding.Status
			var existingLog []map[string]interface{}
			if len(matchedFinding.StatusLog) > 0 {
				_ = json.Unmarshal(matchedFinding.StatusLog, &existingLog)
			}

			if updatedStatus != "invalid" {
				if (updatedStatus == "closed" || updatedStatus == "resolved") && targetStatus == "open" {
					updatedStatus = "open"
					existingLog = append(existingLog, map[string]interface{}{
						"status": "open",
						"time":   time.Now().Format("2006-01-02 15:04:05"),
						"user":   "system",
						"reason": "Reopened by subsequent scan finding defects",
					})
				} else if updatedStatus == "open" && targetStatus == "closed" {
					updatedStatus = "closed"
					existingLog = append(existingLog, map[string]interface{}{
						"status": "closed",
						"time":   time.Now().Format("2006-01-02 15:04:05"),
						"user":   "system",
						"reason": "Automatically closed (resolved to合格 by scan)",
					})
				}
			}
			logBytes, _ := json.Marshal(existingLog)

			matchedFinding.TaskReportID = ctx.report.ID
			matchedFinding.LineNumber = f.LineNumber
			matchedFinding.Detail = f.Detail
			matchedFinding.Severity = f.Severity
			matchedFinding.Category = f.Category
			matchedFinding.CodeSnippet = f.CodeSnippet
			matchedFinding.Suggestion = f.Suggestion
			matchedFinding.Status = updatedStatus
			matchedFinding.StatusLog = logBytes

			if err := models.DB.Save(matchedFinding).Error; err != nil {
				log.Printf("[TaskHooks] Failed to update TestCaseFinding record: %v", err)
			}
		}
	}

	for i := range allOldFindings {
		oldF := &allOldFindings[i]
		if !matchedOldIDs[oldF.ID] {
			if oldF.Status == "closed" || oldF.Status == "resolved" {
				continue
			}
			var existingLog []map[string]interface{}
			if len(oldF.StatusLog) > 0 {
				_ = json.Unmarshal(oldF.StatusLog, &existingLog)
			}
			existingLog = append(existingLog, map[string]interface{}{
				"status": "resolved",
				"time":   time.Now().Format("2006-01-02 15:04:05"),
				"user":   "system",
				"reason": "Automatically marked as resolved (not detected in the latest scan)",
			})
			logBytes, _ := json.Marshal(existingLog)

			oldF.Status = "resolved"
			oldF.StatusLog = logBytes
			if err := models.DB.Save(oldF).Error; err != nil {
				log.Printf("[TaskHooks] Failed to logically resolve obsolete TestCaseFinding: %v", err)
			}
		}
	}

	return nil
}

// CopyFindingFields 动态拷贝 models.AnalysisFinding 到泛型结构体指针中
func CopyFindingFields(src *models.AnalysisFinding, dst interface{}) {
	sVal := reflect.ValueOf(src).Elem()
	dVal := reflect.ValueOf(dst).Elem()

	for i := 0; i < sVal.NumField(); i++ {
		sField := sVal.Type().Field(i)
		fieldName := sField.Name
		if fieldName == "ID" || fieldName == "CreatedAt" {
			continue
		}
		dField := dVal.FieldByName(fieldName)
		if dField.IsValid() && dField.CanSet() && dField.Type() == sVal.Field(i).Type() {
			if fieldName == "AssigneeID" {
				if !dField.IsNil() && sVal.Field(i).IsNil() {
					continue
				}
			}
			if fieldName == "Severity" {
				oldSeverity := dField.String()
				if oldSeverity != "" {
					continue
				}
			}
			if fieldName == "Feedback" {
				oldFeedback := dField.String()
				if oldFeedback != "" && sVal.Field(i).String() == "" {
					continue
				}
			}
			dField.Set(sVal.Field(i))
		}
	}
}

// SetFieldValue 动态设置结构体中某个字段的值
func SetFieldValue(obj interface{}, fieldName string, val interface{}) {
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	f := v.FieldByName(fieldName)
	if f.IsValid() && f.CanSet() {
		f.Set(reflect.ValueOf(val))
	}
}

// GetFieldValue 动态获取结构体中某个字段的值
func GetFieldValue(obj interface{}, fieldName string) interface{} {
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	f := v.FieldByName(fieldName)
	if f.IsValid() {
		return f.Interface()
	}
	return nil
}

// parseLineInterval 解析 "55", "55-63", "55,56" 等行号格式为闭区间
func parseLineInterval(lineStr string) (start, end int) {
	lineStr = strings.TrimSpace(lineStr)
	lineStr = strings.TrimPrefix(lineStr, "L")
	lineStr = strings.TrimPrefix(lineStr, "l")
	if lineStr == "" {
		return 0, 0
	}

	if strings.Contains(lineStr, "-") {
		parts := strings.Split(lineStr, "-")
		if len(parts) == 2 {
			s, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			e, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err1 == nil && err2 == nil {
				return s, e
			}
		}
	}

	if strings.Contains(lineStr, ",") {
		parts := strings.Split(lineStr, ",")
		minVal, maxVal := -1, -1
		for _, p := range parts {
			val, err := strconv.Atoi(strings.TrimSpace(p))
			if err == nil {
				if minVal == -1 || val < minVal {
					minVal = val
				}
				if val > maxVal {
					maxVal = val
				}
			}
		}
		if minVal != -1 {
			return minVal, maxVal
		}
	}

	if val, err := strconv.Atoi(lineStr); err == nil {
		return val, val
	}

	return 0, 0
}

// calculateLineSimilarity 计算行号区间重叠和邻近度
func calculateLineSimilarity(l1Str, l2Str string) float64 {
	s1, e1 := parseLineInterval(l1Str)
	s2, e2 := parseLineInterval(l2Str)
	if s1 <= 0 || s2 <= 0 {
		return 0.0
	}

	startMax := s1
	if s2 > startMax {
		startMax = s2
	}
	endMin := e1
	if e2 < endMin {
		endMin = e2
	}

	overlap := 0
	if startMax <= endMin {
		overlap = endMin - startMax + 1
	}

	startMin := s1
	if s2 < startMin {
		startMin = s2
	}
	endMax := e1
	if e2 > endMax {
		endMax = e2
	}
	unionSize := endMax - startMin + 1

	if overlap > 0 && unionSize > 0 {
		iou := float64(overlap) / float64(unionSize)
		if (s1 <= s2 && e1 >= e2) || (s2 <= s1 && e2 >= e1) {
			iou = iou * 1.2
			if iou > 1.0 {
				iou = 1.0
			}
		}
		return iou
	}

	dist := 0
	if s2 > e1 {
		dist = s2 - e1
	} else {
		dist = s1 - e2
	}

	if dist <= 15 {
		return 1.0 - float64(dist)*0.06
	}

	return 0.0
}

// normalizeCode 去除多余空格和注释，规范化代码片段
func normalizeCode(code string) string {
	reMulti := regexp.MustCompile(`/\*[\s\S]*?\*/`)
	code = reMulti.ReplaceAllString(code, "")
	reSingle := regexp.MustCompile(`//.*`)
	code = reSingle.ReplaceAllString(code, "")

	var sb strings.Builder
	for _, r := range code {
		if r != ' ' && r != '\t' && r != '\r' && r != '\n' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// computeCodeHash 计算代码片段的 MD5
func computeCodeHash(code string) string {
	normalized := normalizeCode(code)
	if normalized == "" {
		return ""
	}
	hash := md5.Sum([]byte(normalized))
	return hex.EncodeToString(hash[:])
}

// calculateStringSimilarity 基于 Levenshtein 编辑距离计算字符串相似度
func calculateStringSimilarity(s1, s2 string) float64 {
	s1 = strings.TrimSpace(s1)
	s2 = strings.TrimSpace(s2)
	if s1 == "" && s2 == "" {
		return 1.0
	}
	if s1 == "" || s2 == "" {
		return 0.0
	}

	r1, r2 := []rune(s1), []rune(s2)
	len1, len2 := len(r1), len(r2)
	dp := make([]int, len2+1)
	for j := 0; j <= len2; j++ {
		dp[j] = j
	}
	for i := 1; i <= len1; i++ {
		prev := dp[0]
		dp[0] = i
		for j := 1; j <= len2; j++ {
			temp := dp[j]
			if r1[i-1] == r2[j-1] {
				dp[j] = prev
			} else {
				minVal := dp[j-1] + 1
				if dp[j]+1 < minVal {
					minVal = dp[j] + 1
				}
				if prev+1 < minVal {
					minVal = prev + 1
				}
				dp[j] = minVal
			}
			prev = temp
		}
	}
	dist := dp[len2]
	maxLen := len1
	if len2 > maxLen {
		maxLen = len2
	}
	return 1.0 - float64(dist)/float64(maxLen)
}

// askLLMIfSameFinding 使用大模型辅助语义匹配
func askLLMIfSameFinding(ctx *taskContext, oldPath, oldLine, oldTitle, oldDetail, oldSnippet, newPath, newLine, newTitle, newDetail, newSnippet string) bool {
	backend := ""
	if ctx.runParams.AIBackend != nil {
		backend = *ctx.runParams.AIBackend
	}
	if backend == "" {
		backend = models.AppConfig.AI.Backend
	}
	if backend == "" {
		backend = "claude"
	}

	invoker := GetAIInvoker(backend)
	if invoker == nil {
		log.Printf("[askLLMIfSameFinding] Failed to get AI invoker for backend: %s", backend)
		return false
	}

	prompt := fmt.Sprintf(`你是一个资深的代码安全审计专家。请判断以下两个在不同扫描周期中上报的问题，是否属于【代码中的同一个核心缺陷】。

# Old Finding (历史记录)
- 文件路径: %s
- 行号: %s
- 标题: %s
- 描述: %s
- 代码片段: 
%s

# New Finding (本次发现)
- 文件路径: %s
- 行号: %s
- 标题: %s
- 描述: %s
- 代码片段:
%s

# Task
请分析：
1. 这两个问题在物理位置上是否属于同一处或极近的业务代码逻辑（文件路径、行号以及代码上下文是否高度重合）？
2. 两者描述的安全隐患/缺陷（例如特定的线程创建问题、内存泄漏点等）是否本质相同？

请必须以 JSON 格式输出，不要输出任何 markdown 格式的代码块（不要带 %s 或 %s 标记），不要输出任何解释文字。格式如下：
{
  "is_same": true
}`, oldPath, oldLine, oldTitle, oldDetail, oldSnippet, newPath, newLine, newTitle, newDetail, newSnippet, "```json", "```")

	tmpDir := filepath.Join(models.AppConfig.Storage.Root, "tmp")
	_ = os.MkdirAll(tmpDir, 0755)
	outputPath := filepath.Join(tmpDir, fmt.Sprintf("finding_match_%d.json", time.Now().UnixNano()))
	defer func() {
		_ = os.Remove(outputPath)
	}()

	req := AIRequest{
		ParentContext: ctx.ctx,
		WorkDir:       ctx.codesPath,
		PromptMsg:     prompt,
		OutputPath:    outputPath,
		TimeoutMin:    2,
	}

	log.Printf("[askLLMIfSameFinding] Invoking LLM to double-check finding similarity (Report ID: %d)...", ctx.report.ID)
	if err := invoker.Invoke(req); err != nil {
		log.Printf("[askLLMIfSameFinding] LLM invocation failed: %v", err)
		return false
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		log.Printf("[askLLMIfSameFinding] Failed to read LLM output: %v", err)
		return false
	}

	cleaned := cleanJSONFromAI(data)

	type Response struct {
		IsSame bool `json:"is_same"`
	}
	var res Response
	if err := json.Unmarshal(cleaned, &res); err != nil {
		log.Printf("[askLLMIfSameFinding] Failed to parse LLM JSON output %q: %v", string(cleaned), err)
		return false
	}

	log.Printf("[askLLMIfSameFinding] LLM Match result: is_same=%t", res.IsSame)
	return res.IsSame
}

// handleCampaignHook 泛型化的专项缺陷处理器
func handleCampaignHook[T any](ctx *taskContext, findings []models.AnalysisFinding) error {
	log.Printf("[TaskHooks] Processing campaign hook for Task: %s, Repo ID: %d, findings count: %d", ctx.taskType.Name, ctx.repo.ID, len(findings))

	var allOldFindings []T
	if err := models.DB.Model(new(T)).Where("repo_id = ?", ctx.repo.ID).Find(&allOldFindings).Error; err != nil {
		log.Printf("[TaskHooks] Failed to load old findings for repo: %v", err)
	}

	matchedOldIDs := make(map[uint]bool)
	matchedFindingsMap := make(map[int]*T) // index of finding -> matched old finding

	// ==========================================
	// Phase 1: 确定性硬规则内存比对 (无大模型调用，极快)
	// ==========================================
	for idx, f := range findings {
		var matchedFinding *T
		fHash := computeCodeHash(f.CodeSnippet)

		// 1.1 精确行号/代码片段 Hash 比对
		for i := range allOldFindings {
			oldF := &allOldFindings[i]
			oldID := GetFieldValue(oldF, "ID").(uint)
			if matchedOldIDs[oldID] {
				continue
			}

			oldPath := GetFieldValue(oldF, "FilePath").(string)
			oldLine := GetFieldValue(oldF, "LineNumber").(string)
			oldTitle := GetFieldValue(oldF, "Title").(string)
			oldSnippet := GetFieldValue(oldF, "CodeSnippet").(string)

			if oldPath == f.FilePath {
				lineSim := calculateLineSimilarity(oldLine, f.LineNumber)
				if lineSim >= 0.8 && oldTitle == f.Title {
					matchedFinding = oldF
					break
				}
				if lineSim >= 0.5 && computeCodeHash(oldSnippet) == fHash {
					matchedFinding = oldF
					break
				}
			}
		}

		// 1.2 高分评分匹配 (无需 LLM)
		if matchedFinding == nil {
			for i := range allOldFindings {
				oldF := &allOldFindings[i]
				oldID := GetFieldValue(oldF, "ID").(uint)
				if matchedOldIDs[oldID] {
					continue
				}

				oldPath := GetFieldValue(oldF, "FilePath").(string)
				oldLine := GetFieldValue(oldF, "LineNumber").(string)
				oldTitle := GetFieldValue(oldF, "Title").(string)
				oldCategory := GetFieldValue(oldF, "Category").(string)

				if oldPath == f.FilePath {
					catSim := 0.0
					if oldCategory == f.Category {
						catSim = 1.0
					}
					lineSim := calculateLineSimilarity(oldLine, f.LineNumber)
					titleSim := calculateStringSimilarity(oldTitle, f.Title)

					score := 0.3*catSim + 0.3*lineSim + 0.4*titleSim

					// 物理位置高度相近（行号相似度较高且在同文件）且标题有一定相关性（大于 0.4），无需大模型判定，直接归并
					if lineSim >= 0.9 && titleSim >= 0.4 {
						matchedFinding = oldF
						break
					}

					if score >= 0.85 {
						matchedFinding = oldF
						break
					}
				}
			}
		}

		if matchedFinding != nil {
			matchedOldIDs[GetFieldValue(matchedFinding, "ID").(uint)] = true
			matchedFindingsMap[idx] = matchedFinding
		}
	}

	// ==========================================
	// Phase 2: 并发收集大模型校验任务并执行
	// ==========================================
	type llmMatchTask struct {
		findingIdx    int
		oldFindingIdx int
		oldPath       string
		oldLine       string
		oldTitle      string
		oldDetail     string
		oldSnippet    string
		newPath       string
		newLine       string
		newTitle      string
		newDetail     string
		newSnippet    string
	}

	var llmTasks []llmMatchTask

	for idx, f := range findings {
		if matchedFindingsMap[idx] != nil {
			continue // 已经匹配成功，不需要大模型
		}

		// 寻找该缺陷对应的所有可能候选历史缺陷 (基于模糊评分)
		for i := range allOldFindings {
			oldF := &allOldFindings[i]
			oldID := GetFieldValue(oldF, "ID").(uint)
			if matchedOldIDs[oldID] {
				continue
			}

			oldPath := GetFieldValue(oldF, "FilePath").(string)
			oldLine := GetFieldValue(oldF, "LineNumber").(string)
			oldTitle := GetFieldValue(oldF, "Title").(string)
			oldSnippet := GetFieldValue(oldF, "CodeSnippet").(string)
			oldCategory := GetFieldValue(oldF, "Category").(string)

			if oldPath == f.FilePath {
				catSim := 0.0
				if oldCategory == f.Category {
					catSim = 1.0
				}
				lineSim := calculateLineSimilarity(oldLine, f.LineNumber)
				titleSim := calculateStringSimilarity(oldTitle, f.Title)

				score := 0.3*catSim + 0.3*lineSim + 0.4*titleSim

				// 物理位置高度相近（行号相似度较高且在同文件），给予强加分确保能进入 LLM 语义识别判定
				if lineSim >= 0.8 {
					score += 0.2
					if score > 1.0 {
						score = 1.0
					}
				}

				// 模糊匹配区间 [0.45, 0.85)
				if score >= 0.45 && score < 0.85 {
					oldDetail := GetFieldValue(oldF, "Detail").(string)
					llmTasks = append(llmTasks, llmMatchTask{
						findingIdx:    idx,
						oldFindingIdx: i,
						oldPath:       oldPath,
						oldLine:       oldLine,
						oldTitle:      oldTitle,
						oldDetail:     oldDetail,
						oldSnippet:    oldSnippet,
						newPath:       f.FilePath,
						newLine:       f.LineNumber,
						newTitle:      f.Title,
						newDetail:     f.Detail,
						newSnippet:    f.CodeSnippet,
					})
				}
			}
		}
	}

	// 并发执行大模型比对
	if len(llmTasks) > 0 {
		log.Printf("[TaskHooks] Concurrently invoking LLM for %d similarity verification tasks...", len(llmTasks))

		// 限制最大并发量为 10，避免大模型限频或过多连接
		concurrencyLimit := 10
		if concurrencyLimit > len(llmTasks) {
			concurrencyLimit = len(llmTasks)
		}

		sem := make(chan struct{}, concurrencyLimit)

		// 结果收集 map（key: findingIdx_oldFindingIdx -> value: isSame）
		var resultsMu sync.Mutex
		resultsMap := make(map[string]bool)

		var wg sync.WaitGroup
		for _, task := range llmTasks {
			wg.Add(1)
			go func(t llmMatchTask) {
				defer wg.Done()
				sem <- struct{}{}        // Acquire token
				defer func() { <-sem }() // Release token

				isSame := askLLMIfSameFinding(ctx, t.oldPath, t.oldLine, t.oldTitle, t.oldDetail, t.oldSnippet, t.newPath, t.newLine, t.newTitle, t.newDetail, t.newSnippet)

				resultsMu.Lock()
				key := fmt.Sprintf("%d_%d", t.findingIdx, t.oldFindingIdx)
				resultsMap[key] = isSame
				resultsMu.Unlock()
			}(task)
		}
		wg.Wait()

		// ==========================================
		// Phase 3: 顺序判定模糊匹配的缺陷归属 (顺序，防竞争)
		// ==========================================
		for idx := range findings {
			if matchedFindingsMap[idx] != nil {
				continue
			}

			// 按顺序寻找最符合的候选并匹配
			for i := range allOldFindings {
				oldF := &allOldFindings[i]
				oldID := GetFieldValue(oldF, "ID").(uint)
				if matchedOldIDs[oldID] {
					continue
				}

				key := fmt.Sprintf("%d_%d", idx, i)
				if resultsMap[key] {
					// 发现符合大模型相同的缺陷且当前历史缺陷未被绑定过
					matchedFindingsMap[idx] = oldF
					matchedOldIDs[oldID] = true
					break
				}
			}
		}
	}

	// ==========================================
	// 顺序同步落库与老问题逻辑关闭阶段
	// ==========================================
	for idx, f := range findings {
		matchedFinding := matchedFindingsMap[idx]
		targetStatus := "open"
		if f.Severity == "合格" {
			targetStatus = "closed"
		}

		if matchedFinding == nil {
			statusLog := []map[string]interface{}{
				{
					"status": targetStatus,
					"time":   time.Now().Format("2006-01-02 15:04:05"),
					"user":   "system",
					"reason": "Initial scan discovery",
				},
			}
			logBytes, _ := json.Marshal(statusLog)

			var newFinding T
			CopyFindingFields(&f, &newFinding)
			SetFieldValue(&newFinding, "RepoID", ctx.repo.ID)
			SetFieldValue(&newFinding, "TaskReportID", ctx.report.ID)
			SetFieldValue(&newFinding, "Status", targetStatus)
			SetFieldValue(&newFinding, "StatusLog", datatypes.JSON(logBytes))

			if err := models.DB.Create(&newFinding).Error; err != nil {
				log.Printf("[TaskHooks] Failed to create campaign finding record: %v", err)
			}
		} else {
			oldID := GetFieldValue(matchedFinding, "ID").(uint)
			matchedOldIDs[oldID] = true

			updatedStatus := GetFieldValue(matchedFinding, "Status").(string)
			var existingLog []map[string]interface{}
			logBytesVal := GetFieldValue(matchedFinding, "StatusLog")
			if logBytesVal != nil {
				if bytes, ok := logBytesVal.([]byte); ok && len(bytes) > 0 {
					_ = json.Unmarshal(bytes, &existingLog)
				} else if datatypesJson, ok := logBytesVal.(datatypes.JSON); ok && len(datatypesJson) > 0 {
					_ = json.Unmarshal(datatypesJson, &existingLog)
				}
			}

			if updatedStatus != "invalid" {
				if (updatedStatus == "closed" || updatedStatus == "resolved") && targetStatus == "open" {
					updatedStatus = "open"
					existingLog = append(existingLog, map[string]interface{}{
						"status": "open",
						"time":   time.Now().Format("2006-01-02 15:04:05"),
						"user":   "system",
						"reason": "Reopened by subsequent scan finding defects",
					})
				} else if updatedStatus == "open" && targetStatus == "closed" {
					updatedStatus = "closed"
					existingLog = append(existingLog, map[string]interface{}{
						"status": "closed",
						"time":   time.Now().Format("2006-01-02 15:04:05"),
						"user":   "system",
						"reason": "Automatically closed (resolved to合格 by scan)",
					})
				}
			}
			newLogBytes, _ := json.Marshal(existingLog)

			CopyFindingFields(&f, matchedFinding)
			SetFieldValue(matchedFinding, "TaskReportID", ctx.report.ID)
			SetFieldValue(matchedFinding, "Status", updatedStatus)
			SetFieldValue(matchedFinding, "StatusLog", datatypes.JSON(newLogBytes))

			oldLine := GetFieldValue(matchedFinding, "LineNumber").(string)
			if oldLine != f.LineNumber {
				SetFieldValue(matchedFinding, "LineNumber", f.LineNumber)
			}

			if err := models.DB.Save(matchedFinding).Error; err != nil {
				log.Printf("[TaskHooks] Failed to update campaign finding record: %v", err)
			}
		}
	}

	// 历史遗留且未匹配到的缺陷，逻辑状态置为 resolved
	for i := range allOldFindings {
		oldF := &allOldFindings[i]
		oldID := GetFieldValue(oldF, "ID").(uint)
		if !matchedOldIDs[oldID] {
			oldStatus := GetFieldValue(oldF, "Status").(string)
			if oldStatus == "closed" || oldStatus == "resolved" {
				continue
			}

			var existingLog []map[string]interface{}
			logBytesVal := GetFieldValue(oldF, "StatusLog")
			if logBytesVal != nil {
				if bytes, ok := logBytesVal.([]byte); ok && len(bytes) > 0 {
					_ = json.Unmarshal(bytes, &existingLog)
				} else if datatypesJson, ok := logBytesVal.(datatypes.JSON); ok && len(datatypesJson) > 0 {
					_ = json.Unmarshal(datatypesJson, &existingLog)
				}
			}

			existingLog = append(existingLog, map[string]interface{}{
				"status": "resolved",
				"time":   time.Now().Format("2006-01-02 15:04:05"),
				"user":   "system",
				"reason": "Automatically marked as resolved (not detected in the latest scan)",
			})
			newLogBytes, _ := json.Marshal(existingLog)

			SetFieldValue(oldF, "Status", "resolved")
			SetFieldValue(oldF, "StatusLog", datatypes.JSON(newLogBytes))

			if err := models.DB.Save(oldF).Error; err != nil {
				log.Printf("[TaskHooks] Failed to logically resolve obsolete campaign finding: %v", err)
			} else {
				log.Printf("[TaskHooks] Campaign finding ID %d logically resolved.", oldID)
			}
		}
	}

	return nil
}
