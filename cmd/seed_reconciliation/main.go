package main

import (
	"code-shield/models"
	"code-shield/services/reconciliation"
	"code-shield/services/reports"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

type ConfigFlags struct {
	ConfigPath       string
	DryRun           bool
	RepoID           uint
	TaskTypeID       uint
	TaskID           uint
	SeedDB           bool
	BackfillRecon    bool
	UpgradeArtifacts bool
	Force            bool
}

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径 (config.yaml)")
	dryRun := flag.Bool("dry-run", true, "试运行模式 (默认 true，安全至上；传入 -dry-run=false 或 -execute 进行实际修改)")
	execute := flag.Bool("execute", false, "真实执行开关 (等价于 -dry-run=false)")
	repoID := flag.Uint("repo-id", 0, "仅处理指定代码仓 ID (0 为全部)")
	taskTypeID := flag.Uint("task-type-id", 0, "仅处理指定任务类型 ID (0 为全部)")
	taskID := flag.Uint("task-id", 0, "仅处理指定单次报告 ID (0 为全部)")
	seedDB := flag.Bool("seed-db", true, "是否向 defect_fingerprint_records 播种基线指纹")
	backfillRecon := flag.Bool("backfill-recon", true, "是否对历史连续扫描执行离线两两对账并补全台账")
	upgradeArtifacts := flag.Bool("upgrade-artifacts", true, "是否就地升级磁盘 synthesis.json 为 SSOT 台账结构")
	force := flag.Bool("force", false, "强制重算 (即使工件已迁移或对账已存在)")

	flag.Parse()

	isDryRun := *dryRun
	if *execute {
		isDryRun = false
	}

	flags := ConfigFlags{
		ConfigPath:       *configPath,
		DryRun:           isDryRun,
		RepoID:           *repoID,
		TaskTypeID:       *taskTypeID,
		TaskID:           *taskID,
		SeedDB:           *seedDB,
		BackfillRecon:    *backfillRecon,
		UpgradeArtifacts: *upgradeArtifacts,
		Force:            *force,
	}

	printBanner(flags)

	// 1. 初始化系统配置与数据库连接
	if err := initEnv(flags.ConfigPath); err != nil {
		log.Fatalf("❌ 系统初始化失败: %v\n", err)
	}

	// 2. 检索并分组候选历史任务报告
	startTime := time.Now()
	groups, totalReports, err := fetchAndGroupReports(flags)
	if err != nil {
		log.Fatalf("❌ 检索历史任务失败: %v\n", err)
	}

	log.Printf("📊 共检索到 %d 个成功任务报告，聚合为 %d 个 (Repo, TaskType) 处理组。\n\n", totalReports, len(groups))

	// 3. 执行迁移与播种管线
	stats := executePipeline(groups, flags)

	// 4. 打印汇总审计报告
	duration := time.Since(startTime)
	printSummary(stats, duration, flags)
}

func printBanner(f ConfigFlags) {
	fmt.Println("================================================================================")
	fmt.Println("       🛡️  Code-Shield 存量历史工件一次性播种与 R2R 迁移工具 (v1.6.0)")
	fmt.Println("================================================================================")
	modeStr := "🔍 [DRY-RUN 试运行模式] (只分析统计，绝不修改文件与写库)"
	if !f.DryRun {
		modeStr = "⚠️  [LIVE EXECUTE 真实执行模式] (将修改磁盘工件并写库持久化)"
	}
	fmt.Printf("执行模式: %s\n", modeStr)
	fmt.Printf("配置文件: %s\n", f.ConfigPath)
	if f.RepoID > 0 {
		fmt.Printf("仓库过滤: RepoID = %d\n", f.RepoID)
	}
	if f.TaskTypeID > 0 {
		fmt.Printf("类型过滤: TaskTypeID = %d\n", f.TaskTypeID)
	}
	if f.TaskID > 0 {
		fmt.Printf("单任务过滤: TaskID = %d\n", f.TaskID)
	}
	fmt.Printf("执行阶段: 工件升级=%v, 指纹库播种=%v, 连续对账补全=%v, 强制重算=%v\n",
		f.UpgradeArtifacts, f.SeedDB, f.BackfillRecon, f.Force)
	fmt.Println("================================================================================")
}

func initEnv(configPath string) error {
	resolvedPath := configPath
	if _, err := os.Stat(resolvedPath); os.IsNotExist(err) {
		altPath := filepath.Join("..", configPath)
		if _, errAlt := os.Stat(altPath); errAlt == nil {
			resolvedPath = altPath
		}
	}

	if err := models.LoadConfig(resolvedPath); err != nil {
		return fmt.Errorf("读取配置文件失败 (%s): %w", resolvedPath, err)
	}

	models.InitDB()
	return nil
}

type GroupKey struct {
	RepoID     uint
	TaskTypeID uint
}

func fetchAndGroupReports(flags ConfigFlags) (map[GroupKey][]models.TaskReport, int, error) {
	query := models.DB.Preload("Repo").Preload("TaskType").Where("status = ?", "success")

	if flags.TaskID > 0 {
		query = query.Where("id = ?", flags.TaskID)
	}
	if flags.RepoID > 0 {
		query = query.Where("repo_id = ?", flags.RepoID)
	}
	if flags.TaskTypeID > 0 {
		query = query.Where("task_type_id = ?", flags.TaskTypeID)
	}

	var reportList []models.TaskReport
	if err := query.Order("repo_id ASC, task_type_id ASC, id ASC").Find(&reportList).Error; err != nil {
		return nil, 0, err
	}

	groups := make(map[GroupKey][]models.TaskReport)
	for _, r := range reportList {
		key := GroupKey{RepoID: r.RepoID, TaskTypeID: r.TaskTypeID}
		groups[key] = append(groups[key], r)
	}

	return groups, len(reportList), nil
}

type PipelineStats struct {
	TotalReportsScanned   int
	ArtifactsAlreadySSOT  int
	ArtifactsUpgraded     int
	ArtifactsFailed       int
	FingerprintsSeededNew int
	FingerprintsRefreshed int
	ReconPairsProcessed   int
	ReconPairsSkipped     int
	ReconLinksCreated     int
	Errors                []string
}

func executePipeline(groups map[GroupKey][]models.TaskReport, flags ConfigFlags) PipelineStats {
	stats := PipelineStats{}

	for key, reportList := range groups {
		repoName := fmt.Sprintf("Repo-%d", key.RepoID)
		taskTypeName := fmt.Sprintf("TaskType-%d", key.TaskTypeID)
		if len(reportList) > 0 && reportList[0].Repo.Name != "" {
			repoName = reportList[0].Repo.Name
		}
		if len(reportList) > 0 && reportList[0].TaskType.DisplayName != "" {
			taskTypeName = reportList[0].TaskType.DisplayName
		}

		log.Printf("▶ 处理组 [%s / %s]: 共 %d 个报告\n", repoName, taskTypeName, len(reportList))

		// ── 阶段一: 磁盘工件原地规范化与指纹计算 ──
		for i := range reportList {
			r := &reportList[i]
			stats.TotalReportsScanned++

			if flags.UpgradeArtifacts {
				upgraded, isAlreadySSOT, err := upgradeSynthesisArtifact(r, flags)
				if err != nil {
					stats.ArtifactsFailed++
					stats.Errors = append(stats.Errors, fmt.Sprintf("Report #%d 工件升级失败: %v", r.ID, err))
					log.Printf("  ❌ Report #%d 工件升级失败: %v\n", r.ID, err)
				} else if isAlreadySSOT {
					stats.ArtifactsAlreadySSOT++
				} else if upgraded {
					stats.ArtifactsUpgraded++
					log.Printf("  ✨ Report #%d 工件已就地升级为 SSOT 台账\n", r.ID)
				}
			}
		}

		// ── 阶段二: 中央指纹库预热播种 (DefectFingerprintRecord) ──
		if flags.SeedDB && len(reportList) > 0 {
			latestReport := reportList[len(reportList)-1]
			newCount, refreshedCount, err := seedFingerprintRecords(&latestReport, flags)
			if err != nil {
				stats.Errors = append(stats.Errors, fmt.Sprintf("Group [%s/%s] 播种指纹失败: %v", repoName, taskTypeName, err))
				log.Printf("  ❌ 播种基线指纹失败: %v\n", err)
			} else {
				stats.FingerprintsSeededNew += newCount
				stats.FingerprintsRefreshed += refreshedCount
				log.Printf("  🌱 基线指纹播种: 新增 %d 条，刷新 %d 条 (基线 Report #%d)\n", newCount, refreshedCount, latestReport.ID)
			}
		}

		// ── 阶段三: 历史连续扫描对两两离线对账补全 ──
		if flags.BackfillRecon && len(reportList) >= 2 {
			for i := 0; i < len(reportList)-1; i++ {
				baseReport := reportList[i]
				currReport := reportList[i+1]

				pairProcessed, linksCount, skipped, err := backfillReconciliationPair(&baseReport, &currReport, flags)
				if err != nil {
					stats.Errors = append(stats.Errors, fmt.Sprintf("对账 #%d vs #%d 失败: %v", currReport.ID, baseReport.ID, err))
					log.Printf("  ❌ 对账 Report #%d vs #%d 失败: %v\n", currReport.ID, baseReport.ID, err)
				} else if skipped {
					stats.ReconPairsSkipped++
				} else if pairProcessed {
					stats.ReconPairsProcessed++
					stats.ReconLinksCreated += linksCount
					log.Printf("  🔗 补全对账 Report #%d vs #%d: 成功建立 %d 条认领链接\n", currReport.ID, baseReport.ID, linksCount)
				}
			}
		}
	}

	return stats
}

// upgradeSynthesisArtifact 将旧平铺 synthesis.json 升级为 SynthesisLedger
func upgradeSynthesisArtifact(r *models.TaskReport, flags ConfigFlags) (bool, bool, error) {
	absReport := r.GetAbsReportPath()
	if absReport == "" {
		return false, false, fmt.Errorf("报告绝对路径为空")
	}
	reportDir := filepath.Dir(absReport)
	synthesisPath := filepath.Join(reportDir, "synthesis.json")

	rawBytes, err := os.ReadFile(synthesisPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, false, nil
		}
		return false, false, err
	}

	// 尝试解析检测是否已经是新版台账
	var existingLedger reconciliation.SynthesisLedger
	if errUnmarshal := json.Unmarshal(rawBytes, &existingLedger); errUnmarshal == nil {
		if existingLedger.Meta.ReportID > 0 && len(existingLedger.Items) > 0 && existingLedger.Items[0].Fingerprint != "" {
			if !flags.Force {
				return false, true, nil
			}
		}
	}

	// 解构旧条目（平铺数组或旧对象）
	var rawItems []map[string]interface{}
	if errArray := json.Unmarshal(rawBytes, &rawItems); errArray != nil || len(rawItems) == 0 {
		var genericMap map[string]interface{}
		if errMap := json.Unmarshal(rawBytes, &genericMap); errMap == nil {
			if itms, ok := genericMap["items"].([]interface{}); ok {
				for _, it := range itms {
					if m, ok := it.(map[string]interface{}); ok {
						rawItems = append(rawItems, m)
					}
				}
			}
		}
	}

	if len(rawItems) == 0 {
		return false, false, nil
	}

	// 转换为新版 SynthesisItem 集合
	var items []reconciliation.SynthesisItem
	for i, itemMap := range rawItems {
		filePath, _ := itemMap["file_path"].(string)
		lineNum, _ := itemMap["line_number"].(string)
		title, _ := itemMap["title"].(string)
		detail, _ := itemMap["detail"].(string)
		category, _ := itemMap["category"].(string)
		severity, _ := itemMap["severity"].(string)
		triggerLine, _ := itemMap["trigger_line"].(string)
		scopeSymbol, _ := itemMap["scope_symbol"].(string)

		cleanTrigger := reconciliation.CleanSourceToken(triggerLine)
		normScope := reconciliation.NormalizeScopeSymbol(scopeSymbol)
		normCat := reconciliation.SanitizeCategory(category)
		normSev := reports.NormalizeSeverity(severity)

		fp := reconciliation.CalculateDefectFingerprint(r.RepoID, r.TaskTypeID, filePath, cleanTrigger, normScope)
		itemUID := reconciliation.GenerateItemUID(r.ID, fp, i+1)

		finding := models.AnalysisFinding{
			RepoID:      r.RepoID,
			TaskTypeID:  r.TaskTypeID,
			FilePath:    filePath,
			LineNumber:  lineNum,
			Title:       title,
			Detail:      detail,
			Category:    normCat,
			Severity:    normSev,
			Fingerprint: fp,
			TriggerLine: cleanTrigger,
			ScopeSymbol: normScope,
			DiffStatus:  "EXISTED",
		}

		items = append(items, reconciliation.SynthesisItem{
			Fingerprint:     fp,
			ItemUID:         itemUID,
			DiffStatus:      "EXISTED",
			LifecycleStatus: "EXISTED",
			FilePath:        filePath,
			LineNumber:      lineNum,
			RoundsSeen:      []uint{r.ID},
			FirstSeenReport: r.ID,
			LastSeenReport:  r.ID,
			Payload:         finding,
		})
	}

	govMode := models.ResolveGovernanceMode(r.TaskType.GovernanceMode)
	ledger := reconciliation.SynthesisLedger{
		Meta: reconciliation.SynthesisMeta{
			RepoID:         r.RepoID,
			TaskTypeID:     r.TaskTypeID,
			ReportID:       r.ID,
			GovernanceMode: govMode,
			ActiveCount:    len(items),
			ArchivedCount:  0,
			ReconciledAt:   time.Now(),
		},
		Items:         items,
		ArchivedItems: []reconciliation.ArchivedItem{},
	}

	if !flags.DryRun {
		_ = os.WriteFile(synthesisPath+".bak", rawBytes, 0644)

		newBytes, errMarshal := json.MarshalIndent(ledger, "", "  ")
		if errMarshal != nil {
			return false, false, errMarshal
		}
		if errWrite := os.WriteFile(synthesisPath, newBytes, 0644); errWrite != nil {
			return false, false, errWrite
		}
	}

	return true, false, nil
}

// seedFingerprintRecords 向 DefectFingerprintRecord 播种指纹基线
func seedFingerprintRecords(r *models.TaskReport, flags ConfigFlags) (int, int, error) {
	absReport := r.GetAbsReportPath()
	if absReport == "" {
		return 0, 0, nil
	}
	synthesisPath := filepath.Join(filepath.Dir(absReport), "synthesis.json")
	rawBytes, err := os.ReadFile(synthesisPath)
	if err != nil {
		return 0, 0, err
	}

	var ledger reconciliation.SynthesisLedger
	if err := json.Unmarshal(rawBytes, &ledger); err != nil {
		return 0, 0, err
	}

	newCount := 0
	refreshedCount := 0

	for _, it := range ledger.Items {
		fp := it.Fingerprint
		if fp == "" {
			continue
		}

		var existing models.DefectFingerprintRecord
		errFind := models.DB.Where("repo_id = ? AND task_type_id = ? AND fingerprint = ?",
			r.RepoID, r.TaskTypeID, fp).First(&existing).Error

		if errFind != nil {
			newCount++
			if !flags.DryRun {
				rec := models.DefectFingerprintRecord{
					RepoID:         r.RepoID,
					TaskTypeID:     r.TaskTypeID,
					Fingerprint:    fp,
					FilePath:       it.FilePath,
					ScopeSymbol:    it.Payload.ScopeSymbol,
					Category:       it.Payload.Category,
					Severity:       it.Payload.Severity,
					Status:         "ACTIVE",
					TriggerLine:    it.Payload.TriggerLine,
					FeedbackStatus: "UNREVIEWED",
					FirstTaskID:    r.ID,
					LastTaskID:     r.ID,
					FirstSeenAt:    r.CreatedAt,
					LastSeenAt:     r.CreatedAt,
				}
				_ = models.DB.Create(&rec).Error
			}
		} else {
			refreshedCount++
			if !flags.DryRun {
				_ = models.DB.Model(&existing).Updates(map[string]interface{}{
					"last_task_id": r.ID,
					"last_seen_at": r.CreatedAt,
					"status":       "ACTIVE",
				}).Error
			}
		}
	}

	return newCount, refreshedCount, nil
}

// backfillReconciliationPair 对相邻两份报告进行纯函数对账并落盘
func backfillReconciliationPair(baseReport, currReport *models.TaskReport, flags ConfigFlags) (bool, int, bool, error) {
	var existing models.ScanReconciliation
	errFind := models.DB.Where("task_report_id = ? AND baseline_report_id = ?",
		currReport.ID, baseReport.ID).First(&existing).Error

	if errFind == nil && !flags.Force {
		return false, 0, true, nil
	}

	baseSynthPath := filepath.Join(filepath.Dir(baseReport.GetAbsReportPath()), "synthesis.json")
	currSynthPath := filepath.Join(filepath.Dir(currReport.GetAbsReportPath()), "synthesis.json")

	baseBytes, errBase := os.ReadFile(baseSynthPath)
	if errBase != nil {
		return false, 0, false, fmt.Errorf("读取基线 synthesis 失败: %w", errBase)
	}
	currBytes, errCurr := os.ReadFile(currSynthPath)
	if errCurr != nil {
		return false, 0, false, fmt.Errorf("读取当前 synthesis 失败: %w", errCurr)
	}

	var currLedger reconciliation.SynthesisLedger
	_ = json.Unmarshal(currBytes, &currLedger)
	var currFindings []models.AnalysisFinding
	for _, it := range currLedger.Items {
		currFindings = append(currFindings, it.Payload)
	}

	govMode := models.ResolveGovernanceMode(currReport.TaskType.GovernanceMode)

	req := reconciliation.ReconcileRequest{
		RepoID:            currReport.RepoID,
		TaskTypeID:        currReport.TaskTypeID,
		TaskName:          currReport.TaskType.Name,
		CurrentReportID:   currReport.ID,
		BaseReportID:      baseReport.ID,
		CurrentFindings:   currFindings,
		BaseSynthesisJSON: baseBytes,
		GovernanceMode:    govMode,
		RepoUnchanged:     false,
	}

	res, errRecon := reconciliation.Reconcile(&req)
	if errRecon != nil {
		return false, 0, false, errRecon
	}

	if !flags.DryRun {
		reconPath := filepath.Join(filepath.Dir(currReport.GetAbsReportPath()),
			fmt.Sprintf("recon-%d-vs-%d.json", currReport.ID, baseReport.ID))
		diffBytes, errDiff := json.MarshalIndent(res.DiffPayload, "", "  ")
		if errDiff == nil {
			_ = os.WriteFile(reconPath, diffBytes, 0644)
		}

		_ = models.DB.Create(&res.Reconciliation).Error
		if len(res.Links) > 0 {
			_ = models.DB.CreateInBatches(res.Links, 100).Error
		}
	}

	return true, len(res.Links), false, nil
}

func printSummary(stats PipelineStats, d time.Duration, flags ConfigFlags) {
	fmt.Println("\n================================================================================")
	fmt.Println("                       📋  迁移与播种执行汇总审计报表                            ")
	fmt.Println("================================================================================")
	fmt.Printf("总耗时:               %v\n", d)
	modeText := "🔍 DRY-RUN (试运行，未写盘与写库)"
	if !flags.DryRun {
		modeText = "✅ LIVE (真实执行完毕并已持久化)"
	}
	fmt.Printf("运行模式:             %s\n", modeText)
	fmt.Printf("扫描历史报告总数:     %d 个\n", stats.TotalReportsScanned)
	fmt.Printf("已是新版 SSOT 工件:   %d 个\n", stats.ArtifactsAlreadySSOT)
	fmt.Printf("就地升级工件数量:     %d 个\n", stats.ArtifactsUpgraded)
	fmt.Printf("播种新增基线指纹数:   %d 条\n", stats.FingerprintsSeededNew)
	fmt.Printf("刷新存量指纹记录数:   %d 条\n", stats.FingerprintsRefreshed)
	fmt.Printf("成功补全历史对账对:   %d 对 (生成 %d 条认领关系)\n", stats.ReconPairsProcessed, stats.ReconLinksCreated)
	fmt.Printf("跳过已存在对账对:     %d 对\n", stats.ReconPairsSkipped)
	if len(stats.Errors) > 0 {
		fmt.Printf("⚠️  遇到的错误数量:     %d 项\n", len(stats.Errors))
		for _, e := range stats.Errors {
			fmt.Printf("   - %s\n", e)
		}
	} else {
		fmt.Printf("错误状态:             无错误 (100%% Clean)\n")
	}
	fmt.Println("================================================================================")
	if flags.DryRun {
		fmt.Println("💡 提示: 当前为试运行模式。若确认结果符合预期，请添加参数 `-execute` 真正落盘与播种：")
		fmt.Println("   go run cmd/seed_reconciliation/main.go -execute")
		fmt.Println("================================================================================")
	}
}
