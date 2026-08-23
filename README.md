# 码盾 · Code-Shield 🛡️

**码盾**（Code-Shield）是一套由 AI 驱动的企业级代码质量与安全自动化看护系统。系统基于微前端架构设计，无缝嵌入 `code-bench` 开发者综合工作台。通过集成大语言模型（LLM）执行器（支持 **Claude CLI** 与 **OpenCode CLI**），码盾能够对多语言代码仓库进行多维度深度扫描分析，重点拦截多线程安全、内存泄漏、浮点精度失真、Coredump 风险及第三方库漏洞等关键缺陷，并自动生成结构化检视报告、建立缺陷追踪并触发多渠道通知。

---

## 🌟 核心特性

### 🤖 强大的 AI 驱动代码检视
- **双引擎 CLI 支持**：原生适配 **Claude CLI** 与 **OpenCode CLI**，支持模型配置热更新。
  - **OpenCode**：支持全局 Agent (`~/.config/opencode/agents/`) 配置文件生命周期自动同步与工具调用权限控制。
- **自定义任务类型**：支持动态创建和管理多套检视策略，灵活配置 Analysis（切片分析）与 Synthesis（汇总阶段）提示词模板。
- **动态解析与定位**：智能识别 LLM 输出的单行、连续行范围及离散行号，支持在工作台一键复制精确文件名与行号。

### ⚙️ 多层级执行引擎与调度架构
- **单引擎 (single)**：适用于小型代码仓，将代码整体提交给 AI 进行单次上下文分析。
- **分片引擎 (chunked)**：适用于大型单体项目，按目录树深度自动分片，多协程并发（默认 5）向 AI 提交，最后自动汇总。前端实时渲染分片进度（如 `分析中 (12/89)`）。
- **持久化任务队列 (DB-backed Queue)**：彻底由内存队列升级为基于 PostgreSQL 的数据库持久化拉取与抢占模型，支持 `max_queue_size` 排队容量管控，服务重启任务不丢失。
- **分片失败恢复机制 (Chunk Recovery)**：支持对单次扫描中偶发失败的分片进行局部精确补扫恢复，在执行日志中高优标记并由调度队列原子抢占运行。
- **工作时间自动限流 (Work Hours Throttle)**：支持配置工作日及工作时间段内的并发比例（如白天限流 10% 或 0% 暂停），非工作时间/夜间自动恢复 100% 满速扫描，兼顾生产负载与扫描吞吐。
- **多 LLM 负载均衡映射**：支持配置多 LLM 服务器端点与并发上限，按模型类型精确分流。

### 📊 全方位项目与缺陷管理
- **系统数据看板**：可视化展示近期缺陷分布趋势、底层模型请求次数、平均延迟及耗时数据。
- **关键问题 (Issues) 追踪**：将高优安全隐患与缺陷自动提升为 Issue 跟踪闭环，默认指派人为当前处理人，支持按团队、部门、严重级别多维检索。
- **规范化分页交互**：全量接入 `@code/common` 标准分页体系（URL 双向绑定、5 页滑动窗口、15/25/50/100 阶梯选项）。
- **报告一键导出**：支持专项分析报告导出为企业级排版的 Excel/CSV 文件，自适应中文字符列宽。

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
│   (Vite/AntD)   │◄────│    (Go/Gin)     │◄────│   OpenCode)     │
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
| **frontend** | React 18, Vite 5, TS, AntD 5, @code/common | `5173` | Web 控制台，支持 Module Federation 嵌入 `code-bench` |
| **notifier** | Go, Win32 GUI, Outlook COM | `8081` | 独立的邮件投递微服务（Windows 环境） |

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
  backend: "claude"                   # CLI 后端：claude 或 opencode
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
├── config.yaml             # 系统配置文件
├── main.go                 # 程序入口与集中式路由配置
├── models/                 # 数据模型（引用 code-common/backend）
│   ├── config.go           # 本地配置解析
│   ├── db.go               # GORM v2 数据库连接初始化
│   └── models.go           # Task / Schedule / Issue / AuditLog 等模型
├── handlers/               # HTTP API 控制层
│   ├── auth.go             # 鉴权与当前用户信息
│   ├── audit_log.go        # 触发日志与审计接口
│   ├── task.go             # 扫描任务创建、取消、重试、恢复
│   ├── task_type.go        # 任务类型与提示词管理
│   ├── schedule.go         # 定时调度管理
│   ├── repo.go             # 代码仓管理
│   ├── issue.go            # 缺陷与问题追踪
│   └── excel_exporter.go   # Excel 导出控制器
├── services/               # 核心业务组件与引擎
│   ├── queue.go            # 基于 DB 的持久化抢占式任务队列
│   ├── dispatcher.go       # 并发流控与工作时间限流分发器
│   ├── task_runner.go      # 任务生命周期执行状态机
│   ├── engine_single.go    # 单体分析执行引擎
│   ├── engine_chunked.go   # 自动切片并发分析执行引擎
│   ├── opencode_cli.go     # OpenCode CLI 驱动适配器
│   ├── claude_cli.go       # Claude CLI 驱动适配器
│   ├── agent_sync.go       # OpenCode Agent 配置同步服务
│   └── reports/            # 任务报告领域聚合与多格式导出引擎
│       ├── report_service.go # 报告元信息/总结/问题清单/诊断核心服务
│       ├── exporter_md.go    # Markdown 导出器
│       ├── exporter_json.go  # JSON 结构化导出器
│       └── exporter_excel.go # 出版级 Excel (.xlsx) 导出器
├── cron_jobs/              # 定时任务调度器
├── frontend/               # React 前端工程 (接入 @code/common)
│   ├── src/components/report/ # 现代化任务报告中心组件群 (Viewer/Tabs/Cards)
└── Makefile                # 自动化编译与运行脚本
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
