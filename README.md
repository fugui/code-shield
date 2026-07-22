# 码盾 · Code-Shield 🛡️

**码盾**（Code-Shield）是一套 AI 驱动的代码质量自动看护系统，专为提升代码质量和安全性而设计。系统通过集成高级 LLM CLI 工具（如 Claude 和 OpenCode）对代码仓库进行深度分析，重点检测多线程安全、内存泄漏和第三方库漏洞等关键问题，并自动生成检视报告、进行问题追踪和通知推送。

## 🌟 核心特性

### 🤖 强大的 AI 驱动代码检视
- **灵活的 AI 引擎支持**：支持配置不同的底层 AI 执行器（当前支持 **Claude CLI** 和 **OpenCode CLI**）。
  - **OpenCode**：支持全局 Agent (`~/.config/opencode/agents/`) 持久化配置、工具调用权限控制。
- **自定义任务类型**：支持通过 Web UI 动态管理不同的检视任务类型，自由配置阶段提示词（Analysis / Synthesis）、运行时参数（如 `SkipTests`）和执行引擎。
- **动态解析行号**：支持识别 LLM 吐出的多种行号格式（如单行 `100`，行范围 `100-125`，离散行 `41,42`）。

### ⚙️ 多层级执行引擎适配
为了适应不同体量的代码仓，内置了两种执行引擎（可在任务类型中配置）：
- **单引擎 (single)**：将整个代码仓作为整体提交给 AI 分析，适合小型项目。
- **分片引擎 (chunked)**：按目录深度自动分片，多并发（默认 5）向 AI 提交，最后综合汇总。适合大型单体项目，前端支持 **实时展示分片处理进度**（如 `分析中 (12/89)`）。

### 📊 全方位项目与数据管理
- **系统数据看板**：可视化展示近期代码问题走势、底层模型请求次数、平均延迟及耗时数据（支持动态多选模型筛选）。
- **关键问题 (Issues) 追踪**：将高优的安全问题拦截并建立 Issue，支持按团队/部门/负责人的组合查询，跟踪处理状态。
- **灵活的权限控制**：系统账号管理支持分配不同权限，支持批量管理团队、仓库与人员信息。

### ⏰ 自动化与动态调度
- **定时调度**：支持 Cron 表达式配置检视计划。
- **多维度触发**：支持全局/分组/团队/单仓触发，支持在调度配置中 **覆盖任务运行参数**（RunParams）。
- **高并发控制**：基于 `worker_count` 全局工作池控制最大并行任务数，保护系统资源。

### 📧 即时反馈与通知
- **自动邮件**：支持通过独有的 `notifier` 组件（基于 Win32 GUI + Outlook COM）将精美的报告发送到负责人邮箱。
- **智能投递**：支持基于检视风险评分（Score）的阈值触发自动告警。

---

## 🏗️ 系统架构

```text
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   前端 (React)   │────▶│  shield-server  │────▶│  CLI (Claude/   │
│   (Vite/AntD)   │◄────│    (Go/Gin)     │◄────│   OpenCode)     │
└─────────────────┘     └────────┬────────┘     └─────────────────┘
                                 │
                                 ▼
                          ┌─────────────────┐
                          │    notifier     │
                          │  (Go / Win32)   │
                          │   Outlook COM   │
                          └─────────────────┘
```

| 组件 | 技术栈 | 默认端口 | 说明 |
|------|--------|------|------|
| **shield-server** | Go 1.21+, Gin, GORM, PostgreSQL | `8080` | 核心主服务，提供 API 接口及任务调度管线 |
| **notifier** | Go 1.21+, Winigo, Outlook COM | `8081` | 独立的邮件投递微服务（仅限 Windows 运行） |

---

## 🚀 快速开始

### 运行环境
- **主服务**：Linux / macOS / Windows, Go 1.21+, PostgreSQL 12+, Node.js 18+（用于前端构建）。
- **通知服务**：需 Windows 系统并安装 Microsoft Outlook。
- **AI 依赖**：主机环境中必须可用 `claude` 或 `opencode` 命令行工具。

### 安装与启动

#### 1. 克隆项目
```bash
git clone https://github.com/fugui/code-shield.git
cd code-shield
```

#### 2. 安装依赖并构建
```bash
# 编译并构建整个系统（一键完成前端构建与后端编译）
make build
```

#### 3. 配置系统
修改 `config.yaml`（配置 PostgreSQL 数据库）：
```yaml
server:
  port: ":8080"
  worker_count: 5              # 全局任务池并发度
storage:
  root: "/path/to/data"        # 数据落地根目录（代码克隆与结果存储）
database:
  host: "127.0.0.1"            # PostgreSQL 地址
  port: 5432
  user: "code_shield"
  password: "code_shield_password"
  dbname: "code_shield"
  sslmode: "disable"
ai:
  backend: "opencode"          # AI 后端："claude" 或 "opencode"
notification:
  webhook: "http://127.0.0.1:8081/api/notify/email"
```

#### 4. 启动服务
```bash
make run
```
浏览器访问 `http://localhost:8080`。  
默认管理员账号：`admin@code-shield.com` / `admin123`

---

## 🌐 子路径（Sub-path）部署

当系统需要运行在非根域名的子路径下（例如 `http://www.cndev.net/shield/`）时，需要对前端进行基准路径（Base Path）打包，并配合反向代理（如 Nginx）进行请求转发。

系统已原生支持子路径配置，具体构建与部署步骤如下：

### 1. 前端子路径打包
前端使用 Vite 进行构建，支持通过环境变量 `VITE_BASE_PATH` 注入子路径。

在构建系统时，在命令前加上环境变量：
```bash
# 在根目录使用 Make 一键构建（注入子路径环境变量）
VITE_BASE_PATH=/shield/ make build

# 或者手动进入前端目录进行构建
cd frontend
VITE_BASE_PATH=/shield/ npm run build
```

> [!NOTE]
> - 注入的 `VITE_BASE_PATH` 必须以斜杠 `/` 开头和结尾（例如 `/shield/`）。
> - 这一步会使前端的静态资源引用路径（如 `/shield/assets/...`）以及 React Router 的 `basename` 均自动适配为该子路径。

### 2. Nginx 反向代理配置
在 Nginx 中配置反向代理，将子路径流量正确分发。静态文件可由 Nginx 直接高效托管，API 请求则转发给后端的 Go 服务。

示例如下：

```nginx
server {
    listen 80;
    server_name www.cndev.net;

    # 1. 托管前端静态资源
    location /shield/ {
        # 指向打包生成的 dist 目录绝对路径
        alias /path/to/code-shield/frontend/dist/;
        index index.html;
        try_files $uri $uri/ /shield/index.html;
    }

    # 2. 转发 API 请求至 Go 后端服务 (Gin)
    location /shield/api/ {
        # 注意：末尾的斜杠 '/' 非常重要，Nginx 会自动在转发时剥离掉 '/shield/api/' 部分并映射到 '/api/'
        # 从而使后端 Gin 服务可以直接在常规根路径上处理 '/api/...' 请求，无需修改后端代码
        proxy_pass http://127.0.0.1:8080/api/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### 3. 启动后端服务
后端服务无需做任何特殊修改，按照常规配置启动即可：
```bash
make run
```
后端服务默认监听 `:8080`，Nginx 会将 `http://www.cndev.net/shield/api/` 的请求平滑转发至 `http://127.0.0.1:8080/api/`。

---

## 📖 核心使用指南

### 1. 配置任务类型 (Task Types)
管理员可以在 "任务类型管理" 中创建全新的检视任务：
- **Prompt 维护**：在线编辑 Analysis（切片分析）与 Synthesis（汇总阶段）的提示词。如果是 OpenCode 后端，系统会自动将提示词同步至全局 `~/.config/opencode/agents/`。
- **引擎选择**：指定 `chunked` 等引擎，并提供 JSON 格式配置（如 `{"max_files": 50, "concurrency": 5}`）控制切片粒度。

### 2. 调度配置 (Schedules)
- **参数覆盖**：在创建调度时，可以勾选 `SkipTests` 从而覆盖默认行为，跳过测试代码的分析以节省 Token。
- **避免冲突**：系统内置 409 防并发检测，同一代码仓库如果处于 `pending` / `running` 状态，不会被重复拉起。

### 3. 查看与追踪
- **实时进度**：大型代码仓进入分析阶段后，前端将展示具体的切片进度 `分析中 (12/89)`。
- **问题列表**：所有高价值（严重、阻塞等）的检视结果被提取至「问题列表」集中追踪。
- **统计图表**：访问数据大盘跟踪 API 耗时、成功率、团队代码安全健康度等指标。

---

## 🛠️ 目录结构

```text
code-shield/
├── config.yaml             # 系统配置文件
├── main.go                 # 程序入口
├── models/                 # 数据库模型与全局配置解析
├── handlers/               # HTTP API 接口路由与处理逻辑
├── services/               # 核心业务组件
│   ├── task_runner.go      # 任务生命周期状态机
│   ├── engine*.go          # Single / Chunked 执行引擎实现
│   ├── opencode_cli.go     # OpenCode AI 后端适配器
│   ├── claude_cli.go       # Claude AI 后端适配器
│   └── agent_sync.go       # OpenCode Agent 配置文件生命周期同步服务
├── cron_jobs/              # 定时调度逻辑
├── frontend/               # React 前端源码
├── notifier/               # Windows Outlook 邮件投递代理服务
├── templates/              # CSV 数据导入模板
└── Makefile                # 一键构建与启动脚本
```

---

## ❓ 常见排障

**Q: 执行状态一直卡在 "分析中"，且控制台看到 `max_tokens must be at least 1`？**
> 检查系统是否传递了负数的 tokens 值。当前网关拦截了负数，如果是下游 LLM 模型配额问题，请通过日志确认。

**Q: OpenCode 引擎报错 `Command Execution Failed`，且未生成有效报告？**
> 系统开启了 `--dangerously-skip-permissions`，请检查当前宿主机上的 OpenCode CLI 版本，确保符合配置要求。同时，请确认 `~/.config/opencode/agents/` 的写入权限，因为任务类型更新会自动同步至此目录。

**Q: 如何更改全局允许并发执行的最大任务数？**
> 请修改 `config.yaml` 中的 `server.worker_count`，默认为 5。此修改需重启服务生效。

## 🏷️ 版本历史 (Release History)

### v1.2.0 (2026-07-05)
- **报告导出增强**：支持专项分析漏洞报告导出 Excel (CSV) 格式，修复了导出 Excel 时特定字段类型不匹配导致的 JSON 解析失败问题。
- **提示词优化**：在多线程创建审计提示词中，优先推荐使用平台内置的调度组与定时器以规范并发使用。
- **权限控制收紧**：限制代码实时看护功能仅系统管理员可见，提升了平台数据与功能的整体安全性。
- **文档结构优化**：删除了已废弃的旧 `shield-server` 目录前缀，并在 README 中更新了真实的项目结构目录与指令路径。

### v1.1.0 (2026-05-31)
- **报告概览全面重构（报告入口）**：重塑 `/shield/reports` 报告概览页面为真正“以报告为入口”的架构，不再受限于代码仓维度，支持最新时间倒序展示。
- **报告 ID 单体快速交互**：表格首列新增「报告 ID」，支持直接点击 ID 快速滑出详细报告抽屉，极大地简化了用户获取具体审计报告的步骤。
- **高精确缺陷数量统计**：后端在任务完成时直接基于问题严重性分级（阻塞、严重、主要、提示、建议、高风险、中风险、低风险）统计缺陷数并写入数据库；前端配合指标 JSON 实现高保真容错渲染，消除了正则解析 AI 报告的误差。
- **微前端极简级联检索**：提供了极简化高集成的筛选控制面板，支持一键重置，并且在 `code-bench` 宿主中完美支持动态折叠/展现二级分组菜单及 Admin 权限守卫。
- **测试任务增强 (Go + C/C++)**：在 `ut-effectiveness` 和 `ut-quality` 任务中增加了对 GO 语言 (`.go` 文件) 的完整支持，并升级了 [engine_chunked.go](file:///home/fugui/codes/code-shield/services/engine_chunked.go) 的 `isTestFile` 过滤算法，自动过滤业务代码，确保 UT 任务精确只扫测试文件。
- **底层架构鲁棒性**：修复了 GORM v2 中由 `Select("task_reports.*")` 引发的数据库分页 COUNT 语法错误与 Builder 状态污染问题，保证海量数据分页稳定流畅。

---

*Code-Shield - 让代码更安全、更可靠 🛡️*
