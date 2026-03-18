# 码盾 · Code-Shield 🛡️

**码盾**（Code-Shield）是一套 AI 驱动的代码质量自动看护系统，专为提升代码质量和安全性而设计。系统通过集成 Claude AI 对代码仓库进行深度分析，重点检测多线程安全、内存泄漏和第三方库漏洞等关键问题，并自动生成检视报告。

## 核心功能

### 🤖 AI 代码检视
- **智能分析**：利用 Claude AI 进行深度代码分析，发现潜在问题
- **定向巡检**：重点检测以下领域：
  - 多线程与并发安全（竞态条件、死锁、资源竞争）
  - 内存泄漏（未关闭的资源、无法回收的对象）
  - 第三方库安全（已知漏洞、API 误用）
- **五级分级**：阻塞、严重、主要、提示、建议

### 📊 项目管理
- **团队管理**：组织架构管理，支持设置团队负责人
- **成员管理**：维护人员信息及邮箱，支持批量导入
- **仓库管理**：代码仓库配置，关联团队和负责人
- **问题追踪**：关键问题的状态跟踪和指派处理

### ⏰ 自动化调度
- **定时检视**：支持 Cron 表达式配置检视计划
- **多种模式**：支持全部/按分组/按团队/指定仓库等多种触发模式
- **手动触发**：随时手动触发检视任务

### 📧 通知推送
- **邮件报告**：自动生成 Word 格式检视报告
- **智能投递**：通过 Outlook 发送邮件到项目相关人员
- **即时反馈**：检视完成后自动通知相关人员

## 系统架构

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   前端 (React)   │────▶│  shield-server  │────▶│  Claude AI API  │
│                 │◄────│    (Go/Gin)     │◄────│                 │
└─────────────────┘     └────────┬────────┘     └─────────────────┘
                                 │
                                 ▼
                          ┌─────────────────┐
                          │    notifier     │
                          │  (Node.js)      │
                          │  Outlook COM    │
                          └─────────────────┘
```

| 组件 | 技术栈 | 端口 | 说明 |
|------|--------|------|------|
| shield-server | Go + Gin + GORM + SQLite | 8080 | 主服务，提供 API 和前端界面 |
| notifier | Node.js + Express | 8081 | 邮件通知服务（需 Windows 环境） |

## 快速开始

### 环境要求

- **shield-server**：Go 1.21+、Node.js 18+
- **notifier**：Windows 操作系统、Node.js 18+、Microsoft Outlook

### 安装步骤

#### 1. 克隆代码

```bash
git clone https://github.com/fugui/code-shield.git
cd code-shield
```

#### 2. 安装依赖

```bash
# 安装前端依赖
cd shield-server/frontend && npm install

# 安装 notifier 依赖
cd ../../notifier && npm install
```

#### 3. 编译构建

```bash
cd ..
make build
```

#### 4. 启动服务

```bash
make run
```

或使用 Docker：

```bash
docker-compose up -d
```

#### 5. 访问系统

打开浏览器访问 http://localhost:8080

默认管理员账号：
- 用户名：`admin`
- 密码：`admin123`

## 使用指南

### 1. 初始化配置

登录系统后，按以下步骤初始化：

1. **创建团队**：进入"团队管理"，添加团队并设置负责人
2. **导入成员**：进入"成员管理"，添加团队成员（邮箱用于接收通知）
3. **添加仓库**：进入"仓库管理"，配置代码仓库信息
4. **设置调度**：进入"检视调度"，配置自动检视计划

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

在"检视概览"页面：
- 查看所有仓库的检视状态
- 点击检视记录查看详细报告
- 点击"发送通知"手动发送邮件
- 支持导出 Word 文档

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
├── shield-server/          # 主服务
│   ├── main.go            # 程序入口
│   ├── config.yaml        # 配置文件
│   ├── handlers/          # HTTP 处理器
│   ├── models/            # 数据模型
│   ├── services/          # 业务逻辑
│   │   ├── review_executor.go   # 检视执行器
│   │   └── worker_pool.go       # 工作队列
│   ├── cron_jobs/         # 定时任务
│   ├── frontend/          # React 前端
│   │   ├── src/
│   │   └── package.json
│   ├── code-review-prompt.md  # AI 检视提示词
│   └── docs/              # API 文档
├── notifier/              # 邮件通知服务
│   └── src/
│       └── index.js       # 邮件发送逻辑
├── templates/             # 模板文件
├── Makefile
└── README.md
```

## 配置说明

### shield-server/config.yaml

```yaml
server:
  port: ":8080"                    # 服务端口
notifier:
  url: "http://127.0.0.1:8081/api/notify/email"  # 通知服务地址
```

### 环境变量

| 变量名 | 说明 | 默认值 |
|--------|------|--------|
| `PORT` | 服务端口 | `:8080` |
| `NOTIFIER_URL` | 通知服务地址 | `http://127.0.0.1:8081/api/notify/email` |
| `DB_PATH` | 数据库文件路径 | `code_shield.db` |

## API 接口

### 认证

```http
POST /api/login
Content-Type: application/json

{
  "username": "admin",
  "password": "admin123"
}
```

响应中的 `token` 用于后续请求：

```http
Authorization: Bearer <token>
```

### 主要接口

| 接口 | 方法 | 说明 |
|------|------|------|
| `/api/teams` | GET/POST | 团队列表/创建 |
| `/api/members` | GET/POST | 成员列表/创建 |
| `/api/repos` | GET/POST | 仓库列表/创建 |
| `/api/reviews/overview` | GET | 检视概览 |
| `/api/reviews` | GET | 检视记录列表 |
| `/api/reviews/trigger` | POST | 手动触发检视 |
| `/api/reviews/:id/notify` | POST | 手动发送通知 |
| `/api/schedules` | GET/POST | 检视调度配置 |
| `/api/issues` | GET/POST | 关键问题管理 |

## 开发指南

### 前端开发

```bash
cd shield-server/frontend
npm install
npm run dev
```

### 后端开发

```bash
cd shield-server
go run main.go
```

### 数据库

系统使用 SQLite 数据库，数据文件位于 `shield-server/code_shield.db`。

## 常见问题

### Q: 邮件发送失败怎么办？

1. 确认 notifier 服务已启动并运行在 Windows 环境
2. 确认 Outlook 客户端已安装并配置邮箱账户
3. 检查 shield-server 中的 `notifier.url` 配置是否正确
4. 查看 notifier 控制台日志排查错误

### Q: 检视任务一直处于 pending 状态？

1. 检查 shield-server 是否正常启动
2. 查看控制台日志是否有错误
3. 确认工作队列已启动（默认 5 个并发 workers）

### Q: 如何修改 AI 检视的提示词？

编辑 `shield-server/code-review-prompt.md` 文件，修改后重启服务生效。

## 技术栈

### 后端
- **语言**：Go 1.21+
- **Web 框架**：Gin
- **ORM**：GORM
- **数据库**：SQLite
- **定时任务**：robfig/cron

### 前端
- **框架**：React 18
- **构建工具**：Vite
- **UI 组件**：Ant Design
- **HTTP 客户端**：Axios

### 邮件服务
- **运行时**：Node.js 18+
- **框架**：Express
- **文档生成**：html-to-docx
- **邮件发送**：Outlook COM Object + PowerShell

## 贡献指南

欢迎提交 Issue 和 Pull Request。在提交代码前，请确保：

1. 代码通过编译检查：`go build`
2. 前端构建成功：`npm run build`
3. 提交信息清晰描述改动内容

## 许可证

[MIT License](LICENSE)

## 相关链接

- [Claude CLI 文档](https://docs.anthropic.com/en/docs/claude-code/cli)
- [Gin 框架文档](https://gin-gonic.com/docs/)
- [GORM 文档](https://gorm.io/docs/)

---

**Code-Shield** - 让代码更安全、更可靠 🛡️
