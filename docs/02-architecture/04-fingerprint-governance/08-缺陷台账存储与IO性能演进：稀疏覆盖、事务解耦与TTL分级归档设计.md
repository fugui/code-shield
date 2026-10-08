# 缺陷台账存储与 I/O 性能演进：稀疏覆盖、事务解耦与 TTL 分级归档设计

> **文档状态**：`Ready for Implementation (实施终稿 v3, 已达完全可实施级)` · 针对架构审计中"数据库 I/O 压力与写放大加剧"问题的落地工程方案  
> **修订依据**：[01-缺陷台账存储与IO性能演进设计(08号文档)架构深度检视与副作用评估报告](../../06-reviews/01-缺陷台账存储与IO性能演进设计(08号文档)架构深度检视与副作用评估报告.md) 及工程实施级深度复审意见  
> **设计范围**：缺陷台账持久化链路、`scan_scope_entries` 覆盖清单稀疏化存储、事务范围瘦身与读写解耦、PostgreSQL 连接池与长事务优化、数据生命周期 TTL 滑动窗口与归档清理机制、存量数据回填迁移与历史报告透明回退  
> **关联文档**：[07-问题清单跨轮增量治理重构设计：统一缺陷台账SSOT与分层对账算法](07-问题清单跨轮增量治理重构设计：统一缺陷台账SSOT与分层对账算法.md) · [01-CodeShield-数据模型与系统配置变更设计](../06-data-models/01-CodeShield-数据模型与系统配置变更设计.md) · [RFC-001-多租户隔离方案评估与分库架构选型](../../03-rfcs/RFC-001-多租户隔离方案评估与分库架构选型.md)

---

## 〇、一页纸摘要（TL;DR）

**背景**：在 07 号重构方案落地后，Code-Shield 成功建立了以数据库为单一真实源（SSOT）的缺陷台账体系，彻底终结了"同仓跨轮扫描假 NEW 泛滥"、"局部扫描误判已修复"等业务缺陷。但在架构审计中，敏锐地指出了相伴而生的数据库层负担：
1. **写放大严重**：单次扫描需向 `scan_scope_entries`（逐文件级别，通常数千条）、`defect_observations`（原始观测事实）、`defect_events` 及 `defects` 写入或更新数据；
2. **长事务霸占连接与阻碍 GC**：在 `PersistScanFacts` 中，全表查询、跨轮匹配（CPU 密集）与大批量插入被包裹在单个大事务内。在 PostgreSQL 环境下，引发写连接池耗尽（Connection Pool Starvation）、阻止 Autovacuum 回收死元组诱发表膨胀（Table Bloat），并存在并发扫描与人工标注时的死锁（Deadlock）隐患；
3. **存储无节制膨胀**：当前 `cron_jobs` 仅有清理磁盘临时文件的任务，完全缺乏数据库维度的清理归档机制，轮次快照数据将永久堆积。

**本设计核心改进措施**：
1. **覆盖清单稀疏化存储（Sparse Scope Storage）**：数据库仅记录发生变更（`diff_touched`）、扫描失败（`FAILED`）或命中缺陷的关键文件；其余数千个未变更且无缺陷的正常文件外置为轻量压缩 Manifest 文件。单次扫描写入行数**直降 95% 以上**。
2. **大事务瘦身与读写解耦（Short Commit Transaction）**：将重型 Matching 匹配计算移出写事务，流水线重构为「无锁快照读 $\to$ 纯内存计算 $\to$ 极简短事务提交」，并在短事务内**保留毫秒级 Advisory Lock 串行保护结合乐观锁重试循环**，写连接占用时间从几百毫秒压缩至 **<10 毫秒**。
3. **分级生命周期与 TTL 定时归档（Retention & Vacuum GC）**：台账主体（`defects`）永久保留并**冗余核心文本元数据实现自给自足**，轮次级快照（`scan_scope_entries`、`defect_observations`）建立 **30 天 / 最近 10 轮滑动窗口 TTL**；新增定时批量清理任务与 PostgreSQL VACUUM 回收机制，配套**回滚超期阻断**与**别名命中原子累加**安全护栏。
4. **存量平滑迁移与历史报告透明回退**：提供一次性存量文本回填迁移工具（`backfill_defect_texts`），并改造 `report_service.go` 的底层数据加载器，确保 TTL 清理后历史报告详情页透明回退读取归档 JSON，彻底实现生产无感过渡。

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
1. **95% 的数据库行数由"毫无变化且无缺陷的健康文件"贡献**：在代码维护期，每次 Git Push 通常仅修改 3~10 个文件，其余 2,990+ 个文件完全未变且无缺陷，但每轮都被机械地插入 1 行到 `scan_scope_entries`。
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

在实施存储与 I/O 优化时，必须恪守以下五项原则，确保 07 号重构的核心业务价值不受丝毫侵蚀：

| 编号 | 不变量原则 | 说明与红线 |
| :--- | :--- | :--- |
| **I1** | **台账精确度零折损** | 无论是全量还是稀疏存储，对账引擎回答"某缺陷是否落在扫描覆盖域内"、"某缺陷是否应当判定已修复"的结论必须与全量存储 **100% 逐比特一致**。 |
| **I2** | **覆盖判定语义等价** | 任何未在数据库建行的文件，必须能够通过快速内存查询/Manifest 文件恢复其覆盖事实，严禁因未落库而误判为 `COVERAGE_GAP`。**生命周期判定引擎必须面向抽象接口 `ScopeCoverageEvaluator` 而非物理落库行集，彻底解耦稀疏优化与逻辑覆盖判定。** |
| **I3** | **匹配计算无写锁** | 所有的候选检索、综合打分、全局最优指派必须基于内存纯计算完成，**严禁在持有数据库写锁期间执行复杂计算**。 |
| **I4** | **分级生命周期管理** | 核心缺陷台账（`defects`）是持久资产，必须永久保存；历史扫描观测与逐文件覆盖明细属于可再生快照，强制实施 TTL 滚动清理。 |
| **I5** | **台账文本自给自足** | `defects` 主表必须冗余持久化核心文本元数据（标题、分类、代码片段、修复建议、摘要），确保即使历史 `AnalysisFinding` 和 `defect_observations` 被 TTL 清理后，前端治理工作台仍能完整渲染缺陷信息，**严禁出现"盲台账/空壳幽灵"现象**。 |

---

## 三、方案一：覆盖清单稀疏存储（Sparse Scope Storage）与外置 Manifest

### 3.1 覆盖明细的本质业务分析

在对账与生命周期管理中，系统对 `scan_scope_entries` 的诉求仅有两点：
1. **修复守卫判定**：当存量缺陷在当前轮未复现时，系统需要判断"发现该缺陷的文件本轮是否被扫描且覆盖了？如果被覆盖了，该文件是否被 Git Diff 触碰（代码已修改）？"；
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

> **[核心澄清]**：稀疏落库仅影响 `scan_scope_entries` 的**物理存储行数**。生命周期对账引擎的覆盖判定**不直接依赖稀疏表**，而是通过 §3.3 定义的内存 `ScopeCoverageEvaluator` 接口基于全量计划文件集进行判定。稀疏表与覆盖判定逻辑完全解耦，任何文件（无论是否落库）均可在内存中获得精确的覆盖/变更/失败状态。

### 3.3 覆盖判定接口抽象与内存实现（`ScopeCoverageEvaluator`）

#### 3.3.1 覆盖判定接口契约

```go
// ScopeCoverageEvaluator 统一定义生命周期对账的覆盖判定契约。
// 该接口基于本次扫描的全量计划文件集（Planned Files）在内存中构建，
// 与数据库稀疏落库行为完全解耦。生命周期状态机推进代码仅通过此接口判定覆盖。
type ScopeCoverageEvaluator interface {
    // IsCovered 精确判断文件是否在本次扫描的计划且成功覆盖范围内
    // 对于全量扫描：所有计划文件中未失败的均视为已覆盖
    // 对于局部扫描：仅扫描计划内的文件视为已覆盖，计划外文件返回 false
    IsCovered(normPath string) bool

    // IsChanged 判断文件本轮是否被 Git Diff 触碰（代码变更）
    IsChanged(normPath string) bool

    // IsFailed 判断文件是否处理失败，返回失败标识及失败原因
    IsFailed(normPath string) (failed bool, reason string)
}
```

#### 3.3.2 内存高性能判定器实现

```go
// MemoryScopeEvaluator 基于全量计划文件集构建的内存高性能覆盖判定器。
type MemoryScopeEvaluator struct {
    plannedFiles map[string]bool   // 全量计划扫描文件集合 (所有 Planned Files)
    failedFiles  map[string]string // 扫描失败文件及错误原因
    touchedFiles map[string]bool   // diff_touched=true 的文件
}

// NewMemoryScopeEvaluator 从全量 ScopeEntry 切片装配内存判定器
func NewMemoryScopeEvaluator(fullEntries []models.ScanScopeEntry) *MemoryScopeEvaluator {
    e := &MemoryScopeEvaluator{
        plannedFiles: make(map[string]bool, len(fullEntries)),
        failedFiles:  make(map[string]string),
        touchedFiles: make(map[string]bool),
    }
    for _, entry := range fullEntries {
        e.plannedFiles[entry.NormPath] = true
        if entry.Outcome == ScopeFailed {
            e.failedFiles[entry.NormPath] = entry.FailReason
        }
        if entry.DiffTouched {
            e.touchedFiles[entry.NormPath] = true
        }
    }
    return e
}

func (e *MemoryScopeEvaluator) IsCovered(normPath string) bool {
    if _, failed := e.failedFiles[normPath]; failed {
        return false
    }
    return e.plannedFiles[normPath] // 在计划集内且未失败 → 已覆盖
}

func (e *MemoryScopeEvaluator) IsChanged(normPath string) bool {
    return e.touchedFiles[normPath]
}

func (e *MemoryScopeEvaluator) IsFailed(normPath string) (bool, string) {
    reason, failed := e.failedFiles[normPath]
    return failed, reason
}
```

#### 3.3.3 对账代码适配要求

在生命周期状态机推进的核心判定处（现网 [`ledger.go:641-642`](file:///home/fugui/codes/code-shield/services/defectlifecycle/ledger.go#L641)），必须替换为接口调用：

```go
// 旧代码（直接依赖物理落库行，稀疏化后必然 nil 导致自愈卡死）:
//   scope := scopeEntryByPath(input.Scope, defect.NormPath)
//   covered := scope != nil && scope.Outcome == ScopeScanned

// 新代码（通过抽象接口，基于内存全量计划文件集判定）:
covered := input.Evaluator.IsCovered(defect.NormPath)
changed := input.Evaluator.IsChanged(defect.NormPath)
```

这保证了无论 `scan_scope_entries` 表如何稀疏优化，对账引擎获得的覆盖判定结果与全量存储模式 **100% 逐比特一致**（恪守原则 I1/I2）。

### 3.4 稀疏过滤判别算法实现（`filterSparseScope` 算法定义）

为确保落库行数精准裁剪且代码完全可实施，`filterSparseScope` 的过滤判定规则如下：

```go
// filterSparseScope 将全量计划范围条目裁剪为稀疏落库集合与外置压缩 Manifest 字节
func filterSparseScope(input ScanInput, fullScope []models.ScanScopeEntry) (sparse []models.ScanScopeEntry, manifestGz []byte) {
    // 1. 从本轮 Findings 中构建规范化命中文件集合
    findingPaths := make(map[string]bool, len(input.Findings))
    for _, f := range input.Findings {
        norm := NormalizePath(input.RepoRoot, f.FilePath)
        if norm != "" {
            findingPaths[norm] = true
        }
    }

    // 2. 遍历计划全集，严格过滤关键稀疏事实
    sparse = make([]models.ScanScopeEntry, 0, 64)
    for _, entry := range fullScope {
        hasFindings := findingPaths[entry.NormPath]
        isFailed := entry.Outcome == ScopeFailed
        isTouched := entry.DiffTouched

        // 仅在失败、发生变更、或命中缺陷时保留物理行
        if isFailed || isTouched || hasFindings {
            sparse = append(sparse, entry)
        }
    }

    // 3. 全量数据序列化并 Gzip 压缩为 Manifest
    manifestGz, _ = compressScopeManifest(fullScope)
    return sparse, manifestGz
}
```

### 3.5 降载收益对比

对一次包含 3,000 个文件的典型增量扫描（变更了 5 个文件，报出 2 个缺陷）：

| 指标 | 现网全量落库方案 | 本设计稀疏落库方案 | 改善幅度 |
| :--- | :--- | :--- | :--- |
| **单次扫描写表行数** | 3,000 行 `ScanScopeEntry` | **7 行**（5个变更 + 2个发现） | **减少 99.76%** |
| **数据库单表索引膨胀** | +48 KB / 轮 | **+0.2 KB / 轮** | **减少 99.5%** |
| **覆盖明细插入耗时** | 120ms ~ 350ms | **< 3ms** | **提速 50~100 倍** |
| **全量覆盖明细归档** | 依赖庞大数据库表 | 磁盘保存 `scope_manifest.json.gz`（约 8KB） | 随时可查，对库零压力 |
| **生命周期判定精度** | 100% 正确 | **100% 正确**（通过 `ScopeCoverageEvaluator` 内存全量集保证） | **零折损** |

---

## 四、方案二：持久化流程"读写解耦"与大事务瘦身

### 4.1 三阶段解耦流水线设计

彻底打破当前在单个写事务内进行"查表 $\to$ Matching 计算 $\to$ 写表"的紧密耦合，重构为三个明确隔离的执行阶段：

```text
┌────────────────────────────────────────────────────────────────────────┐
│  Phase 1: 只读快照加载 (Read Snapshot) - [无排他写锁，并发友好]           │
│   · 批量查询 repo 下活跃的 defects 与 aliases                            │
│   · 构建全量 ScopeCoverageEvaluator（内存覆盖判定器）                     │
│   · 计算稀疏入库切片与 Manifest 压缩数据                                  │
│   · 同步预写 Manifest 临时文件（事务前完成，不走裸协程）                    │
│   · 耗时: 3~15ms                                                       │
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
│   · 获取同仓毫秒级 Advisory Lock (pg_advisory_xact_lock 串行保护)        │
│   · 校验最新扫描版本 (validateLatestLedgerReport 防时间线倒流)           │
│   · 批量写入稀疏 ScanScopeEntry (仅几十行)                              │
│   · 批量写入本轮 DefectObservation (数十至百行)                         │
│   · 批量更新/插入 Defects 状态变更 (仅变更行，含文本冗余刷新)              │
│   · 批量追加 DefectEvent (仅产生状态跃迁的事件)                          │
│   · 回写 TaskReport 摘要计数与 scope_hash                               │
│   · 耗时: 5~15ms (排他写锁瞬间获取并释放)                                │
│   · 若遇版本冲突触发重试，遇到滞后旧报告则降级标记为 superseded           │
└────────────────────────────────────────────────────────────────────────┘
```

### 4.2 重构后的执行伪代码

```go
// 显式哨兵错误定义（支持 errors.Is 精确匹配）
var (
    ErrDefectVersionConflict = errors.New("defect row_version conflict")
    ErrSupersededReport      = errors.New("report is superseded by a newer committed report")
)

func PersistScanFactsOptimized(input ScanInput) (*ScanFactsResult, error) {
    // ── 准备阶段：构建覆盖判定器与稀疏入库切片 ──
    groups := BuildObservationGroups(input.RepoRoot, input.Report.ID, input.Findings)
    fullScopeEntries := mergeChunkExecutionScopeEntries(input, buildScopeEntries(input))

    // 基于全量计划文件集构建内存覆盖判定器（对账专用，与稀疏落库解耦）
    evaluator := NewMemoryScopeEvaluator(fullScopeEntries)

    // 计算稀疏入库切片与 Manifest 压缩数据
    sparseEntries, manifestBytes := filterSparseScope(input, fullScopeEntries)

    // 同步预写 Manifest 到临时文件（事务前完成，不使用裸协程）
    tempManifestPath, err := writeTempManifest(input.Report.ID, manifestBytes)
    if err != nil {
        return nil, fmt.Errorf("write temp manifest: %w", err)
    }
    defer func() {
        // 若未成功 Rename 转正，自动清理临时文件
        if _, statErr := os.Stat(tempManifestPath); statErr == nil {
            _ = os.Remove(tempManifestPath)
        }
    }()

    // ── 带指数退避的并发重试循环 ──
    const maxRetries = 3
    var lastErr error

    for attempt := 1; attempt <= maxRetries; attempt++ {
        // ── Phase 1: 无锁快照读 ──
        var defects []models.Defect
        var aliases []models.DefectAlias
        if err := input.DB.Where("repo_id = ? AND task_type_id = ?", input.Repo.ID, input.TaskType.ID).
            Order("id").Find(&defects).Error; err != nil {
            return nil, fmt.Errorf("read defects snapshot (attempt %d): %w", attempt, err)
        }
        if err := input.DB.Where("repo_id = ? AND task_type_id = ?", input.Repo.ID, input.TaskType.ID).
            Order("id").Find(&aliases).Error; err != nil {
            return nil, fmt.Errorf("read aliases snapshot (attempt %d): %w", attempt, err)
        }

        // ── Phase 2: 纯内存匹配计算（零 DB 锁占用） ──
        ledgerGroups := ledgerObservationGroups(groups)
        matching := RunMatching(MatchingInput{
            Observations: ledgerGroups,
            Defects:      defects,
            Aliases:      aliases,
            Budget:       RuntimeMatchBudget(),
            Thresholds:   RuntimeMatchThresholds(),
        })
        decisions := buildDecisions(matching, groups, input)

        // ── Phase 3: 极简短事务提交（<15ms） ──
        var result *ScanFactsResult
        txErr := input.DB.Transaction(func(tx *gorm.DB) error {
            // 1. 获取同仓短事务排他锁（毫秒级串行保护，防止并发交叉死锁）
            if tx.Dialector.Name() == "postgres" {
                lockKey := fmt.Sprintf("%d:%d", input.Repo.ID, input.TaskType.ID)
                if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
                    return fmt.Errorf("acquire short ledger lock: %w", err)
                }
            }

            // 2. 乱序时间线校验：防旧报告覆盖新报告
            if err := validateLatestLedgerReport(tx, input.Report); err != nil {
                if errors.Is(err, ErrSupersededReport) {
                    return err // 明确返回滞后错误，不进行重试
                }
                return err
            }

            // 3. 批量写稀疏覆盖清单（仅几十行）
            if len(sparseEntries) > 0 {
                if err := tx.CreateInBatches(sparseEntries, 100).Error; err != nil {
                    return err
                }
            }

            // 4. 提交台账流转（使用 evaluator 进行覆盖判定，刷新 Defect 文本冗余）
            ledgerRes, err := commitLedgerOptimized(tx, groups, decisions, evaluator, input)
            if err != nil {
                return err
            }

            // 5. 回写报告汇总指标
            if err := updateReportSummary(tx, input.Report.ID, ledgerRes); err != nil {
                return err
            }

            result = ledgerRes
            return nil
        })

        if txErr == nil {
            // 事务提交成功，原子 Rename 将 Manifest 临时文件转正为正式路径
            finalPath := manifestFinalPath(input.Report.ID)
            if renameErr := os.Rename(tempManifestPath, finalPath); renameErr != nil {
                log.Printf("[Warn] Finalize manifest failed for report %d: %v (non-critical)", input.Report.ID, renameErr)
            }
            return result, nil
        }

        // 遇到滞后旧报告：跳过台账提交，降级返回
        if errors.Is(txErr, ErrSupersededReport) {
            log.Printf("[Ledger] Report %d is superseded by a newer completed scan. Skipping ledger commit.", input.Report.ID)
            return markReportSuperseded(input.DB, input.Report.ID)
        }

        // 遇到乐观锁版本冲突：退避重试（重新加载快照）
        if errors.Is(txErr, ErrDefectVersionConflict) {
            lastErr = txErr
            backoff := time.Duration(attempt*50) * time.Millisecond
            time.Sleep(backoff)
            continue
        }

        return nil, txErr
    }

    return nil, fmt.Errorf("commit scan facts conflict after %d retries: %w", maxRetries, lastErr)
}
```

### 4.3 乱序扫描报告（Out-of-Order Commit）与 Advisory Lock 协同策略

针对并发调度可能导致的乱序提交与死锁隐患，本设计确立以下协同策略：

1. **Advisory Lock 保留在 Phase 3 短事务内**：
   * 现网使用的 `SELECT pg_advisory_xact_lock(...)` 在 Phase 1~2 纯读和计算阶段**完全不加锁**；
   * 仅在进入 Phase 3 准备写数据库的这 **5~15ms** 内获取该锁，并在事务 `COMMIT` 时瞬间自动释放。
   * **收益**：既实现了并发读计算的完全并行，又在最后的落库微秒区间实现了仓级排他，从源头上消除了行锁死锁，使乐观锁 `row_version` 冲突概率降至极低。
2. **乱序提交业务降级（`ErrSupersededReport`）**：
   * 在 `validateLatestLedgerReport` 判定 `report.ID < latest.ID` 时，明确返回 `ErrSupersededReport`。
   * **业务处理原则**：后续扫描（如 Report 11）已经在更新的代码基线上确立了最新的缺陷台账真相，早前启动但晚完成的扫描（Report 10）属于已过时事实。系统**不进行重试**（因为重试永远无法改变 ID 大小），而是直接将 Report 10 标记为 `status = 'success', ledger_status = 'superseded'`，保留其代码分析报告产物，但跳过对主台账的写回，杜绝时间线倒流与重试死循环。

### 4.4 PostgreSQL 连接池与事务会话安全参数加固

在 [`models/db.go`](file:///home/fugui/codes/code-shield/models/db.go) 数据库连接初始化时，对连接池及事务会话参数进行防御性配置：

```go
// PostgreSQL 生产连接池与会话防御性加固 (models/db.go)
sqlDB, err := db.DB()
if err == nil {
    sqlDB.SetMaxOpenConns(models.AppConfig.Database.MaxOpenConns) // 默认 50
    sqlDB.SetMaxIdleConns(models.AppConfig.Database.MaxIdleConns) // 默认 10
    sqlDB.SetConnMaxLifetime(30 * time.Minute)
    sqlDB.SetConnMaxIdleTime(10 * time.Minute)
}

// 会话级事务超时保护 (注意：严格包裹方言检查，保障 SQLite 单测兼容)
if db.Dialector.Name() == "postgres" {
    db.Exec("SET idle_in_transaction_session_timeout = '10000';") // 10秒空闲断开
    db.Exec("SET statement_timeout = '60000';")                   // 60秒SQL兜底
    db.Exec("SET lock_timeout = '5000';")                        // 5秒行锁超时
}
```

---

## 五、方案三：缺陷台账分级生命周期与 TTL 定时归档

### 5.1 数据冷热分级与保留策略矩阵

建立清晰的生命周期分级，让活跃工作台始终轻盈，同时历史快照具备滚动回收能力：

| 数据分级 | 涉及数据表 | 业务定位 | 推荐保留周期 (TTL) | 清理/归档动作 |
| :--- | :--- | :--- | :--- | :--- |
| **Tier 1: 核心 SSOT (热)** | `defects`<br>`defect_aliases` | 缺陷身份台账、当前治理状态、**自包含文本元数据**、自愈别名索引 | **永久保留**<br>(随代码仓销毁) | 状态流转推进（ACTIVE $\to$ RESOLVED $\to$ OBSOLETE），超长休眠可选择逻辑归档。**每次新观测确认时刷新冗余文本字段**（见 §6.2）。 |
| **Tier 2: 审计流水 (温)** | `defect_events` | 状态跃迁历史、人工确认与指派日志 | **保留 90 天**<br>或最近 50 次状态变更 | 保留所有包含人工意见（`actor_type = 'HUMAN'`）的关键事件；自动迁移/系统常规扫描事件超期批量清理。 |
| **Tier 3: 轮次事实快照 (冷)** | `scan_scope_entries`<br>`defect_observations`<br>`AnalysisFinding` | 单次扫描逐文件明细、原始观测事实快照 | **滑动窗口：保留最近 10 次 COMPLETE 扫描**<br>或最长 **30 天** | **清理前置条件**：确保已完成存量缺陷文本回填（§6.4）。超期自动批量分块物理删除；被清理轮次报告详情通过本地归档 JSON 透明回退（§6.5）。 |
| **Tier 4: 语义判决缓存 (冷)** | `defect_ai_verdict_cache` | AI 仲裁结果缓存 | **保留 60 天** | 长期未命中的冷缓存自动过期。 |

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

    // 清理超过 24 小时的未关联孤儿 Manifest 临时文件
    cleanedOrphans := services.PurgeOrphanManifestTempFiles(
        models.AppConfig.System.ScopeManifestDir, 24*time.Hour)
    if cleanedOrphans > 0 {
        log.Printf("[GC] Cleaned %d orphaned manifest temp files.", cleanedOrphans)
    }

    // 针对 PostgreSQL 执行非阻塞式 VACUUM ANALYZE，促使引擎及时回收 Dead Tuples 并刷新统计信息
    if models.DB.Dialector.Name() == "postgres" {
        _ = models.DB.Exec("VACUUM (ANALYZE) scan_scope_entries, defect_observations, analysis_findings;").Error
    }
    log.Printf("[GC] Ledger retention cleanup completed. Pruned snapshots for %d expired reports.", cleanedReports)
}
```

### 5.3 批量清理算法（保留各仓基线，安全不踩踏）

采用**"保底 N 轮完整基线 + 结合时间窗"**的安全算法：

```sql
-- 安全识别可清理的报告快照 ID：
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

对于获取到的过期 `report_id` 集合，分批次执行分块删除（Batch Delete 500 行并休眠），避免大事务锁竞争。

### 5.4 TTL 清理安全护栏：回滚超期阻断与别名命中解耦

1. **回滚超期阻断防护**：在 [`ledger.go`](file:///home/fugui/codes/code-shield/services/defectlifecycle/ledger.go) 的 `rollbackReportLedger` 函数入口校验水位，超出保留窗口的历史报告禁止回滚：
   ```go
   if report.LedgerCommittedAt != nil &&
       time.Since(*report.LedgerCommittedAt) > time.Duration(retentionDays)*24*time.Hour {
       return fmt.Errorf("report %d is outside the snapshot retention window; rollback rejected", reportID)
   }
   ```
2. **别名命中原子累加**：更新别名命中时改用 SQL 原子递增，彻底废除依赖 `defect_observations` 历史条目的全量 `COUNT(*)` 回算：
   ```go
   tx.Model(&models.DefectAlias{}).Where("id = ?", alias.ID).
       Updates(map[string]interface{}{
           "hit_count":      gorm.Expr("hit_count + 1"),
           "last_report_id": input.Report.ID,
           "updated_at":     time.Now(),
       })
   ```
3. **专项治理覆盖率统计适配**：[`campaign.go`](file:///home/fugui/codes/code-shield/services/defectlifecycle/campaign.go) 的覆盖率统计改为读取 `task_reports` 汇总字段（`total_files`、`scanned_files`），不查明细表。

---

## 六、数据模型、存量迁移与透明回退适配

### 6.1 配置项扩展（`config.yaml`）

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

并在 [`models/config.go`](file:///home/fugui/codes/code-shield/models/config.go) 中映射该结构体字段。

### 6.2 `Defect` 模型扩展：冗余自包含文本元数据

在 [`models/models.go`](file:///home/fugui/codes/code-shield/models/models.go) 中扩展 `Defect` 结构体：

```go
type Defect struct {
    // ── 既有指纹与治理状态 ──
    ID                   uint           `gorm:"primaryKey" json:"id"`
    RepoID               uint           `gorm:"not null;uniqueIndex:uq_defects_current_identity" json:"repo_id"`
    TaskTypeID           uint           `gorm:"not null;uniqueIndex:uq_defects_current_identity" json:"task_type_id"`
    Status               string         `gorm:"size:32;not null;default:'ACTIVE'" json:"status"`
    CanonicalFingerprint string         `gorm:"size:64;not null;uniqueIndex:uq_defects_current_identity" json:"canonical_fingerprint"`
    NormPath             string         `gorm:"size:512;not null" json:"norm_path"`
    LineStart            *int           `json:"line_start"`
    LineEnd              *int           `json:"line_end"`

    // ── 新增：自包含核心文本元数据 ──
    Title                string         `gorm:"size:500;not null;default:''" json:"title"`
    Category             string         `gorm:"size:255;not null;default:''" json:"category"`
    CodeSnippet          string         `gorm:"type:text;not null;default:''" json:"code_snippet"`
    Suggestion           string         `gorm:"type:text;not null;default:''" json:"suggestion"`
    DetailSummary        string         `gorm:"type:text;not null;default:''" json:"detail_summary"`

    RowVersion           int            `gorm:"not null;default:1" json:"row_version"`
    CreatedAt            time.Time      `json:"created_at"`
    UpdatedAt            time.Time      `json:"updated_at"`
}
```

### 6.3 存量历史缺陷文本平滑回填迁移方案（Data Backfill Migration）

为防止版本上线后存量历史缺陷沦为“空壳台账”，且避免 TTL 定时任务因“存在空文本缺陷”被永久卡死，提供独立的一次性平滑回填迁移程序 [`cmd/backfill_defect_texts/main.go`](file:///home/fugui/codes/code-shield/cmd/backfill_defect_texts/main.go)：

```go
// cmd/backfill_defect_texts 一次性存量文本回填逻辑核心
func BackfillDefectTexts(db *gorm.DB) error {
    var total int64
    db.Model(&models.Defect{}).Where("title = '' OR title IS NULL").Count(&total)
    log.Printf("[Migration] Found %d defects needing text backfill...", total)

    const batchSize = 200
    var processed int64

    for {
        var defects []models.Defect
        if err := db.Where("title = '' OR title IS NULL").Order("id ASC").Limit(batchSize).Find(&defects).Error; err != nil {
            return err
        }
        if len(defects) == 0 {
            break
        }

        err := db.Transaction(func(tx *gorm.DB) error {
            for _, d := range defects {
                // 查找该缺陷最近一次关联的 Finding 文本
                var finding models.AnalysisFinding
                subQuery := tx.Table("defect_observations").Select("report_id").Where("defect_id = ?", d.ID).Order("id DESC").Limit(1)
                if err := tx.Where("task_report_id = (?) AND norm_path = ?", subQuery, d.NormPath).First(&finding).Error; err == nil {
                    tx.Model(&models.Defect{}).Where("id = ?", d.ID).Updates(map[string]interface{}{
                        "title":          finding.Title,
                        "category":       finding.Category,
                        "code_snippet":   finding.CodeSnippet,
                        "suggestion":     finding.Suggestion,
                        "detail_summary": finding.Detail,
                    })
                }
            }
            return nil
        })
        if err != nil {
            return err
        }
        processed += int64(len(defects))
        log.Printf("[Migration] Backfilled %d / %d defects.", processed, total)
    }
    return nil
}
```

### 6.4 历史报告详情查询接口（`report_service.go`）透明回退改造

在 [`services/reports/report_service.go`](file:///home/fugui/codes/code-shield/services/reports/report_service.go) 的 `loadAllFindingsRaw` 中，解除原代码对 `!useLedger` 的排他限制，建立两级透明回退：

```go
// services/reports/report_service.go: loadAllFindingsRaw 透明回退重构
func loadAllFindingsRaw(report *models.TaskReport) ([]FindingItemDTO, error) {
    useLedger := report.LedgerCommittedAt != nil && models.DB != nil

    var rawList []map[string]interface{}
    // 1. 若启用了台账，优先从数据库查询本轮 Findings
    if useLedger {
        var dbFindings []models.AnalysisFinding
        if err := models.DB.Where("task_report_id = ?", report.ID).Order("id").Find(&dbFindings).Error; err == nil {
            for _, dbf := range dbFindings {
                rawList = append(rawList, analysisFindingToRawMap(dbf))
            }
        }
    }

    // 2. 透明回退：只要 DB 未查到数据（无论是否 useLedger，覆盖被 TTL 清理场景），无条件反序列化本地归档 JSON
    if len(rawList) == 0 {
        jsonPath := report.GetSynthesisJSONPath()
        if jsonBytes, err := os.ReadFile(jsonPath); err == nil {
            _ = json.Unmarshal(jsonBytes, &rawList)
        }
    }
    // ... 后续流转映射逻辑保持不变 ...
    return dtoList, nil
}
```

通过这一改造，无论历史报告的数据是否已被 TTL 清理，Web 前端查看 30 天前的报告详情时**均能 100% 完整展现当时的 Findings 清单**。

### 6.5 索引轻量化调整

在 [`models/models.go`](file:///home/fugui/codes/code-shield/models/models.go) 中：
* `scan_scope_entries` 表数据量缩减 95%，原有的唯一索引 `(report_id, norm_path)` 体积大幅缩小；
* 确保 `defect_observations` 包含 `(report_id, verdict)` 复合索引，保证 TTL 批量删除能在毫秒级定位。

---

## 七、实施路线图与验证口径

### 7.1 分阶段实施演进路线

```text
┌────────────────────────────────────────────────────────────────────────┐
│  Phase 1: 基础建设、模型扩充与存量回填 (P0 - 前置基线，零停机)           │
│   · Defect 模型扩展：新增 title/category/code_snippet 等文本字段         │
│   · 运行 AutoMigrate 并在发布后执行 cmd/backfill_defect_texts 回填存量   │
│   · 改造 report_service.go：实现 DB 清理后透明读取归档 JSON 兜底       │
└───────────────────────────────────┬────────────────────────────────────┘
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│  Phase 2: 稀疏存储与短事务重构 (P0 - 紧急，消除写放大与锁争用)           │
│   · 实现 ScopeCoverageEvaluator 接口与 MemoryScopeEvaluator            │
│   · 实现 filterSparseScope 与 Manifest 预写/转正机制                    │
│   · 重构 PersistScanFacts 为三阶段流水线，Advisory Lock 移入短事务     │
│   · 完善乐观锁重试循环与乱序报告 ErrSupersededReport 降级处理          │
│   · 适配 ledger.go 对账判定代码使用 evaluator 接口                      │
└───────────────────────────────────┬────────────────────────────────────┘
                                    ▼
┌────────────────────────────────────────────────────────────────────────┐
│  Phase 3: 生命周期 TTL 归档与定时 GC (P1 - 根治存储膨胀)                 │
│   · 实现 PurgeExpiredScanSnapshots 安全批量清理算法                      │
│   · 别名命中计数改为原子递增（废除全历史 COUNT 回算）                     │
│   · 回滚超期阻断防护（rollbackReportLedger 水位校验）                    │
│   · 注册 cron_jobs 每日 04:00 的 LedgerRetentionGCJob 及 VACUUM 回收    │
│   · 专项治理覆盖率统计适配（改读 task_reports 汇总字段）                │
└────────────────────────────────────────────────────────────────────────┘
```

### 7.2 验收与实测口径

1. **写放大降低验证**：
   * 针对拥有 > 2,000 源文件的真实仓库执行一次仅修改单文件的增量扫描；
   * 验证 `scan_scope_entries` 表新增行数从 > 2,000 行降至 **< 50 行**；
   * 验证 `scope_manifest-{report_id}.json.gz` 文件正常生成，体积在 **10 KB 以内**。
2. **生命周期对账正确性验证**：
   * 运行全部测试套件（`go test ./services/defectlifecycle/...`）；
   * 验证同 commit 重复扫描增量为 0，局部扫描保护（`COVERAGE_GAP`）与 07 号规范一致；
   * 验证存量缺陷所在文件未变更且无 findings 时，`ScopeCoverageEvaluator.IsCovered()` 正确判定覆盖，自愈推进不卡死。
3. **高并发连接与乱序提交验证**：
   * 在 PostgreSQL 环境下启动 10 个协程并发提交扫描结果；
   * 验证写事务平均耗时控制在 **15ms 以内**，连接池平稳无耗尽、零 `deadlock detected` 报错；
   * 模拟老任务晚于新任务提交，验证触发 `ErrSupersededReport` 时安全降级跳过台账，系统不抛异常。
4. **TTL GC 与回退完整性验证**：
   * 模拟写入 20 轮连续报告并触发 `RunLedgerRetentionGCJob`；
   * 验证存量缺陷文本非空，前端工作台展示完整；
   * 访问已被清理了 DB Finding 的第 1 轮报告详情 API，验证其能**透明反序列化本地 JSON 并完整返回 Finding 清单**，零空页面故障。
5. **安全护栏验证**：
   * 验证对超期历史报告执行 `rollbackReportLedger` 时明确返回超期拒绝错误；
   * 验证 TTL 批量清理后，存量别名的 `hit_count` 数据不丢失、不归零。
