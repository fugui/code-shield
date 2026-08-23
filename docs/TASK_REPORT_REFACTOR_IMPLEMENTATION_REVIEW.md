# Code-Shield 任务报告重构实施检视报告

> **检视对象**：2026-08-23 提交至 `origin/main` 的全部 16 个提交（`86442cd` ~ `9e40de5`）
> **检视基准**：`docs/TASK_REPORT_REFACTOR_DESIGN.md`（v2.0.0）
> **检视日期**：2026-08-23
> **检视方式**：设计文档逐项对照 + 全量 diff 阅读 + 真实数据样例核对 + 构建/测试验证

---

## 0. 验证结果

| 验证项 | 结果 |
| :--- | :--- |
| `go build ./...` / `go vet ./...` | 通过 |
| `go test ./services/reports/... ./handlers/...` | 通过 |
| `npx tsc --noEmit`（frontend） | 通过 |
| 真实数据核对（`reports/deep_review/*`、`reports/ut_effectiveness/*` synthesis JSON） | 发现实体模式卡片过滤语义问题（见 3.3） |
| Git 状态 | 工作区干净，`main` 与 `origin/main` 一致 |

---

## 1. 总体结论

本次重构方向正确、落地质量总体良好：

- **达成设计目标的部分**：三 Tab 一站式查看、统一导出中心、双治理模式自适应清单、分级按需加载与竞态保护、失败/空数据/单引擎状态机、独立 Iframe 打印引擎、URL 深链联动、综合风险分估值口径统一。
- **规模与结构**：净增 5348 行 / 删除 2156 行。前端以约 2600 行组件族替换旧 `ReportSidebar.tsx`（550 行）+ `PublicReportFindings.tsx`（686 行）；`handlers/task.go` 由 1037 行瘦身至 651 行；旧 `excel_exporter.go` 整体删除。
- **需要整改**：存在 **1 个 P0 功能破坏**（旧端点删除不彻底）、**1 个 P0 设计项未落地**（只读安全分享）、以及若干 P1 正确性/一致性问题和可进一步清理的冗余。

---

## 2. 正确性问题

### 2.1 【P0】`9e40de5` 删除旧接口后，ExecutionLogs 的运行轨迹快照功能失效

`9e40de5` 从 `main.go` 移除了 5 条旧路由（`/tasks/:id/report`、`/synthesis`、`/synthesis/csv`、`/summary`、`/findings`）并删除了对应 handler，但前端仍有引用：

- `frontend/src/pages/ExecutionLogs.tsx:242` 仍请求 `/api/tasks/${reportId}/summary`；
- 该页面展开执行日志行时依赖此接口渲染“运行轨迹与诊断快照”（静态分析耗时、综合报告耗时、分片网格、故障分片等，见 745~810 行）。

接口 404 后前端静默失败（`res.ok` 判断），该面板将永远为空。

**建议**：迁移到 `/api/tasks/:id/report/diagnostics`（返回 `analysis_duration`、`chunks`、`error_message` 等，字段基本对应），或最小代价保留 `/tasks/:id/summary` 兼容路由。二选一后做一次全仓 `rg` 旧端点核对，避免再次出现“删了接口没删调用方”。

### 2.2 【P1】ReportViewer 的状态流转链路是死代码，且请求的端点不存在

- `291bef8` 已将问题卡片状态改为只读 Badge，`FindingCard.tsx` 不再渲染任何状态/责任人操作控件；
- 但 `useTaskReport.updateFindingStatus`（乐观更新 + 失败回滚，约 80 行）、`ReportViewer.handleStatusChange`、`ReportFindingsTab.onStatusChange`、`FindingCard.onStatusChange` 仍被串联传递，**没有任何 UI 触发**；
- 该链路提交到 `/api/campaign/findings/:id/status`，而 `main.go` 中**从未注册此端点**（真实流转走 `/api/analysis/:campaign/findings/:id` 的 PATCH）；
- 隐患：合成 JSON 的 `id` 字段在真实数据中全部为 0，后端回退为“数组下标 + 1”，并非 CampaignFinding 的 DB ID。即使补上端点，按此 ID 提交也会写错记录。

**建议**：明确二选一——
1. 若确认“报告中心只读、治理流转在工作台/专项页完成”（现状），删除整条 `updateFindingStatus` / `onStatusChange` / `isReadOnly` 死链，让代码与行为一致；
2. 若恢复卡片内流转，则需先补齐后端端点，并让 `FindingItemDTO.ID` 返回 CampaignFinding 真实主键。

### 2.3 【P1】流水线时序存在硬编码虚构耗时

- `services/reports/report_service.go` `GetReportDiagnostics` 中，“初始化克隆”固定 1.0s、“前置检查”固定 0.5s；
- `ReportDiagnosticsTab.tsx` 兜底步骤还硬编码“代码克隆 1s / 前置预检 0.5s / 报告综合 2s / 结果入库 1s”。

真实 `summary JSON` 已有 `analysis/synthesis/merging` 的 `status` 与 `duration_seconds`，可完全数据驱动。虚构耗时一旦被用户/管理层采信，会损害报告可信度。

**建议**：克隆/预检从任务元数据或日志取数；取不到时展示 `--` 而不是编造数值，前端兜底步骤改为“暂无数据”。

### 2.4 【P1】实体评估模式：严重度卡片计数与过滤语义不一致

`ReportFindingsTab.tsx` 实体模式下：

- “不合格 / 风险”卡片计数 = `fatal + critical + major`；
- 但点击该卡片后只发送 `severity=fatal`，`critical/major` 的实体被排除，卡片数字与列表结果对不上；
- 真实 `ut_effectiveness` 数据中还存在 `建议/提示`（suggestion/minor）级别的实体：单选“合格”卡片时 `severity=pass` 会把这些实体一并隐藏。

**建议**：实体模式点击“不合格/风险”时映射为 `fatal,critical,major`（后端已支持逗号多值）；或改用 `status=fail` 过滤并让卡片计数口径与状态口径一致。

### 2.5 【P2】其他细节

- `ExportReportHandler`：`Content-Disposition` 的 `filename=` 直接放入中文，RFC 5987 规定 `filename*` 才携带 UTF-8 编码；建议 `filename=` 用 ASCII 兜底（如 `report.xlsx`）。
- `GetReportFindings`：`pageSize > 500` 时回落到 50 而非 500，语义意外；`GetReportAggregate` 静默截断为 500 条（当前前端未使用 aggregate 接口，但 API 消费者需知悉）。
- `handlers/campaign_excel.go` 的 `getCampaignStatusChinese` 与 `reports.GetStatusChinese` 语义重复且不一致：实体模式 `analyzing` 一处显示“问题分析”、一处显示“复核中”。
- `ArchiveExporter` 将 summary JSON 原始内容写入 `diagnostics.json` 条目，文件名与内容语义不符。
- `loadAllFindingsRaw`：CampaignFinding 查询失败时静默跳过 DB 字段；AnalysisFinding 回退分支把状态硬编码为 `open`，历史报告将全部显示“待处理”。
- `ReportFindingsTab` 关键词搜索无防抖，每次按键触发一次请求（竞态保护避免了错乱，但仍建议 300ms 防抖）。

---

## 3. 完整性：设计 v2.0.0 未落地 / 偏离项

| 设计项 | 状态 | 说明与建议 |
| :--- | :--- | :--- |
| 【P0】只读安全分享 `/shield/share/report/:token` + `/api/public/share/report/:token` | **未落地** | 头部“复制链接”生成的是 `/public/report/:id`，该路由被 `PrivateRoute` 包裹、需登录；Toast 却宣称“便于在团队中快速分享”，语义误导。`ReportViewer` 的 `mode="readonly"` 分支存在但无任何调用方。建议：本期若不做 token 分享，至少删除 `readonly` 死分支并修正分享文案/路由命名，避免 `/public/` 前缀误导。 |
| 导出菜单 CSV / ZIP 项 | 有意收窄（`7a2253a`） | 后端 `format=csv/zip` 仍可用，但无 UI 入口，属“半成品”状态。建议在菜单保留 ZIP 归档项，或明确后端也同步下线，二选一。 |
| 失败任务“下载完整日志文件” | 未落地 | 仅提供 200 行截断 + 展开查看，无下载完整日志入口。 |
| URL 深链 `?tab=findings` 定位 Tab | 未落地 | 仅同步了 `taskId/reportId`，Tab 维度未接入。 |
| 打印页眉/页脚页码 | 未落地 | iframe 打印引擎未实现 `@page` margin box 页码。 |
| `data/reports/{task_id}/` 目录规范 | 未落地 | 仍按 legacy 目录 + Glob 兜底（已加 TODO 注释，可接受为渐进项）。 |
| 全屏展开 `pushState` 后退联动 | 未落地 | 当前仅切换抽屉宽度，无 URL 同步。 |

---

## 4. 简洁性与冗余清理

### 4.1 已完成的合理清理（肯定）

- 删除 `ReportSidebar.tsx`（550 行），`PublicReportFindings.tsx` 由 686 行收敛为 25 行；
- `handlers/task.go` 1037 → 651 行，报告读取/CSV 拼接职责移交 `services/reports`；
- 删除旧 `handlers/excel_exporter.go`（430 行）；
- `useTaskReport` 移除三处旧接口回退逻辑（约 100 行），并补齐竞态保护与 aggregate 空指针兜底；
- `printHelper` 移除内联暗色变量清洗逻辑，收敛为 iframe 打印引擎；
- 新组件族无历史遗留的 `<style>` 标签内联样式块。

### 4.2 仍建议清理的冗余

1. **死代码链**：`updateFindingStatus` / `handleStatusChange` / `onStatusChange` / `isReadOnly`（见 2.2），约 100+ 行。
2. **未使用属性/字段**：`ReportExportMenu` 的 `taskId` prop；`ExportOptions.Format`（各导出器实际只用 `Scope` / `IsCSV`）。
3. **两套 Excel 导出器重复**：`handlers/campaign_excel.go`（204 行）与 `services/reports/exporter_excel.go`（352 行）的样式、表头、状态中文映射高度重复——这正是设计文档第 1.2 节点名批评的“两套导出代码割裂”问题，重构后仍存在。建议抽取共享的 XLSX 写入/状态映射工具，两个入口复用。
4. **双份打印样式**：`printHelper` 的 `printDocStyles` 与 `report.css` 的 `@media print` 块（约 150 行）规则重复；建议以 iframe 引擎为准，删除或大幅收敛 CSS 侧重复规则。
5. **残留旧样式引用**：`ExecutionLogs.tsx:747` 仍引用已删除的 `report-sidebar-spinner` / `report-sidebar-spin`，加载动画不会旋转。
6. **内联样式仍多**：`FindingCard`、`ReportDiagnosticsTab`、`ReportEmptyState` 等组件仍大量使用内联 `style`，与 `report.css` 并存，可后续逐步收敛（非本次阻塞项）。
7. **双份严重级别映射**：前端 `reportUtils.normalizeSeverity` 与后端 `NormalizeSeverity` 重复实现。当前后端已归一化，前端可仅保留 `getSeverityMeta` 展示映射，或注明前端为防御性兜底。

---

## 5. 提交逐条检视简表

| 提交 | 主题 | 结论 |
| :--- | :--- | :--- |
| `86442cd` | 重构落地（组件族/导出引擎/统一 Viewer） | 通过，架构合理；具体问题见上文 2.x/3.x |
| `ad39ba3` | URL 快速链接与深链联动 | 通过；分享语义问题见 3 |
| `fd0d59f` | 综合风险分估值口径统一 | 通过 |
| `4e0797c` `fbd2275` `7f98aee` | 抽屉打印修复 + iframe 打印引擎 + 亮色主题 | 通过；双份打印样式见 4.2 |
| `7611977` `c227f8b` | 头部视觉优化、耗时 0ms 修复 | 通过 |
| `234f02f` `145e0db` `d9a5b94` | 筛选布局/卡片看板/边距系统 | 通过 |
| `291bef8` | 卡片状态改只读 Badge | 有问题：造成状态流转死代码与不存在端点，见 2.2 |
| `7a2253a` | 移除 CSV/ZIP 菜单项 | 通过（有意收窄），建议同步收敛后端入口 |
| `10d5ace` | 严重度卡片默认全选高亮 | 通过；实体模式卡片过滤见 2.4 |
| `9e40de5` | 全面清理冗余 + 竞态/空指针修复 | 有问题：竞态与空指针修复正确、清理方向正确，但旧端点删除不彻底，见 2.1 |

---

## 6. 优先修复清单

| 优先级 | 事项 | 位置 |
| :--- | :--- | :--- |
| P0 | ExecutionLogs 迁移到 `/report/diagnostics` 或保留 `/summary` 兼容路由 | `frontend/src/pages/ExecutionLogs.tsx:242` |
| P1 | 清理或接线状态流转死代码（含补端点、修正 ID 语义） | `useTaskReport.ts` / `FindingCard.tsx` / `main.go` |
| P1 | 移除流水线硬编码虚构耗时，改真实数据或 `--` | `report_service.go` / `ReportDiagnosticsTab.tsx` |
| P1 | 实体模式“不合格/风险”卡片过滤改为 `fatal,critical,major` | `ReportFindingsTab.tsx` |
| P2 | 分享语义与 `readonly` 死分支处理 | `ReportHeader.tsx` / `ReportViewer.tsx` |
| P2 | 导出菜单与后端 CSV/ZIP 能力收敛二选一 | `ReportExportMenu.tsx` / `report_handler.go` |
| P2 | Excel 导出器共享化、打印样式去重、死字段/死 prop 清理 | `handlers/campaign_excel.go` 等 |

---

> 注：本检视基于 `origin/main`（`9e40de5`）与 `docs/TASK_REPORT_REFACTOR_DESIGN.md` v2.0.0。上述 P0/P1 修复后建议重新构建并手工回归 ExecutionLogs 展开快照、实体模式清单过滤与打印预览。
