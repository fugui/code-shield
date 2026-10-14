# AGENTS.md - 码盾 (Code-Shield) 🛡️ 智能体与开发者指南

本文档是专为参与 **Code-Shield** 开发的 AI 助手与团队成员制定的行动指引、架构规范与工程约束。

---

## 📌 1. 项目定位与核心技术栈

**码盾（Code-Shield）** 是一套由 AI 驱动的企业级代码质量与安全自动化看护系统，无缝嵌入 `code-bench` 开发者综合工作台。

* **后端服务**：Go 1.22+, Gin, GORM v2, PostgreSQL
* **前端工程**：React 18, Vite 5, TypeScript, Ant Design 5, `@code/common`
* **AI 调度适配**：适配无状态 Claude Code CLI, OpenCode CLI, Codex CLI 等执行器

---

## 🗂️ 2. 关键目录结构与职责划分

```text
code-shield/
├── main.go                     # API 路由集中注册与服务启动入口
├── Makefile                    # 统一构建、测试、门禁与打包入口
├── config.yaml[.example]       # 运行时配置文件模板
├── models/                     # GORM 数据模型及全局配置单例 (config.go)
├── handlers/                   # HTTP 控制器与鉴权中间件 (AuthMiddleware)
├── services/                   # 核心业务服务与调度层
│   ├── dispatcher/             # 持久化任务队列与抢占调度、分片失败恢复 (recovery)
│   ├── engines/                # AI 扫描执行引擎 (single / chunked / debate / assessment)
│   ├── invoker/                # 底层 CLI 执行驱动适配 (Claude / OpenCode / Codex)
│   └── runner/                 # 子进程与执行器
└── frontend/                   # React 18 前端 SPA
    ├── src/pages/              # 路由页面视图
    ├── src/components/         # 业务公共组件
    └── src/styles/             # 本地样式文件
```

---

## 🛡️ 3. 架构防腐红线 (ArchGuard 静态检查)

执行 `make lint-arch` 时系统会自动校验以下边界，**严禁违反**：
1. **引擎解耦**：`services/engines/` 下的扫描执行引擎**绝对禁止直接访问 `models.DB`**（必须由上层 dispatcher 注入或通过隔离接口操作）。
2. **驱动层独立**：`services/invoker/` CLI 驱动适配器**绝对禁止反向依赖 `engines` 或 `runner`**。
3. **避免循环依赖**：`services/runner/` **绝对禁止循环依赖 `code-shield/services` 根包**。

---

## 🎨 4. 代码风格与设计约束

### 4.1 后端 (Go)
* **严格错误处理**：必须显式且严格地处理所有 `error`，**严禁使用 `_ = err` 忽略任何错误**，一律采用 `if err != nil { return ... }`。
* **数据模型归口**：所有数据库持久化 Model 必须集中放置在 `models/` 目录下。
* **接口安全分类**：在 `main.go` 中注册路由时，除公开开放接口外，业务 API 必须统一挂载 `handlers.AuthMiddleware()` 进行 JWT 保护。

### 4.2 前端 (React / TypeScript)
* **深浅主题 Tokens (SSOT)**：样式颜色必须统一使用 `theme.css` 的语义变量（如 `var(--color-bg-surface)`、`var(--color-text-primary)`、`var(--color-border-primary)`），**严禁硬编码 `#fff`、`#000` 等绝对色值**。
* **样式抽象 (BEM)**：禁止滥用 JSX `style={{ ... }}`（真动态计算属性除外）；严禁在 JS 中模拟 hover，必须使用原生 CSS `:hover`。
* **通用分页规范**：具备表格分页的页面，必须集成 `@code/common` 标准分页规范：
  * 使用 `useSearchParams` 实现 URL 状态同步。
  * 选项阶梯固定为 `15`、`25`、`50`、`100`，默认值为 `25`。
  * 控件固定为 `[首页] [上一页] [5页连续数字滑动窗口] [下一页] [末页] [条/页下拉框]`。
* **类型安全**：严禁滥用 `any`，所有 API 交互及组件 Props 均须定义清晰的 TypeScript `interface`。

---

## 🛠️ 5. 常用开发与测试命令

* **全量编译打包**：
  ```bash
  make build
  ```
* **独立编译后端**：
  ```bash
  make backend
  ```
* **独立编译前端**：
  ```bash
  make frontend
  ```
* **运行架构防腐门禁检查 (ArchGuard)**：
  ```bash
  make lint-arch
  ```
* **运行 Go 单元测试**：
  ```bash
  go test ./...
  # 运行特定模块测试，例如：
  go test -v ./models/...
  go test -v ./services/dispatcher/...
  ```
* **前端静态检查与风格校验**：
  ```bash
  make lint
  ```

---

## ⚙️ 6. 运行环境与后台进程生命周期

* **网络访问基准**：本地调试与测试网络请求统一使用物理 IP **`192.168.56.18`**，**切勿使用 `127.0.0.1` 或 `localhost`**。
* **进程及时释放**：在完成代码编译运行与状态验证后，**必须在回复用户前主动释放并关闭所有启动的后台测试进程和服务**，严禁留存后台进程占用物理端口（例如 `8080`、`8082` 等），避免引发启动冲突。

---

## 🚀 7. 开发流与交付准则

1. **语言要求**：所有交流、代码注释及提交日志统一使用**中文**。
2. **约定式提交**：Git 提交信息必须符合 Conventional Commits 规范，如 `feat: 新增分片失败补扫调度逻辑`、`fix: 修复数据模型解析错误`。
3. **交付流程**：每次代码修改完成后，必须自检代码；确认 `make lint-arch`、单元测试及编译全部通过后，**直接 push 到 GitHub 仓库**，无需在汇报后等待口头二次指示。
