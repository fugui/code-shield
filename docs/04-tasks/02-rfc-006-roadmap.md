# RFC-006 实施路线图：缺陷对账与跨报告比对机制演进（SDD + TDD）

> **关联文档**：[RFC-006 缺陷对账机制现状深度诊断与演进建议报告](../03-rfcs/RFC-006-缺陷对账机制现状深度诊断与演进建议报告.md)  
> **状态**：Implemented  
> **实施策略**：严格按 **P0-A -> P0-B -> P1 -> P2** 四阶段递增交付，每阶段先编写红灯测试用例与基准（TDD），实施后再执行 `go test`（含 `TEST_DB_DSN` 物理隔离 PostgreSQL）、`make lint-arch` 与前端检查闭环。

---

## 一、生产库基线审计结论（P0-B 只读 SQL 实测快照）

在本地 PostgreSQL `code_shield`（179 条 `defects`，438 条 `defect_observations`，观测时间跨度 `2026-10-08` ~ `2026-10-25`）运行 RFC-006 §3 四组只读审计 SQL，实测结论如下：

| 审计项 | 实测结果 | 根因印证 |
| :--- | :--- | :--- |
| `0.50 <= confidence < 0.65` 误合并审计 | `AI_FALLBACK` 命中 **18** 例，最小置信度 `0.5000`，均值 `0.5896` | `assign_band: 0.12` 与灰度兜底阈值倒挂导致低分强制合并 |
| 高密度文件候选截断审计 (`> 3` 缺陷/文件) | **19** 个 `(repo_id, task_type_id, norm_path)` 超 3 条，最高 **13** 条（`include/fmt/chrono.h`） | `max_candidates_per_observation: 3` + `AliasPath` 优先填充触发候选池截断 |
| 历史异常反弹 (`STATUS_CHANGED -> ACTIVE`) | `COVERAGE_GAP -> ACTIVE` **23** 次（19 缺陷）；`VERIFIED_PENDING -> ACTIVE` **14** 次（13 缺陷）；`DORMANT -> ACTIVE` **3** 次 | `updateMatchedDefect` 遗漏归零 `missed_count`/`dormant_rounds` 与 `matchedVerdict` 语义映射偏差 |
| `defect_class_major` 分布审计 | 共 **28** 个原始类别串，其中 `"无问题"` **26** 条、`"逻辑与正确性"` **29** 条 | `entity_assessment` 评估类任务结果混入缺陷账本，且缺乏受控词表归约 |

---

## 二、总体演进设计与模块契约

### 2.1 四阶段演进路线图

```text
[P0-A: 零指纹破坏止血]
  ├── 配置纠偏 (config.yaml / config.yaml.example / NormalizeDefaults 防线)
  ├── 召回预算与状态映射修复 (matching.go: 强特征优先 / budget后移 / HUMAN_CLOSED / matchedVerdict)
  ├── 账本衰变与不变更冻结 (ledger.go / scope_evaluator.go: missed_count归零 / GetBlobHash / RequireChange)
  ├── 同文件同函数实例区分 (identity.go: occurrence++ / TargetEntity隔离)
  └── 物理稳定器错误处理与预编译正则 (runner.go / anchor.go / coverage.go)
         │
         ▼
[P0-B: 黄金基准与受控词典定稿]
  ├── 建立 A/B/C/D 四类黄金回归基准 (reconciliation_benchmark_test.go)
  └── 10大内建任务词典全覆盖 + 修复"悬空指针"归类 + 禁止category回退title (stabilizer_taxonomy.go)
         │
         ▼
[P1: v1->v2 双轨平滑迁移与 4-Tier 离散槽位匹配重构]
  ├── 双轨 Identity (SlotKey/CoreSlotKey v2 + LegacySlotKey/LegacyCoreSlotKey v1)
  ├── 单次物理稳定化合并 (PersistScanFacts 直接复用 Stabilizer，消除磁盘二次读取)
  ├── 括号平衡作用域提取 (anchor.go: braceDepth 匹配, 上限 300 行, 支持 Python 缩进)
  ├── entity_assessment 显式物理隔离 (只写 AnalysisFinding + Scope，不写 defects 账本)
  └── 4-Tier 离散槽位匹配替代加权评分主导 + 原地热升级 + 废弃 slot_matching.SettleLedgerLifecycle
         │
         ▼
[P2: SSOT 双报告比对、主干分支守卫、上游确定性与前端对齐]
  ├── SSOT 双报告对比服务与接口 (GET /api/reports/compare?base_report_id=A&target_report_id=B)
  ├── 默认分支/局部范围衰变守卫 (TaskReport.Branch + RunParams.Branch + only_advance_on_default_branch)
  ├── 上游确定性增强 (debate 截断前确定性排序 + 可选 TargetSymbol + invoker/native PreferSeed)
  └── 前端标签统一与双报告比对视图 (reconciliationLabels.ts + ReportCompareTab.tsx + Design Tokens)
```

### 2.2 关键详细设计规范

#### 1. P0-A：零破坏性核心修正
1. **配置单调性自愈 (`models/config.go: NormalizeDefaults`)**：
   - `config.yaml.example` 与 `config.yaml` 修正为：`max_candidates_per_observation: 64`、`assign_band: 0.75`、`reject_below: 0.50`、`gray_zone_fallback_merge_score: 0.75`、`high_risk_severities: ["致命", "严重"]`，并确保具备 `scanner.determinism` 配置块。
   - 在 `GovernancePolicyConfig.NormalizeDefaults()` 增加防错兜底：
     - 兼容默认空值（`<= 0` 时保留既有测试契约：`64`, `0.90`, `0.65`, `0.45`, `0.70`, `0.60`）。
     - 若正数配置违反单调性 `AssignBand < RejectBelow`，自动回正为 `max(RejectBelow+0.05, 0.75)` 且不超过 `StrongSameThreshold`。
     - 若正数配置 `MaxCandidatesPerObservation > 0 && MaxCandidatesPerObservation < 16`，自动修正为 `64`。
2. **`matching.go` 召回与 Verdict 修复**：
   - `retrieveCandidates` 按信号强度由强至弱注入值槽：`AliasSlotHealed` -> `AliasRoot` -> `AliasResource` -> `AliasChain` -> `AliasRename` -> `AliasPath`。
   - `addAlias` 先检查 `candidates[alias.DefectID]` 是否已存在（存在则补强来源标签而不占新增配额），再判断 `len(candidates) >= limit`。
   - 对纯 `AliasPath` 弱命中优先纳入未终结缺陷（跳过仅靠路径撞车的 `RESOLVED`/`OBSOLETE`），避免挤占预算。
   - `SLOT_PRECISE` 精确槽位检索放开 `HUMAN_CLOSED`（仅跳过 `MERGED`），使人工豁免缺陷在精确复现时返回 `EXEMPT_RECHECK` 并拦截新开。
   - `matchedVerdict` 将 `VERIFIED_PENDING` 与 `HUMAN_CLOSED` 从 `REOPENED`/`PROBABLE` 调整为 `VerdictExisted`。
3. **`ledger.go` 与 `scope_evaluator.go` 生命周期守卫**：
   - `ScopeCoverageEvaluator` 接口新增 `GetBlobHash(normPath string) string`；`MemoryScopeEvaluator` 同步记录 `blobHashes`。
   - `updateMatchedDefect` 在任何复现更新中强制重置 `"missed_count": 0` 与 `"dormant_rounds": 0`。
   - `advanceUnmatchedDefects` 在进入 `missed++` 前检查：当 `input.Lifecycle.RequireChange == true` 且文件被覆盖、拥有非空前后 `BlobHash` 且 `defect.BlobHash == currentBlobHash` 时，冻结衰变并保持原状态。
4. **`identity.go` 多实例区分与防合并**：
   - `assignOccurrenceIndexes`：在保持同物理行重叠区（如 `"3"` 与 `"3-4"`）且语义一致时不裂变为多条的前提下，对互不重叠或 `CleanToken` / `TargetEntity` 不同的同类同框切片递增 `occurrence++`。
   - `BuildObservationGroups`：若两个观测点 `TargetEntity` 均非空且不相等，禁止在聚合分支合并。
5. **稳定器错误处理与性能稳态 (`runner.go` / `anchor.go` / `coverage.go`)**：
   - `runner.go` 严禁忽略 `stabilizer.Stabilize` 的 `error`，返回错误时记录日志并在降级或失败路径显式处理。
   - 将 `CleanSourceToken`、词边界匹配等高频正则提升至包级预编译变量，消除并行压测下的 CPU 抖动超时。
   - 修复 `coverage.go` 在 `PlannedFiles == 0` 且存在 `PlanReconciliation.MissingUnits/UnmatchedUnits` 时误判未降级的边界问题。

#### 2. P0-B：10 类内置任务全覆盖词典与黄金基准
- 修复 `stabilizer_taxonomy.go` 中 `悬空指针|悬垂指针` 错误归入 `ClassNPD` 的缺陷，迁入 `ClassUAF`。
- 按工作区 10 个内建任务 `tasks/*/meta.json` 扩展标准受控类别（含 `ClassMemoryLeak`, `ClassResourceLeak`, `ClassConcurrencyRace`, `ClassDeadlock`, `ClassLockSync`, `ClassNullDeref`, `ClassOutOfBounds`, `ClassDoubleFree`, `ClassUseAfterFree`, `ClassLogicBoundary`, `ClassExceptionSafety`, `ClassPerformance`, `ClassSecurityInjection`, `ClassFloatPrecision`, `ClassUnorderedContract`, `ClassObservability`, `ClassInterfaceCompat`, `ClassCodeQuality`, `ClassArchitecture`, `ClassTestEffectiveness` 等）。
- 约束 `ReduceControlledDefectClass(rawClass, title)`：**当 `rawClass` 非空时只匹配 `rawClass`，严禁因为未命中词表而回退扫描 `title` 自由文本**；仅当 `rawClass == ""` 时允许参考 `title`。

#### 3. P1：`v1 -> v2` 双轨迁移与 4-Tier 离散槽位匹配
1. **双轨 `Identity` 结构 (`types.go` & `identity.go`)**：
   - 保留主轨道（`v2`）：使用受控分类 `ReduceControlledDefectClass`、`ReconstructStatementWindow` 生成的 `ScopeBodyHash`/`CleanToken`、`ResourceIdentity = TargetEntity`（移除 `extractIdentityToken` 中 `len < 4` 的短路过滤）。
   - 同时保留兼容轨道（`v1`）：`LegacyDefectClassMajor`、`LegacyCoreSlotKey`、`LegacySlotKey`。
   - 在 `Stabilizer.Stabilize` 中同步计算并缓存 `NormPath` 与括号平衡提取的 `ScopeBodyHash`（通过 `fileLinesCache` 每文件仅读盘一次），并由 `PersistScanFacts` 直接复用 `input.StabilizedFindings`（若为空则在事务前调一次 `Stabilize`）。
2. **`anchor.go` 括号平衡作用域提取**：
   - `ExtractScopeAndBodyFromLines` 基于花括号深度 `braceDepth`（跳过字符串与注释，且对 `.py` 使用缩进守卫，单函数体上限 300 行）精准定位函数结束行，隔离紧邻下方函数的修改。
3. **`entity_assessment` 治理模式物理分流 (`service.go`)**：
   - 在 `PersistScanFacts` 开始事务前读取 `task.GovernanceMode`（或请求参数覆盖）；当为 `entity_assessment` 时，事务内仅持久化 `AnalysisFinding`、`ScanScopeEntry` 与报告覆盖率元信息，**跳过 `RunMatching` 与全部 `defects`/`defect_observations`/`defect_aliases`/`defect_events` 写入**。
4. **4-Tier 离散槽位匹配架构 (`matching.go` & `ledger.go`)**：
   - **Tier 1 (`SLOT_PRECISE`)**：精确匹配 `SlotKey`（支持 `v2` 主槽、`v1` 历史槽、以及 `AliasSlotHealed` 别名槽），配合兼容类别检查 `classesCompatible(defect, identity)`。
   - **Tier 2 (`CORE_SLOT_SINGLETON`)**：在同一 `(NormPath, ScopeKey, CoreSlotKey/LegacyCoreSlotKey)` 内唯一候选直接确定关联（即使代码下移跨过行号窗或函数体内增删无关语句）。
   - **Tier 3 (`SLOT_ALIAS` / `ELASTIC_SELF_HEAL`)**：跨路径重命名 (`AliasRename`) 或同一作用域内发生局部重写/换行微调但结构特征一致的弹性自愈关联。
   - **Tier 4 (`SCOPED_AI_ARBITRATION`)**：仅在同文件/相近作用域仍存在明确多义性候选时进入受限 AI 仲裁（最多保留有意义的少数候选，绝不做盲目跨类别兜底合并）。
   - **热升级闭环 (`ledger.go`)**：当存量 `v1` 缺陷被 `v2` 观测点匹配时，在 `updateMatchedDefect` 中原地刷新 `defect_class_major`, `core_slot_key`, `slot_key`（检查槽位唯一冲突），并将旧 `v1 SlotKey` 写入 `defect_aliases (AliasSlotHealed)` 实现可逆热升级。
   - **清理冗余路径**：`slot_matching.go` 中的 `SettleLedgerLifecycle` 标记为 `Deprecated`，生产链路统一由 `RunMatching` + `ledger.commitLedger` 收口。

#### 4. P2：SSOT 双报告比对、主干守卫、上游确定性与前端对齐
1. **SSOT 双报告比对引擎与接口**：
   - 后端服务：`defectlifecycle.CompareReports(db *gorm.DB, baseReportID, targetReportID uint) (*ReportCompareResult, error)`（置于 `services/defectlifecycle/projection.go`）。
   - 路由：`GET /api/reports/compare?base_report_id=A&target_report_id=B`（挂入受保护路由 `api`，校验正整数 ID 及仓库存在性与一致性；若两份报告分属不同仓库，返回 `400 Bad Request` 明确错误）。
   - **核心归因规则（Single Source of Truth）**：
     - 全量加载 Report A 与 Report B 的 `defect_observations`（并将所有 `DefectStatusMerged` 的 `DefectID` 通过 `merged_into_id` 解析至最终主缺陷 ID）。
     - 若为 `entity_assessment` 报告（无 `defect_observations` 账本记录），自动回退读取两份报告的 `analysis_findings` 并按 `(SlotKey, CoreSlotKey, NormPath)` 做物理槽位比对。
     - 从 DB `scan_scope_entries` **并融合 gzip 范围清单 (`report-{id}-scope-manifest.json.gz`)** 加载两份报告的完整文件覆盖集与 `BlobHash`，彻底避免稀疏作用域导致 `ONLY_A_CODE_UNCHANGED` 被误判为 `ONLY_A_NOT_COVERED`。
     - 输出 5 个互斥桶：`COMMON_PERSISTED`、`ONLY_A_CODE_CHANGED`、`ONLY_A_CODE_UNCHANGED`、`ONLY_A_NOT_COVERED`、`ONLY_B_NEW` 及汇总计数 `Summary`。
2. **主干分支 / 局部范围衰变守卫**：
   - `models.TaskReport` 与 `models.RunParams` 新增 `Branch` 字段；`LifecyclePolicyConfig` / `LifecyclePolicy` 新增 `OnlyAdvanceOnDefaultBranch`（默认 `true`）。
   - `runner.go` 在任务创建/写报告时记录实际分析分支与 `TargetScope`；若当前报告 `TargetScope != ""`（局部扫描）或分支非仓库默认分支且开启守卫，`advanceUnmatchedDefects` 冻结衰变推进，仅正常投影当前已发现观测。
3. **上游 LLM 稳定性增强**：
   - `services/engines/debate/engine.go`：在执行 `hunterOut.Candidates[:maxCap]` 截断前，对候选列表按“风险优先级权重 + `FilePath` 字典序 + `LineRange` + `Category`”进行确定性排序。
   - `services/engines/debate/types.go` & `output_contract.go`：为 `HunterCandidate`、`JudgeFinalVerdict` 与 `OutputContract` 补充可选 `TargetSymbol`，在 `assembleFindingFromJudge` 中向 `AnalysisFinding.TargetSymbol` 透传。
   - `services/invoker/invoker.go` & `native.go`：当开启 `PreferSeed`（`SeedPolicy == "deterministic"` 或 `Seed > 0`）时，注入基于请求特征/显式配置的确定性 `seed` 至聊天请求体中；检查并保留逐任务模型绑定能力。
4. **前端标签统一与比对视图**：
   - 新建 `frontend/src/components/report/reconciliationLabels.ts`，统一 `EXISTED -> "本轮复现 (历史存量)"`、`PROBABLE -> "疑似存量关联"` 等映射，并新增 `PASS -> "显式通过清理"`。
   - 在 `FindingCard.tsx` 与 `ReportReconciliationTab.tsx` 复用该字典。
   - 新建 `frontend/src/components/report/ReportCompareTab.tsx`，并在 `ReportViewer.tsx` 挂载“对比分析 (`compare`)”标签页（同步 `baseReportId` URL SearchParam，兼容从 `RepoReviewHistory.tsx` / `ReportsOverview.tsx` 跳转选择基线报告），样式全部基于 `--color-*` Design Tokens 并写入 `report.css`。

---

## 三、分阶段任务拆解与验收门禁 (TDD Checklist)

### 阶段 P0-A：零破坏性止血（不改线上槽位结构）
- [x] **P0-A.1** 修复 `config.yaml`、`config.yaml.example`、`models/config.go` (`NormalizeDefaults` 阈值单调性自愈与 `scanner.determinism`) 并补充 `models/config_test.go`。
- [x] **P0-A.2** 修复 `services/defectlifecycle/matching.go` 候选召回优先级、`addAlias` 预算边界、`HUMAN_CLOSED` 精准豁免召回及 `matchedVerdict` 映射。
- [x] **P0-A.3** 修复 `services/defectlifecycle/scope_evaluator.go` (`GetBlobHash`) 与 `ledger.go` (`missed_count`/`dormant_rounds` 归零与 `RequireChange` 不变更冻结)。
- [x] **P0-A.4** 修复 `services/defectlifecycle/identity.go` (`occurrence++` 与 `TargetEntity` 防误合并)、`runner.go` (`Stabilize` 错误处理)、`anchor.go` 正则预编译与 `coverage.go` 降级边界，修复既有失效单测。
- [x] **P0-A 门禁**：`go test ./models/... ./services/defectlifecycle/... ./services/coverage/... ./services/runner/...` 与 `TEST_DB_DSN=... go test ./models/... ./services/defectlifecycle/... ./services/coverage/...` 全绿通过。

### 阶段 P0-B：黄金基准测试与 10 类受控词典
- [x] **P0-B.1** 在 `services/defectlifecycle/reconciliation_benchmark_test.go` 建立并运行黄金基准 Test A / B / C / D。
- [x] **P0-B.2** 审计全量 10 个 `tasks/*/meta.json`，完善 `stabilizer_taxonomy.go`（修复“悬空指针”归类、补全 10 任务词表、限制非空 `category` 禁止回退 `title`），补充完备测试。
- [x] **P0-B 门禁**：基准 A/B/C/D 及词典全量回归测试 100% 通过。

### 阶段 P1：`v1 -> v2` 双轨迁移与 4-Tier 离散匹配核
- [x] **P1.1** `anchor.go` 实现基于花括号深度（及 Python 缩进）的 `ExtractScopeAndBodyFromLines`，并消除 `CleanSourceToken` / 词边界性能抖动。
- [x] **P1.2** `stabilizer.go` 与 `identity.go` 启用双轨身份（`v2` 主轨道 + `v1` `LegacySlotKey`/`LegacyCoreSlotKey` + 移除 `len < 4` 过滤 + 缓存 `ScopeBodyHash` 消除重复读盘）。
- [x] **P1.3** `service.go` 实现 `PersistScanFacts` 物理稳定化统一入口与 `entity_assessment` 显式旁路。
- [x] **P1.4** 重构 `matching.go` 为 4-Tier 离散槽位匹配流水线，`ledger.go` 实现匹配命中 `v1 -> v2` 原地热升级与 `AliasSlotHealed` 写回；标记 `slot_matching.SettleLedgerLifecycle` 为 `Deprecated`。
- [x] **P1 门禁**：黄金基准 A/B/C/D、`v1 -> v2` 热升级测试、`entity_assessment` 隔离测试与 `make lint-arch` 全部通过。

### 阶段 P2：SSOT 双报告比对、主干守卫、上游确定性与前端对齐
- [x] **P2.1** 实现默认分支与局部作用域衰变守卫（`models.TaskReport.Branch`、`RunParams.Branch`、`OnlyAdvanceOnDefaultBranch`、`runner.go`、`ledger.go`）及测试。
- [x] **P2.2** 实现上游确定性增强（`debate/engine.go` 截断前确定性排序、`TargetSymbol` 可选透传、`invoker/native.go` `PreferSeed` 注入）及测试。
- [x] **P2.3** 实现 `defectlifecycle.CompareReports` 与 `GET /api/reports/compare`，编写隔离库测试验证与 `GetReportReconciliation` 100% 同源一致性。
- [x] **P2.4** 完成前端 `reconciliationLabels.ts`、`FindingCard.tsx`、`ReportReconciliationTab.tsx`、`ReportCompareTab.tsx`、`ReportViewer.tsx` 与 Vitest 测试。
- [x] **P2 最终门禁**：`make lint-arch`、`make test`（含 `TEST_DB_DSN` 核心包测试）、`make lint`、`cd frontend && npm test`、`make build` 全绿通过，更新 RFC 状态并提交推送 `origin/main`。
