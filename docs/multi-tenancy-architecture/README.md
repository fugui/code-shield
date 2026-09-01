# Code-Shield 多租户与分库隔离系统架构设计 🛡️

## 1. 架构背景与业务愿景
随着组织架构向多子公司、多业务线演进，不同子公司在代码安全与智能检视体系中提出了强烈的**多租户（Multi-Tenancy）与保密隔离**诉求：
1. **数据与信息物理级保密隔离**：各子公司之间的代码仓、扫描任务、漏洞报告、执行日志相互绝对不可见，消除跨租户数据泄漏隐患。
2. **专属模型服务资源（BYOM & Quota）**：每个子公司使用独立的大模型资源（包括自建私有化大模型、专属 API Key 及独立并发槽位配额）。
3. **独立的 Git 凭据与代码仓管理**：支持子公司各自管理代码仓与私有 Git 域名，凭据加密隔离。
4. **自治的扫描任务类型扩展**：各子公司可根据自身技术栈定制专属于自己的扫描任务类型（TaskType）、Prompt 与前后置规则。
5. **代码侵入性极低**：在架构上采用 **GORM 动态分库（Database-per-Tenant）模式**，使上层现有的大量业务 CRUD 代码、表结构模型几乎无需修改。

---

## 2. 方案全景与文档索引

本设计文档集涵盖了从顶层选型、底层连接池、模型调度、代码与任务沙箱到运维迁移的全套技术落地规范：

| 序号 | 文档名称 | 核心内容概要 |
| :--- | :--- | :--- |
| **01** | [01-多租户隔离方案评估与分库架构选型.md](./01-多租户隔离方案评估与分库架构选型.md) | 对比逻辑隔离 vs 分库隔离 vs 物理独立；确立 Master-Tenant 架构模型 |
| **02** | [02-GORM动态分库路由与连接池管理深度设计.md](./02-GORM动态分库路由与连接池管理深度设计.md) | `TenantDBManager` 设计、中间件注入、Context 驱动与无感代码适配 |
| **03** | [03-租户模型服务资源隔离与动态调度设计.md](./03-租户模型服务资源隔离与动态调度设计.md) | 租户级专属模型资源池（BYOM）、并发配额控制与 `ModelDispatcher` 升级 |
| **04** | [04-代码仓凭据保险库与工作区安全沙箱.md](./04-代码仓凭据保险库与工作区安全沙箱.md) | 租户级 Git 凭据 AES-GCM 加密存储、多域名适配与本地克隆目录隔离 |
| **05** | [05-租户自定义任务类型与动态规则扩展设计.md](./05-租户自定义任务类型与动态规则扩展设计.md) | 系统内置 + 租户私有 `TaskType` 机制、Prompt 动态存储与安全执行沙箱 |
| **06** | [06-多租户系统迁移运维与实施路线图.md](./06-多租户系统迁移运维与实施路线图.md) | AutoMigrate 升级策略、存量数据迁移、超管跨库大盘与分阶段实施甘特图 |

---

## 3. 总体架构拓扑图

```mermaid
flowchart TD
    subgraph 接入层 (Ingress & Auth)
        User[子公司用户 / 开发者 / 管理员] --> Gateway[API 网关 / Caddy]
        Gateway --> AuthMW[租户识别与鉴权中间件]
    end

    subgraph 控制面 (Control Plane)
        AuthMW --> MasterDB[(Master 管理主库<br>租户元数据 / 租户DB连接配置)]
        AuthMW --> TenantMgr[TenantDBManager 连接池管理器]
    end

    subgraph 数据面-持久化 (Tenant Data Plane)
        TenantMgr --> DB_A[(子公司 A 独立数据库<br>data/tenants/1001/shield.db)]
        TenantMgr --> DB_B[(子公司 B 独立数据库<br>data/tenants/1002/shield.db)]
        TenantMgr --> DB_N[(子公司 N 独立数据库<br>MySQL / SQLite)]
    end

    subgraph 资源面-调度与模型 (Model & Worker Plane)
        TaskRunner[TaskRunner 任务调度器] --> ModelRouter[租户模型分发路由]
        ModelRouter --> ModelA[子公司 A 专属模型池<br>内网私有 LLM / 独立并发]
        ModelRouter --> ModelB[子公司 B 专属模型池<br>自备商业 API Key]
    end

    subgraph 存储面-文件隔离 (Storage Plane)
        TaskRunner --> StorageA[子公司 A 工作区<br>data/tenants/1001/codes/ & reports/]
        TaskRunner --> StorageB[子公司 B 工作区<br>data/tenants/1002/codes/ & reports/]
    end
```
