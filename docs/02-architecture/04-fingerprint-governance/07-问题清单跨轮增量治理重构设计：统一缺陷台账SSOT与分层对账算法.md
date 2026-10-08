# 问题清单跨轮增量治理重构设计：统一缺陷台账 SSOT 与分层对账算法

> **文档状态**：`Draft (待评审)` · 拟提交 RFC 评审后转入 `Accepted`
> **设计范围**：同一代码仓多次扫描的「问题清单」增量治理全链路——数据模型、身份体系、对账算法、生命周期状态机、扫描覆盖真相、报告投影、迁移路线
> **重构性质**：**推倒式重构（Re-architecture）**，不是对既有对账代码的增量修补
> **关联文档**：[01 确定性源码指纹](01-确定性源码指纹与抗抖动增量生命周期架构设计.md) · [02 缺陷生命周期与空间几何对齐](02-下一代确定性缺陷生命周期与空间几何对齐架构设计.md) · [03 多任务通用分类收敛](03-基于现网实测的确定性指纹演进与多任务通用分类收敛设计.md) · [04 企业治理与历史记忆闭环](04-多任务企业治理与历史记忆闭环深度设计.md) · [05 报告对报告系统化对账](05-同一代码仓多次扫描问题清单的系统化对账与增量治理架构设计.md) · [06 混合对账与 R5 残差 AI 仲裁](06-混合对账架构演进：确定性漏斗增强与R5残差AI语义仲裁落地设计.md) · [实测复盘：重复缺陷无法识别归因](../../04-reports/02-post-mortems/01-实测复盘-现网连续扫描重复缺陷无法识别归因分析报告.md)

---

## 〇、一页纸摘要（TL;DR）

**问题**：同一代码仓被反复扫描，每轮都会产出一份「问题清单」。系统里明明存着历史清单，但新报告仍会把大量老问题标为 `NEW`。根因不是指纹算法不够聪明，而是**「身份、观测、治理状态」三件事情被揉进了四套并行机制里**，每套机制各自维护一份"真相"，互相覆盖、互相矛盾。

**结论**：本轮重构做三件事：

1. **数据模型**：把「缺陷台账」从报告 JSON 文件搬进数据库，成为唯一的、持久的、可演进的 SSOT；历史报告降级为**不可变观测快照**。新增八张表：`scan_runs`（扫描与覆盖真相）/ `scan_scope_entries`（逐文件覆盖明细）/ `defects`（台账实例）/ `defect_aliases`（身份别名）/ `defect_observations`（扫描观测）/ `defect_links`（判定决策）/ `defect_events`（状态审计）/ `defect_ai_verdict_cache`（AI 判决缓存）。
2. **身份体系**：确立铁律——**指纹是检索手段，不是身份本身；身份 = 持久台账行 + 稳定别名集合**。身份由确定性物理量算出候选桶（`K1/K2`），由历史匹配结果累积别名，随代码漂移自愈，永不因内容变化而"变成新问题"。
3. **对账算法**：用「规范化锚定 → 单轮内多视角合并 → 倒排检索 → 确定性打分 → **全局最优指派** → 受限 AI 仲裁」五层替代现有的贪心 `if/else` 漏斗。AI 只回答"是否同一"，不产生身份、不决定生命周期；任何降级路径都**禁止产生假 NEW**。

**验收口径（一句话）**：同一 commit、同一扫描范围重扫，增量必须为 0；局部扫描不得判定修复；22 组现网实测重复对必须全部认领；同函数内两个不同缺陷必须不被合并。

---

## 一、问题定义与现状全景

### 1.1 业务诉求的精确表述

用户诉求在工程上应被翻译为三条可验收的能力，而不是"去重"这一模糊词：

| 编号 | 能力 | 精确定义 | 反例（当前系统实际表现） |
| --- | --- | --- | --- |
| C1 | **同仓同任务跨轮认领** | 第 N 轮报出的问题，若是第 N-1 轮（乃至更早）已报出的同一物理缺陷，必须标记为存量而非新增 | 同一 `SensorDevice::GetEnumCfgItem` 空指针，R1=EXISTED → R2=NEW |
| C2 | **修复判定可信** | 只有"扫描确实覆盖了该文件"且"该区域代码确实发生实质变化"时，才允许判定已修复 | 分片失败 / 局部扫描时把未覆盖到的问题批量标记为 RESOLVED |
| C3 | **同一 code base 的问题清单唯一** | 报告、工作台、导出、门禁看到的是同一份台账的同一份结论 | 报告头部 `new_defects_count` 与对账摘要 `new_count` 来自两套算法，数字不一致 |

### 1.2 现状：四套并行机制与三个"唯一真值中心"

当前代码里同时存在四套跨轮比对实现，且每套都声称自己是 SSOT：

| # | 机制 | 入口 | 存储载体 | 自述定位 |
| --- | --- | --- | --- | --- |
| M1 | 指纹 Diff 引擎 | `defects.DiffAndEnrichFindings`（[diff_engine.go:15](../../../services/defects/diff_engine.go#L15)），由 [runner.go:196](../../../services/runner/runner.go#L196) 与 [services.go:242](../../../services/services.go#L242) 调用 | 数据库表 `defect_fingerprint_records`（[models.go:677](../../../models/models.go#L677)，注释自称"SSOT 唯一真值中心"） | 逐条指纹增量打标 `NEW/EXISTED/REOPENED/RESOLVED` |
| M2 | 报告对报告对账（R2R） | `reconciliation.Reconcile`（[engine.go:14](../../../services/reconciliation/engine.go#L14)），由 [synthesis.go:79](../../../services/runner/synthesis.go#L79) 调用 | **上一份成功报告的 synthesis JSON 文件**（`SynthesisLedger`，[types.go](../../../services/reconciliation/types.go)），自称"完整问题台账 SSOT" | R1~R6 漏斗 + R5 残差 AI 仲裁，产出 `items[]/archived_items[]` |
| M3 | 专项治理归并 | `governance.HandleGenericCampaign`（[campaign.go:37](../../../services/governance/campaign.go#L37)），由 [finalize.go:131](../../../services/runner/finalize.go#L131) 调用 | 数据库表 `campaign_findings`（[models.go:631](../../../models/models.go#L631)） | 路径+标题哈希、行号相似度、标题相似度、LLM 模糊比对，并自动 resolve |
| M4 | 报告条目明细表 | `models.AnalysisFinding`（[models.go:474](../../../models/models.go#L474)） | 数据库表 `analysis_findings` | 本身携带 `fingerprint/diff_status` 字段，被 M1 写入 |

再叠加两张判定记录表 `scan_reconciliations`（[models.go:754](../../../models/models.go#L754)）与 `reconciliation_links`（[models.go:794](../../../models/models.go#L794)），系统里关于"这个问题是不是新问题"共有 **3 份持久化结论 + 1 份文件结论**。

### 1.3 现状失效模式清单（含代码证据）

以下问题不是"实现有 bug"，而是**架构层面无法收敛**。逐条给出定位，作为重构的验收反例。

| 编号 | 失效模式 | 证据 | 后果 |
| --- | --- | --- | --- |
| F1 | **双写覆盖**：M1 把 `NEW/EXISTED` 写进 `AnalysisFinding.diff_status` 与 `TaskReport.new_defects_count`；M2 随后用 `SynthesisItem.diff_status` 重写报告侧结论 | [diff_engine.go](../../../services/defects/diff_engine.go) 尾部更新 `new_defects_count`；[synthesis.go:87](../../../services/runner/synthesis.go#L87) 将台账写入 `report-{id}-synthesis-*.json` | 报告头部与对账摘要数字不一致；M1 的结论对报告实际不可见，成为"昂贵的死代码" |
| F2 | **基线取"上一份报告文件"而非台账** | [synthesis.go:47](../../../services/runner/synthesis.go#L47) 取 `id < current ORDER BY id desc` 的成功报告，再读其 synthesis 文件 | 上一轮分片失败/局部扫描/文件被清理 → 血统断裂，整轮误判为全量 NEW |
| F3 | **贪心匹配 + `break`**：Tier 循环内一旦找到第一个相似候选即认领 | [funnel.go:94](../../../services/reconciliation/funnel.go#L94) Tier 1~4 逐层贪心 | 结果依赖输入顺序；同函数相邻两个缺陷可能踩踏合并或错配 |
| F4 | **残差熔断方向错误**：残差合计 > 10 直接放弃全部匹配 | [arbitration.go:282](../../../services/reconciliation/arbitration.go#L282) `if total > 10 { return nil }` | 大重构一轮直接产出成百条假 NEW，正是用户投诉的现象 |
| F5 | **AI 逐对调用**：残差两两配对逐次调用 LLM | [arbitration.go](../../../services/reconciliation/arbitration.go) 双层循环内 `arbitratePairWithAI` | 成本 O(n·m)、耗时不可控；首个成功即 `break`，顺序敏感 |
| F6 | **模板族启发式硬编码**：文件名含 `mcpmanager`/`.proto` 才聚族 | [arbitration.go:80](../../../services/reconciliation/arbitration.go#L80)（`ClusterTemplateFamilies` 内部，函数起点 L30） | 对特定项目特化，换仓即失效，且聚类结果参与展示语义 |
| F7 | **覆盖真相缺失**：`HasFailedChunks` 只在 M3 中拦截自动 resolve，M1/M2 不看 | [finalize.go](../../../services/runner/finalize.go) vs [diff_engine.go](../../../services/defects/diff_engine.go) vs [pruner.go](../../../services/reconciliation/pruner.go) | 扫描不完整仍判修复；覆盖缺口 `COVERAGE_GAP` 与"真修复"混淆 |
| F8 | **身份 = 内容哈希**：`SHA256(repo\|task\|path\|scope\|token)` 被当作缺陷身份证 | [fingerprint.go:15](../../../services/defects/fingerprint.go#L15) | 代码一改（作用域改名、语句换行、注释）身份即断裂。这是"每次都产生新问题"的第一性原因 |
| F9 | **上游字段不稳定直接进入匹配**：`category/scope_symbol/trigger_line/line_number/title` 五类字段由 LLM 自由生成 | [实测复盘 §3](../../04-reports/02-post-mortems/01-实测复盘-现网连续扫描重复缺陷无法识别归因分析报告.md) | 同问题仅因分类写法不同就被拆成多条（"空指针解引用"至少 8 种写法） |
| F10 | **M3 用标题做身份**：`path + title` 唯一索引 + 标题相似度 0.4 权重 | [campaign.go](../../../services/governance/campaign.go) OnConflict 四列 | 标题是 LLM 自由文本，天然抖动；工作台与报告结论再次分叉 |

### 1.4 根因收敛：三个被混淆的概念

所有失效模式最终收敛到同一句话：**系统把"身份"当成了"内容的函数"**。

| 概念 | 正确语义 | 当前错误实现 |
| --- | --- | --- |
| **身份 Identity** | "这是同一个缺陷实例"——持久、可演进、跨轮不变，应存为台账行 + 别名 | 由当前轮的内容哈希即时计算，内容变则身份变（F8） |
| **观测 Observation** | "本轮扫描看到了什么"——不可变事实快照，含 LLM 原始文本与物理锚点 | 与身份混存，并被反复重写（F1） |
| **治理状态 Governance** | "该缺陷现在处于什么状态、谁负责、是否豁免、修复证据" | 分散在 4 张表与报告 JSON 中，多写者（F1/F2） |

因此本轮设计的第一原则是：**把这三件事在数据模型与模块边界上彻底拆开**。

### 1.5 为什么"再打补丁"无法收敛

01~06 号文档已经把能打的补丁打完了：路径归一化 SSOT、作用域规范化、受控 Taxonomy、几何对齐、防假修复守卫、R5 语义仲裁。它们共同的问题是：

- 每一层都在**修正输入的不确定性**，但都建立在"内容哈希即身份"这一错误地基上（F8），因此每新增一种漂移形态就要新增一条容错规则——规则数量随漂移形态组合爆炸。
- 对账产物仍然落在**报告文件**上，天然是"上一轮的函数"而非"历史的函数"（F2），无法支持"第 N 轮 vs 全部历史"。
- 判定权分散在 M1/M2/M3 三处（F1/F7/F10），任何一处改进都会被另两处的旧结论稀释。

**要一次解决，必须先换地基，而不是继续加容错层。**

---

## 二、设计原则（不可违背的不变量）

以下 9 条不变量是本设计的红线，任何实现方案、后续演进都不得突破。它们同时构成代码评审与测试断言的清单。

| 编号 | 不变量 | 违反症状 | 强制手段 |
| --- | --- | --- | --- |
| **I1** | **单一写者**：全系统只有 `ledger` 模块有权写 `defect.status / diff_status / 增量计数` | 报告与工作台数字打架 | 其他模块只读；`AnalysisFinding.DiffStatus` 改为只读投影字段；CI 静态检查禁止跨包写 |
| **I2** | **单一 SSOT**：台账在数据库，报告是投影视图；历史报告仅是观测快照 | 删报告文件即丢血统 | 报告生成改为读台账投影；synthesis JSON 降级为导出产物 |
| **I3** | **身份非内容函数**：身份是台账行 + 别名集合；指纹只用于检索与置信度计算 | 改一行代码就变新问题 | 别名追加式自愈；禁止用「新指纹查不到」直接判定 NEW |
| **I4** | **物理量优先**：**身份键**只能由确定性物理量构成（路径、符号、语句形状、Token、行区间、Blob/Hash）；打分允许使用受控枚举分类（taxonomy 收敛后的 `defect_class_major`），但**禁止**让 LLM 自由文本进入匹配 | LLM 措辞漂移导致误判 | 匹配输入结构中不出现 `title/detail/suggestion`；`defect_class` 必须是受控字典值 |
| **I5** | **覆盖先于修复**：没有覆盖证据，不得判定修复；任何覆盖不确定一律降级为 `COVERAGE_GAP` | 局部扫描批量误销账 | 生命周期判定强制读取 `scan_scope` |
| **I6** | **全局最优且顺序无关**：匹配是全局指派问题，不是逐条贪心 | 同输入不同顺序结论不同 | 残差走最小代价指派；输出强制排序 |
| **I7** | **AI 判决受限**：AI 只回答"这两个锚点是否同一缺陷"，不得新建身份、不得改状态机、不得写库 | AI 幻觉污染台账 | 结构化 JSON schema + 举证字段 + 结果落缓存表再应用 |
| **I8** | **降级保守**：任何超预算/超时/失败路径，结论只能是"疑似存量待确认"，永远不能是"新增" | 熔断引发假 NEW 风暴（F4） | 降级分支统一返回 `PROBABLE`，禁止返回 `NEW` |
| **I9** | **可重放幂等**：相同输入（同一 scan + 同一基线 + 同一算法版本）必须产出逐字节一致的判定 | 重跑对账结果漂移 | 算法版本号入库；AI 判决缓存；无 map 迭代序依赖 |

---

## 三、总体架构

### 3.1 领域分层：身份 / 观测 / 台账 / 投影

```text
┌──────────────────────────────────────────────────────────────────────────────┐
│  L4 投影层 Projection (只读)                                                  │
│   · 报告问题清单视图   · 工作台/我的缺陷   · 导出 Excel   · PR 门禁            │
│   全部从 defects + scan_runs 派生；diff_status / item_uid 仅在此层生成         │
└───────────────────────────────▲──────────────────────────────────────────────┘
                                │ 读
┌───────────────────────────────┴──────────────────────────────────────────────┐
│  L3 台账层 Ledger (唯一写者)                                                  │
│   · defects 生命周期状态机  · defect_events 审计  · 修复证据链               │
│   · defect_aliases 身份别名索引（唯一写入口）                                 │
└───────────────────────────────▲──────────────────────────────────────────────┘
                                │ 裁决
┌───────────────────────────────┴──────────────────────────────────────────────┐
│  L2 对账层 Matching (纯函数 / 可重放)                                        │
│   · 候选检索 → 确定性打分 → 全局指派 → 受限 AI 仲裁 → 指派决策输出             │
│   不读写数据库，输入 Candidate/Observation，输出 Decision[]                    │
└───────────────────────────────▲──────────────────────────────────────────────┘
                                │ 规范化后的观测
┌───────────────────────────────┴──────────────────────────────────────────────┐
│  L1 身份层 Identity (纯函数 / 确定性)                                        │
│   · 路径规范化 · 符号提取 · 语句形状 · 锚点吸附 · 指纹阶梯 K1/K2/F1..F4       │
│   · 单轮内观测合并（多视角）                                                  │
└───────────────────────────────▲──────────────────────────────────────────────┘
                                │ 原始观测 + 扫描范围
┌───────────────────────────────┴──────────────────────────────────────────────┐
│  L0 采集层 Ingest                                                            │
│   · 引擎原始 findings（AnalysisFinding DTO）  · scan_scope 覆盖清单          │
└──────────────────────────────────────────────────────────────────────────────┘
```

### 3.2 一次扫描的完整数据流

```text
① BeginScan
   报告创建时登记 scan_runs：repo / task_family / head_commit / baseline_scan_id /
   governance_mode / scope_hash / algorithm_version

② 引擎产出原始 findings（含 LLM 自由文本）+ 覆盖清单（chunk → files → 成功/失败）

③ identity.Normalize(findings, repoRoot)
   → 每个 finding 得到确定性锚点与指纹阶梯（K1/K2/F1/F2/F3）
   → 单轮内多视角合并（同一锚点被多个 chunk 报出 → 合成一个观测组）

④ ledger.Reconcile(scan, observations)
   a. 从 defects + defect_aliases 构建候选桶（倒排）；
   b. 确定性打分；c. 残差全局最优指派；d. 残余灰色带交 AI（批量+缓存）；
   e. 输出 Decision[]（每个观测组 → MATCH(defect_id) / NEW）

⑤ ledger.Commit(scan, decisions)  【唯一事务】
   → upsert observations；更新 defects（last_scan/severity/别名自愈）；
   → 按 scan_scope 真相推进生命周期（ACTIVE / COVERAGE_GAP / DORMANT / RESOLVED /
     VERIFIED_PENDING / OBSOLETE）；
   → 追加 defect_events；写 scan_runs 汇总计数

⑥ projection.Render(scan)
   → 报告 items[]（含 diff_status/item_uid）；工作台；导出；门禁
   → 汇总计数回写 task_reports（由同一事务派生，不再由 diff_engine 单独写）
```

### 3.3 唯一裁决者与调用点收敛

重构后，任务执行链路中三处调用收敛为一处：

| 现有调用点 | 现状 | 重构后 |
| --- | --- | --- |
| [runner.go:196](../../../services/runner/runner.go#L196) / [services.go:242](../../../services/services.go#L242) `DiffAndEnrichFindings` | 指纹打标 | **删除调用**，职责并入 `ledger.Commit` |
| [synthesis.go:79](../../../services/runner/synthesis.go#L79) `reconciliation.Reconcile` | R2R 对账 | 改为 `ledger.Reconcile + projection.Render`，只读台账，不再读上一份报告文件 |
| [finalize.go:131](../../../services/runner/finalize.go#L131) `HandleGenericCampaign` | 专项归并 + 自动销账 + LLM 比对 | **删除归并逻辑**，仅保留工作台所需的指派/评论交互（读写 `defects`/`defect_events`） |

### 3.4 身份命名空间策略（关键架构决策）

身份的作用域决定了"跨任务类型是否同一条问题"。给出两档策略，默认取保守档：

| 策略 | 命名空间 | 语义 | 适用 |
| --- | --- | --- | --- |
| `task_scoped`（默认） | `repo_id + task_family` | 同一任务族内跨轮认领；不同任务族各维护台账 | 与现状一致，风险最低；不同任务提示词对"同一行"的判定标准不同 |
| `repo_unified` | `repo_id` | 全仓统一缺陷台账；`task_type_id` 仅作为发现者归因 | "问题清单唯一"的终极形态；需 `defect_class` 跨任务收敛成熟后开启 |

**实现要求**：身份命名空间参数化为 `Namespace{RepoID, TaskFamily}`，`tasks` 元数据中声明 `task_family`（缺省回退为 `task_type.name`），使后续从 `task_scoped` 切到 `repo_unified` 只需一次台账合并迁移（按 `norm_path+scope_key+stmt_shape+occ` 归并，保留最早 `first_seen`）。

---

## 四、数据模型设计（SSOT）

> 说明：本系统采用 RFC-001 确定的**物理分库**多租户方案，每个租户独立数据库，因此以下表结构**不需要 `tenant_id`**。所有表由 GORM `AutoMigrate` 维护。
>
> **方言约定**：下列 DDL 以 PostgreSQL 语义书写（可读性最佳），但 RFC-001 的默认交付形态是**每租户独立 SQLite 文件**，企业规模可切 MySQL/PostgreSQL。P0 必须同时产出三套方言映射：`BIGSERIAL→SQLite INTEGER PRIMARY KEY AUTOINCREMENT / MySQL BIGINT AUTO_INCREMENT`、`TIMESTAMPTZ→SQLite TEXT(ISO8601) / MySQL DATETIME(6)`、`JSONB→TEXT/JSON`、部分唯一索引（`WHERE`）→SQLite 用表达式索引或触发器，MySQL 用生成列 + 唯一索引；按月分区与 `CREATE INDEX CONCURRENTLY` **仅 PostgreSQL 启用**。

### 4.1 表职责矩阵（新增 / 改造 / 废弃）

| 表 | 类型 | 职责 | 数据量级 | 处置 |
| --- | --- | --- | --- | --- |
| `scan_runs` | 新增 | 一次扫描的执行事实：commit、基线、治理模式、**覆盖真相**、汇总计数 | 低（每轮 1 行） | 新建 |
| `scan_scope_entries` | 新增 | 逐文件覆盖明细（成功/失败/排除/是否被 diff 触碰） | 中（每轮 = 文件数） | 新建，可按 scan_id 分区 |
| `defects` | 新增 | **缺陷台账实例（SSOT）**：身份锚点、当前状态、生命周期、修复证据、人工反馈 | 低（= 活跃缺陷数） | 新建 |
| `defect_aliases` | 新增 | 身份别名倒排索引：指纹/路径/符号漂移的累积映射 | 中（≈ defects × 1.2） | 新建 |
| `defect_observations` | 新增 | 每轮每个（合并后）观测的不可变快照与判定结果 | 高（每轮 = findings 数） | 新建，建议按月分区 + TTL 归档 |
| `defect_links` | 新增 | 判定决策的多对多关系（SPLIT/MERGED/MULTI_VIEW/人工确认） | 中 | 新建 |
| `defect_events` | 新增 | 状态机流转与人工操作审计（含指派/评论） | 中 | 新建；吸收现 `CampaignFinding.status_log` |
| `defect_ai_verdict_cache` | 新增 | AI 语义判决缓存（幂等 + 降本） | 低 | 新建 |
| `defect_fingerprint_records` | 现有 | 上一代指纹台账 | 中 | **迁移后废弃**（映射见 §4.8） |
| `campaign_findings` | 现有 | 专项缺陷清单（含状态/指派/评论） | 中 | **迁移后废弃**；工作台改读 `defects` |
| `analysis_findings` | 现有 | 报告条目明细 | 高 | 保留为观测快照表或停写归档（推荐：写入 `defect_observations`，本表只读） |
| `scan_reconciliations` / `reconciliation_links` | 现有 | 上一代对账会话/链接 | 中 | 保留只读，历史查询兼容；新逻辑不再写 |

### 4.2 `scan_runs`：扫描执行与覆盖真相

```sql
CREATE TABLE scan_runs (
  id                BIGSERIAL PRIMARY KEY,
  report_id         BIGINT      NOT NULL,          -- 关联 task_reports.id
  repo_id           BIGINT      NOT NULL,
  task_type_id      BIGINT      NOT NULL,
  task_family       VARCHAR(64) NOT NULL,          -- 身份命名空间
  baseline_scan_id  BIGINT,                        -- 上一轮「完整」扫描（NULL = 首扫）
  base_commit       VARCHAR(64),
  head_commit       VARCHAR(64),
  governance_mode   VARCHAR(32) NOT NULL,          -- full_ledger / change_focus / entity_assessment
  coverage_state    VARCHAR(24) NOT NULL,          -- COMPLETE / PARTIAL / CHANGE_FOCUS
  scope_hash        VARCHAR(64) NOT NULL,          -- 覆盖清单指纹（判定"同范围重扫"的依据）
  planned_files     INT NOT NULL DEFAULT 0,
  covered_files     INT NOT NULL DEFAULT 0,
  failed_files      INT NOT NULL DEFAULT 0,
  excluded_files    INT NOT NULL DEFAULT 0,
  raw_findings      INT NOT NULL DEFAULT 0,        -- 本轮原始条数（含多视角重复）
  observation_count INT NOT NULL DEFAULT 0,        -- 合并后观测组数
  new_count         INT NOT NULL DEFAULT 0,
  existed_count     INT NOT NULL DEFAULT 0,
  reopened_count    INT NOT NULL DEFAULT 0,
  resolved_count    INT NOT NULL DEFAULT 0,
  coverage_gap_count INT NOT NULL DEFAULT 0,
  probable_count    INT NOT NULL DEFAULT 0,        -- 灰色带待确认（保守不判 NEW）
  ai_calls          INT NOT NULL DEFAULT 0,
  ai_tokens         INT NOT NULL DEFAULT 0,
  algorithm_version VARCHAR(24) NOT NULL,
  started_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at       TIMESTAMPTZ,
  status            VARCHAR(24) NOT NULL DEFAULT 'running',
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (report_id)
);

CREATE INDEX idx_scan_runs_repo_family ON scan_runs (repo_id, task_family, id DESC);
CREATE INDEX idx_scan_runs_coverage    ON scan_runs (repo_id, task_family, coverage_state, head_commit);
```

**设计要点**

- `baseline_scan_id` 指向**上一轮 coverage_state = COMPLETE 的扫描**，而不是"上一份报告"。这是对失效模式 F2 的结构性修复：报告缺失、失败、局部扫描都不会污染基线。
- `scope_hash = H(sorted(planned_files) + governance_mode + excludes)`：若本轮与上轮 `head_commit` 相同且 `scope_hash` 相同，则机器可判定"同范围重扫"，此为 §10 零增量验收的判据。
- 计数一律在 `ledger.Commit` 事务内从决策结果派生，**报告头部数字与对账摘要数字必须来自同一行**。
- `UNIQUE (report_id)` 采用 **upsert 语义**（`ON CONFLICT (report_id) DO UPDATE`），使同一报告的重试/重跑在 `BeginScan` 阶段幂等，而不是直接抛唯一键冲突；重复重扫时若已存在同 `head_commit + scope_hash` 的 COMPLETE 扫描，则复用该 scan 记录。

### 4.3 `scan_scope_entries`：逐文件覆盖明细

```sql
CREATE TABLE scan_scope_entries (
  id           BIGSERIAL PRIMARY KEY,
  scan_id      BIGINT NOT NULL,
  norm_path    VARCHAR(512) NOT NULL,
  blob_hash    VARCHAR(64),                     -- 该文件在本轮 commit 上的内容哈希
  outcome      VARCHAR(24) NOT NULL,            -- SCANNED / FAILED / EXCLUDED / UNCHANGED_SKIPPED
  chunk_name   VARCHAR(256),
  diff_touched BOOLEAN NOT NULL DEFAULT false,  -- 是否落在本轮 git diff 变更区间
  hunk_ranges  JSONB,                           -- [{"start":120,"end":168}, ...]
  fail_reason  TEXT,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_scope_scan_path ON scan_scope_entries (scan_id, norm_path);
CREATE INDEX idx_scope_scan_outcome     ON scan_scope_entries (scan_id, outcome);
```

**大仓降载策略（可选）**：对 > 50k 文件的大仓，`scan_scope_entries` 可降级为「对象存储 manifest 文件 + `scope_hash` 摘要」，数据库中只保留计数与 manifest 指针；生命周期判定所需的"文件是否被覆盖"由 manifest 的布隆过滤器在内存中回答，误判方向配置为"认为未覆盖"（保守）。

### 4.4 `defects`：缺陷台账实例（核心 SSOT）

```sql
CREATE TABLE defects (
  id                BIGSERIAL PRIMARY KEY,
  repo_id           BIGINT      NOT NULL,
  task_family       VARCHAR(64) NOT NULL,
  task_type_id      BIGINT      NOT NULL,        -- 首次发现者（归因）

  -- ── 确定性身份锚点（全部为物理量） ──
  norm_path         VARCHAR(512) NOT NULL,
  scope_key         VARCHAR(64)  NOT NULL,       -- 路径无关的符号标识 H(SymbolPath|Kind|arity)
  symbol_path       VARCHAR(384),                -- 人类可读符号路径（展示/检索）
  stmt_shape        VARCHAR(64)  NOT NULL,       -- 语句形状哈希（标识符/字面量归一）
  occurrence_index  INT          NOT NULL DEFAULT 0,
  instance_ordinal  INT          NOT NULL DEFAULT 0,  -- 同锚点多缺陷实例序号（历史锚定，见 §5.2）
  anchor_key        VARCHAR(64)  NOT NULL,       -- K1 候选桶键
  context_key       VARCHAR(64),                 -- K2 上下文强键
  l1_fp             VARCHAR(64)  NOT NULL,       -- F1 当前精确指纹（可随代码漂移更新）
  l2_fp             VARCHAR(64),                 -- F2 路径无关结构指纹（跨文件移动/拆分）
  l3_fp             VARCHAR(64),                 -- F3 作用域指纹

  -- ── 物理证据快照（随轮次刷新，是"最近一次观测"） ──
  line_start        INT,
  line_end          INT,
  blob_hash         VARCHAR(64),
  scope_body_hash   VARCHAR(64),
  trigger_token     TEXT,

  -- ── 治理属性（语义字段，不参与身份） ──
  defect_class      VARCHAR(64) NOT NULL,        -- 受控分类（taxonomy 收敛后）
  cwe               VARCHAR(32),
  severity          VARCHAR(16),                 -- 确定性校准器产生，非 LLM 自由裁量
  severity_history  JSONB,
  owner_id          BIGINT,                      -- 责任人（现 CampaignFinding.assignee_id）

  -- ── 生命周期状态机 ──
  status            VARCHAR(24) NOT NULL,        -- ACTIVE/COVERAGE_GAP/DORMANT/VERIFIED_PENDING/RESOLVED/OBSOLETE/FALSE_POSITIVE/WONT_FIX/MERGED
  status_reason     TEXT,
  missed_count      INT NOT NULL DEFAULT 0,      -- 仅"覆盖且代码已变更但未复现"时累加（修复判定用）
  dormant_rounds    INT NOT NULL DEFAULT 0,      -- 退火计数：DORMANT 后每轮"被覆盖且未复现"累加，与 missed_count 分离
  first_scan_id     BIGINT NOT NULL,
  last_scan_id      BIGINT NOT NULL,
  first_seen_at     TIMESTAMPTZ NOT NULL,
  last_seen_at      TIMESTAMPTZ NOT NULL,

  -- ── 修复证据链（Proof of Fix） ──
  resolved_scan_id  BIGINT,
  resolved_at       TIMESTAMPTZ,
  resolved_commit   VARCHAR(64),
  resolved_diff_hunk TEXT,
  fix_pattern       VARCHAR(32),                 -- FIX_GUARD/FIX_DELETE/FIX_REFACTOR/FIX_DISAPPEARED
  proof             JSONB,
  merged_into_id    BIGINT,

  -- ── 人工反馈 ──
  feedback_status   VARCHAR(32),                 -- UNREVIEWED/FALSE_POSITIVE/WONT_FIX/CONFIRMED
  feedback_reason   TEXT,
  feedback_user_id  BIGINT,
  feedback_at       TIMESTAMPTZ,

  algorithm_version VARCHAR(24) NOT NULL,
  row_version       INT NOT NULL DEFAULT 1,      -- 乐观锁
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at        TIMESTAMPTZ
);

-- 同一锚点允许多个缺陷实例（同语句上并存的独立缺陷），用 instance_ordinal 区分
CREATE UNIQUE INDEX idx_defects_identity ON defects (repo_id, task_family, anchor_key, instance_ordinal)
  WHERE deleted_at IS NULL;
CREATE INDEX idx_defects_status ON defects (repo_id, task_family, status);
CREATE INDEX idx_defects_loc    ON defects (repo_id, task_family, norm_path, scope_key);
CREATE INDEX idx_defects_seen   ON defects (repo_id, task_family, last_scan_id);
```

**关键设计说明**

1. **`anchor_key` 不是唯一键**，唯一键是 `(repo, task_family, anchor_key, instance_ordinal)`。原因：同一代码语句可能承载两个独立缺陷（例如同一行既是空指针又是整数溢出）。这是现网"同一函数相邻两个缺陷踩踏合并"问题的正确解法——不是靠独占锁，而是靠实例分槽。**配套约束**：单轮内合并（§5.3）必须以 `defect_class_major` 为必要条件，否则两个独立缺陷会先在合并阶段被吃成一个观测组，槽位机制形同虚设（见 S5）。
2. **`instance_ordinal` 只增不改**：新观测落在已有锚点上时，优先匹配既有实例；只有当它既不被任何实例认领、又跨轮稳定出现时，才追加新槽位。**禁止用当前内容哈希重算槽位**，否则会重蹈 F8。
3. `l1_fp/l2_fp/l3_fp` 是"最近一次观测的指纹"，**允许随代码漂移而变化**；历史取值通过 `defect_aliases` 保留，检索时对新旧指纹都可命中。F4 为查询期派生键，**不落库、不写别名**（见 §5.2）。
4. `scope_key` 是**路径无关**的符号标识（`H(SymbolPath|Kind|arity)`），因此可在文件重命名、文件拆分、符号跨文件迁移后保持不变；`norm_path` 只承担"同文件"维度，不再进入符号键。
5. `severity` 只作为低权重的兼容性打分项，不参与身份键；`severity_history` 保留跨轮变化，取代现有 `severity_range/severity_triage` 的告警噪音。
6. `task_type_id` 记录"首次发现者"，用于归因分析；是否参与命名空间由 §3.4 策略控制。

### 4.5 `defect_aliases`：身份别名倒排索引

```sql
CREATE TABLE defect_aliases (
  id            BIGSERIAL PRIMARY KEY,
  defect_id     BIGINT NOT NULL,
  repo_id       BIGINT NOT NULL,
  task_family   VARCHAR(64) NOT NULL,
  alias_type    VARCHAR(16) NOT NULL,   -- 强键：CONTEXT(K2)/L1(F1)；弱键：ANCHOR(K1)/L2(F2)/L3(F3)/PATH/SYMBOL；人工：HUMAN
  alias_value   VARCHAR(512) NOT NULL,  -- 必须容纳 norm_path 全量（defects.norm_path = VARCHAR(512)），禁止截断
  alias_class   VARCHAR(8)  NOT NULL,   -- STRONG（全局唯一，一个键只属于一个实例）/ BUCKET（桶级，可多实例共享）/ HUMAN
  first_scan_id BIGINT NOT NULL,
  last_scan_id  BIGINT NOT NULL,
  hit_count     INT NOT NULL DEFAULT 1,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 强键（K2/F1）与人工确认（HUMAN）：一个别名值只能归属一个台账实例，冲突即视为并发已认领
CREATE UNIQUE INDEX idx_alias_strong ON defect_aliases (repo_id, task_family, alias_type, alias_value)
  WHERE alias_class IN ('STRONG', 'HUMAN');
-- 弱键（K1/F2/F3/PATH/SYMBOL）：桶级别名天然可能被同文件/同符号的多个实例共享，唯一性只到实例粒度
CREATE UNIQUE INDEX idx_alias_bucket ON defect_aliases (repo_id, task_family, alias_type, alias_value, defect_id);
-- 检索索引：强弱键统一走这条，单次 O(log n)
CREATE INDEX idx_alias_lookup ON defect_aliases (repo_id, task_family, alias_type, alias_value);
CREATE INDEX idx_alias_defect ON defect_aliases (defect_id);
```

**这是本设计最核心的一张表**：它把"内容哈希"从身份降级为检索手段，同时让身份具备**自愈能力**——每次确认匹配，就把当轮的 `K1/K2/F1/F2/F3` 与路径/符号追加为别名；代码漂移后新指纹虽不与旧 `l1_fp` 相等，但会命中历史别名，从而被正确认领。

**为什么必须区分强弱键**：`F3/F4/PATH/SYMBOL` 这类桶级键本身容忍同文件、同符号内的多个实例（正是 S5/S6 要保护的场景）。若对它们施加"一个键只有一个归属"的全局唯一约束，同文件第二个缺陷在写入别名时就会与第一个冲突，`ON CONFLICT DO UPDATE hit_count+1` 会把命中记到别人的实例上，造成静默的身份污染。因此：

- `CONTEXT(K2)/L1(F1)` 是**精确键**，同一时刻只能指向一个实例 → `STRONG`，唯一索引兜底并发；人工确认的 `HUMAN` 别名属于同一强约束（人已明确该键归属哪个实例），因此共享同一条部分唯一索引。
- `ANCHOR(K1)/L2(F2)/L3(F3)/PATH/SYMBOL` 是**桶键/回溯键**，允许多实例共享 → `BUCKET`，唯一性是 `(键, defect_id)`。
- 自愈写入按 `alias_class` 选择冲突目标：`STRONG/HUMAN` 走 `ON CONFLICT (repo_id, task_family, alias_type, alias_value) DO UPDATE SET hit_count = hit_count + 1`；`BUCKET` 走 `ON CONFLICT (repo_id, task_family, alias_type, alias_value, defect_id) DO UPDATE SET hit_count = hit_count + 1, last_scan_id = EXCLUDED.last_scan_id`。

### 4.6 `defect_observations` / `defect_links` / `defect_events` / `defect_ai_verdict_cache`

```sql
-- 观测快照：不可变事实（每轮每观测组一行）
CREATE TABLE defect_observations (
  id             BIGSERIAL PRIMARY KEY,
  scan_id        BIGINT NOT NULL,
  report_id      BIGINT NOT NULL,
  repo_id        BIGINT NOT NULL,
  task_family    VARCHAR(64) NOT NULL,
  defect_id      BIGINT,                       -- 提交事务内必被回填：新建实例也在同一事务中创建后写回，不以 NULL 作为长期状态
  group_uid      VARCHAR(64) NOT NULL,         -- 单轮内多视角合并组标识
  view_count     INT NOT NULL DEFAULT 1,       -- 同组合并了几个原始 finding
  verdict        VARCHAR(24) NOT NULL,         -- NEW/EXISTED/REOPENED/PROBABLE（MULTI_VIEW 只是合并组内的明细标记，不单独成行）
  match_tier     VARCHAR(16),                  -- EXACT/CONTEXT/STRUCT/SCOPE/FILE/ASSIGN/AI/HUMAN
  confidence     NUMERIC(4,3),
  reason         TEXT,
  anchor_key     VARCHAR(64) NOT NULL,
  context_key    VARCHAR(64),
  l1_fp          VARCHAR(64) NOT NULL,
  l2_fp          VARCHAR(64),
  l3_fp          VARCHAR(64),
  norm_path      VARCHAR(512) NOT NULL,
  scope_key      VARCHAR(64) NOT NULL,
  stmt_shape     VARCHAR(64) NOT NULL,
  occurrence_index INT NOT NULL DEFAULT 0,
  line_start     INT, line_end INT,
  blob_hash      VARCHAR(64), scope_body_hash VARCHAR(64),
  defect_class   VARCHAR(64), severity VARCHAR(16),
  payload        JSONB NOT NULL,               -- 原始 AnalysisFinding 完整快照（含 LLM 文本）
  algorithm_version VARCHAR(24) NOT NULL,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_obs_group  ON defect_observations (scan_id, group_uid);
CREATE INDEX idx_obs_scan   ON defect_observations (scan_id, verdict);
CREATE INDEX idx_obs_defect ON defect_observations (defect_id, scan_id);
CREATE INDEX idx_obs_anchor ON defect_observations (repo_id, task_family, anchor_key);

-- 判定决策图：支持 1:N / N:1 拓扑（拆分/合并/多视角/人工确认）
CREATE TABLE defect_links (
  id              BIGSERIAL PRIMARY KEY,
  scan_id         BIGINT NOT NULL,
  observation_id  BIGINT NOT NULL,
  defect_id       BIGINT,                      -- NULL = 不认领（NEW）
  relation        VARCHAR(24) NOT NULL,        -- SAME/MULTI_VIEW/SPLIT_FROM/MERGED_INTO/PROBABLE/RESOLVED_BY_CHANGE
  match_tier      VARCHAR(16),
  confidence      NUMERIC(4,3),
  score_detail    JSONB,                       -- 各分项得分（可解释性）
  reason          TEXT,
  algorithm_version VARCHAR(24) NOT NULL,
  confirmed       BOOLEAN NOT NULL DEFAULT false,
  confirmed_by    BIGINT, confirmed_at TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_links_scan   ON defect_links (scan_id);
CREATE INDEX idx_links_defect ON defect_links (defect_id, scan_id);
CREATE INDEX idx_links_obs    ON defect_links (observation_id);

-- 状态机与人工操作审计（工作台时间线直接读此表）
CREATE TABLE defect_events (
  id          BIGSERIAL PRIMARY KEY,
  defect_id   BIGINT NOT NULL,
  scan_id     BIGINT,
  event_type  VARCHAR(32) NOT NULL,   -- DISCOVERED/CONFIRMED/SEVERITY_CHANGED/COVERAGE_GAP/DORMANT/RESOLVED/VERIFIED_PENDING/REOPENED/OBSOLETE/FALSE_POSITIVE/WONT_FIX/MERGED_FROM/SPLIT_FROM/EXEMPT_RECHECK/ASSIGNED/COMMENTED
  from_status VARCHAR(24),
  to_status   VARCHAR(24),
  actor       VARCHAR(64) NOT NULL DEFAULT 'system',
  reason      TEXT,
  comment     TEXT,
  evidence    JSONB,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_events_defect ON defect_events (defect_id, id DESC);
CREATE INDEX idx_events_scan   ON defect_events (scan_id);

-- AI 判决缓存：保证幂等、抑制成本与抖动
CREATE TABLE defect_ai_verdict_cache (
  id             BIGSERIAL PRIMARY KEY,
  cache_key      VARCHAR(160) NOT NULL,       -- H(l2_a|l2_b|algorithm_version|prompt_version)
  is_same        BOOLEAN NOT NULL,
  confidence     NUMERIC(4,3),
  reason         TEXT,
  model          VARCHAR(64),
  prompt_version VARCHAR(16) NOT NULL,
  tokens         INT NOT NULL DEFAULT 0,
  hit_count      INT NOT NULL DEFAULT 0,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_hit_at    TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_ai_cache_key ON defect_ai_verdict_cache (cache_key);
```

### 4.7 状态枚举与生命周期状态机

**观测判定 `defect_observations.verdict`**

| 枚举 | 含义 | 是否产生"新问题"视觉效果 |
| --- | --- | --- |
| `NEW` | 全历史（含冷归档、含别名）均无匹配，且已通过灰色带确认 | 是（唯一允许标 NEW 的来源） |
| `EXISTED` | 计入存量台账 | 否 |
| `REOPENED` | 命中历史 `RESOLVED/DORMANT/VERIFIED_PENDING` 实例 | 是（标记"复发"，非"新增"） |
| `MULTI_VIEW`（**仅明细标记，不单独成行**） | 该条原始 finding 与同轮另一观测组的代表条目共享同一实例（多视角）；明细存于该行的 `payload.merged_views[]` | 否 |
| `PROBABLE` | 灰色带候选、AI 未决或超预算降级；**保守不判 NEW** | 否（计入"疑似存量待确认"） |

> 行级 `verdict` 只取 `NEW/EXISTED/REOPENED/PROBABLE` 四值，与 `UNIQUE (scan_id, group_uid)` 一致；`MULTI_VIEW` 是组内明细标记，只在 `payload.merged_views[]` 中出现（见 §5.3）。

**台账状态 `defects.status` 与流转**

```text
                        ┌─────────────────(命中)──────────────────┐
                        ▼                                          │
  [首扫] ── DISCOVERED ──▶ ACTIVE ──未被覆盖──▶ COVERAGE_GAP ──覆盖且未复现──▶ …
                          │  ▲                                            │
                          │  └────────── 命中（REOPENED）◀──────────┐      │
                          │                                          │      │
                          ├─ 高危未复现(1 轮) ──▶ VERIFIED_PENDING ──┘      │
                          │        │ 证据充分/人工确认                        │
                          │        └──────────────▶ RESOLVED ◀──────────────┘
                          ├─ 中危未复现(≥2 轮)+变更 ──▶ RESOLVED
                          ├─ 低危未复现(≥2 轮) ──▶ DORMANT ──dormant_rounds≥8──▶ OBSOLETE(归档)
                          ├─ 人工 FALSE_POSITIVE / WONT_FIX ──▶ 同名终态（不再进入报告）
                          ├─ 路径已删除(≥1 完整轮) ──▶ OBSOLETE(归档)
                          └─ 人工/算法合并 ──▶ MERGED(merged_into_id 指向吸收方)
```

| 状态 | 报告可见 | 计入风险分 | 说明 |
| --- | --- | --- | --- |
| `ACTIVE` | 是 | 是 | 正常存活 |
| `COVERAGE_GAP` | 是（独立分区） | 是 | 本轮未复现但覆盖证据不足或代码未变；**绝不能显示为已修复** |
| `VERIFIED_PENDING` | 是（"待确认已修复"） | 计 0.5 权重 | 高危疑似修复，等待人工确认 |
| `DORMANT` | 否（冷区） | 否 | 仅**建议级**退火休眠，仍可被别名唤醒 |
| `RESOLVED` | 否（"本轮修复"清单） | 否 | 有证据链 |
| `OBSOLETE` | 否 | 否 | 文件删除/长期休眠归档，超期物理清理 |
| `FALSE_POSITIVE` / `WONT_FIX` | 否 | 否 | 人工终态；后续复现进入"豁免复核"而非 NEW |
| `MERGED` | 否 | 否 | 已并入其他实例 |

### 4.8 旧数据模型迁移映射

| 旧字段/表 | 新落点 | 迁移规则 |
| --- | --- | --- |
| `defect_fingerprint_records`（全行） | `defects` + `defect_aliases` | 按 `(repo, task)` **全状态**重建锚点（不能用 `status=ACTIVE` 过滤，否则会丢掉 RESOLVED/VERIFIED_PENDING 实例，血统断裂）；复用 `RunFingerprintMigration` 的物理校准能力，生成 `anchor_key/l1_fp`，并把旧 `fingerprint` 写入 `aliases(L1/STRONG)`；`missed_count/feedback/resolved_*` 原样继承 |
| `defect_fingerprint_records.status` | `defects.status` | `ACTIVE→ACTIVE`；`RESOLVED→RESOLVED`；`VERIFIED_PENDING→VERIFIED_PENDING` |
| `campaign_findings`（状态与指派） | `defects` + `defect_events` | 按历史匹配结果与台账对齐后迁移；`status_log` 逐条转 `defect_events`；`assignee_id` → `defects.owner_id` |
| `analysis_findings.diff_status` | 删除该列（或保留为只读投影缓存） | 增量结论改由 `defect_observations` 承载 |
| `task_reports.new/existed/resolved_defects_count` | `scan_runs` 派生回写 | 迁移期保留列，由 `ledger.Commit` 统一写入，M1 停止写 |
| `scan_reconciliations` / `reconciliation_links` | 只读归档 | 历史报告详情页继续可查；新链路不再写 |
| `synthesis JSON`（`items[]/archived_items[]`） | `defect_observations` + `defects` | 一次性回填（已有 `cmd/seed_reconciliation` 可扩展）；回填后 synthesis 文件降级为导出产物，不再作为基线 |

### 4.9 幂等与并发控制

| 场景 | 机制 |
| --- | --- |
| 同一报告重复执行对账（重试/重跑） | `scan_runs.report_id` 唯一 + upsert；`defect_observations (scan_id, group_uid)` 唯一；`Commit` 整体在单事务内，可安全重入 |
| 同一仓同一 commit 并发两次扫描 | 扫描前置检查（同 `head_commit + scope_hash` 且存在 `coverage_state=COMPLETE` 的扫描时复用该 scan 或告警跳过）；`scan_runs` 的唯一键只覆盖同一报告，不覆盖不同报告，不能作为并发去重手段 |
| 两轮扫描并发写台账 | `defects.row_version` 乐观锁；**强键**（CONTEXT/L1）别名唯一索引冲突即视为并发已认领，桶键（K1/F2/F3/PATH/SYMBOL）按 `(键, defect_id)` 幂等追加，不视为并发信号 |
| 多租户 | 各租户独立数据库，天然隔离，无跨租户锁竞争 |

---

## 五、算法设计：确定性身份与分层对账

### 5.1 Layer 0：规范化与物理锚定

目标：把 LLM 输出的不可靠文本，转换为**只依赖代码物理事实**的锚点结构。所有规则必须是纯函数、可单测、无外部状态。

#### 5.1.1 路径规范化（Path Normalization）

| 规则 | 处理 | 反例（现状） |
| --- | --- | --- |
| 仓库相对化 | 剥离 `repoRoot`、`repoBase` 前缀，统一 POSIX 分隔符 | 报告中出现 `/home/xxx/codes/repo-a/src/a.cpp` 与 `src/a.cpp` 两种形态 |
| 大小写与 Unicode | 展示保留原样，**索引键**统一 `NFC + lower` | 同一文件在索引中变成两个键 |
| 冗余段清理 | 去除 `./`、折叠 `//`、解析 `..` | 同一文件两种路径 |
| 生成物与三方目录 | `vendor/`、`third_party/`、`build/`、`*.pb.cc` 等按任务族配置排除或折叠为 `<generated>` | 生成代码噪声污染对账 |
| 文件重命名映射 | 从 `git diff -M` 提取 rename 对，旧路径写入 `defect_aliases(PATH)` | 文件移动后全量判 NEW |
| 符号链接与子模块 | 解析真实路径；子模块保留 `submodule/` 前缀作为路径一部分 | 子模块内文件路径歧义 |

> 复用与替代：[anchor.go](../../../services/reconciliation/anchor.go) 的 `CanonicalizeRelativePath` 采用"磁盘存在性 + 常见顶级目录启发式"推断，逻辑复杂且依赖离线文件；新版改为**显式规则 + git 元数据驱动**，无磁盘探测，天然可测。

#### 5.1.2 符号提取（SymbolPath）

目标：把"作用域"从正则猜测升级为**语言感知的稳定标识**。

```go
// services/identity/extractor.go
type Symbol struct {
    Path        string // 规范化符号路径，如 "BufferPool::Allocate"
    ScopeKey    string // H(Path | Kind | arity) 前 16 字节：路径无关，文件移动/拆分后保持不变
    Kind        string // FUNCTION / METHOD / CLASS / LAMBDA / FILE_SCOPE
    Arity       int    // 参数个数（重载区分，避免参数改名导致身份漂移）
    BodyHash    string // 符号体规范化 Token 流的 SHA-256（变更守卫用）
    StartLine   int
    EndLine     int
}

type Extractor interface {
    Extract(repoRoot, normPath string, src []byte, targetLine int) (*Symbol, error)
}
```

提取策略分语言优先级（高 → 低），任一级失败则降级：

| 语言 | 一级（AST） | 二级（Ctags/正则回退） | 三级（兜底） |
| --- | --- | --- | --- |
| Go | `go/parser` + `go/ast` | `func` 正则 | `FILE_SCOPE` |
| C/C++ | `clang -ast-dump=json`（可用时） | `ctags --output-format=json` | 现有 C++ 正则 |
| Python | `ast` 模块 | `def/class` 正则 | `FILE_SCOPE` |
| Java/Kotlin | 语法树解析器 | 现有正则 | `FILE_SCOPE` |
| 其他 | — | 泛化正则 | `FILE_SCOPE` |

符号路径规范化规则（在现有 `NormalizeScopeSymbol` 上升级）：

- 剥离外层 namespace，仅保留最内层 `Class::Method`（与现状一致，防止命名空间嵌套膨胀）。
- 参数只保留 **arity**，不保留类型与形参名（形参改名不应改变身份）。
- lambda / 匿名函数统一为 `含方法名#L<相对序号>`，序号由所在符号内出现顺序确定。
- 无法定位符号时，`ScopeKey = H("@file"|norm_path)` 且 `Kind = FILE_SCOPE`（此分支必须回退为带 `norm_path`，否则不同文件的 FILE_SCOPE 会互相碰撞），并在观测上标注 `anchor_confidence = LOW`（该标记必须进入可观测性指标，而不是被静默吞掉）。

#### 5.1.3 语句形状（StmtShape）与出现序号（Occurrence）

这是替代"行号 + 触发文本"的关键升级。

```text
输入语句（任意语言）:
    if (p[0]->m_cfgItemMap.find(item) != p[0]->m_cfgItemMap.end()) {

① Token 化（语言感知的词法器；非词法语言退化为正则切分）
    [if, (, p, [, 0, ], ->, m_cfgItemMap, ., find, (, item, ), ..., end, (, ), ), {]

② 形状归一（Shape Normalization）
    · 标识符   → $ID      （消除变量/成员/形参改名影响）
    · 字面量   → $LIT     （消除常量取值影响，但保留"有字面量"这一事实）
    · 运算符/关键字/括号 → 原样保留（保留结构语义）
    · 注释/空白/格式    → 丢弃
    形状序列: [if, (, $ID, [, $LIT, ], ->, $ID, ., $ID, ... ]

③ StmtShape = SHA-256(形状序列)[:16]
④ occurrence_index = 该 StmtShape 在所在 Symbol 体内出现的第几次（从 0 计）
```

| 代码变更形态 | 行号方案 | 形状+序号方案 |
| --- | --- | --- |
| 上方插入/删除 30 行 | 行号全错 → 需几何窗口容错 | 不变 |
| 整体缩进/格式化 | Token 清洗后不变 | 不变 |
| 变量/形参改名 | Token 变化 → 指纹漂移 | 不变（`$ID`） |
| 常量取值修改 | Token 变化 | 不变（`$LIT`） |
| 同符号内插入另一条同形状语句 | 序号后移，可能错配 | 由 §5.5 上下文分数消歧 |
| 语句被重写 | 变化 | 变化（正确：此时需靠上下文与 AI 判定） |

#### 5.1.4 锚点吸附（Anchor Snapping）

LLM 报出的 `line_number` 与 `trigger_line` 都可能不准，必须回到物理文件校准：

1. 解析 `line_number`：现有实现（`services/defects/anchor.go::ParseLineNumberRange`）**只支持 `"120"` 与 `"120-168"`**，`"41,42"` 这类逗号形态会因 `Atoi` 失败被静默丢弃为 `(0,0)`——因此必须在采集侧先做格式归一，不能依赖解析器容错。**新规范强制单一格式**：统一输出 `start-end`；多行用空格分隔的多个区间，不再输出逗号形态。
2. 目标行 ±15 行窗口内，按 `物理Token 全等 → 形状相等 → Token Jaccard ≥ 0.6` 三级匹配，选最近命中行。
3. 窗口内无命中 → 全文反查；仍无命中 → `anchor_confidence = LOW`，`line_start` 取原值并保留 `raw_anchor` 原文，禁止伪造。
4. 命中后向上扫描定位符号，向下计算 `symbol.EndLine`，记录 `blob_hash`（文件级）与 `scope_body_hash`（符号级）用于防假修复。

#### 5.1.5 规范化输出结构

```go
// services/identity/types.go
type NormalizedFinding struct {
    // 物理锚点
    NormPath    string
    Symbol      Symbol
    StmtShape   string
    Occurrence  int
    LineStart   int
    LineEnd     int
    BlobHash    string
    ScopeBodyHash string
    CleanToken  string   // 精确 Token（去掉注释/空白/引号）
    ShapeTokens string   // 形状 Token 流（用于相似度）
    PrevShape   string
    NextShape   string
    AnchorConfidence string // HIGH / MEDIUM / LOW

    // 指纹阶梯
    AnchorKey  string // K1
    ContextKey string // K2
    L1         string // F1
    L2         string // F2
    L3         string // F3
    L4         string // F4（仅查询期派生，不落库、不写别名）

    // 治理属性（不参与身份）
    DefectClass string
    Severity    string
    Payload     models.AnalysisFinding // 原始快照（title/detail/suggestion 等）
}
```

### 5.2 指纹阶梯（Fingerprint Ladder）

**阶梯中的每一项都只是检索键或打分特征，任何一项都不构成身份**。

| 代号 | 名称 | 组成公式 | 用途 | 漂移容忍度 |
| --- | --- | --- | --- | --- |
| `K1` | Anchor Key | `H(repo \| family \| norm_path \| scope_key \| stmt_shape \| occurrence)` | 候选桶主键；台账唯一键的一部分 | 容忍行号/改名/格式/常量变化；不容忍语句结构变化与文件移动 |
| `K2` | Context Key | `H(K1 \| prev_shape \| next_shape)` | 高置信精确命中（上下文消歧） | 在 K1 基础上容忍同形状语句插入 |
| `F1` | Exact FP | `H(K2 \| clean_token)` | 最快路径精确命中；与历史指纹可比较 | 最严格 |
| `F2` | Move-Tolerant Struct FP | `H(repo \| family \| scope_key \| stmt_shape \| occurrence)` | **路径无关**结构等价：文件重命名/拆分、符号跨文件迁移 | 高（不含 `norm_path`，因此必须配合硬门禁 2 使用） |
| `F3` | Scope FP | `H(repo \| family \| norm_path \| scope_key \| defect_class_major)` | 作用域级兜底（语句被重写） | 很高（有误合并风险，需 AI/人工） |
| `F4` | Path FP | `H(repo \| family \| norm_path \| defect_class_major)` | 文件级兜底（大重构）；**查询期派生，不落库** | 最高（仅作为候选生成，不单独成判据） |

**存储与检索**：`K1/K2/F1/F2/F3` 写入 `defect_aliases`（`K2/F1` 为 `STRONG`，其余为 `BUCKET`，见 §4.5）；台账 `defects` 上仅保存"最近一次"取值（`anchor_key/context_key/l1_fp/l2_fp/l3_fp`）。`F4` 不落库、不写别名，只在候选集为空时以 `(repo, family, norm_path)` 扩召回。检索时对本次观测的 5 个已落库键逐一查别名索引，取并集作为候选集。

**自愈规则**：每次判定为同一缺陷，即把本次观测的所有键按 `alias_class` 写入（强键全局唯一，桶键按 `(键, defect_id)` 唯一，见 §4.5 的冲突目标）。下一次代码漂移产生的新键也会被正确认领。

### 5.3 Layer 0.5：单轮内多视角合并（Intra-Scan View Merge）

同一物理缺陷常被同一轮的多个分片、多个 Agent（Hunter/Challenger/Judge）重复报出。必须在进入对账前合并，否则会同时污染"新建"计数与告警噪声。

合并键（按优先级）：

1. `K2` 相同**且 `defect_class_major` 相同** → 直接合并。
2. `K1` 相同**且 `defect_class_major` 相同** → 合并。
3. `norm_path + scope_key` 相同，行区间重叠或距离 ≤ 5，且 `Token Jaccard ≥ 0.9`，**且 `defect_class_major` 相同** → 合并。

> **反例保护（对应 S5）**：锚点相同但 `defect_class_major` 不同的观测**禁止合并**——同一行既可能是空指针也可能是整数溢出。这类观测保留为同一 `anchor_key` 下的多个实例，在 §5.6 的 1:N 后处理中各自占用 `instance_ordinal` 槽位。

合并策略：

- 代表条目（primary）选取顺序：严重度权重最高 → `detail` 最长 → `line_start` 最小 → 原始索引最小（**全序，无随机**）。
- 合并结果保留全部原始条目于 `payload.merged_views[]`，`view_count` 记录数量。
- 合并组非代表条目**不单独成行**：`defect_observations` 只写 1 行（与 `UNIQUE (scan_id, group_uid)` 一致），非代表条目的完整快照保存在该行的 `payload.merged_views[]` 中，以 `view_count` 计数；它们不产生 `new_count`，投影层也不产生多行（S10）。
- 合并绝不改变锚点（锚点取 primary）。

> 对比现状：[funnel.go](../../../services/reconciliation/funnel.go) 用 `claimedBaseMap` 的布尔状态"顺带"产生 `SAME_MULTI_VIEW`，其判定依赖遍历顺序；新版合并发生在**匹配之前**，与基线无关，顺序无关。

### 5.4 Layer 1：候选检索（Candidate Retrieval）

```text
候选集 C(obs) =  别名索引查询(K2) ∪ 别名索引查询(F1) ∪ 别名索引查询(K1)
               ∪ 别名索引查询(F2) ∪ 别名索引查询(F3)
               ∪ 活体桶 [(repo, family, norm_path, scope_key)]
               ∪ 活体桶 [(repo, family, scope_key)]          // F2 级：文件拆分/符号迁移
               ∪ rename 映射后的旧路径桶
               ∪ (仅当候选为空) 活体桶 [(repo, family, norm_path)]   // F4 级兜底（派生，不查别名）
```

工程约束：

- 所有查询走 `(repo_id, task_family, alias_type, alias_value)` 检索索引与 `(repo_id, task_family, norm_path, scope_key)` 复合索引，单次 O(log n)。
- 候选集上限 `maxCandidates = 64`（可配）；超出时按 `S(obs, cand)` 预打分取 Top-64，防止大仓同符号桶爆炸。
- 候选集内必须包含"已 RUNNING/COVERAGE_GAP/DORMANT/VERIFIED_PENDING/RESOLVED"全部状态的实例——**冷归档也必须参与候选**，否则会导致"已修复问题复发被判 NEW"。

### 5.5 Layer 2：确定性打分与分档

#### 5.5.1 硬门禁（Hard Gates）

以下条件不满足，直接判定"不同一"，不进入打分：

1. `norm_path` 相同，或存在 git rename 映射（旧路径 → 新路径）；
2. 或 `scope_key` 相同，且 `F2`（路径无关结构指纹，见 §5.2）命中同一台账实例（同仓内符号整体移动，如文件拆分、符号迁移到新文件）；
3. 或存在历史 `HUMAN` 别名证据。

> 现状 Tier 4（同文件 Token ≥ 0.85 忽略 scope 漂移）在文件重命名场景直接失效；新版通过门禁 1 的 rename 映射与门禁 2 的路径无关符号键 + F2 解决。注意：`scope_key` 本身**不含 `norm_path`**，只相等不足以认领（同仓同名函数很多），必须叠加 F2 命中才构成跨文件门禁。

#### 5.5.2 打分函数

```text
S(obs, cand) = 0.26 · 1[K2 相等]
             + 0.16 · shape_equal            // stmt_shape 完全相等
             + 0.14 · token_sim              // CleanToken 的 2-gram Jaccard
             + 0.10 · shape_sim              // ShapeTokens 的 2-gram Jaccard（抗改名）
             + 0.10 · context_sim            // (prev_shape/next_shape) 匹配率
             + 0.08 · class_sim              // 受控 defect_class 主类相等（taxonomy 收敛后的枚举，非 LLM 自由文本）
             + 0.08 · proximity              // 1 - min(|Δ行|/200, 1)，同符号内归一
             + 0.04 · symbol_equal           // scope_key 相等
             + 0.02 · blob_change_consistency // 代码是否确实发生过变化的证据一致性
             + 0.02 · severity_compat
```

- 权重之和恒为 **1.000**（上述分项逐项相加校验：0.26+0.16+0.14+0.10+0.10+0.08+0.08+0.04+0.02+0.02 = 1.00），`S ∈ [0,1]`，**全部分项必须可序列化进 `defect_links.score_detail`**，供人工仲裁时展示"为什么判为同一"。CI 必须对权重和做断言（容差 ±0.001），权重和不为 1 时直接编译/测试失败。
- 权重与阈值统一存放于 `config.governance.identity`，**禁止硬编码散落在函数内**；任何修改必须递增 `algorithm_version`。
- 初始权重由 22 组实测对 + 反例集标定；上线后按 §9 指标回归校准，但权重变更走 RFC。

#### 5.5.3 分档判定（三档 + 保守兜底）

| 分数区间 | 处置 | 记入 `match_tier` |
| --- | --- | --- |
| `S ≥ 0.90` | 直接认领（唯一候选时）；多候选时进入全局指派 | `EXACT`/`CONTEXT`/`STRUCT` |
| `0.65 ≤ S < 0.90` | 进入全局指派；指派后若被更强观测抢占或存在竞争 → AI 仲裁归属；仲裁不决 → `PROBABLE` | `ASSIGN`/`AI` |
| `0.45 ≤ S < 0.65` | 不自动认领（落入 §5.6 的 U 集合）；默认 `PROBABLE`，仅当 AI 明确判定"不同一"时才可升为 `NEW` | `PROBABLE`/`AI` |
| `S < 0.45` | 无 plausible 候选（`reject_below`），允许进入 `NEW` 判定 | — |

#### 5.5.4 NEW 的三重前置门禁（防假 NEW 的核心机制）

```text
判定 NEW 必须同时满足：
  G1  候选集为空，或所有候选 S < reject_below(0.45)
  G2  全局指派后落入 U（未被任何台账实例认领）
  G3  灰色带处置（maxS ≥ 0.45 时必须逐档处理，禁止直接判 NEW）：
        · 0.45 ≤ maxS < 0.65 且落入 U → 必须经 AI 明确判定"不同一"才可为 NEW；
          否则记 PROBABLE，进人工复核队列
        · 0.65 ≤ maxS < 0.90 且落入 U（说明被更强观测抢占）→ 禁止判 NEW，
          记 PROBABLE（或 SPLIT 后延到下一轮再定）
        · AI 预算耗尽 / 超时 / invoker 不可用 → 一律降级 PROBABLE（不计 NEW）

唯一例外：仓库/任务族首次扫描（无任何台账与别名）→ 全部观测为 NEW。
```

> **执行顺序固定为：打分 → 全局指派 → 三重门禁**。指派只回答"谁归属谁"，NEW 只能由门禁产生，且只有两种来源：**候选集为空/全部低于 `reject_below`**，或 **AI 对唯一灰带候选明确否证**。AI 不得凭空产生新身份（I7）；`maxS ≥ 0.45` 的观测即使落入 U，也只能落 `PROBABLE`。这条规则直接消灭失效模式 F4：现状"残差 > 10 条即全部放弃匹配"会让整轮进入假 NEW 风暴；新版把"资源不足"映射为"待确认"，而不是"新增"。

### 5.6 Layer 3：全局最优指派（Global Assignment）

贪心匹配是本系统历史上多起"错配/漏配"的直接原因（F3）。新版把残差匹配建模为**带虚拟节点的最小代价二分指派**。

```text
分区（Partition）：
  按 rename 映射后的目标文件分组；同组内再按 scope_key 分桶。
  单分区 |O| , |C| ≤ 200 条；超出则按 (norm_path, scope_key) 继续细分。

建图：
  对每个观测 o ∈ O 与候选 c ∈ C，若通过硬门禁，则边代价 cost(o,c) = 1 - S(o,c)
  每个观测可连虚拟"未自动认领"节点 U，代价 unclaimed_cost = 1 - θ_accept
    （θ_accept := matching.thresholds.assign_band = 0.65，与 §5.5.3 的分档常量同源，不再另设符号）
  每个候选可连虚拟"本轮未复现"节点 D，代价 0（未复现不是错误，由生命周期层定夺）

求解：
  最小代价指派（Hungarian / Jonker-Volgenant，O(n³)，n ≤ 200 → 实测 < 5ms）：
  每个观测被指派给「某个候选 c」或「U」，每个候选最多接受一个观测。
  等价目标：maximize Σ_{匹配成功} S(o,c) - θ_accept · |匹配成功观测数|
  （推导：总代价 = Σ(1-S) + |U|·(1-θ_accept) = |O| - ΣS - θ_accept·|U|，
    |O| 为常数且 |U| = |O| - |匹配成功|，故最小化代价 ⇔ 上式最大化。
    含义：只有 S > θ_accept = 0.65 的匹配才值得占用候选，低分匹配会拉低目标函数，
    从而自然落入 U 并交给 §5.5.4 判定，而不是被强行认领。）

  ⚠️ U 不是 NEW：U 只表示"该观测未在指派中获得候选归属"，其最终处置（NEW / PROBABLE）
     必须回到 §5.5.4 的三重门禁逐条判定。禁止把 U 直接映射为 NEW——否则落入灰带
     （maxS ≥ 0.45）的观测会被系统性判为新增，直接违反 I8 与 PROBABLE 档的定义。

确定性与平局：
  1) 代价按 observation.id 升序、candidate.id 升序构造矩阵
  2) 代价相等时按 (anchor_key, defect_id) 字典序打破平局
  3) 不使用 map 迭代序；结果统一按 (scan_id, observation 排序键) 输出

1:N 后处理（拆分 / 多视角 / 合并）：
  · 一个候选被 k 个观测命中、锚点相同且 defect_class_major 相同
    → 理论上已被 §5.3 单轮合并吸收；若仍到达此处（跨分区等边界情况），记 MULTI_VIEW 且不新增实例
  · 一个候选被 k 个观测命中、锚点相同但 defect_class_major 不同（S5：同一行空指针 + 整数溢出）
    → **不合并**：生成 SPLIT_FROM 链接，各自占用 instance_ordinal 槽位，分别独立跟踪
  · 一个候选被 k 个观测命中且锚点不同 → 生成 SPLIT_FROM 链接，保留候选实例，
    其余观测进入下一轮（若下一轮仍稳定出现，则各自获得新实例槽位）
  · k 个候选被同一观测命中 → 生成 MERGED_INTO 候选链接，交人工确认，暂不自动合并
```

**为什么必须全局最优**：贪心对"观测顺序"敏感，先处理的观测会抢占本应属于它的候选。指派是**对称且顺序无关**的，这让"同样的输入 → 同样的结果"（I9）在算法层面成立。

### 5.7 Layer 4：受限 AI 语义仲裁

AI 的角色被严格限定在**灰色带**，且必须满足"批量、缓存、举证、预算"四项约束。

| 约束 | 规范 | 对比现状 |
| --- | --- | --- |
| 触发范围 | 仅 `0.45 ≤ S < 0.90` 且未在指派中唯一确定的候选对 | 现状对全部残差逐对调用（F5） |
| 批量聚合 | 按 `(norm_path, scope_key)` 聚类，一次调用提交该簇内全部候选对，返回数组 | 现状每对一次调用 |
| 输入内容 | **主证据必须是物理源码片段**（锚点上下文各 ±8 行）；LLM 生成的 title/detail 仅作辅助字段 | 现状以 LLM 文本为主证据 |
| 输出契约 | 严格 JSON 数组，逐项 `{pair_id, is_same_defect, confidence, evidence_quote, reason}`；`evidence_quote` 必须能在源码片段中定位，否则该判决作废 | 现状 `is_same_defect/confidence/reason` 无举证要求 |
| 缓存 | `cache_key = H(l2_a \| l2_b \| algorithm_version \| prompt_version)`，命中即复用，计入 `hit_count`。`l2` 为**路径无关**的 F2，因此文件移动/拆分后缓存仍可命中；同一 diff 的重复求值也能直接复用 | 无缓存 |
| 预算 | 单轮 `max_ai_calls`（默认 20）与 `max_ai_tokens`（默认 80k）双闸；超限立即熔断 | 现状仅"总数 > 10 全部放弃" |
| 熔断降级 | 剩余灰色对 → `PROBABLE`，**绝不 NEW** | 现状熔断 → 全部 NEW（F4） |
| 不可用降级 | invoker 为空/报错 → 全部灰色对 `PROBABLE` + 人工队列 | 现状静默回落启发式 |

### 5.8 Layer 5：模板族（派生视图，不参与身份）

现状 `ClusterTemplateFamilies`（[arbitration.go:30](../../../services/reconciliation/arbitration.go#L30)）包含 `mcpmanager`、`.proto` 等硬编码文件名特征，属于对特定项目的过拟合。

新版规范：

1. 模板族是**只读派生视图**（`defect_family_members` 物化视图或查询期计算），**不写入 `defects` 的身份字段，不影响生命周期与增量判定**。
2. 聚类规则确定性且可配置：`(symbol_path 的末段方法名, defect_class 主类, stmt_shape)` 三元组相同，且跨 ≥ 2 个 `norm_path`。
3. 聚类键写入视图与报告展示，**严禁**任何代码路径依据 `template_family_id` 合并台账行。
4. 展示语义变更：报告中的模板族仅用于"同类问题集中治理建议"，不再改变 `diff_status`。

### 5.9 Layer 6：生命周期推进与修复判定

修复判定必须同时满足"覆盖"与"变更"两类证据（I5）。

```text
对每个 ACTIVE / COVERAGE_GAP / DORMANT 实例 d：

  covered      := scan_scope_entries 中 (scan_id, d.norm_path) ∈ {SCANNED}
                  （若为大仓 manifest 降级：bloom 未命中 → 视为未覆盖，保守）
  file_changed := d.blob_hash != 当前 blob_hash
  scope_changed:= d.scope_body_hash != 当前符号体 hash
  region_touched := 存在 hunk 与 d 的 [line_start, line_end] 相交
  seen         := 本轮存在 f1/f2/f3/别名命中 d 的观测

  if seen:
       status → ACTIVE（若原为 RESOLVED/DORMANT/VERIFIED_PENDING → REOPENED 事件）
       missed_count → 0；dormant_rounds → 0
  else if d.status == DORMANT:
       covered → dormant_rounds += 1（代码未变也累加）；dormant_rounds ≥ 8 → OBSOLETE
       !covered → 状态与 dormant_rounds 均不变（覆盖缺口不推进退火）
  else if !covered:
       status → COVERAGE_GAP（missed_count 不变；本轮记为覆盖缺口，不判修复）
  else if !(file_changed || scope_changed || region_touched):
       status → COVERAGE_GAP（代码未动却未复现 = 模型漏报，必须保留）
  else:  // 覆盖 + 代码确实变更 + 未复现
       按严重度分档推进（见下表）
```

严重度分档推进策略（`missed_count` 仅在此分支累加）：

| 严重度 | 1 轮未复现 | ≥2 轮未复现 | 终态与归档 |
| --- | --- | --- | --- |
| 致命 / 严重 | `VERIFIED_PENDING`（报告"待确认已修复"分区，风险分计 0.5 权重） | 仍 `VERIFIED_PENDING`，除非有 `resolved_diff_hunk` 或人工确认 | `RESOLVED` 需人工确认或强证据；**不会因为未复现就消失** |
| 一般 | `COVERAGE_GAP`（继续观察） | `RESOLVED`（附 `resolved_commit` 与 diff hunk） | 复现即 `REOPENED` |
| 建议 | `COVERAGE_GAP` | `DORMANT`（退火休眠，移出报告） | `dormant_rounds` 每轮"被覆盖且未复现"累加（**代码未变也累加**），连续 8 轮 → `OBSOLETE` 归档；再复现即唤醒 |

补充规则：

- **退火计数与修复计数必须分离**：`missed_count` 只在"覆盖 + 代码变更 + 未复现"时累加（修复判定用）；`dormant_rounds` 在 `DORMANT` 状态下只要"被覆盖且未复现"就累加（退火归档用）。若两者合并成同一个计数器，"代码未再变化"的轮次会落入 `COVERAGE_GAP` 而不累加，`DORMANT → OBSOLETE` 将永远不可达。
- **文件删除**：`scan_scope_entries` 中路径不存在且上一完整轮存在 → `OBSOLETE`（记录删除 commit）；连续 20 轮未复现物理清理（保留现有有界治理思想）。
- **人工终态**：`FALSE_POSITIVE` / `WONT_FIX` 实例仍参与候选检索；若再次被观测命中，生成 `EXEMPT_RECHECK` 事件进入"豁免复核"队列，**不回到报告主清单**，也不判 NEW。
- **高危永不静默**：高危实例永不进入 `DORMANT`，最坏状态是 `VERIFIED_PENDING`，始终在报告中有独立分区。
- **变更焦点模式（change_focus）**：仅对 `diff_touched = true` 的文件执行上述判定；其余实例冻结（`missed_count` 不变、状态不变），报告严格不展示，`coverage_gap_count ≡ 0`（沿用现设计铁律）。
- **实体评估模式（entity_assessment）**：与 `full_ledger` 使用**同一套**对账与覆盖判定（不另立算法），仅差异在于投影层不生成"缺陷增量分区"，且不参与退火归档计数；这是 §4.2 枚举了该模式却未在算法层定义行为的补齐。

### 5.10 复杂度与性能预算

| 阶段 | 复杂度 | 预算（10k 文件 / 5k 台账 / 500 观测） |
| --- | --- | --- |
| 文件解析与锚定（Layer 0，冷路径） | O(Σ 文件字节 + 语句数) | 10k 文件全量 ≤ 90s；AST 缓存命中后单文件 ≤ 0.2ms |
| 形状/指纹/合并（Layer 0.5） | O(n log n) | < 50ms（基于缓存后的 Token 流） |
| 候选检索 | O(n · log N · k) | < 100ms |
| 打分 | O(n · k · L)（L = Token 长度） | < 200ms |
| 全局指派 | O(Σ n_i³)，n_i ≤ 200 | < 500ms |
| AI 仲裁 | O(calls) 网络受限 | ≤ 5min（双闸熔断，且与主链路异步/可降级） |
| 事务提交 | O(n + N) | < 300ms |
| **对账核心合计（不含文件解析冷路径与 AI）** | — | **< 2s** |

> 口径澄清：`< 2s` 指的是**对账核心链路**（检索 → 打分 → 指派 → 提交），其输入来自已完成锚定的观测；文件解析/锚定是 O(全仓字节数) 的冷路径，10k 文件不可能在 2s 内完成，必须靠增量跳过（`UNCHANGED_SKIPPED`）+ Token/AST 缓存 + 并行解析控制总时长，并与 `scan_runs.coverage_state` 联动。此前"单文件 < 5ms × 10k = 50s > 2s"的自相矛盾由此消除。

---

## 六、扫描覆盖真相（Scan Scope Manifest）

### 6.1 为什么必须先解决覆盖问题

失效模式 F7 表明：系统此前把"没报出"等同于"已修复"，而"没报出"的三种可能原因（代码真被修了 / 分片失败没扫到 / 扫描范围本就不含该文件）从未被区分。**覆盖真相是修复判定的事实基础，必须先于对账落地**。

### 6.2 采集点与数据结构

| 采集点 | 数据来源 | 落库字段 |
| --- | --- | --- |
| 仓库同步完成 | `PrepareAndSync` 检出结果 | `head_commit`、`base_commit`、仓库根路径 |
| 文件清单装配 | 分片器（chunker）输出的 planned 文件列表（`SemanticChunk.AllFiles`） | `planned_files`、`scope_hash`、`scan_scope_entries` 预写占位行 |
| 每个分片执行结果 | **需先扩展** `engines.ChunkDetails`，新增 `Files []string`（现状只有 `ChunkName/Status/Attempts/Retries/ErrorMessage`，不含文件清单） | `scan_scope_entries.outcome = SCANNED / FAILED`、`chunk_name`、`fail_reason` |
| 跳过与排除 | 引擎的过滤规则（测试文件、生成物、超大文件） | `outcome = EXCLUDED` + 原因 |
| Git 变更区间 | `git diff --unified=0 base_commit head_commit` | `diff_touched`、`hunk_ranges` |
| 未变更跳过 | 增量跳过策略（若启用） | `outcome = UNCHANGED_SKIPPED`（**不计覆盖**） |

> **落地前置项（P0/P1 必做）**：分片级 `Status` 无法表达"重试后半成功"的逐文件覆盖真相，且 `ChunkDetails` 当前不携带文件列表。必须先把 `chunker` 的 chunk→files 映射（`SemanticChunk.AllFiles`）与每文件执行结果贯通到 `EngineResult`，`scan_scope_entries` 才有可信数据源；否则 §6 整节与 I5 无法成立。本项已计入 T9。

### 6.3 覆盖状态与生命周期判定矩阵

| 本轮覆盖 | 代码变更 | 本轮复现 | 判定结果 |
| --- | --- | --- | --- |
| 是 | 任意 | 是 | `ACTIVE`（若历史为终态 → `REOPENED`） |
| 是 | 是 | 否 | 按 §5.9 严重度分档推进（唯一允许判修复的分支） |
| 是 | 否 | 否 | `COVERAGE_GAP`（模型漏报，保留） |
| 否 | 任意 | 否 | `COVERAGE_GAP`（**不判修复、不累加 missed_count**） |
| 否 | 任意 | 是 | `ACTIVE`（覆盖不足但问题仍在，属严重风险，触发告警） |
| 文件已删除 | — | 否 | `OBSOLETE` |

### 6.4 同范围重扫的零增量铁律

```text
if 本轮.head_commit == 上轮.head_commit
   and 本轮.scope_hash == 上轮.scope_hash
   and 上轮.coverage_state == COMPLETE
then
   期望（硬性）：new_count == 0 且 resolved_count == 0
   期望（软性）：existed_count + reopened_count == 上轮活跃实例数
                 且 coverage_gap_count 只能由"模型本轮漏报"解释，不得出现"新增/修复"语义
   若 hard 条件违反 → 写入 scan_runs.status = 'ANOMALY' 并告警（这是对账引擎的自检金标准）
   若仅 soft 条件违反（模型抖动导致部分条目未复现 → COVERAGE_GAP）→ 计入漏报率指标，不判修复
```

该铁律在影子期与灰度期作为**线上自检**执行，可直接量化对账引擎的稳定性。注意：同 commit 同 scope 完整重扫时，覆盖清单完整，实例若被复现必须是 `EXISTED`；**不得**把"完整覆盖 + 未复现"写成覆盖缺口等式（那等于断言模型零召回，与 §10 的 S1 逐字节一致目标自相矛盾）。

---

## 七、报告与视图投影

### 7.1 增量语义的精确定义（对前端单一契约）

投影层基于 `scan_runs` 与本轮观测，生成唯一一份 `diff_status`，字段名与现有前端保持兼容（[FindingCard.tsx:101](../../../frontend/src/components/report/FindingCard.tsx#L101) 读取 `diff_status || lifecycle_status`，[FindingCard.tsx:206](../../../frontend/src/components/report/FindingCard.tsx#L206) 读取 `item_uid`）。

| 投影值 | 来源状态/事件 | 报告分区 | 对前端展示文案 |
| --- | --- | --- | --- |
| `NEW` | 观测 `verdict = NEW` | 本轮新增 | "本轮新增" |
| `EXISTED` | 观测 `verdict = EXISTED`（含 `payload.merged_views[]` 中的多视角明细） | 存量跟踪 | "历史存量" |
| `REOPENED` | 观测 `verdict = REOPENED` | 复发 | "修复后复发" |
| `PROBABLE` | 观测 `verdict = PROBABLE` | 疑似存量待确认 | "疑似历史存量 · 待确认" |
| `COVERAGE_GAP` | 实例本轮未被覆盖/未复现 | 覆盖缺口（独立分区） | "本轮未复现（覆盖不足，未判修复）" |
| `RESOLVED_IN_SCAN` | 本轮产生 `RESOLVED` 事件 | 本轮修复 | "本轮已修复（附证据）" |
| `VERIFIED_PENDING` | 高危 1 轮未复现 | 待确认已修复 | "疑似已修复 · 待确认" |

字段兼容映射（**字段级兼容，但需约 1 人日的前端分支补充**——新增投影值 `PROBABLE/COVERAGE_GAP/RESOLVED_IN_SCAN` 在现有 `FindingCard` 的 `switch` 中没有对应 case，会落到 default 直接显示英文原文）：

| 现有字段 | 新投影字段 | 说明 |
| --- | --- | --- |
| `item_uid` | `F{first_scan_id}-{anchor_key[:8]}`（冷归档/复发保持首次值） | 稳定业务编号，跨轮不变 |
| `lifecycle_status` | `defects.status` | 直接透传 |
| `coverage_gap` | `diff_status == COVERAGE_GAP` | 布尔同步保留 |
| `template_family_id` | 派生视图值 | 仅展示 |
| `recon_relation` | `defect_links.relation`（最高置信一条） | 审计展示 |
| `severity_range` / `severity_triage` | `severity_history` 派生 | 移除"跨轮严重度冲突"告警噪音：严重度由确定性校准器产生，跨轮不同即视为校准器回归问题，走 §9 指标而非人工 triage |

前端改造清单（并入 T12）：`FindingCard.tsx` 的 `renderDiffStatusBadge` 增加 `PROBABLE`（"疑似历史存量 · 待确认"）、`COVERAGE_GAP`（"本轮未复现（覆盖不足，未判修复）"）、`RESOLVED_IN_SCAN`（"本轮已修复（附证据）"）三个分支；既有分支保持不变，因此属**低风险兼容改造**而非零改造。

### 7.2 报告生成时序（替换现有 synthesis 链路）

```text
ExecuteSynthesis(ctx, findings):
   ① scan := ledger.BeginScan(...)                       // 登记 scan_runs
   ② obs  := identity.Normalize(findings, ctx.CodesPath) // 含单轮合并
   ③ decisions := ledger.Reconcile(scan, obs)            // 纯函数 + AI 灰色带
   ④ ledger.Commit(scan, decisions)                      // 唯一事务：
   │      · 写 defect_observations / defect_links
   │      · upsert defects + defect_aliases
   │      · 推进生命周期 + appending defect_events
   │      · 汇总 new/existed/reopened/resolved/coverage_gap
   │      · 回写 task_reports 增量计数（唯一写者）
   ⑤ view := projection.Render(scan)                     // 报告问题清单（含 diff_status）
   ⑥ 交给 Tier4 生成 Markdown 报告：输入为 view + 台账上下文（新增/存量/缺口/修复证据）
   ⑦ 导出 legacy synthesis JSON（仅作为外部兼容产物，写盘但不参与后续对账）
```

### 7.3 工作台 / 详情 / 导出 / 门禁

| 消费方 | 现实现 | 重构后 |
| --- | --- | --- |
| 报告详情（`GetReportFindings`） | 读 synthesis 文件 + `campaign_findings` 状态拼接 | 读 `defect_observations`（本轮）+ `defects` 现状（一次 JOIN，无文件解析） |
| 工作台"我的缺陷"（`GetMyFindings`） | 读 `campaign_findings` | 读 `defects`（`owner_id = me`） + `defect_events` 时间线 |
| 专项清单/导出（`campaign_generic.go`、`campaign_excel.go`） | 读 `campaign_findings` | 读 `defects` 聚合视图（字段名保持 DTO 兼容） |
| PR 门禁 | 无统一实现 | 读 `scan_runs.new_count` 与 `NEW` 观测：仅阻断 `NEW`，`PROBABLE` 走提示 |
| 报告头部计数 | `task_reports` 与 `scan_reconciliations` 双源 | 单源 `scan_runs`（同事务派生） |

### 7.4 兼容与降级

- **旧报告只读兼容**：历史报告继续通过现有 `SynthesisLedger` 解析路径展示，不回填也可用；回填后自动切换。
- **legacy 导出**：保留 `synthesis-*.json` 与 `recon-*.json` 导出（由投影生成），供外部系统与既有自动化消费。
- **前端 DTO 不变**：`FindingItemDTO` 字段集保持；新增 `PROBABLE/COVERAGE_GAP/RESOLVED_IN_SCAN` 三个投影值需前端按 `diff_status` 补渲染分支（见 §7.1，约 1 人日），DTO 本身不需要改。

---

## 八、确定性与可复现性

| 机制 | 规范 |
| --- | --- |
| 算法版本 | `algorithm_version = "ledger-v1"`，任何权重/阈值/规则变更必须递增；写入 `scan_runs/defects/observations/links` |
| 排序稳定性 | 所有输出集合按显式排序键排序（观测：`norm_path, scope_key, occurrence, group_uid`；链接：`observation_id, defect_id`）；禁止依赖 map 迭代序 |
| 平局打破 | 打分相等时按 `(anchor_key, defect_id)` 字典序；指派矩阵按 id 升序构造 |
| AI 幂等 | 判决缓存表 + `prompt_version`；缓存命中不消耗预算且结果恒定 |
| 时钟无关 | 判定逻辑不读取 `now()`；`now()` 仅用于审计时间戳，不参与任何阈值判断（避免"跨天/跨月结论漂移"） |
| 重放命令 | `code-shield ledgerctl replay --scan <id> [--algorithm ledger-v1] [--dry-run]`：用历史 `defect_observations.payload` 与历史 scope 重跑对账，输出与线上结果的 diff。**冷归档回源**：观测按月归档时，replay 必须先按 `scan_id` 从冷归档（对象存储）拉回输入，归档清理只能发生在"该 scan 已导出离线快照且超出回放窗口"之后（否则 S12 幂等无法保证） |
| 快照测试 | 每个金标场景产出 `decisions.snapshot.json`，CI 逐字节比对；算法版本升级时须人工 review diff |
| 双跑对账 | 灰度期同时跑新旧算法，差异落 `ledger_shadow_diff` 表（临时表，灰度结束后删除） |

---

## 九、可观测性与质量指标

### 9.1 核心指标（每个 scan_runs 行内已有部分计数，其余由定时任务聚合）

| 指标 | 定义 | 目标 | 告警阈值 |
| --- | --- | --- | --- |
| `identity_recall` | 金标集中被正确认领的存量数 / 金标存量总数 | ≥ 0.95 | < 0.90 |
| `false_new_rate` | 人工标记"实为重复"的 NEW 数 / NEW 总数 | ≤ 0.02 | > 0.05 |
| `zero_delta_violation` | 同 commit 同 scope 重扫时 NEW ≠ 0 的轮次数 | 0 | ≥ 1 |
| `flip_rate` | 连续两轮中同一台账条目在 NEW/EXISTED 间翻转的比例 | 0 | > 0.01 |
| `coverage_ratio` | `covered_files / planned_files` | ≥ 0.98 | < 0.95 |
| `unmatched_residual_rate` | 未认领观测 / 观测总数 | ≤ 0.05（首扫除外） | > 0.15 |
| `ai_arbitration_rate` | AI 判决对 / 灰色带候选对 | 记录趋势 | 突增 2× |
| `ai_cost_per_scan` | `ai_tokens` | ≤ 预算 80% | > 预算 |
| `probable_backlog` | `PROBABLE` 待人工确认队列长度 | 递减 | 连续 3 轮增长 |
| `alias_churn` | 每轮新增别名数 / 活跃实例数 | ≤ 0.2 | > 0.5（说明锚点仍不稳定） |
| `reopen_rate_30d` | 30 天内 RESOLVED 后复发的比例 | ≤ 0.05 | > 0.1（说明修复判定过松） |
| `resolve_precision` | RESOLVED 后 30 天未复发的比例 | ≥ 0.95 | < 0.90 |

### 9.2 可解释性要求

- 报告/工作台每个条目必须能展示：命中层级、置信度、`score_detail` 分项、命中的别名类型、上一个被认领的 scan_id。
- 每次状态变化在 `defect_events` 中有 `reason` 与 `evidence`（覆盖证据、diff hunk、AI 判决摘要）。
- `PROBABLE` 条目必须在 UI 提供"确认同一 / 确认为新 / 拆分"三个一键操作，人工结论写回 `defect_links(confirmed)` 与 `defect_aliases(HUMAN)`，成为下一轮的先验。

---

## 十、验收金标（Golden Acceptance Criteria）

### 10.1 场景金标矩阵

| 编号 | 场景 | 构造方式 | 期望结果 |
| --- | --- | --- | --- |
| S1 | **零增量重扫** | 同一 commit、同一 scope 连扫两次 | 硬性：`new_count = 0`、`resolved_count = 0`；软性：同一缺陷的 `diff_status` 保持一致，差异只允许是 `EXISTED → COVERAGE_GAP`（模型本轮漏报），**不允许**出现 `NEW/RESOLVED`。若要求逐字节一致，必须在同一 `algorithm_version` + 关闭 AI 抖动（`temperature=0` 且缓存命中）下断言 |
| S2 | **局部扫描** | 仅扫描 30% 文件 | 未覆盖实例 → `COVERAGE_GAP`，`resolved_count = 0`，告警覆盖不足 |
| S3 | **现网 22 组实测重复对** | 直接复用[实测复盘 §6](../../04-reports/02-post-mortems/01-实测复盘-现网连续扫描重复缺陷无法识别归因分析报告.md) 的 22 组样本 | 22/22 全部认领；其中 ≥ 18 组落在 `EXACT/CONTEXT/STRUCT`，其余 ≤ `AI` 层且置信度 ≥ 0.75 |
| S4 | **漂移鲁棒性** | 对同一缺陷施加：上方插入 30 行 / 整体格式化 / 变量改名 / 常量取值修改 / 注释增删 / 函数改名 / 文件移动 / 文件拆分 / 宏包裹 | 全部认领为 `EXISTED`，`alias_churn` 有新增但 `new_count = 0`。其中"文件移动/拆分"依赖 `F2`（路径无关结构指纹）在硬门禁 2 中命中，测试需单独断言 `l2_fp` 匹配路径 |
| S5 | **反例：同函数两个独立缺陷** | 同一函数内空指针 + 整数溢出，两轮各自稳定 | 两条独立台账实例，互不合并；`defect_links` 无交叉。断言点：两者 `K1/K2` 相同但 `defect_class_major` 不同，§5.3 合并必须被抑制，`instance_ordinal` 分别为 0/1 |
| S6 | **反例：同形状兄弟语句** | 同符号内两处相同形状语句（如循环内两次相似调用）各自是独立缺陷 | 通过 `occurrence_index + context_sim` 正确区分，不串位 |
| S7 | **真修复** | 删除脆弱代码并提交 | 覆盖 + 变更 + 未复现 → 按严重度推进；`RESOLVED` 带 `resolved_commit` 与 diff hunk |
| S8 | **假修复（模型漏报）** | 代码完全未改，仅本轮 AI 漏报 | 保持 `COVERAGE_GAP/ACTIVE`，**绝不判 RESOLVED** |
| S9 | **复发** | 先修复并确认 RESOLVED，再还原代码 | `REOPENED` 事件，`item_uid` 保持首次值，不产生新身份 |
| S10 | **多视角重复上报** | 同锚点被 3 个分片各报一次 | 合并为 1 条观测组（`view_count = 3`），`new_count = 1` 或 `existed_count = 1` |
| S11 | **冷归档唤醒** | `DORMANT` 实例对应代码再次出现 | 无损唤醒为 `ACTIVE`，继承 `first_seen` 与人工反馈 |
| S12 | **幂等重放** | `ledgerctl replay` 同一 scan 两次 / 换机器执行 | 决策序列逐字节一致（含 AI 缓存命中路径） |
| S13 | **人工终态复核** | `FALSE_POSITIVE` 实例再次复现 | 进入"豁免复核"队列，报告主清单不出现、不判 NEW |
| S14 | **文件删除** | 删除含缺陷的文件并提交 | `OBSOLETE`，记录删除 commit，不计入修复收益 |

### 10.2 测试工程要求

| 要求 | 规范 |
| --- | --- |
| 金标语料库 | `testdata/golden/` 下按场景建目录：`base/` 与 `current/` 两棵最小代码树 + `findings.json` + `expected.json` |
| 快照测试 | 每个场景断言完整 `decisions.snapshot.json`；CI 失败必须人工 review diff（防止"悄悄改判定"） |
| 属性测试 | 随机生成代码漂移（插入/删除/改名/重排），断言 `new_count = 0`（幂等性属性）与"不误合并"（反例属性） |
| 指标回归 | CI 输出 `identity_recall / false_new_rate / flip_rate` 三项，低于门限即失败 |
| 性能测试 | 生成 10k 文件 / 5k 台账 / 500 观测的合成仓库，断言对账 < 2s（不含 AI） |

### 10.3 上线放行门限（Go/No-Go）

1. S1~S14 全绿。
2. 影子期连续 ≥ 20 轮真实扫描中：`false_new_rate ≤ 0.02`、`zero_delta_violation = 0`、`flip_rate ≤ 0.01`。
3. 回填校验：回填后的台账与旧 `defect_fingerprint_records` 对同一仓的活跃实例数偏差 ≤ 2%，且偏差全部可由人工解释（历史误合并/误拆分）。
4. 人工抽检 50 条 `PROBABLE`：确认同一比例 ≥ 90%。

---

## 十一、迁移与落地路线图

### 11.1 阶段划分

| 阶段 | 目标 | 交付物 | 准入 / 回滚 |
| --- | --- | --- | --- |
| **P0 冻结与建模**（4d） | 停止新增对账逻辑；建立新表与迁移框架 | 八张新表 AutoMigrate（§4.1）；`engines.ChunkDetails.Files` 扩展设计确认；`algorithm_version` 落地；`ledgerctl` 骨架 | 回滚：仅删新表，无行为变更 |
| **P1 身份层落地**（5d） | 确定性锚定与指纹阶梯可独立运行 | `services/identity`；S4/S6 单测；锚点质量报告 | 回滚：新包无调用方 |
| **P2 对账层落地（影子）**（6d） | 新算法与旧算法并行跑，只记录不生效 | `services/matching`；`ledger_shadow_diff`；S3/S5/S12 金标 | 回滚：关闭影子开关 |
| **P3 台账回填**（4d） | 把历史 `defect_fingerprint_records` / `campaign_findings` / synthesis 文件回填为台账 | 回填脚本（dry-run 默认）；校验报告；`defect_events` 历史时间线 | 回滚：台账可重放重建，旧表未删 |
| **P4 投影切换**（5d） | 报告 / 工作台 / 导出 / 门禁全部改读台账 | `projection.Render`；工作台改读 `defects`；前端补 `PROBABLE/COVERAGE_GAP/RESOLVED_IN_SCAN` 分支；synthesis 降级为导出 | 回滚：读路径开关切回旧逻辑（保留 1 个版本） |
| **P5 删码与收口**（3d） | 删除 M1/M2/M3 的旧实现，形成单一写者 | 删除清单（§11.3）执行完毕；文档与索引更新 | 旧表置只读后保留 1 个季度再物理删除 |

总工期估算：**约 54 人日**（口径以 §15.1 WBS 为准）。上表括号内为**阶段串行关键路径的粗略工期（合计 27 人日）**，只覆盖主干开发；测试（T13）、回填（T14）、影子期（T15）、删码收口（T16）与前端分支可部分并行，故与 WBS 不等。关键路径为 P1 → P2 → P4。若按 2 人并行推进，日历工期约 6~7 周（不含跨团队评审时间）。

### 11.2 历史数据回填方案

```text
输入（按 repo × task_family 分组，按时间升序）：
  · task_reports（成功报告）
  · 各报告的 synthesis JSON（新版台账 或 旧版平铺数组）
  · 各报告的 raw-findings JSON（不可变原始观测，最可信）
  · defect_fingerprint_records（上一代台账状态：missed_count/feedback/resolved）
  · campaign_findings（工作台状态与指派）

步骤：
  1. 对每份报告：用 raw-findings 作为观测源（缺失则退回 synthesis 条目），
     用当前 algorithm_version 重算锚点 → 生成 defect_observations 草稿
  2. 按报告时间顺序**重放对账**（replay），累积 defects 与 aliases
  3. 用 defect_fingerprint_records 的状态与 missed_count 校正实例生命周期终态
  4. 用 campaign_findings 的 owner/status_log 校正 owner_id 与 defect_events
  5. 校验：
     · 活跃实例数与旧表偏差 ≤ 2%
     · 抽样 100 条比对 first_seen_scan 与人工认知一致
     · 回填后**新产生的** RESOLVED 实例必须有 resolved_scan_id 与证据；继承自旧 `defect_fingerprint_records` 的历史 RESOLVED 允许证据缺失，但必须标记 `proof.legacy=true` 并在报告中降级展示
  6. 默认 dry-run 输出差异清单，人工确认后执行 --execute
```

### 11.3 旧资产处置清单（去混乱）

| 文件 / 对象 | 处置 | 说明 |
| --- | --- | --- |
| [services/defects/diff_engine.go](../../../services/defects/diff_engine.go) | **删除** | 逻辑并入 `ledger`（生命周期）与 `matching`（匹配） |
| [services/defects/migration.go](../../../services/defects/migration.go) | **升级**为 `ledgerctl backfill` | 保留其"物理校准重算"思想 |
| [services/defects/anchor.go](../../../services/defects/anchor.go) | **保留并下沉** | `CleanSourceToken/NormalizeScopeSymbol/ParseLineNumberRange` 迁入 `identity` |
| [services/defects/fingerprint.go](../../../services/defects/fingerprint.go) | **重写** | 变为 `identity/keys.go` 的指纹阶梯 |
| [services/reconciliation/funnel.go](../../../services/reconciliation/funnel.go) | **重写** | 变为 `matching/retrieve.go` + `matching/score.go` |
| [services/reconciliation/arbitration.go](../../../services/reconciliation/arbitration.go) | **重写** | 变为 `matching/assign.go` + `matching/arbitrate.go`（批量+缓存+预算） |
| [services/reconciliation/pruner.go](../../../services/reconciliation/pruner.go) | **迁移** | 变为 `ledger/lifecycle.go`，接入覆盖真相 |
| [services/reconciliation/change_focus.go](../../../services/reconciliation/change_focus.go) | **迁移** | 变为 `ledger/mode_change_focus.go` |
| [services/reconciliation/engine.go](../../../services/reconciliation/engine.go) / `types.go` | **拆分** | 编排逻辑入 `ledger`；`SynthesisLedger` 降级为投影导出 DTO |
| [services/governance/campaign.go](../../../services/governance/campaign.go) | **删除匹配与自动销账** | 保留查询/DTO；工作台改读台账 |
| [services/governance/calibrator.go](../../../services/governance/calibrator.go) / [taxonomy.go](../../../services/governance/taxonomy.go) | **保留** | 严重度确定性校准与分类收敛仍有价值，作为 `defect_class/severity` 生产者 |
| [services/governance/feedback.go](../../../services/governance/feedback.go) | **改造** | 误报豁免写入 `defects.feedback_*` + `defect_aliases(HUMAN)` |
| `models.DefectFingerprintRecord` / `CampaignFinding` | **标记 Deprecated** | 迁移完成、灰度稳定后删除 |
| `models.ScanReconciliation` / `ReconciliationLink` | **只读保留** | 历史审计查询 |
| `cmd/seed_reconciliation` | **升级**为 `cmd/ledgerctl` | 新增 `backfill / replay / verify / shadow-diff` 子命令 |
| [services/runner/synthesis.go](../../../services/runner/synthesis.go) | **精简** | 只负责编排 `BeginScan→Reconcile→Commit→Render→LLM 合成` |
| [services/runner/postprocess.go](../../../services/runner/postprocess.go) `GetEffectiveFindings` | **改造** | 改读 `projection.Render` 结果，不再解析 synthesis 文件 |
| [services/reports/report_service.go](../../../services/reports/report_service.go) | **改造** | 改读台账投影；移除 `campaign_findings` JOIN |
| [handlers/workbench.go](../../../handlers/workbench.go) / [handlers/campaign_generic.go](../../../handlers/campaign_generic.go) | **改造** | 查询 `defects`，DTO 字段保持兼容 |

---

## 十二、目标代码结构与接口

### 12.1 模块目录

```text
services/
├── identity/                 # L1 确定性身份（纯函数，无 DB）
│   ├── path.go               # 路径规范化 + rename 映射
│   ├── extractor.go          # 语言感知符号提取（AST 优先）
│   ├── shape.go              # Token 化 / 形状归一 / occurrence
│   ├── anchor.go             # 行号吸附与物理锚点
│   ├── keys.go               # K1/K2/F1/F2/F3/F4 指纹阶梯
│   ├── merge.go              # 单轮内多视角合并
│   └── types.go
├── matching/                 # L2 对账（纯函数 + 受限 AI）
│   ├── retrieve.go           # 倒排候选检索
│   ├── score.go              # 确定性打分与硬门禁
│   ├── assign.go             # 全局最小代价指派
│   ├── arbitrate.go          # 批量 AI 仲裁（缓存/预算/举证）
│   ├── family.go             # 模板族派生视图（不影响身份）
│   └── types.go
├── ledger/                   # L3 台账（唯一写者）
│   ├── scan.go               # BeginScan / 覆盖清单采集
│   ├── reconcile.go          # 编排 identity + matching
│   ├── commit.go             # 唯一事务：观测/实例/别名/链接/事件/计数
│   ├── lifecycle.go          # 状态机与退火/修复判定
│   ├── mode_change_focus.go  # 变更焦点模式
│   ├── alias.go              # 别名索引维护与自愈
│   └── types.go
└── projection/               # L4 只读投影
    ├── report.go             # 报告问题清单（含 diff_status/item_uid）
    ├── workbench.go          # 工作台/我的缺陷
    ├── export.go             # Excel / legacy synthesis 导出
    └── gate.go               # PR 门禁视图
```

依赖方向（编译期强制，禁止反向依赖）：

```text
handlers → projection → ledger → matching → identity
runner   → ledger
                          ledger → models
（identity / matching 不依赖 models 之外的 DB 与 invoker 具体实现）
```

### 12.2 关键接口

```go
// ── L1 身份层（纯函数） ──
type Normalizer interface {
    Normalize(repoRoot string, findings []models.AnalysisFinding) ([]NormalizedObservation, error)
}

// ── L2 对账层（纯函数 + 受控 AI） ──
type Matcher interface {
    Match(req MatchRequest) (MatchResult, error)
}
type MatchRequest struct {
    Candidates  []Candidate   // 台账实例（含别名键）
    Observations []Observation
    AI          Arbitrator    // 可为 nil（则灰色带全部落 PROBABLE）
    Budget      Budget
    Options     MatchOptions  // 权重、阈值、algorithm_version
}

// ── L3 台账层（唯一写者） ──
type Ledger interface {
    BeginScan(ctx context.Context, in ScanInput) (*ScanRun, error)
    // Reconcile 编排 identity + matching 的纯函数链路，产出决策（不写库）
    Reconcile(ctx context.Context, run *ScanRun, obs []NormalizedObservation) (MatchResult, error)
    // Commit 是唯一事务：观测/实例/别名/链接/事件/计数
    Commit(ctx context.Context, run *ScanRun, decisions MatchResult) (*ScanVerdict, error)
    // 人工操作
    ConfirmLink(ctx context.Context, linkID uint, by uint, same bool) error
    SetFeedback(ctx context.Context, defectID uint, fb FeedbackInput) error
    Assign(ctx context.Context, defectID uint, ownerID uint, by uint, comment string) error
}

// ── L4 投影层（只读） ──
type Projector interface {
    RenderReport(scanID uint) ([]FindingView, error)
    RenderWorkbench(filter WorkbenchFilter) ([]DefectView, error)
}
```

### 12.3 单一写者强制手段

1. `defects/defect_observations/scan_runs/defect_aliases` 的写方法**只在 `ledger` 包导出**；其他包只能通过 `ledger.Ledger` 接口调用。
2. `models.AnalysisFinding.DiffStatus` 在 `ingest` 之后即冻结，投影层通过 `FindingView.DiffStatus` 覆盖。
3. CI 增加静态检查：禁止 `services/**` 中除 `services/ledger` 外出现 `DB.Create/Updates/Save` 针对上述四张表的调用。

---

## 十三、配置项设计

```yaml
governance:
  identity:
    algorithm_version: "ledger-v1"
    namespace: "task_scoped"            # task_scoped | repo_unified
    path:
      exclude_globs: ["**/vendor/**", "**/third_party/**", "**/*.pb.cc", "**/build/**"]
      case_insensitive_index: true
    symbol:
      prefer_ast: true
      namespace_depth: 2                # 保留最内层 2 段
    anchor:
      window_lines: 15                  # 行号吸附窗口
      low_confidence_ratio_alert: 0.05  # LOW 锚点占比告警
  matching:
    weights:                            # 必须和为 1.0，修改需递增 algorithm_version
      context_key: 0.26
      shape_equal: 0.16
      token_sim: 0.14
      shape_sim: 0.10
      context_sim: 0.10
      class_sim: 0.08
      proximity: 0.08
      symbol_equal: 0.04
      blob_change_consistency: 0.02
      severity_compat: 0.02
    thresholds:
      auto_accept: 0.90
      assign_band: 0.65                 # [0.65, 0.90) 走指派/AI
      probable_band: 0.45               # [0.45, 0.65) 双向唯一才认领
      reject_below: 0.45
    max_candidates: 64
    max_partition_size: 200
  arbitration:
    enabled: true
    max_ai_calls_per_scan: 20
    max_ai_tokens_per_scan: 80000
    context_lines: 8
    prompt_version: "arb-v1"
    require_evidence_quote: true
    degrade_to: "PROBABLE"              # 熔断降级目标（禁止 NEW）
  lifecycle:
    scope_guard_enabled: true
    resolve_requires_coverage: true
    resolve_requires_code_change: true
    severity_policy:
      high:   { miss_to_pending: 1, miss_to_resolved: 2, human_confirm: true }
      medium: { miss_to_resolved: 2 }
      low:    { miss_to_dormant: 2, dormant_to_obsolete: 8 }
    obsolete_purge_after_rounds: 20
```

> `weights` 的逐项和必须等于 `1.0`（当前为 0.26+0.16+0.14+0.10+0.10+0.08+0.08+0.04+0.02+0.02），`assign_band` 同时充当 §5.6 指派图中"未自动认领"虚拟节点的 `θ_accept`，**全系统只此一处定义，不得再引入独立常数**；配置加载时对权重和与阈值单调性（`auto_accept > assign_band ≥ probable_band == reject_below`）做强校验，不满足即拒绝启动。

> 与现状配置的关系：现有 `governance.fingerprint/lifecycle/feedback_memory` 三段保留字段名，新增段落由 `SystemDynamicConfig("governance")` 承载并热加载（沿用现有多租户分层配置机制）。

---

## 十四、风险与缓解

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| 权重/阈值初始标定不足，导致误合并或漏认领 | 台账可信度下降 | 以 22 组实测对 + S5/S6 反例集标定；影子期可调参但需递增算法版本；误合并可通过 `SPLIT_FROM` 人工回退 |
| 别名表随轮次膨胀 | 存储与查询压力 | 只对 **BUCKET 弱键**做 TTL（按 `last_scan_id` 在最近 5 轮内）；`STRONG` 强键与人工确认别名**永不淘汰**（I3 的底线）；物理解除前必须保证该实例仍可由"活体桶（norm_path/scope_key）+ F1/F2 强键"命中，否则该实例直接判 `OBSOLETE` 而不是静默失联 |
| `defect_observations` 归档影响 replay/回填 | §8 重放与 S12 幂等失效 | 归档采用"热表 + 冷归档"，重放命令按 scan_id 自动回源冷归档；回填窗口内禁止清理对应观测；TTL 只清理已被导出为离线快照的轮次 |
| 大仓 `scan_scope_entries` 写入量 | 单轮写入放大 | 提供 manifest 降级模式；分区表；只对"存在台账实例的文件"保留明细行 |
| AI 仲裁成本与延迟 | 单轮时长不可控 | 双闸预算 + 批量聚类 + 判决缓存 + 熔断降 `PROBABLE`；AI 不可用时对账仍可完成 |
| 回填历史数据量过大 | 回填窗口长 | 按 repo × task_family 分批；默认 dry-run；允许"只回填近 N 轮 + 更早实例仅保留汇总"的降级策略 |
| 迁移期双写不一致 | 用户看到两套结论 | 影子期只写不读；切换时以 `algorithm_version` 与 `scan_runs` 为唯一真值；保留一个版本的回滚开关 |
| GORM AutoMigrate 在超大表上加索引 | 迁移锁表 | AutoMigrate 只建表；索引走手工迁移脚本，并按目标引擎分方言：PostgreSQL 用 `CREATE INDEX CONCURRENTLY`，MySQL 用 `ALGORITHM=INPLACE`，SQLite（RFC-001 默认交付形态）直接 `CREATE INDEX`（单机库规模下共享锁可接受）。§4 的 DDL 示例为 PostgreSQL 语义，落地时须提供 SQLite/MySQL 等价 DDL（`JSONB→TEXT`、`BIGSERIAL→INTEGER PRIMARY KEY AUTOINCREMENT`、部分唯一索引→触发器或普通索引 + 应用层校验），并且**按月分区仅在 PostgreSQL 部署启用** |
| 前端/外部系统依赖 legacy JSON | 兼容性回归 | `projection/export.go` 持续产出 `synthesis-*.json` 与 `recon-*.json`，字段与现值一致 |
| 团队认知负担 | 落地走形 | 本文档作为 SSOT；9 条不变量纳入 Code Review checklist；`ledgerctl verify` 提供一致性问题自动诊断 |

---

## 十五、实施 WBS 与结论

### 15.1 工作分解

| 编号 | 任务 | 交付物 | 估计 | 依赖 |
| --- | --- | --- | --- | --- |
| T1 | 八张新表建模与 AutoMigrate（含 SQLite/MySQL 方言映射） | `models/ledger_*.go`、迁移脚本 | 4d | — |
| T2 | 路径规范化 + rename 映射 | `identity/path.go` + 单测 | 2d | T1 |
| T3 | 符号提取器（AST 优先 + 回退） | `identity/extractor.go` + 多语言样本 | 4d | T2 |
| T4 | 语句形状 + 锚点吸附 + 指纹阶梯 | `identity/shape.go`、`anchor.go`、`keys.go` | 4d | T3 |
| T5 | 单轮内多视角合并 | `identity/merge.go` | 1d | T4 |
| T6 | 候选检索 + 确定性打分 | `matching/retrieve.go`、`score.go` | 3d | T4 |
| T7 | 全局最优指派 | `matching/assign.go` + 对称性测试 | 3d | T6 |
| T8 | 批量 AI 仲裁 + 缓存 + 预算 | `matching/arbitrate.go`、`defect_ai_verdict_cache` | 3d | T7 |
| T9 | 覆盖真相采集（**前置：扩展 `engines.ChunkDetails.Files` 并贯通 chunk→files 映射**） | `ledger/scan.go`、`scan_scope_entries`、`engines` 结构扩展 | 3d | T1 |
| T10 | 生命周期状态机 + 模式路由 | `ledger/lifecycle.go`、`mode_change_focus.go` | 4d | T9 |
| T11 | 唯一事务提交 + 别名自愈 | `ledger/commit.go`、`alias.go` | 3d | T7,T10 |
| T12 | 投影层（报告/工作台/导出/门禁 + 前端 3 个状态分支） | `projection/*`、`FindingCard.tsx` | 5d | T11 |
| T13 | 金标语料与快照测试 | `testdata/golden/*`、CI 任务 | 5d | T4~T12 |
| T14 | 回填工具与校验 | `cmd/ledgerctl backfill/verify` | 4d | T11 |
| T15 | 影子双跑与指标看板 | `ledger_shadow_diff` + 指标任务 | 3d | T12 |
| T16 | 删码与文档收口 | 删除清单执行 + 索引更新 | 3d | T15 |
| **合计** | — | — | **54 人日**（= §11.1"总工期估算"的口径，两处必须同步修改） | — |

### 15.2 结论

本轮设计不再试图"让指纹更聪明"，而是**换掉身份的定义方式**：

- 身份从"内容哈希"变为"**持久台账行 + 别名集合**"，从根上消除"代码一改就变新问题"。
- 判定权从三套并行机制收敛为**唯一写者**，报告、工作台、门禁共享同一份结论。
- 匹配从**顺序相关的贪心**升级为**顺序无关的全局最优指派**，并保留受限 AI 处理长尾。
- 修复判定从"没报出就算修好"升级为**覆盖真相 + 代码变更 + 证据链**三重门禁。
- 任何资源不足、AI 失败、规则不确定的场景，一律**保守降级为"疑似存量待确认"**，不再制造假 NEW。

---

## 附录 A：术语表

| 术语 | 定义 |
| --- | --- |
| 台账实例（Defect Instance） | 数据库中的一条缺陷身份行，跨轮保持不变 |
| 观测（Observation） | 某一次扫描对某一缺陷的检出事实，不可变 |
| 观测组（Observation Group） | 单轮内多视角上报合并后的观测单元 |
| 锚点（Anchor） | 定位缺陷的确定性物理坐标：路径 + 符号 + 语句形状 + 出现序号 |
| 指纹阶梯（Fingerprint Ladder） | K1/K2/F1/F2/F3/F4 六级检索键，均非身份 |
| 别名（Alias） | 已确认归属于某台账实例的检索键历史取值 |
| 指派（Assignment） | 观测与台账实例之间的全局最优二分匹配 |
| 覆盖真相（Scope Truth） | 本轮实际扫描并成功分析的文件与区间集合 |
| 退火休眠（Annealing） | 低危问题连续未复现后移出主清单、保留唤醒能力的机制 |
| Proof of Fix | 修复证据链：commit、diff hunk、代码变更前后对比 |

## 附录 B：与 01~06 号文档的关系

| 文档 | 保留内容 | 被本设计取代/升级的部分 |
| --- | --- | --- |
| 01 确定性源码指纹 | 物理锚点思想、受控 Taxonomy 归一、L1/L2/L3 分层意识 | "指纹即身份"的定位被 §5.2 取代；三级指纹扩展为六级检索阶梯 |
| 02 缺陷生命周期与几何对齐 | 行号漂移几何对齐、防假修复守卫、平滑观察期、高危不冷寂 | 守卫升级为"覆盖真相优先"（§6）；状态机重构为 §4.7 |
| 03 多任务通用分类收敛 | 受控分类字典与 CWE 映射 | 分类降级为"打分特征 + 展示属性"，不再参与身份 |
| 04 企业治理与历史记忆闭环 | 人机反馈沉淀、误报记忆、增量治理理念 | 反馈落点从报告条目迁移到台账实例与别名（`HUMAN` 别名） |
| 05 报告对报告系统化对账 | R2R 对账的完整问题定义、台账热/冷分区思想、退火与唤醒 | **基线从"上一份报告文件"改为"台账 + 最近完整扫描"**；产物从文件 SSOT 改为 DB SSOT |
| 06 混合对账与 R5 残差 AI 仲裁 | 残差处理与 AI 语义仲裁的必要性、控制成本的思想 | 逐对 AI 改为批量缓存；残差熔断方向修正；贪心漏斗升级为全局指派 |

## 附录 C：与现网实测复盘的对齐

[实测复盘](../../04-reports/02-post-mortems/01-实测复盘-现网连续扫描重复缺陷无法识别归因分析报告.md) 提出的 8 条改进建议与本设计的对应关系：

| 复盘建议 | 本设计落点 |
| --- | --- |
| 建议 1（P0）指纹只用确定性字段 | §5.2 指纹阶梯 + I4 不变量（更强：指纹不再承担身份职责） |
| 建议 2（P1）`scope_symbol` 规范化 | §5.1.2 SymbolPath（AST 优先 + arity + lambda 序号） |
| 建议 3（P1）`trigger_line` 规范化 | §5.1.3 语句形状与 Token 流 |
| 建议 4（P0）category 受控枚举 | §5.5.2 作为打分特征；`defects.defect_class` |
| 建议 5（P1）`line_number` 单一格式 | §5.1.4 锚点吸附 + 统一 `start-end` 契约 |
| 建议 6（P2）多级匹配策略 | §5.4/§5.5/§5.6 检索 → 打分 → 全局指派 |
| 建议 7（P2）稳定扫描覆盖范围 | §6 覆盖真相（从"建议"升级为"事实数据"） |
| 建议 8（P3）二次语义去重 | §5.7 受限 AI 仲裁（批量、缓存、举证、预算） |

## 附录 D：评审修订记录（2026-09-12 第一轮检视）

本轮检视逐条核对了本文档引用的代码证据（M1~M4 调用点、F1~F10 行号、前端字段、RFC-001 分库结论、22 组实测复盘），并修复了下列自洽性问题。**下表是评审发现问题与修订落点的对照，未列出者表示核对无误。**

| 编号 | 问题（评审发现） | 修订落点 |
| --- | --- | --- |
| R1 | 打分权重之和为 1.10 ≠ 1.000，阈值体系失效 | §5.5.2、§13 权重重标定为 1.00，并增加 CI 断言与配置启动校验 |
| R2 | 指派图"新建"虚拟节点代价 0.35 会把 `S<0.65` 系统性判为 NEW，违反 I8 与 PROBABLE 档 | §5.6 改为"未自动认领"虚拟节点 U 并给出正确的等价目标函数推导；NEW 只能由 §5.5.4 三重门禁产生 |
| R3 | `defect_aliases` 全局唯一键与 F3/F4/PATH/SYMBOL 桶级别名冲突，且 `alias_value`(160) 会截断 512 长度路径 | §4.5 引入 `alias_class`（STRONG/BUCKET）、强键唯一索引 + 桶键 `(键, defect_id)` 唯一索引，`alias_value` 扩到 512 |
| R4 | 单轮合并未区分 `defect_class_major`，会吃掉 S5 的反例 | §5.3 合并键三处均加入分类主类条件，补充反例保护说明；§5.6 1:N 后处理同步 |
| R5 | 零增量自检要求 `coverage_gap_count == 活跃实例数`，与 S1 矛盾 | §6.4 拆分为硬性（new/resolved=0）与软性（复现/缺口解释）两组判据；S1 同步细化 |
| R6 | 覆盖采集依赖 `SummaryChunks[].Files`，但 `ChunkDetails` 不含文件清单 | §6.2 明确需先扩展 `engines.ChunkDetails.Files` 并贯通 chunk→files；T9 与 P0 前置项同步 |
| R7 | `F2 ≡ K1`、F4 无落点、`symbol_key` 含路径却用于跨文件门禁 | §5.2 重定义 K1/K2/F1/F2/F3/F4 六键（F2 路径无关、F4 查询期派生）；§4.4/§4.6 字段改名 `scope_key`；§5.5.1 门禁 2 改为 `scope_key + F2` |
| R8 | "六张新表"与实际 8 张不符 | TL;DR、§11.1 P0、T1 统一为八张并列出表名 |
| R9 | 工期三处不一致（24 / 28~32 / 51 人日） | §11.1 与 §15.1 统一为 WBS 54 人日，阶段表标注为关键路径粗略工期 |
| R10 | 性能预算 Layer 0 与总量冲突（5ms × 10k > 2s） | §5.10 拆分为冷路径（文件解析）与对账核心口径，`< 2s` 仅约束核心链路 |
| R11 | 迁移映射 `status=ACTIVE` 过滤会丢 RESOLVED 血统；回填校验要求历史 RESOLVED 必有证据 | §4.8 改为全状态重建；§11.2 校验口径限定为"回填后新产生的 RESOLVED" |
| R12 | 生命周期 `DORMANT→OBSOLETE` 路径不可达（代码未变不累加 `missed_count`） | §4.4 新增 `dormant_rounds`；§5.9 明确退火计数与修复计数分离，并补充 `entity_assessment` 行为 |
| R13 | `MULTI_VIEW` 既要单独成行又与 `(scan_id, group_uid)` 唯一索引冲突 | §4.6/§4.7/§5.3 统一为"仅明细标记，明细存 `payload.merged_views[]`，不单独成行" |
| R14 | `entity_assessment` 模式被枚举却无算法定义 | §5.9 补充：与 `full_ledger` 同算法，仅投影与退火差异 |
| R15 | I4 要求只用物理量，但打分含 `class_sim` | §2 I4 精确化为"身份键只用物理量；受控枚举分类可作为打分特征" |
| R16 | 前端"零改造"不成立（缺 `PROBABLE/COVERAGE_GAP/RESOLVED_IN_SCAN` 分支） | §7.1/§7.4 改为"字段兼容 + 1 人日前端分支"，T12 估时同步调整 |
| R17 | `scan_runs.report_id` 唯一键会让重试报错；并发去重描述不准 | §4.2/§4.9 改为 upsert 语义，并澄清跨报告并发需前置检查 |
| R18 | `Ledger` 接口缺 `Reconcile`，与 §3.2/§7.2 时序不符 | §12.2 接口补齐 `Reconcile` 方法 |
| R19 | 别名/观测 TTL 与 replay、S11 冷归档唤醒冲突 | §14 限定 TTL 只作用于 BUCKET 弱键，强键与人工别名永不淘汰；观测归档需支持回源 |
| R20 | DDL 方言（PostgreSQL）与 RFC-001 默认 SQLite 交付不符 | §4 增加方言约定与三套映射要求；§14 风险行按引擎分方言 |
| R21 | 文档内 21 处 `path.go:15` 链接在常见渲染器下 404 | 全部改为 `path.go#L15` 相对链接并复核目标文件存在 |
| R22 | `ParseLineNumberRange` 实际不支持 `"41,42"`（文档表述有误） | §5.1.4 更正为现状仅支持 `N` / `N-M`，并要求采集侧先归一格式 |
