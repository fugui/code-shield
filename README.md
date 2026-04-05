# 码盾 · Code-Shield 🛡️

**码盾**（Code-Shield）是一套 AI 驱动的代码质量自动看护系统，专为提升代码质量和安全性而设计。系统通过集成 LLM（如 Claude AI）对代码仓库进行深度分析，重点检测多线程安全、内存泄漏和第三方库漏洞等关键问题，并自动生成检视报告。

## 核心功能

### 🤖 AI 代码检视
- **智能分析**：利用 LLM 进行深度代码分析，发现潜在问题。
- **灵活扩展**：支持多种检视任务类型（如通用代码检视、内存泄漏检测等），可自定义 Prompt。
- **定向巡检**：重点检测以下领域：
  - 多线程与并发安全（竞态条件、死锁、资源竞争）
  - 内存泄漏（未关闭的资源、无法回收的对象）
  - 第三方库安全（已知漏洞、API 误用）
- **五级分级**：阻塞、严重、主要、提示、建议。

### 📊 项目管理
- **系统账号管理**：支持分配系统账号，记录真实姓名，追踪最近登录时间及账号状态。
- **团队管理**：组织架构管理，支持设置团队负责人。
- **成员管理**：维护人员信息及邮箱，支持批量导入。
- **仓库管理**：代码仓库配置，关联团队和负责人。
- **问题追踪**：关键问题的状态跟踪和指派处理。

### ⏰ 自动化调度
- **定时检视**：支持 Cron 表达式配置检视计划。
- **多种模式**：支持全部/按分组/按团队/指定仓库等多种触发模式。
- **手动触发**：随时手动触发检视任务。

### 📧 通知推送
- **邮件报告**：自动生成 Word 格式检视报告。
- **智能投递**：通过 Outlook 发送邮件到项目相关人员（需 Windows 环境）。
- **即时反馈**：检视完成后自动通知相关人员，支持设置通知阈值。

## 系统架构

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   前端 (React)   │────▶│  shield-server  │────▶│   LLM API       │
│                 │◄────│    (Go/Gin)     │◄────│ (Claude/OpenAI) │
└─────────────────┘     └────────┬────────┘     └─────────────────┘
                                 │
                                 ▼
                          ┌─────────────────┐
                          │    notifier     │
                          │  (Go / Win32)   │
                          │   Outlook COM   │
                          └─────────────────┘
```

| 组件 | 技术栈 | 端口 | 说明 |
|------|--------|------|------|
| shield-server | Go + Gin + GORM + SQLite | 8080 | 主服务，提供 API，内嵌前端静态资源 |
| notifier | Go + Winigo (Win32 GUI) | 8081 | 邮件通知服务（Windows 桌面应用，内置 GUI，通过 COM 直接操作 Outlook） |

## 快速开始

### 环境要求

- **shield-server**：Go 1.21+、Node.js 18+（仅用于前端构建）
- **notifier**：Windows 操作系统、Go 1.21+、Microsoft Outlook 客户端

### 安装步骤

#### 1. 克隆代码

```bash
git clone https://github.com/fugui/code-shield.git
cd code-shield
```

#### 2. 安装依赖

```bash
# 安装 notifier 依赖
cd notifier && go mod download

# 安装前端依赖并构建
cd ../shield-server/frontend && npm install && npm run build
```

#### 3. 编译构建

在根目录下执行：

```bash
make build
```

#### 4. 启动服务

```bash
make run
```

该命令会同时启动 `notifier` 和 `shield-server`。

#### 5. 访问系统

打开浏览器访问 http://localhost:8080

默认管理员账号：
- 用户名：`admin@code-shield.com`
- 密码：`admin123`

## 使用指南

### 1. 初始化配置

登录系统后，按以下步骤初始化：

1. **创建团队**：进入"团队管理"，添加团队并设置负责人。
2. **导入成员**：进入"成员管理"，添加团队成员（邮箱用于接收通知）。
3. **添加仓库**：进入"仓库管理"，配置代码仓库信息。
4. **系统管理**：在"系统管理"中管理系统账号、配置 AI 检视的 Prompt 任务类型及 Cron 定时调度。

### 2. 检视调度配置

支持多种触发模式：

| 模式 | 说明 |
|------|------|
| 全部仓库 | 检视所有已激活的仓库 |
| 业务分组 | 按服务分组筛选仓库 |
| 指定团队 | 检视特定团队下的所有仓库 |
| 指定仓库 | 只检视选定的仓库 |

Cron 表达式示例：
- `0 9 * * 1`：每周一上午 9 点
- `0 18 * * 5`：每周五下午 6 点
- `0 9 * * 1-5`：工作日每天上午 9 点

### 3. 查看检视报告

在"任务中心"页面：
- 查看所有仓库的检视状态。
- 点击检视记录查看详细 Markdown 报告。
- 手动点击"发送通知"触发邮件发送。
- 支持导出 Word 文档。

## 问题分级标准

系统对发现的问题进行五级分类：

| 级别 | 图标 | 说明 | 处理建议 |
|------|------|------|----------|
| 阻塞 | 🔴 | 会导致系统崩溃、数据丢失或严重安全漏洞 | 立即修复 |
| 严重 | 🟠 | 高风险缺陷，如死锁、内存泄漏、竞态条件 | 优先修复 |
| 主要 | 🟡 | 影响功能正确性或性能的明显缺陷 | 计划修复 |
| 提示 | 🔵 | 代码风格、命名不规范、轻微逻辑隐患 | 建议修复 |
| 建议 | ⚪ | 可选的改进建议，最佳实践推荐 | 酌情处理 |

## 目录结构

```
code-shield/
├── shield-server/          # 主服务 (Go)
│   ├── main.go            # 程序入口
│   ├── config.yaml        # 配置文件
│   ├── handlers/          # HTTP 处理器
│   ├── models/            # 数据模型
│   ├── services/          # 业务逻辑
│   │   ├── task_runner.go  # 任务执行器
│   │   └── queue.go        # 异步任务队列
│   ├── cron_jobs/         # 定时任务调度
│   ├── tasks/             # 检视任务定义 (Prompt/脚本)
│   │   ├── code-review/
│   │   └── memory-leak/
│   └── frontend/          # React 前端
├── notifier/              # 邮件通知服务 (Node.js)
│   └── src/
│       └── index.js       # 邮件发送逻辑 (Outlook COM)
├── templates/             # CSV 导入模板
├── Makefile               # 项目构建脚本
└── README.md
```

## 配置说明

### shield-server/config.yaml

```yaml
server:
  port: ":8080"                    # 服务端口
notifier:
  url: "http://127.0.0.1:8081/api/notify/email"  # 通知服务地址
review:
  notify_threshold: 20             # 自动通知的风险分值阈值
workspace:
  home: "/path/to/workspace"       # 代码克隆和报告生成的根目录
```

## API 接口

### 认证

```http
POST /api/login
Content-Type: application/json

{
  "username": "admin@code-shield.com",
  "password": "admin123"
}
```

响应中的 `token` 用于后续请求的 `Authorization: Bearer <token>` 头部。

### 辅助接口

| 接口 | 方法 | 说明 |
|------|------|------|
| `/api/me` | GET | 获取当前登录用户信息 |
| `/api/password` | PATCH | 修改当前用户登录密码 |

### 主要业务接口

| 接口 | 方法 | 说明 |
|------|------|------|
| `/api/teams` | GET/POST | 团队管理 |
| `/api/members` | GET/POST | 成员管理 |
| `/api/repos` | GET/POST | 仓库管理 |
| `/api/users` | GET/POST | 系统账号管理（限管理员） |
| `/api/tasks` | GET/POST | 任务记录与手动触发 |
| `/api/task-types` | GET/POST | 任务类型配置 |
| `/api/schedules` | GET/POST | 调度配置 |
| `/api/issues` | GET/POST | 关键问题管理 |
| `/api/executions` | GET | 执行日志查看 |

## 开发指南

### 前端开发

```bash
cd shield-server/frontend
npm run dev
```

### 后端开发

```bash
cd shield-server
go run main.go
```

### 数据库

系统使用 SQLite 数据库，数据文件默认位于 `shield-server/code_shield.db`。

## 常见问题

### Q: 邮件发送失败怎么办？

1. 确认 `notifier` 服务已在 Windows 环境启动（显示 GUI 窗口）。
2. 确认 Outlook 客户端已安装，且至少配置了一个活动的邮箱账户。
3. 检查 `shield-server` 中的 `notifier.url` 配置是否能正确访问 `notifier` 服务。
4. 查看 `notifier` GUI 界面中的日志排查 COM 组件执行错误。

### Q: 检视任务一直处于 pending 状态？

1. 检查 `shield-server` 是否正常启动并连接到数据库。
2. 确认 `services.StartWorkerPool` 已成功启动工作协程。
3. 检查 `workspace.home` 路径是否有写权限，以及 Git 是否可用。

### Q: 如何修改 AI 检视的提示词？

在系统界面的"任务类型管理"中直接修改，或编辑 `shield-server/tasks/{type}/prompt.md` 文件。

## 技术栈

### 后端
- **语言**：Go 1.21+
- **框架**：Gin, GORM, robfig/cron

### 前端
- **框架**：React 18, Vite, Ant Design

### 邮件服务
- **运行环境**：Windows (x64)
- **技术栈**：Go 1.21+, Winigo (Win32 GUI), Outlook COM

---

**Code-Shield** - 让代码更安全、更可靠 🛡️
