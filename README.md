# 码盾 · Code-Shield 🛡️

**码盾**（Code-Shield）是一套由 AI 驱动的企业级代码质量与安全自动化看护系统。系统基于微前端架构设计，无缝嵌入 `code-bench` 开发者综合工作台。通过集成大语言模型（LLM）执行器（支持 **Claude CLI** 与 **OpenCode CLI**），码盾能够对多语言代码仓库进行多维度深度扫描分析，重点拦截多线程安全、内存泄漏、浮点精度失真、Coredump 风险及第三方库漏洞等关键缺陷，并自动生成结构化检视报告、建立缺陷追踪并触发多渠道通知。

---

## 🌟 核心特性

### 🤖 强大的 AI 驱动代码检视
- **全方位无状态 AI 执行器支持**：原生适配 **Claude Code CLI**、**OpenCode CLI**、**Codex CLI**、**Native API** 与 **Antigravity (AGY) CLI**，统一无状态 Prompt 注入与多 LLM 调度模型。
  - **Claude**：支持系统提示词内联注入与流式 JSON 解析。
  - **OpenCode**：采用轻量级全局基座 Agent + 任务规约内联注入，实现完全无状态调用；权限批准使用文档化参数 `--auto`。
  - **Codex**：支持 OpenAI Codex CLI headless 免交互模式与模型动态路由；输出通过 `--output-last-message` 由 CLI 直接落盘，兼容默认只读沙箱环境。
  - **Native API & AGY**：支持直接对接标准 LLM 原生接口与 Google Antigravity 执行器。
  - ⚠️ **安全边界**：扫描引擎会使用 bash 工具并自动批准权限（OpenCode `--auto`），并非严格只读。对不可信代码仓库进行扫描时，请将服务部署在容器 / 低权限用户 / 无外网等进程级隔离环境中。
- **内置 10 大专项检视策略**：提供开箱即用的任务模板（`tasks/` 目录），涵盖增量评审 (`change-review`)、全仓深度检视 (`deep-review`)、内存泄漏 (`memory-leak`)、多线程并发 (`thread-create`)、浮点比较 (`float-comparison`)、cJSON解析 (`cjson-scan`)、Coredump风险 (`coredump-risk`)、无序集合 (`unordered-collection`) 及单测有效性/质量评估 (`ut-effectiveness`, `ut-quality`)。
- **自定义任务类型**：支持动态创建和管理多套检视策略，灵活配置 Analysis（切片分析）与 Synthesis（汇总阶段）提示词模板。
- **动态解析与定位**：智能识别 LLM 输出的单行、连续行范围及离散行号，支持在工作台一键复制精确文件名与行号。

### ⚙️ 多层级执行引擎与调度架构
- **单体分析引擎 (single)**：适用于小型代码仓，将代码整体提交给 AI 进行单次上下文分析。
- **分片并发引擎 (chunked)**：适用于大型单体项目，按目录树深度自动分片，多协程并发（默认 5）向 AI 提交，最后自动汇总。前端实时渲染分片进度（如 `分析中 (12/89)`）。
- **双模型对抗引擎 (debate)**：引入生成模型与评审模型多轮对弈辩论，显著抑制大模型幻觉与误报。
- **有效性评估引擎 (assessment)**：面向单元测试设计，包含契约约束与定向修复自愈管线。
- **规划与画像引擎 (planner / profile)**：智能分析仓库规模与特征，自动化编排分片深度与分析策略。
- **持久化任务队列 (DB-backed Queue)**：基于 PostgreSQL 的数据库持久化拉取与抢占模型，支持 `max_queue_size` 排队容量管控，服务重启任务不丢失。
- **分片失败恢复机制 (Chunk Recovery)**：支持对单次扫描中偶发失败的分片进行局部精确补扫恢复，在执行日志中高优标记并由调度队列原子抢占运行。
- **工作时间自动限流 (Work Hours Throttle)**：支持配置工作日及工作时间段内的并发比例（如白天限流 10% 或 0% 暂停），非工作时间/夜间自动恢复 100% 满速扫描，兼顾生产负载与扫描吞吐。
- **多 LLM 负载均衡映射**：支持配置多 LLM 服务器端点与并发上限，按模型类型精确分流。

### 📊 缺陷全生命周期追踪与治理中心
- **确定性指纹锚点与行号稳定器 (Anchor & Stabilizer)**：计算缺陷代码结构特征生成确定性指纹，配合跨版本行号稳定器，代码改动后仍能精准对齐历史缺陷。
- **缺陷台账 (Ledger) 与开发者工作台 (Workbench)**：闭环跟踪历史存量与增量缺陷，支持指派、处理状态流转与误报反馈。
- **专项治理攻坚会战 (Campaign)**：支持按团队、部门及专项类目设立治理会战大盘，一键导出专项 Excel 战报。
- **缺陷类目分类学治理 (Taxonomy & Feedback)**：基于结构化分类学进行缺陷规范化归类、语义标准化、误报校准（Calibrator）与反馈闭环。
- **全流程覆盖度对齐 (Coverage & Reconciliation)**：精准统计检视文件/行覆盖度并与真实代码树对齐。
- **规范化分页交互**：全量接入 `@code/common` 标准分页体系（URL 双向绑定、5 页滑动窗口、15/25/50/100 阶梯选项）。
- **多格式报告导出中心**：支持专项分析报告导出为企业级排版的 Excel/CSV、Markdown、JSON 及归档包，自适应中文字符列宽。

### ⏰ 自动化调度与操作审计
- **定时调度**：支持 Cron 表达式配置日常巡检计划，支持运行时参数覆盖（如 `SkipTests` 跳过测试代码）。
- **触发日志与审计 (Audit Logs)**：全面记录所有手动与定时触发动作（操作人、触发类型、目标摘要、客户端 IP），仅超级管理员（`super_admin`）可查看与管理。

### 📧 即时反馈与通知
- **邮件微服务联动**：无缝对接 `code-notifier` 组件（基于 Windows GUI + Outlook COM），将精美的 Markdown 报告转化为 HTML 正文与 PDF 附件自动投递。

---

## 🏗️ 系统架构

```text
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   前端 (React)   │────▶│  code-shield    │────▶│  CLI (Claude/   │
│  (Vite/Vanilla) │◄────│    (Go/Gin)     │◄────│   OpenCode)     │
└─────────────────┘     └────────┬────────┘     └─────────────────┘
                                 │
                 ┌───────────────┴───────────────┐
                 ▼                               ▼
      ┌────────────────────┐          ┌────────────────────┐
      │  code-notifier     │          │  PostgreSQL 共享库  │
      │  (Go / Win32 GUI)  │          │  (code-common 模型) │
      └────────────────────┘          └────────────────────┘
```

| 组件 | 技术栈 | 默认端口 | 说明 |
| :--- | :--- | :--- | :--- |
| **code-shield** | Go 1.25+, Gin, GORM, PostgreSQL | `8080` | 核心服务，负责 API 接口、持久化队列与扫描调度 |
| **frontend** | React 18, Vite 5, TS, Lucide, @code/common | `5173` | Web 控制台，支持 Module Federation 嵌入 `code-bench` |
| **notifier** | Go, Win32 GUI, Outlook COM | `8081` | 独立的邮件投递微服务（Windows 环境） |

> 📚 **系统深度架构与设计文档中心**：详见 **[docs/README.md](docs/README.md)**（涵盖使用指南、核心流水线规范、AI 扫描引擎设计、物理算力调度、确定性指纹、多租户治理与 RFC 提案）。

---

## ⚙️ 系统配置指南 (config.yaml)

```yaml
# ── HTTP 与基础服务 ──
server:
  port: ":8080"
  data_dir: "./data"                  # 运行时数据目录，自动存放 codes/ 缓存与 reports/ 报告
  worker_count: 5                     # 全局任务并发数，默认 5
  max_queue_size: 2000                # 任务排队最大上限，默认 2000，-1 表示不限制
  gin_log: false

# ── 数据库 ──
database:
  driver: "postgres"
  host: "127.0.0.1"
  port: 5432
  user: "postgres"
  password: "YOUR_POSTGRES_PASSWORD"
  dbname: "code_shield"               # 与 code-bench / code-pipeline 等共享同一数据库
  sslmode: "disable"
  timezone: "Asia/Shanghai"
  max_open_conns: 50
  max_idle_conns: 10

# ── AI 引擎与限流调度 ──
ai:
  backend: "claude"                   # CLI 后端：claude、opencode 或 codex
  output_format: "text"               # 输出格式：text 或 json

  # 工作时间自动限流配置 (可选)
  work_hours_throttle:
    enabled: true                     # 是否启用工作时间自动限流
    workdays: [1, 2, 3, 4, 5]         # 生效星期: 1=周一 ~ 5=周五
    start_time: "09:00"               # 开始限流时刻 (HH:MM)
    end_time: "22:00"                 # 结束限流时刻 (HH:MM)
    scale: 0.10                       # 工作时间内并发比例 (0.10 代表 10%，0.0 代表完全暂停)

  # 多 LLM 服务器并发配置 (可选)
  # models:
  #   - opencode: "models/glm5.1"
  #     claude: "glm5.1"
  #     concurrent: 5
  #   - opencode: "models/qwen3.5"
  #     concurrent: 2

# ── 通知服务 ──
notification:
  webhook: "http://192.168.56.18:8081/api/notify/email"

# ── 认证配置 (接入 code-common) ──
auth:
  jwt_secret: "YOUR_SHARED_JWT_SECRET_KEY"  # 必须与 code-bench 保持一致
  password_login_enabled: true
  admin_list:
    - "admin@code-shield.com"
```

---

## 🛠️ 快速开始

### 1. 构建与编译
```bash
# 一键完成前端构建打包与后端 Go 编译
make build
```

### 2. 运行服务
```bash
# 启动码盾核心服务
make run
```
默认监听 `:8080`，管理员初始账号：`admin@code-shield.com` / `admin123`。

### 3. 前端独立开发
```bash
cd frontend
npm install
npm run dev
```

---

## 🌐 子路径（Sub-path）部署

系统原生支持子路径打包（例如嵌入网关 `/shield/`）：
```bash
# 注入子路径环境变量构建前端
VITE_BASE_PATH=/shield/ make build
```

Nginx 反向代理配置示例：
```nginx
server {
    listen 80;
    server_name 192.168.56.18;

    location /shield/ {
        alias /path/to/code-shield/frontend/dist/;
        index index.html;
        try_files $uri $uri/ /shield/index.html;
    }

    location /shield/api/ {
        proxy_pass http://127.0.0.1:8080/api/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

---

## 📁 目录结构

```text
code-shield/
├── main.go                     # API 集中路由配置与服务启动入口
├── Makefile                    # 统一构建、测试、架构防腐门禁 (lint-arch) 与打包入口
├── config.yaml[.example]       # 系统配置文件与示例模板
├── cron_jobs/                  # 定时任务调度器与日常巡检清理作业 (jobs.go)
├── tasks/                      # 内置 10 大专项检视策略提示词与规约模板库
│   ├── change-review/          # 增量变更代码审查
│   ├── deep-review/            # 全仓深度代码质量审查
│   ├── coredump-risk/          # Coredump 与野指针风险排查
│   ├── memory-leak/            # 内存泄漏与资源未释放排查
│   ├── thread-create/          # 多线程与并发竞争看护
│   ├── float-comparison/       # 浮点数比较精度失真排查
│   ├── cjson-scan/             # cJSON 解析与边界溢出安全扫描
│   ├── unordered-collection/   # 无序集合遍历风险扫描
│   ├── ut-effectiveness/       # 单元测试用例有效性评估
│   └── ut-quality/             # 单元测试用例设计质量评估
├── models/                     # GORM 数据模型及全局配置单例
│   ├── config.go               # 运行时配置解析与动态单例
│   ├── db.go                   # PostgreSQL 连接池与初始化
│   ├── models.go               # Task, Schedule, Issue, TriggerLog, TaskType 等模型
│   ├── ledger_schema.go        # 缺陷台账与指纹锚点持久化结构
│   └── chunk_execution.go      # 分片原子执行记录与重试状态追踪
├── handlers/                   # HTTP API 控制层
│   ├── auth.go                 # 用户鉴权与当前登录态 (AuthMiddleware)
│   ├── task.go                 # 扫描任务创建、取消、重试与局部恢复
│   ├── task_type.go            # 任务类型管理与 Prompt 模板编排
│   ├── report_handler.go       # 任务报告领域聚合与多格式导出入口
│   ├── defect_handler.go       # 缺陷问题追踪与状态流转
│   ├── workbench.go            # 开发者治理工作台与缺陷处理接口
│   ├── campaign_generic.go     # 专项治理攻坚会战大盘与统计接口
│   ├── campaign_excel.go       # 会战专项 Excel 战报导出
│   ├── category_governance.go  # 缺陷类目分类学治理与规则映射
│   ├── feedback_handler.go     # 误报反馈与分类校准接口
│   ├── schedule.go             # 定时调度计划管理
│   ├── repo.go                 # 代码仓库管理与分支同步
│   ├── user.go / department.go # 组织架构、团队与权限管理
│   ├── config.go               # 动态配置中心与运行时更新
│   ├── debug.go                # 系统运行诊断与测试接口
│   └── trigger_log.go          # 触发日志与安全操作审计
├── services/                   # 核心领域业务服务与引擎架构
│   ├── dispatcher/             # 持久化任务队列抢占调度、租约控制、失败恢复 (recovery)、算力阶梯
│   ├── engines/                # AI 扫描执行引擎族
│   │   ├── single/             # 小型仓库单体分析引擎
│   │   ├── chunked/            # 大型项目自动分片并发分析引擎
│   │   ├── debate/             # 双模型多轮对抗辩论引擎 (抑制幻觉与误报)
│   │   ├── assessment/         # 测试用例有效性评估与契约自愈定向修复引擎
│   │   ├── planner/            # 任务切片与分析规划器
│   │   ├── profile/            # 代码仓特征画像分析引擎
│   │   └── chunker/            # 基于 AST 语法的智能代码切片器
│   ├── invoker/                # 底层 AI 执行驱动适配器 (Claude / OpenCode / Codex / Native / AGY)
│   ├── runner/                 # 任务流水线执行编排 (analysis 切片分析 / synthesis 汇总 / git 同步 / 续扫)
│   ├── defectlifecycle/        # 缺陷全生命周期追踪 (指纹锚点 anchor, 台账 ledger, 行号稳定器 stabilizer)
│   ├── governance/             # 缺陷类目治理中心 (分类学 taxonomy, 语义匹配, 误报校准 calibrator, 反馈学习)
│   ├── coverage/               # 扫描覆盖度计算与代码树对齐机制 (reconciliation)
│   ├── queue/                  # 持久化任务队列模型与执行状态快照
│   └── reports/                # 企业级多格式报告导出中心 (Excel / Markdown / JSON / Archive)
├── docs/                       # 系统架构、RFC、方法论与开发指南中心
└── frontend/                   # React 18 前端 SPA (纯原生 Vanilla CSS，无第三方 UI 框架捆绑)
    ├── src/pages/              # 业务路由页面 (Workbench, Campaign, Reports, Scan, ConfigCenter 等)
    ├── src/components/         # 业务公共组件 (report 报告中心, defects 缺陷, execution 轨迹看板)
    └── src/styles/             # 本地与全局样式
```

---

## 🏷️ 版本历史

### v1.6.0 (2026-08-23)
- **任务报告中心统一重构 (Task Report Center Refactor)**：
  - 彻底重构统一了任务报告抽屉与全屏详情查看器（`ReportViewer`），统筹收拢了全局 7 个业务页面的报告入口。
  - 全新设计并拆分三大核心模块：`📑 审计总结报告 (Summary)`、`📋 详细问题清单 (Findings)`、`🔬 运行轨迹与诊断 (Diagnostics)`。
  - 引入统一领域模型 DTO（`TaskReportMeta`、`SummaryDTO`、`FindingsPageResponse`、`DiagnosticsDTO`），杜绝大 JSON 一次性拉取性能瓶颈，支持服务端轻量分页（50 条/页）与级别/状态多维过滤。
- **企业级多格式报告导出引擎 (Multi-Format Exporter Engine)**：
  - 搭建独立报告导出引擎，支持 `Markdown`、`JSON`、`Excel (.xlsx)` 及出版级 `PDF 打印` 多格式导出。
  - Excel 报告内置【检视概要看板】与【问题明细清单】双 Sheet 豪华排版，自适应中文双字节列宽。
- **现代化 CI/CD 流水线诊断看板 (Pipeline Stepper Flow)**：
  - 全新设计流水线阶段时序流（`STAGE 01/02/03`、微光状态徽章、耗时胶囊），支持分片矩阵并发状态与终端输出日志优雅折叠。
- **高质感卡片布局与微前端样式强隔离**：
  - 抽屉视窗宽度扩充至 `min(1200px, 95vw)`，采用科技浅灰底色（`#f1f5f9`）衬托白底圆角大卡片，告别局促拥挤感。
  - 彻底解决微前端模块联邦（Module Federation）样式时序竞争导致的界面跳跃问题，固化组件级内边距与标准 Checkbox 勾选交互。
- **100% 确定性路径寻址 (Deterministic Path Resolution)**：
  - 彻底消除全部 `filepath.Glob` 模糊匹配依赖，历史产物与标准产物路径 100% 确定性寻址。

### v1.5.0 (2026-08-14)
- **数据库持久化任务队列 (DB Persistent Queue)**：将扫描任务队列全面升级为基于 PostgreSQL 数据库持久化拉取与原子抢占模型，新增 `max_queue_size` 限额保护，杜绝重启任务丢失。
- **分片失败恢复与日志标记 (Failed Chunk Recovery)**：支持对偶发失败的分片任务发起精确恢复，在执行日志中高优标记并通过队列原子抢占调度。
- **工作时间自动限流 (Work Hours Throttle)**：支持在配置的工作时间段内自动降低并发占用比例，非工作时间自动恢复 100% 满速扫描。
- **AI 扫描并发流控死锁修复**：彻底修复并发流控到期死锁缺陷，引入定时唤醒与持久化状态。
- **全平台公共库接入**：全面接入 `code-common/backend` 数据模型与鉴权中间件，前端统一采用 `@code/common` 的 `Pagination` 与 `ErrorBoundary`。
- **交互与体验优化**：在审计工作区、个人工作台与公开报告页面新增一键复制精确文件名与行号功能（Tooltip 动态呈现），缺陷分析指派默认处理人设定为当前登录用户。

### v1.4.0 (2026-08-08)
- **公开接口安全收紧**：收紧任务与漏洞查询公开接口，强制要求 JWT / SSO 登录认证。
- **多模型并发负载映射**：支持在 `config.yaml` 中配置多 LLM 服务器端点与差异化并发额度。
- **管理员种子账号初始化优化**：优化默认 admin 账号初始化逻辑，精确匹配邮箱并赋予 `super_admin` 角色与 `IsAdmin` 标记。

### v1.3.0 (2026-07-31)
- **触发日志与操作审计 (Audit Logs)**：新增任务触发历史日志功能，所有手动与定时触发事件均通过 `TaskTriggerLog` 模型进行结构化审计记录。
- **权限控制收紧**：将触发日志仅对超级管理员（`super_admin`）开放，并提供一键清除历史日志功能。
- **专项分析导出性能优化**：优化 Excel 导出列宽计算性能，彻底解决大批量数据导出 502 超时问题。

### v1.2.0 (2026-07-05)
- **专项分析漏洞报告导出**：支持漏洞报告导出为 Excel / CSV 格式。
- **提示词规范化**：多线程审计提示词优化，推荐平台内置调度组。

### v1.1.0 (2026-05-31)
- **以报告为入口的全新概览**：重塑报告列表页面，支持报告 ID 抽屉快速滑出详情与精确缺陷统计。
