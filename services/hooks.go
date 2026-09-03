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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm/clause"
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
	// 1. 如果任务类型启用了专项分析 (IsCampaign)，自动触发通用归并引擎
	if ctx.taskType.IsCampaign {
		log.Printf("[TaskHooks] Running generic campaign hook for %q (GovernanceMode: %s, Report ID: %d)",
			ctx.taskType.Name, ctx.taskType.GovernanceMode, ctx.report.ID)
		if err := handleGenericCampaignHook(ctx, findings); err != nil {
			log.Printf("[TaskHooks] Generic campaign hook for %q failed: %v", ctx.taskType.Name, err)
		}
	}

	// 2. 执行已注册的自定义 Hooks
	taskHooksMu.RLock()
	hooks, ok := taskHooks[ctx.taskType.Name]
	taskHooksMu.RUnlock()
	if !ok {
		return
	}
	log.Printf("[TaskHooks] Running %d custom hooks for task type %q (Report ID: %d)", len(hooks), ctx.taskType.Name, ctx.report.ID)
	for i, hook := range hooks {
		if err := hook(ctx, findings); err != nil {
			log.Printf("[TaskHooks] Hook %d for %q failed: %v", i, ctx.taskType.Name, err)
		}
	}
}

// handleGenericCampaignHook 是元数据驱动的统一专项分析归并引擎
func handleGenericCampaignHook(ctx *taskContext, findings []models.AnalysisFinding) error {
	log.Printf("[TaskHooks] Processing generic campaign hook for Task: %s, Mode: %s, Repo ID: %d, findings count: %d",
		ctx.taskType.Name, ctx.taskType.GovernanceMode, ctx.repo.ID, len(findings))

	// 规范化并清洗 findings 字段（防御性清洗超长标题/多行换行，防止超长字符串破坏数据库、索引及前端展示）
	for i := range findings {
		rawTitle := strings.TrimSpace(findings[i].Title)
		if len([]rune(rawTitle)) > 500 {
			if findings[i].Detail == "" {
				findings[i].Detail = rawTitle
			} else if !strings.Contains(findings[i].Detail, rawTitle) {
				findings[i].Detail = fmt.Sprintf("【原始问题描述】: %s\n\n%s", rawTitle, findings[i].Detail)
			}
		}
		findings[i].Title = SanitizeFindingTitle(rawTitle)
		findings[i].FilePath = strings.TrimSpace(findings[i].FilePath)
		findings[i].LineNumber = strings.TrimSpace(findings[i].LineNumber)
		findings[i].Category = strings.TrimSpace(findings[i].Category)
	}

	var allOldFindings []models.CampaignFinding
	if err := models.DB.Where("task_type_id = ? AND repo_id = ?", ctx.taskType.ID, ctx.repo.ID).Find(&allOldFindings).Error; err != nil {
		log.Printf("[TaskHooks] Failed to load old CampaignFinding for repo: %v", err)
	}

	matchedOldIDs := make(map[uint]bool)
	matchedFindingsMap := make(map[int]*models.CampaignFinding) // index of finding -> matched old finding

	isEntityMode := ctx.taskType.GovernanceMode == models.GovernanceModeEntityAssessment

	if isEntityMode {
		// ── 模式 B (全量实体评估): 确定性 O(1) 路径+用例名称哈希匹配 ──
		oldIndex := make(map[string]*models.CampaignFinding, len(allOldFindings))
		for i := range allOldFindings {
			key := allOldFindings[i].FilePath + "\x00" + allOldFindings[i].Title
			oldIndex[key] = &allOldFindings[i]
		}

		for idx, f := range findings {
			key := f.FilePath + "\x00" + f.Title
			if oldF, ok := oldIndex[key]; ok {
				matchedFindingsMap[idx] = oldF
				matchedOldIDs[oldF.ID] = true
			}
		}
	} else {
		// ── 模式 A (缺陷攻关): Phase 1 确定性规则 + Phase 2 LLM 语义模糊比对 ──
		for idx, f := range findings {
			var matchedFinding *models.CampaignFinding
			fHash := computeCodeHash(f.CodeSnippet)

			// 1.1 精确行号/代码片段 Hash 比对
			for i := range allOldFindings {
				oldF := &allOldFindings[i]
				if matchedOldIDs[oldF.ID] {
					continue
				}

				if oldF.FilePath == f.FilePath {
					lineSim := calculateLineSimilarity(oldF.LineNumber, f.LineNumber)
					if lineSim >= 0.8 && oldF.Title == f.Title {
						matchedFinding = oldF
						break
					}
					if lineSim >= 0.5 && computeCodeHash(oldF.CodeSnippet) == fHash {
						matchedFinding = oldF
						break
					}
				}
			}

			// 1.2 高分加权评分匹配 (无需 LLM)
			if matchedFinding == nil {
				for i := range allOldFindings {
					oldF := &allOldFindings[i]
					if matchedOldIDs[oldF.ID] {
						continue
					}

					if oldF.FilePath == f.FilePath {
						catSim := 0.0
						if oldF.Category == f.Category {
							catSim = 1.0
						}
						lineSim := calculateLineSimilarity(oldF.LineNumber, f.LineNumber)
						titleSim := calculateStringSimilarity(oldF.Title, f.Title)

						score := 0.3*catSim + 0.3*lineSim + 0.4*titleSim

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
				matchedOldIDs[matchedFinding.ID] = true
				matchedFindingsMap[idx] = matchedFinding
			}
		}

		// Phase 2: 并发收集大模型校验任务并执行
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
				continue
			}

			for i := range allOldFindings {
				oldF := &allOldFindings[i]
				if matchedOldIDs[oldF.ID] {
					continue
				}

				if oldF.FilePath == f.FilePath {
					catSim := 0.0
					if oldF.Category == f.Category {
						catSim = 1.0
					}
					lineSim := calculateLineSimilarity(oldF.LineNumber, f.LineNumber)
					titleSim := calculateStringSimilarity(oldF.Title, f.Title)
					score := 0.3*catSim + 0.3*lineSim + 0.4*titleSim

					if score >= 0.45 || (lineSim >= 0.5 && titleSim >= 0.3) {
						llmTasks = append(llmTasks, llmMatchTask{
							findingIdx:    idx,
							oldFindingIdx: i,
							oldPath:       oldF.FilePath,
							oldLine:       oldF.LineNumber,
							oldTitle:      oldF.Title,
							oldDetail:     oldF.Detail,
							oldSnippet:    oldF.CodeSnippet,
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

		if len(llmTasks) > 0 {
			log.Printf("[TaskHooks] Running %d LLM matching tasks in parallel for generic campaign...", len(llmTasks))
			resultsMap := make(map[string]bool)
			var resultsMu sync.Mutex
			var wg sync.WaitGroup
			sem := make(chan struct{}, 8)

			for _, t := range llmTasks {
				task := t
				wg.Add(1)
				go func() {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()

					isSame := askLLMIfSameFinding(ctx,
						task.oldPath, task.oldLine, task.oldTitle, task.oldDetail, task.oldSnippet,
						task.newPath, task.newLine, task.newTitle, task.newDetail, task.newSnippet,
					)

					resultsMu.Lock()
					key := fmt.Sprintf("%d_%d", task.findingIdx, task.oldFindingIdx)
					resultsMap[key] = isSame
					resultsMu.Unlock()
				}()
			}
			wg.Wait()

			for idx := range findings {
				if matchedFindingsMap[idx] != nil {
					continue
				}
				for i := range allOldFindings {
					oldF := &allOldFindings[i]
					if matchedOldIDs[oldF.ID] {
						continue
					}
					key := fmt.Sprintf("%d_%d", idx, i)
					if resultsMap[key] {
						matchedFindingsMap[idx] = oldF
						matchedOldIDs[oldF.ID] = true
						break
					}
				}
			}
		}
	}

	// ── Phase 3: 顺序同步落库与老问题逻辑关闭阶段（容错性隔离持久化，杜绝单条异常导致全仓数据回滚丢弃） ──
	var failedCount int
	for idx, f := range findings {
		matchedFinding := matchedFindingsMap[idx]
		targetStatus := "open"
		if f.Severity == "合格" {
			targetStatus = "closed"
		}

		if matchedFinding == nil {
			nowStr := time.Now().Format("2006-01-02 15:04:05")
			statusLog := []map[string]interface{}{
				{
					"status":            targetStatus,
					"time":              nowStr,
					"user":              "system",
					"reason":            "Initial scan discovery",
					"last_confirmed_at": nowStr,
					"confirm_count":     1,
				},
			}
			logBytes, _ := json.Marshal(statusLog)

			newFinding := models.CampaignFinding{
				TaskTypeID:   ctx.taskType.ID,
				RepoID:       ctx.repo.ID,
				TaskReportID: ctx.report.ID,
				FilePath:     f.FilePath,
				LineNumber:   f.LineNumber,
				Title:        f.Title,
				Detail:       f.Detail,
				Severity:     f.Severity,
				Category:     f.Category,
				CodeSnippet:  f.CodeSnippet,
				Suggestion:   f.Suggestion,
				Status:       targetStatus,
				StatusLog:    datatypes.JSON(logBytes),
			}

			// 使用 OnConflict 保证幂等插入，当 (task_type_id, repo_id, file_path, title) 发生冲突时平滑更新最新属性
			onConflictClause := clause.OnConflict{
				Columns: []clause.Column{
					{Name: "task_type_id"},
					{Name: "repo_id"},
					{Name: "file_path"},
					{Name: "title"},
				},
				DoUpdates: clause.AssignmentColumns([]string{
					"line_number", "detail", "severity", "category",
					"code_snippet", "suggestion", "status", "status_log",
					"task_report_id", "updated_at",
				}),
			}

			if err := models.DB.Clauses(onConflictClause).Create(&newFinding).Error; err != nil {
				log.Printf("[TaskHooks] Warning: Failed to create/upsert CampaignFinding for %s (%s): %v, skipping to protect other findings", f.FilePath, f.Title, err)
				failedCount++
				continue
			}
		} else {
			matchedOldIDs[matchedFinding.ID] = true
			updatedStatus := matchedFinding.Status
			var existingLog []map[string]interface{}
			if len(matchedFinding.StatusLog) > 0 {
				_ = json.Unmarshal(matchedFinding.StatusLog, &existingLog)
			}

			nowStr := time.Now().Format("2006-01-02 15:04:05")
			reopened := false

			if updatedStatus != "invalid" {
				if (updatedStatus == "closed" || updatedStatus == "resolved") && targetStatus == "open" {
					updatedStatus = "open"
					reopened = true
					existingLog = append(existingLog, map[string]interface{}{
						"status":            "open",
						"time":              nowStr,
						"user":              "system",
						"reason":            "Reopened by subsequent scan finding defects",
						"last_confirmed_at": nowStr,
						"confirm_count":     1,
					})
				} else if updatedStatus == "open" && targetStatus == "closed" {
					updatedStatus = "closed"
					existingLog = append(existingLog, map[string]interface{}{
						"status": "closed",
						"time":   nowStr,
						"user":   "system",
						"reason": "Automatically closed (resolved to合格 by scan)",
					})
				}
			}

			// 如果未发生 reopen 或 auto-close 状态变化（例如持续处于 open 状态），则更新首条初始发现记录的确认时间与累计确认次数
			if !reopened && updatedStatus != "closed" && len(existingLog) > 0 {
				targetIdx := -1
				for i := range existingLog {
					if r, ok := existingLog[i]["reason"].(string); ok && strings.HasPrefix(r, "Initial scan discovery") {
						targetIdx = i
						break
					}
				}
				if targetIdx == -1 {
					targetIdx = 0
				}

				existingLog[targetIdx]["last_confirmed_at"] = nowStr
				cnt := 1
				if rawCnt, ok := existingLog[targetIdx]["confirm_count"]; ok {
					switch v := rawCnt.(type) {
					case float64:
						cnt = int(v)
					case int:
						cnt = v
					case int64:
						cnt = int(v)
					}
				}
				existingLog[targetIdx]["confirm_count"] = cnt + 1
			}

			newLogBytes, _ := json.Marshal(existingLog)

			matchedFinding.TaskReportID = ctx.report.ID
			matchedFinding.LineNumber = f.LineNumber
			matchedFinding.Detail = f.Detail
			matchedFinding.Severity = f.Severity
			matchedFinding.Category = f.Category
			matchedFinding.CodeSnippet = f.CodeSnippet
			matchedFinding.Suggestion = f.Suggestion
			matchedFinding.Status = updatedStatus
			matchedFinding.StatusLog = datatypes.JSON(newLogBytes)

			if err := models.DB.Save(matchedFinding).Error; err != nil {
				log.Printf("[TaskHooks] Warning: Failed to update CampaignFinding record ID %d: %v, skipping", matchedFinding.ID, err)
				failedCount++
				continue
			}
		}
	}

	// 历史遗留且本次未匹配到的缺陷/用例，逻辑状态自动置为 resolved
	// 注意：若任务存在失败分片，说明扫描未完全覆盖代码仓所有文件，跳过自动消亡以避免误关缺陷
	if ctx.hasFailedChunks {
		log.Printf("[TaskHooks] Notice: Skipped auto-resolving obsolete findings because task report %d had failed chunks.", ctx.report.ID)
	} else {
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
				newLogBytes, _ := json.Marshal(existingLog)

				oldF.Status = "resolved"
				oldF.StatusLog = datatypes.JSON(newLogBytes)

				if err := models.DB.Save(oldF).Error; err != nil {
					log.Printf("[TaskHooks] Warning: Failed to logically resolve obsolete CampaignFinding ID %d: %v", oldF.ID, err)
					failedCount++
				} else {
					log.Printf("[TaskHooks] CampaignFinding ID %d logically resolved.", oldF.ID)
				}
			}
		}
	}

	if failedCount > 0 {
		log.Printf("[TaskHooks] Completed campaign finding sync for repo %d with %d non-fatal warning(s).", ctx.repo.ID, failedCount)
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
	backend := models.AppConfig.AI.ToolBackends.FindingMatch
	if backend == "" {
		backend = "native"
	}
	if !IsValidAIBackend(backend) {
		if ctx.runParams.AIBackend != nil && *ctx.runParams.AIBackend != "" {
			backend = *ctx.runParams.AIBackend
		} else {
			backend = models.AppConfig.AI.Backend
		}
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

	tmpDir := filepath.Join(models.AppConfig.GetDataDir(), "tmp")
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
		TimeoutMin:    10,
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

// SanitizeFindingTitle 规范化缺陷标题：去除首尾空白、清洗换行符并进行安全长度截断（最多 500 个字符）
func SanitizeFindingTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "未命名缺陷"
	}
	// 清洗换行符，单行呈现
	title = strings.ReplaceAll(title, "\r\n", " ")
	title = strings.ReplaceAll(title, "\n", " ")
	title = strings.ReplaceAll(title, "\r", " ")
	// 安全截断防止单字段超限及 B-Tree 索引溢出
	runes := []rune(title)
	if len(runes) > 500 {
		return string(runes[:497]) + "..."
	}
	return title
}
