# 缺陷台账存储与 I/O 性能演进：稀疏覆盖、事务解耦与 TTL 分级归档设计

> **文档状态**：`Draft (待评审)` · 针对架构审计中“数据库 I/O 压力与写放大加剧”问题的落地改进方案  
> **设计范围**：缺陷台账持久化链路、`scan_scope_entries` 覆盖清单稀疏化存储、事务范围瘦身与读写解耦、PostgreSQL 连接池与长事务优化、数据生命周期 TTL 滑动窗口与归档清理机制  
> **关联文档**：[07-问题清单跨轮增量治理重构设计：统一缺陷台账SSOT与分层对账算法](07-问题清单跨轮增量治理重构设计：统一缺陷台账SSOT与分层对账算法.md) · [01-CodeShield-数据模型与系统配置变更设计](../06-data-models/01-CodeShield-数据模型与系统配置变更设计.md) · [RFC-001-多租户隔离方案评估与分库架构选型](../../03-rfcs/RFC-001-多租户隔离方案评估与分库架构选型.md)

---

## 〇、一页纸摘要（TL;DR）

**背景**：在 07 号重构方案落地后，Code-Shield 成功建立了以数据库为单一真实源（SSOT）的缺陷台账体系，彻底终结了“同仓跨轮扫描假 NEW 泛滥”、“局部扫描误判已修复”等业务缺陷。但在架构审计中，敏锐地指出了相伴而生的数据库层负担：
1. **写放大严重**：单次扫描需向 `scan_scope_entries`（逐文件级别，通常数千条）、`defect_observations`（原始观测事实）、`defect_events` 及 `defects` 写入或更新数据；
2. **长事务霸占连接与阻碍 GC**：在 `PersistScanFacts` 中，全表查询、跨轮匹配（CPU 密集）与大批量插入被包裹在单个大事务内。在 PostgreSQL 环境下，引发写连接池耗尽（Connection Pool Starvation）、阻止 Autovacuum 回收死元组诱代表膨胀（Table Bloat），并存在并发扫描与人工标注时的死锁（Deadlock）隐患；
3. **存储无节制膨胀**：当前 `cron_jobs` 仅有清理磁盘临时文件的任务，完全缺乏数据库维度的清理归档机制，轮次快照数据将永久堆积。

**本设计核心改进措施**：
1. **覆盖清单稀疏化存储（Sparse Scope Storage）**：数据库仅记录发生变更（`diff_touched`）、扫描失败（`FAILED`）或命中缺陷的关键文件；其余数千个未变更且无缺陷的正常文件外置为轻量压缩 Manifest 文件。单次扫描写入行数**直降 95% 以上**。
2. **大事务瘦身与读写解耦（Short Commit Transaction）**：将重型 Matching 匹配计算移出写事务，流水线重构为「无锁快照读 $\to$ 纯内存计算 $\to$ 极简短事务提交」，写连接占用时间从几百毫秒压缩至 **<10 毫秒**。
3. **分级生命周期与 TTL 定时归档（Retention & Vacuum GC）**：台账主体（`defects`）永久保留，轮次级快照（`scan_scope_entries`、`defect_observations`）建立 **30 天 / 最近 10 轮滑动窗口 TTL**；新增定时批量清理任务与 PostgreSQL VACUUM 回收机制。

---

## 一、问题现状与写放大深度剖析

### 1.1 现状持久化链路画像（代码证据）

查验当前落地代码 [`services/defectlifecycle/service.go:60-135`](file:///home/fugui/codes/code-shield/services/defectlifecycle/service.go#L60)，单次扫描完成后的数据入库完全集中在 `PersistScanFacts` 的单个大事务中：

```go
// 现网代码片段：单个重型事务承担全量计算与落库
err := input.DB.Transaction(func(tx *gorm.DB) error {
    // 1. 删除旧原始事实并全量批量插入（通常数百条）
    tx.Where("task_report_id = ?", input.Report.ID).Delete(&models.AnalysisFinding{})
    tx.CreateInBatches(findings, 100)

    // 2. 删除旧覆盖明细并全量插入逐文件清单（通常 1,000 ~ 10,000+ 条）
    tx.Where("report_id = ?", input.Report.ID).Delete(&models.ScanScopeEntry{})
    persistScope(tx, entries) // 逐文件插入

    // 3. 在写事务内加载全量台账与别名（持有读视图）
    tx.Where("repo_id = ? ...").Find(&defects)
    tx.Where("repo_id = ? ...").Find(&aliases)

    // 4. 在写事务内执行 CPU 密集型的 Matching（倒排检索 + 综合打分 + 状压DP指派 + AI残差）
    matching := RunMatching(...) 

    // 5. 在同一事务内提交台账流转（更新 defects, 插入 observations, 插入 events）
    commitLedger(tx, ...)

    // 6. 更新任务报告元数据
    models.UpdateActiveTaskReport(tx, input.Report.ID, updates)
    return nil
})
```

### 1.2 写放大测算与存储膨胀模型

假设一个中等规模的代码仓库：包含 **3,000 个源文件**，平均每轮报出 **150 条缺陷**。系统在经历连续多次代码扫描（如 CI 触发或每日定时体检）后的数据量级增长测算如下：

| 扫描轮次 | `scan_scope_entries` 累计行数 | `defect_observations` 累计行数 | `defects` 活跃台账行数 | 单次写入行数总计 | PostgreSQL 物理存储与 WAL 预估增长 |
| :---: | :---: | :---: | :---: | :---: | :---: |
| **首扫 (R1)** | 3,000 行 | 150 行 | 150 行 | ~3,300 行 | +2.5 MB |
| **第 10 轮** | 30,000 行 | 1,500 行 | ~180 行 | ~3,300 行 | +25 MB |
| **第 50 轮** | 150,000 行 | 7,500 行 | ~220 行 | ~3,300 行 | +130 MB |
| **第 200 轮** | **600,000 行** | **30,000 行** | ~250 行 | ~3,300 行 | **+500 MB+** |

**核心痛点暴露**：
1. **95% 的数据库行数由“毫无变化且无缺陷的健康文件”贡献**：在代码维护期，每次 Git Push 通常仅修改 3~10 个文件，其余 2,990+ 个文件完全未变且无缺陷，但每轮都被机械地插入 1 行到 `scan_scope_entries`。
2. **写放大倍数（Write Amplification Factor）失控**：一次只修改了 2 行代码的增量扫描，对数据库造成的写行数依然高达 3,000+ 行，持续消耗 PostgreSQL 的 WAL 日志写入量与共享内存缓冲池（`shared_buffers`）。

### 1.3 长事务与 PostgreSQL 资源争用机理

生产环境中 Code-Shield 统一采用 **PostgreSQL** 作为核心关系型数据库。虽然 PostgreSQL 具备成熟的 MVCC（多版本并发控制）和行级锁机制，但在当前实现中，大事务包裹高耗时匹配计算依然引发了严重的系统性隐患：
* **连接池耗尽（Connection Pool Starvation）**：PostgreSQL 采用进程模型，服务端通常配置了严格的最大连接数（如 `config.yaml` 中配置的 `max_open_conns: 50`）。当前代码在开启写事务后才执行 CPU 密集的 `RunMatching`（耗时可达 200ms ~ 1500ms），导致写连接被长时间霸占。多个扫描任务并发提交时，连接池被瞬间打满，后续 HTTP 请求与后台任务排队超时。
* **阻断 Autovacuum 诱发严重表膨胀（Table Bloat）**：PostgreSQL 的 MVCC 依赖 `autovacuum` 清理无用的死元组（Dead Tuples）。当存在运行时间长达数秒的长事务时，其所持有的旧 Read View 会导致后台 Autovacuum **无法清理在此事务启动之后由任何表产生的死元组**！高频扫描持续触发长事务将直接导致死元组堆积、表与索引体积异常膨胀，查询性能劣化。
* **行锁交叉与死锁风险（Deadlock Risk）**：在大事务内，系统既要更新 `task_reports` 状态，又要批量更新/插入 `defects`。若此时外部用户在 Web 控制台进行误报标记或人工认领，两者极易产生行锁交叉等待，触发 PostgreSQL 抛出 `ERROR: deadlock detected` 导致扫描结果落库失败。

### 1.4 无清理机制的存储与查询衰退

查验 [`cron_jobs/jobs.go`](file:///home/fugui/codes/code-shield/cron_jobs/jobs.go)，定时任务中仅有针对本地临时分析报告文件的 `CleanExpiredTempArtifacts`，数据库内所有表目前**无任何 TTL 机制**。随着数据量增加：
* 索引树深度增加，查询与插入延迟攀升；
* PostgreSQL 的堆表与索引死元组堆积，引发严重的表膨胀与扫描缓存命中率下降；
* 物理备份与迁移成本指数级上升。

---

## 二、架构演进目标与不可变原则

在实施存储与 I/O 优化时，必须恪守以下四项原则，确保 07 号重构的核心业务价值不受丝毫侵蚀：

| 编号 | 不变量原则 | 说明与红线 |
| :--- | :--- | :--- |
| **I1** | **台账精确度零折损** | 无论是全量还是稀疏存储，对账引擎回答“某缺陷是否落在扫描覆盖域内”、“某缺陷是否应当判定已修复”的结论必须与全量存储 **100% 逐比特一致**。 |
| **I2** | **覆盖判定语义等价** | 任何未在数据库建行的文件，必须能够通过快速内存查询/Manifest 文件恢复其覆盖事实，严禁因未落库而误判为 `COVERAGE_GAP`。 |
| **I3** | **匹配计算无写锁** | 所有的候选检索、综合打分、全局最优指派必须基于内存纯计算完成，**严禁在持有数据库写锁期间执行复杂计算**。 |
| **I4** | **分级生命周期管理** | 核心缺陷台账（`defects`）是持久资产，必须永久保存；历史扫描观测与逐文件覆盖明细属于可再生快照，强制实施 TTL 滚动清理。 |

---

## 三、方案一：覆盖清单稀疏存储（Sparse Scope Storage）与外置 Manifest

### 3.1 覆盖明细的本质业务分析

在对账与生命周期管理中，系统对 `scan_scope_entries` 的诉求仅有两点：
1. **修复守卫判定**：当存量缺陷在当前轮未复现时，系统需要判断“发现该缺陷的文件本轮是否被扫描且覆盖了？如果被覆盖了，该文件是否被 Git Diff 触碰（代码已修改）？”；
2. **扫描故障追溯**：当分片执行失败时，需要知道哪些文件处理失败（`FAILED`），从而将相关缺陷转为 `COVERAGE_GAP` 保护起来，避免误判修复。

对于一个在当前轮次中**「成功扫描完成、没有发生代码变更、且没有报出任何缺陷」**的文件，它在数据库中存在与否，对生命周期状态机推进**毫无区分度**。

### 3.2 覆盖明细三态分流策略

我们将一次扫描涉及的全量文件集合划分为两级存储：

```text
一次扫描的计划文件总集 (Planned Files, 如 3,000 个)
   │
   ├── [落数据库 scan_scope_entries (稀疏表, 约 20~80 行)] ─────────────┐
   │    1. 扫描失败文件 (outcome = FAILED)                             │ 核心判定关键路径
   │    2. 代码发生变更的文件 (diff_touched = true)                      │ (高频索引，快速定位)
   │    3. 本轮报出缺陷的文件 (has_findings = true)                    │
   │                                                                   │
   └── [外置归档 scope_manifest.json.gz (轻量压缩包, 约 5~15 KB)] ───────┴─ 全量审计与离线追溯
        全量 3,000 个文件的逐项状态 (用于报告导出、人工审查或冷排查)
```

### 3.3 稀疏落库与内存覆盖判定器实现

在内存中构建轻量级覆盖判别器，实现对账引擎无感知的等价判定：

```go
// ScopeCoverageIndex 内存轻量覆盖索引
type ScopeCoverageIndex struct {
    ScopeHash      string
    TotalFiles     int
    FailedFiles    map[string]string // norm_path -> fail_reason
    TouchedFiles   map[string]bool   // norm_path -> diff_touched
    FindingFiles   map[string]bool   // norm_path -> has_finding
    DefaultOutcome string            // 默认为 SCANNED (若全轮无全局失败)
}

// IsFileCoveredAndChanged 快速回答对账生命周期的核心问题
func (idx *ScopeCoverageIndex) IsFileCoveredAndChanged(normPath string) (covered bool, changed bool, failed bool) {
    if _, isFailed := idx.FailedFiles[normPath]; isFailed {
        return false, false, true
    }
    changed = idx.TouchedFiles[normPath]
    // 只要不是明确失败，且该文件属于计划扫描集合，即视为覆盖
    return true, changed, false
}
```

### 3.4 降载收益对比

对一次包含 3,000 个文件的典型增量扫描（变更了 5 个文件，报出 2 个缺陷）：

| 指标 | 现网全量落库方案 | 本设计稀疏落库方案 | 改善幅度 |
| :--- | :--- | :--- | :--- |
| **单次扫描写表行数** | 3,000 行 `ScanScopeEntry` | **7 行**（5个变更 + 2个发现） | **减少 99.76%** |
| **数据库单表索引膨胀** | +48 KB / 轮 | **+0.2 KB / 轮** | **减少 99.5%** |
| **覆盖明细插入耗时** | 120ms ~ 350ms | **< 3ms** | **提速 50~100 倍** |
| **全量覆盖明细归档** | 依赖庞大数据库表 | 磁盘保存 `scope_manifest.json.gz`（约 8KB） | 随时可查，对库零压力 |

---

## 四、方案二：持久化流程“读写解耦”与大事务瘦身

### 4.1 三阶段解耦流水线设计

彻底打破当前在单个写事务内进行“查表 $\to$ Matching 计算 $\to$ 写表”的紧密耦合，重构为三个明确隔离的执行阶段：

```text
┌────────────────────────────────────────────────────────────────────────┐
│  Phase 1: 只读快照加载 (Read Snapshot) - [无排他写锁，并发友好]           │
│   · 批量查询 repo 下活跃的 defects 与 aliases                            │
│   · 耗时: 3~8ms                                                        │
└───────────────────────────────────┬────────────────────────────────────┘
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│  Phase 2: 纯内存无锁计算 (In-Memory Matching) - [纯 CPU 计算，零 DB 交互]│
│   · 提取锚点与 Shape                                                    │
│   · 单轮多视角合并 (ObservationGroup)                                   │
│   · 别名倒排过滤 → 10维打分 → 状压 DP / 贪心指派                         │
│   · 构造决策列表 decisions[]                                            │
│   · 耗时: 20~150ms (此期间数据库无任何锁占用)                             │
└───────────────────────────────────┬────────────────────────────────────┘
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│  Phase 3: 极简短写事务 (Short Write Transaction) - [原子批量提交]        │
│   · 批量写入稀疏 ScanScopeEntry (几十行)                                │
│   · 批量写入本轮 DefectObservation (数十至百行)                         │
│   · 批量更新/插入 Defects 状态变更 (仅变更行)                           │
│   · 批量追加 DefectEvent (仅产生状态跃迁的事件)                          │
│   · 回写 TaskReport 摘要计数与 scope_hash                               │
│   · 耗时: 5~15ms (排他写锁瞬间获取并瞬间释放)                            │
└────────────────────────────────────────────────────────────────────────┘
```

### 4.2 重构后的执行伪代码

```go
func PersistScanFactsOptimized(input ScanInput) (*ScanFactsResult, error) {
    // ── Phase 1: 准备数据与只读快照加载 (无写锁) ──
    groups := BuildObservationGroups(input.RepoRoot, input.Report.ID, input.Findings)
    sparseEntries, manifestData := filterSparseScope(input, buildScopeEntries(input))
    
    // 异步或离线持久化压缩 Manifest 文件，不走数据库事务
    go saveScopeManifestAsync(input.Report.ID, manifestData)

    var defects []models.Defect
    var aliases []models.DefectAlias
    if err := input.DB.Where("repo_id = ? AND task_type_id = ?", input.Repo.ID, input.TaskType.ID).
        Order("id").Find(&defects).Error; err != nil {
        return nil, fmt.Errorf("read defects snapshot: %w", err)
    }
    if err := input.DB.Where("repo_id = ? AND task_type_id = ?", input.Repo.ID, input.TaskType.ID).
        Order("id").Find(&aliases).Error; err != nil {
        return nil, fmt.Errorf("read aliases snapshot: %w", err)
    }

    // ── Phase 2: 纯内存匹配计算 (零锁占用) ──
    ledgerGroups := ledgerObservationGroups(groups)
    matching := RunMatching(MatchingInput{
        Observations: ledgerGroups,
        Defects:      defects,
        Aliases:      aliases,
        Budget:       RuntimeMatchBudget(),
        Thresholds:   RuntimeMatchThresholds(),
    })
    decisions := buildDecisions(matching, groups, input)

    // ── Phase 3: 极简短事务提交 (毫秒级排他锁) ──
    var result *ScanFactsResult
    err := input.DB.Transaction(func(tx *gorm.DB) error {
        // 1. 批量写稀疏覆盖清单 (仅几十行)
        if len(sparseEntries) > 0 {
            if err := tx.CreateInBatches(sparseEntries, 100).Error; err != nil {
                return err
            }
        }

        // 2. 提交台账状态机 (仅写本轮 observations, events 及更新变更的 defects)
        ledgerRes, err := commitLedgerOptimized(tx, groups, decisions, sparseEntries, input)
        if err != nil {
            return err
        }

        // 3. 极速回写报告汇总指标
        if err := updateReportSummary(tx, input.Report.ID, ledgerRes); err != nil {
            return err
        }

        result = ledgerRes
        return nil
    })

    return result, err
}
```

### 4.3 PostgreSQL 连接池与事务会话安全参数加固

在生产环境中，PostgreSQL 承载了高并发扫描任务。必须在 [`models/db.go`](file:///home/fugui/codes/code-shield/models/db.go) 数据库连接初始化时，对连接池及事务会话参数进行防御性配置，防止长事务和僵死连接打满连接池：

```go
// PostgreSQL 生产连接池与会话防御性加固 (models/db.go)
sqlDB, err := db.DB()
if err == nil {
    // 1. 连接生命周期与池化配置 (参考 config.yaml)
    sqlDB.SetMaxOpenConns(models.AppConfig.Database.MaxOpenConns) // 默认 50
    sqlDB.SetMaxIdleConns(models.AppConfig.Database.MaxIdleConns) // 默认 10
    sqlDB.SetConnMaxLifetime(30 * time.Minute)
    sqlDB.SetConnMaxIdleTime(10 * time.Minute)
}

// 2. 会话级事务超时保护 (防止异常事务挂死在 IDLE IN TRANSACTION 状态)
if db.Dialector.Name() == "postgres" {
    // 事务内空闲超过 10 秒自动回滚断开，彻底杜绝僵死长事务霸占连接与阻断 Autovacuum
    db.Exec("SET idle_in_transaction_session_timeout = '10000';") 
    // 单条 SQL 最长执行 60 秒兜底保护
    db.Exec("SET statement_timeout = '60000';")                   
    // 获取行级锁超时 5 秒，防止并发争抢时无限阻塞死等
    db.Exec("SET lock_timeout = '5000';")                        
}
```

---

## 五、方案三：缺陷台账分级生命周期与 TTL 定时归档

### 5.1 数据冷热分级与保留策略矩阵

建立清晰的生命周期分级，让活跃工作台始终轻盈，同时历史快照具备滚动回收能力：

| 数据分级 | 涉及数据表 | 业务定位 | 推荐保留周期 (TTL) | 清理/归档动作 |
| :--- | :--- | :--- | :--- | :--- |
| **Tier 1: 核心 SSOT (热)** | `defects`<br>`defect_aliases` | 缺陷身份台账、当前治理状态、自愈别名索引 | **永久保留**<br>(随代码仓销毁) | 状态流转推进（ACTIVE $\to$ RESOLVED $\to$ OBSOLETE），超长休眠（如 1 年以上 OBSOLETE）可选择逻辑归档。 |
| **Tier 2: 审计流水 (温)** | `defect_events` | 状态跃迁历史、人工确认与指派日志 | **保留 90 天**<br>或最近 50 次状态变更 | 保留所有包含人工意见（`actor_type = 'HUMAN'`）的关键事件；自动迁移/系统常规扫描事件超期批量清理。 |
| **Tier 3: 轮次事实快照 (冷)** | `scan_scope_entries`<br>`defect_observations`<br>`AnalysisFinding` | 单次扫描逐文件明细、原始观测事实快照 | **滑动窗口：保留最近 10 次 COMPLETE 扫描**<br>或最长 **30 天** | 超期自动批量分块物理删除（或未来按月 DROP PARTITION 分区）；被清理轮次的概要依然在 `task_reports` 和导出报告 JSON 中永久完整可查。 |
| **Tier 4: 语义判决缓存 (冷)** | `defect_ai_verdict_cache` | AI 仲裁结果缓存 | **保留 60 天** | 长期未命中的冷缓存自动过期（若按 09 设计彻底移除 AI 仲裁，则整表物理废弃）。 |

### 5.2 归档清理机制实现（`LedgerRetentionGCJob`）

在 [`cron_jobs/jobs.go`](file:///home/fugui/codes/code-shield/cron_jobs/jobs.go) 中注册每日凌晨执行的自动 GC 定时任务：

```go
// cron_jobs/jobs.go: 每日凌晨 04:00 执行台账与快照生命周期清理
_, err = globalCron.AddFunc("0 4 * * *", RunLedgerRetentionGCJob)

func RunLedgerRetentionGCJob() {
    if models.DB == nil {
        return
    }
    retentionDays := models.AppConfig.System.LedgerRetentionDays
    if retentionDays <= 0 {
        retentionDays = 30 // 默认保留 30 天快照
    }
    maxRetainedScans := models.AppConfig.System.MaxRetainedScansPerRepo
    if maxRetainedScans <= 0 {
        maxRetainedScans = 10 // 默认每仓保留最近 10 轮完整快照
    }

    log.Println("[GC] Starting ledger retention and snapshot cleanup...")
    cleanedReports, err := services.PurgeExpiredScanSnapshots(models.DB, retentionDays, maxRetainedScans)
    if err != nil {
        log.Printf("[GC] Scan snapshot cleanup failed: %v", err)
        return
    }

    // 针对 PostgreSQL 执行非阻塞式 VACUUM ANALYZE，促使引擎及时回收 Dead Tuples 并刷新统计信息
    if models.DB.Dialector.Name() == "postgres" {
        _ = models.DB.Exec("VACUUM (ANALYZE) scan_scope_entries, defect_observations;").Error
    }
    log.Printf("[GC] Ledger retention cleanup completed. Pruned snapshots for %d expired reports.", cleanedReports)
}
```

### 5.3 批量清理算法（保留各仓基线，安全不踩踏）

清理逻辑不能简单按时间 `created_at < now() - 30d` 一刀切，必须防止“某些代码仓很久没扫，导致唯一的基线快照被删掉”的极端情况。采用**“保底 N 轮完整基线 + 结合时间窗”**的安全算法：

```sql
-- 安全识别可清理的报告快照 ID：
-- 对每个 (repo_id, task_family)，保留最新 N 轮扫描，仅清理超出 N 轮且创建时间超过 30 天的报告快照
WITH ranked_reports AS (
    SELECT 
        id AS report_id,
        repo_id,
        task_type_id,
        created_at,
        ROW_NUMBER() OVER(PARTITION BY repo_id, task_type_id ORDER BY id DESC) as rn
    FROM task_reports
    WHERE status IN ('success', 'degraded')
)
SELECT report_id 
    FROM ranked_reports 
    WHERE rn > 10 AND created_at < NOW() - INTERVAL '30 days';
```

对于获取到的过期 `report_id` 集合，在 PostgreSQL 下分批次执行分块删除（Batch Delete），每次删除 500 行并短暂休眠，避免单次大事务产生海量 WAL 和锁竞争：
```sql
-- 逐批轻量删除示例，避免长锁阻塞并发扫描
DELETE FROM scan_scope_entries 
WHERE report_id IN (SELECT report_id FROM expired_ids LIMIT 50);
```

> **架构演进提示（PostgreSQL 原生时间分区）**：  
> 对于企业级超高频扫描场景，`scan_scope_entries` 与 `defect_observations` 可进一步演进为 **PostgreSQL Declarative Partitioning（声明式按月分区表）**。届时清理超期快照只需执行 `DROP TABLE scan_scope_entries_y2026m01;`，可在 **0 毫秒** 内瞬间释放磁盘空间，零 WAL 日志开销，零死元组碎片产生。

## 六、数据模型与配置适配

### 6.1 配置项扩展（`config.yaml`）

在 `config.yaml` 的 `system` 节点下增加生命周期与 I/O 调优参数：

```yaml
system:
  # 缺陷台账历史快照保留天数 (默认 30 天)
  ledger_retention_days: 30
  
  # 同一代码仓保留的最新完整扫描快照数 (默认 10 轮，防止低频扫描仓基线丢失)
  max_retained_scans_per_repo: 10
  
  # 覆盖明细是否启用稀疏存储 (默认 true，推荐开启)
  enable_sparse_scope_storage: true
  
  # 覆盖清单全量压缩产物存储目录 (为空时跟随 report artifacts)
  scope_manifest_dir: ""
```

并在 [`models/config.go`](file:///home/fugui/codes/code-shield/models/config.go) 中映射该结构体字段及默认值填充逻辑。

### 6.2 索引轻量化调整

在 [`models/models.go`](file:///home/fugui/codes/code-shield/models/models.go) 中，为 `scan_scope_entries` 增加条件过滤索引支持：
* 随着稀疏存储落地，`scan_scope_entries` 表数据量大幅缩减，原有的唯一索引 `(scan_id, norm_path)` 体积将降至极小。
* 在 `defect_observations` 上确保 `(report_id, verdict)` 复合索引高效覆盖，使历史快照清理能在常数级时间完成定位。

---

## 七、实施路线图与验证口径

### 7.1 分阶段实施演进路线

```text
┌────────────────────────────────────────────────────────────────────────┐
│  Phase 1: 稀疏存储与短事务重构 (P0 - 紧急，消除写放大与锁冲突)           │
│   · 实施 filterSparseScope 稀疏过滤与 Manifest 文件压缩外置              │
│   · 重构 PersistScanFacts 为三阶段流水线，Matching 计算移出事务           │
│   · PostgreSQL 连接池与会话超时加固 (idle_in_transaction_session_timeout)│
└───────────────────────────────────┬────────────────────────────────────┘
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│  Phase 2: 生命周期 TTL 归档与定时 GC (P1 - 彻底根除存储膨胀)             │
│   · 实现 PurgeExpiredScanSnapshots 安全批量清理算法                      │
│   · 针对快照表触发 PostgreSQL VACUUM (ANALYZE) 空间回收                 │
│   · 在 cron_jobs 注册每日 04:00 的 LedgerRetentionGCJob                 │
│   · 补充配置项与单元测试验证                                             │
└───────────────────────────────────┬────────────────────────────────────┘
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│  Phase 3: 观测与可解释性联动 (P2 - 运维与诊断体验闭环)                   │
│   · 前端 / API 透视稀疏状态与 Manifest 下载通道                          │
│   · 完善系统健康度监控面板中的 PostgreSQL 连接池使用率与表膨胀/GC 指标   │
└────────────────────────────────────────────────────────────────────────┘
```

### 7.2 验收与实测口径

1. **写放大降低验证**：
   * 针对拥有 > 2,000 源文件的真实仓库执行一次仅修改单文件的增量扫描；
   * 验证 `scan_scope_entries` 表新增行数从 > 2,000 行降至 **< 50 行**；
   * 验证 `scope_manifest-{report_id}.json.gz` 文件正常生成，体积在 **10 KB 以内**。
2. **生命周期对账正确性验证**：
   * 运行全部既有用例（`service_test.go`, `ledger_test.go`, `matching_test.go`）；
   * 验证同 commit 重复扫描增量为 0，局部扫描保护（`COVERAGE_GAP`）行为与 07 号文档规范 100% 保持一致，无回归风险。
3. **高并发连接与死锁验证**：
   * 在 PostgreSQL 生产环境下，启动 10 个协程并发提交扫描结果；
   * 验证 `PersistScanFacts` 在整个压测周期内**连接池平稳无耗尽、零 `deadlock detected` 报错、零锁超时中断**，平均写事务耗时控制在 **15ms 以内**。
4. **TTL GC 幂等验证**：
   * 模拟写入 20 轮连续报告，触发 `RunLedgerRetentionGCJob`；
   * 验证仅清理第 1~10 轮的快照记录，最新 10 轮及当前活跃台账 `defects` 完整无损。
