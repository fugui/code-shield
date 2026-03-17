# 团队内部代码定期检视与重点问题跟踪系统

## 1. 架构概览 (Architecture Overview)
- **前端 (Frontend)**: React 单页应用，用于展示 Dashboard、检视列表、问题详情等。
- **后端 (Backend)**: 采用 GO 语言开发， 提供 RESTful API。
- **数据库 (Database)**: SQLite 嵌入式关系型数据库。
- **定时任务 (Cron Jobs)**: 用于触发定期的代码检视分配任务。
- **单一运行文件**: 使用 GO 语言的 embedding 机制， 将前端代码和后端代码打包成一个单一运行文件，方便部署（除了 config.yaml 外）。

## 2. 主要功能
- 团队管理： 团队管理，包括团队的创建、编辑、删除等操作。
- 代码仓管理： 我们团队有多个代码仓，每个代码仓有一个主分支，我们只需要关注主分支的提交。
- 定期代码检视： 定期对代码仓进行AI代码检视，并会输出报告（AI检视不在本系统内， 本系统只负责管理代码仓和检视任务和报告）。
- 重点问题跟踪： 对多线程、锁、内存泄漏、开源三方库等重点问题进行跟踪。

## 3. UI 视图设计 (UI Mockups)
以下是根据需求生成的界面原型图，请确认是否符合您的期望。

### 3.1 代码仓管理 (Repository Management)
![代码仓管理](/home/fugui/.gemini/antigravity/brain/454c3508-7d13-47fd-823a-f749f239959a/repo_management_ui_1773199702860.png)

### 3.2 定期代码检视报告 (Code Review Reports)
![代码检视报告](/home/fugui/.gemini/antigravity/brain/454c3508-7d13-47fd-823a-f749f239959a/code_review_reports_ui_1773199729923.png)

### 3.3 重点问题跟踪 (Key Issues Tracking)
![重点问题跟踪](/home/fugui/.gemini/antigravity/brain/454c3508-7d13-47fd-823a-f749f239959a/key_issues_tracking_ui_1773199746077.png)

## 4. API 定义 (API Definitions)
由于后端改用 Go 语言开发，我们将设计如下 RESTful API：

### 4.1 团队与组织架构 (Teams & Repositories)
- `GET /api/teams` - 获取团队列表及其负责人(Leader)信息。
- `POST /api/teams` - 创建新团队。
- `GET /api/repos` - 获取代码仓列表，含所属团队、Owner、URL 及主分支最新 Commit。
- `POST /api/repos` - 录入新的代码仓。

### 4.2 检视任务与报告 (Review Tasks & Reports)
- `POST /api/reviews/trigger` - 触发特定仓的 AI 代码检视。可对比 `base_commit` 和 `head_commit` 之间的差异代码（默认对比上一次成功检视以来的提交）。
- `GET /api/reviews` - 获取全量或指定代码仓的历史检视报告。
- `GET /api/reviews/:id` - 查看具体某次检视报告详情（代码异味、安全风险、改进建议等）。
- `POST /api/reviews/:id/webhook` - 用于接收外部 AI 检视服务的回调结果。

### 4.3 重点问题 (Key Issues)
- `GET /api/issues` - 获取所有重点问题列表（支持类型过滤：多线程、锁、内存泄漏、三方库等，以及按 `repo_id` 过滤）。
- `POST /api/issues` - 从 AI 检视报告中提取或手动创建重点问题。
- `PATCH /api/issues/:id` - 更新问题状态（打开、处理中、已解决）和解决人。

## 5. 数据库 Schema (SQLite)
根据 SQLite 及核心功能，简化版建表语句：

```sql
-- 团队组织
CREATE TABLE teams (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    leader_name TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 代码仓
CREATE TABLE repositories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    team_id INTEGER REFERENCES teams(id),
    name TEXT UNIQUE NOT NULL,
    url TEXT NOT NULL,
    owner_name TEXT NOT NULL,
    branch DEFAULT 'main',
    is_active BOOLEAN DEFAULT 1,
    last_commit_hash TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 检视报告
CREATE TABLE review_reports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id INTEGER REFERENCES repositories(id),
    base_commit TEXT NOT NULL,
    head_commit TEXT NOT NULL,
    status TEXT DEFAULT 'pending', -- pending, success, failed
    ai_summary TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- 重点问题
CREATE TABLE key_issues (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repo_id INTEGER REFERENCES repositories(id),
    report_id INTEGER REFERENCES review_reports(id),
    issue_type TEXT NOT NULL, -- multithreading, lock, memory_leak, library
    title TEXT NOT NULL,
    file_path TEXT,
    line_number INTEGER,
    status TEXT DEFAULT 'open', -- open, in_progress, resolved
    assignee TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```
