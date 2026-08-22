# Code-Shield 专项分析通用化与元数据驱动架构实施代码检视报告（二轮）

> **报告版本**：v2.0  
> **检视日期**：2026-08-22  
> **基准设计文档**：[docs/campaign_governance_architecture.md](file:///home/fugui/codes/code-shield/docs/campaign_governance_architecture.md)  
> **检视范围**：自 commit `576310e5920b0d9f2a554dbc83c06a5d9dbc3ecc` 起的全部实施与重构提交（至 `HEAD` 共 5 个 commits），含第一轮检视报告整改后的复核
> **前置报告**：[docs/campaign_governance_review_report.md](file:///home/fugui/codes/code-shield/docs/campaign_governance_review_report.md)

---

## 1. 总体评价

本次专项分析通用化与元数据驱动架构改造，与设计文档整体对齐度高：

1. **统一数据存储模型**：`CampaignFinding` 替代原 7 张独立分表，具备 4 元组复合唯一索引；
2. **通用双治理模式归并引擎**：`defect_tracking`（确定性规则 + LLM 语义归并）与 `entity_assessment`（路径 + 实体名精确哈希匹配）双分支；
3. **参数化动态 RESTful API**：`/api/analysis/:campaign/*` 六类端点，配合 `sync.Map` 进程内缓存 + TTL + 主动失效；
4. **元数据驱动自适应前端**：动态侧边栏、通用路由 `/analysis/:campaignKey`、按 `governance_mode` 切换看板指标；
5. **并发安全的启动迁移**：`pg_advisory_xact_lock` 事务级锁 + `ON CONFLICT DO NOTHING` 幂等迁移；
6. **存量 BUG-1/2/3 全部修复**：级联清理、工作台全专项聚合、综合报告 CSV 覆盖。

全流程净精简冗余代码约 3800 行，实现了"零编码配置，新任务类型一键升级为专项治理"的核心目标。

## 2. 第一轮检视问题整改复核（commit 2878e74）

第一轮检视报告（v1.0）指出的 7 项问题，经复核均已修复：

| # | 问题 | 修复结果 |
| :---: | :--- | :--- |
| 1 | 单测 AutoMigrate 引用已删除的旧分表模型导致编译失败 | ✅ 已替换为 `CampaignFinding`，`go test ./...` 通过 |
| 2 | `AuditingWorkspace.tsx` 残留 10 处 TS 编译错误 | ✅ 已清理并补齐 `Finding.repo` 接口，`npx tsc --noEmit` 通过 |
| 3 | 工作台前端未解包分页对象导致 `findings.filter` 白屏 | ✅ 已按 `Array.isArray(data) ? data : data?.items` 安全解包 |
| 4 | 部门看板展开子表格未适配实体评估模式 | ✅ 子表格已按 `isEntityMode` 分支展示用例总数/合格数/合格率 |
| 5 | `TestCaseFinding` 结构体残留 | ✅ 已彻底删除 |
| 6 | 前端调试临时目录 `tasks/test-type-170138.783060/` 误入版本库 | ✅ 已删除 |
| 7 | `tasks/float-comparison/meta.json` 被误置为停用 | ✅ 已恢复 `is_active: true` |

## 3. 构建与质量验证结果

| 检查项 | 结果 |
| :--- | :---: |
| `go build ./...` | ✅ 通过 |
| `go vet ./...` | ✅ 通过 |
| `go test ./...` | ✅ 通过（含 `TestCampaignHooks`） |
| `npx tsc --noEmit`（前端类型检查） | ✅ 通过 |
| `gofmt` 检查 | ⚠️ `models/db.go`、`models/models.go` 存在格式差异（多余空行/空格） |

## 4. 本轮检视发现的问题

### 4.1 设计约束未落地（中优先级）

#### 问题 1：`campaign_path` 部分唯一索引缺失

- **位置**：`models/models.go`（TaskType.CampaignPath 定义）
- **描述**：设计文档 4.1 要求 `CampaignPath` 带部分唯一索引 `idx_campaign_path_active, where: is_campaign=true`，实际模型仅声明 `size:100;default:''`，无唯一约束；且 `UpdateTaskType` 使用 `Updates(map)`，GORM 对 map 更新不触发 `BeforeUpdate` 校验 Hook。
- **影响**：管理员可将两个专项配置为相同 `campaign_path`，此时 `ResolveCampaignMiddleware` 与 `GetMyFindings` 的 `First()` 查询结果不确定。
- **建议**：补充部分唯一索引，并在 handler 层做冲突预检（创建/更新时查询同名路径是否已存在）。

### 4.2 运行时逻辑缺陷（中优先级）

#### 问题 2：趋势接口历史状态重建错误

- **位置**：`handlers/campaign_generic.go`（GetDynamicCampaignTrends）
- **描述**：`statusOnDate` 初值取 finding 的**当前**状态，而非"初始状态 + 按时间顺序回放 StatusLog"。示例：20 天前创建、5 天前 resolved 的缺陷，前 14 天的趋势点会被错误计为"已解决"。
- **影响**：30 天收敛趋势曲线失真。
- **建议**：以创建时的初始状态为基准，逐条回放 StatusLog 中 `time <= 当日 23:59:59` 的记录。

#### 问题 3：实体模式趋势页仍为缺陷口径

- **位置**：`frontend/src/pages/CampaignAnalysis.tsx`（renderTrendChart）
- **描述**：趋势 Tab 的标题/图例/文案仍写死"存量缺陷收敛趋势 / 未整改缺陷数 / 缺陷走势"，未按 `isEntityMode` 切换为"实体合格率"口径，与 repos/depts 两个 Tab 的双模式适配不一致。

#### 问题 4：归并引擎在部分分片失败时会误关缺陷

- **位置**：`services/engine_chunked.go` + `services/hooks.go`
- **描述**：所有专项任务类型均为 chunked 模式；只要存在成功 chunk，`finalize` 即执行 hook，而 hook 将"本次未匹配到的旧记录"一律置为 resolved。若某 chunk 失败，其覆盖文件中的存量缺陷会因"本次未扫到"被自动关闭。
- **建议**：hook 执行前校验 `failedChunks == 0`，或对失败 chunk 的文件范围做豁免。

#### 问题 5：归并引擎并发写入非原子

- **位置**：`services/hooks.go`（handleGenericCampaignHook）
- **描述**：hook 为"全量读旧记录 → 内存匹配 → 逐条 Create/Save"；同仓同任务类型并发执行两个报告时，重复记录靠唯一索引兜底，但 `Create` 冲突仅打日志不重试，`Save` 为最后写者胜。迁移有 `pg_advisory_xact_lock` 保护，hook 无对应保护。
- **建议**：采用 `ON CONFLICT DO UPDATE` 原子 upsert，或先加锁再读写。

### 4.3 口径不一致（中优先级）

#### 问题 6：修复率/合格率在三个接口间口径不一

- **repos 接口**：`total_defects = open + resolved`（不含 closed/invalid）；fix_rate = resolved / total_defects。
- **departments 接口**：`total_issues` 为全量 Count（含 closed/invalid）；fix_rate = resolved / total_issues。
- **trends 接口**：`total_issues` 为全量 Count；`resolved` 不计 closed/invalid；fix_rate = resolved / total_issues。
- **影响**：同一数据在仓库看板、部门看板、趋势图展示的百分比可能不一致。
- **建议**：统一"已处理 = resolved + closed + invalid"的口径，三处保持一致。

### 4.4 冗余代码与残留（低优先级）

1. **死代码**：
   - `models/db.go` 迁移结构 `tableMigration.campaignPath` 字段赋值后未使用；
   - `frontend/src/menu.ts` 的 `DEFAULT_CAMPAIGN_ICONS` 同时保留新旧两套 key（当前仅旧 key 命中）；
   - `frontend/src/pages/UniversalCampaignPage.tsx` 的"旧静态路径 → 新名字"fallback 映射为死代码（`campaign_path` 已直接使用旧别名）。
2. **硬编码残留**：
   - `services/task_runner.go:1137` 与 `handlers/task.go:295` 仍存在 `name == "ut_effectiveness"` 特判；
   - `frontend/src/components/AuditingWorkspace.tsx` 多处 `workspaceType === 'ut'` 特判（多数已被 `governanceMode` 参数覆盖，可收敛）。
3. **backfill 服务未元数据化**：`services/backfill.go` 仍按 7 个任务名硬编码查询，应改为 `WHERE is_campaign = true`。
4. **格式与小瑕疵**：
   - `models/db.go`、`models/models.go` 未过 gofmt；
   - `handlers/task_type.go` 存在重复的 `os.MkdirAll` 调用；
   - 部门接口对每个部门发 4~5 条 SQL，存在 N+1 放大；
   - `UpdateDynamicCampaignFinding` 审计文案固定为"人工核销了专项缺陷"，实体模式下语义不符；
   - 工作台类型下拉仅从当前页数据派生，分页时部分类型不出现在筛选中。

## 5. 整改优先级建议

| 优先级 | 整改项 |
| :---: | :--- |
| P1（上线前） | 问题 1（唯一索引与校验）、问题 4（失败分片误关缺陷）、问题 2（趋势状态重建） |
| P2（近期） | 问题 6（口径统一）、问题 3（趋势页双模式）、问题 5（并发 upsert） |
| P3（清理项） | 死代码、硬编码残留、backfill 元数据化、gofmt、N+1、审计文案 |

## 6. 总结

整体而言，本次重构与设计文档高度对齐，通用化效果显著，第一轮检视问题已全部整改并验证通过，前后端构建与测试均绿灯。剩余问题不影响主流程，建议按上述优先级推进整改，重点保障唯一性约束、失败分片数据安全与趋势口径一致性。
