# GEMINI.md - Code-Shield 🛡️ 开发者指南

## 📜 核心工作规章
为了保持团队高效协同与代码库的健康度，所有团队成员（包括 AI 助手）必须严格遵守以下规章：
*   **沟通语言**：团队成员全部为简体中文母语，所有交流、文档、代码注释及提交日志应**尽可能使用中文**进行沟通与书写。
*   **提交日志规范**：Git 提交日志必须符合 **Conventional Commits（约定式提交）** 规范。提交消息必须使用如 `feat:`、`fix:`、`docs:`、`refactor:`、`style:`、`test:`、`chore:` 等前缀，前缀后需加半角冒号与空格，随后紧跟中文描述。例如：`fix: 修复漏扫快速补扫逻辑`。
*   **开发流与交付**：每次代码修改完成后，**必须进行修改代码的代码检视**。确认无误且本地构建与测试全部通过后，请**直接 push 到 GitHub** 仓库。
*   **代码精简原则**：始终保持代码极其简洁。**严禁 Copy-Paste 产生冗余雷同代码**，遇到无用或冗余代码应及时予以重构或彻底删除。

---

## 🎨 代码风格与开发规范

### 1. 后端 (Go 语言)
*   **命名规范**：遵循标准 Go 命名法则。
    *   外部包可导出对象使用首字母大写 `PascalCase`。
    *   内部变量、包内私有函数/结构体使用小驼峰 `camelCase`。
*   **错误处理**：必须显式且严格地处理所有 `error`。**禁止使用 `_ = err` 忽略任何错误**，应当使用标准的 `if err != nil { return ... }` 处理。
*   **数据库交互**：采用 **GORM v2** 进行持久化。所有数据库的模型定义（Model）必须统一放置在 `shield-server/models` 目录下。
*   **接口路由**：API 路由在 `shield-server/main.go` 中进行集中式配置，路由应严格区分为：
    *   **免密路由 (Unprotected)**：无需认证的开放 API。
    *   **受保护路由 (Protected)**：使用 JWT/SSO 鉴权的受保护路由，必须挂载 `handlers.AuthMiddleware()` 中间件。
*   **任务与引擎**：AI 核心检视逻辑包含 `single`（单仓分析）和 `chunked`（分片并发分析）两种执行引擎，所有实现与接口均定义在 `shield-server/services` 目录下。

### 2. 前端 (React / TypeScript)
*   **核心技术栈**：React 18, Vite 5, TypeScript, Ant Design (AntD)。
*   **样式与 UI 规范**：优先使用 **Vanilla CSS** 进行精细化布局与设计，拒绝粗糙的默认 UI 配置。保持界面的**高端质感**与**流畅动效**（例如：分片分析进度条的实时流畅呈现、防止界面卡死的优雅骨架屏等）。
*   **组件设计**：公共组件统一放置在 `frontend/src/components` 下，确保单一职责且具备高复用性；路由页面与视图存放于 `frontend/src/pages`。
*   **类型安全**：**严禁滥用 `any` 类型**。所有 API 响应数据、组件 Props、以及内部状态（State）都必须定义清晰明确的 TypeScript `interface`。

### 3. 系统配置 (Config)
*   **配置文件**：系统默认加载 `shield-server/config.yaml`。
*   **解析与全局管理**：所有配置文件的解析代码和全局配置单例均集中放置于 `shield-server/models/config.go`。修改配置项时，请务必保持注释及说明高度一致。

---

## 🛠️ 常用开发命令速查
*为方便 AI 助手与开发人员进行日常校验与本地测试，以下提供核心开发命令汇总：*

*   **依赖安装与编译构建**：
    *   一键全系统构建：`make build`
    *   仅编译后端：`make -C shield-server backend`
    *   仅编译前端：`make -C shield-server frontend`
*   **运行服务**：
    *   快捷启动系统：`make run`
    *   启动前端开发服务器：`cd shield-server/frontend && npm run dev`
*   **测试与格式化**：
    *   运行后端 Go 测试：`cd shield-server && go test ./...`
    *   格式化 Go 代码：`cd shield-server && go fmt ./...`
    *   前端静态检查：`cd shield-server/frontend && npm run lint`
