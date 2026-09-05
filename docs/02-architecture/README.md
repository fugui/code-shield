# 🏛️ 02-architecture: 现行核心系统架构与领域设计规范 (Living Architecture SSOT)

本目录为 Code-Shield 系统的**现行有效核心架构（Living Specifications）**与单一真实源（SSOT）。本目录内的所有文档均保持与当前代码实现完全同构，按业务领域（Sub-domains）分为 6 大核心子域：

---

## 🗂️ 领域子目录索引

```text
02-architecture/
├── README.md                                             # 本文档：架构全景与领域索引
├── 01-core-pipeline/                                     # 🔄【流水线与服务层】
├── 02-scanner-engines/                                   # 🤖【智能扫描引擎集群】
├── 03-dispatcher/                                        # ⚡【物理算力池与任务调度】
├── 04-fingerprint-governance/                            # 🧬【确定性指纹与缺陷治理】
├── 05-multi-tenancy/                                     # 🏢【多租户与隔离体系】
└── 06-data-models/                                       # 🗄️【数据资产与配置契约】
```

---

## 🧭 领域子域与核心规范一览

### 1. 🔄 [01-core-pipeline: 流水线与核心服务层](01-core-pipeline/README.md)
*   **[01-Services领域驱动子包化重构设计与契约规范](01-core-pipeline/01-Services领域驱动子包化重构设计与契约规范.md)**：7 大自治领域子包拓扑结构、`EngineContext` 解耦契约与 `services.go` 兼容门面设计。
*   **[02-扫描引擎与执行流水线职责边界与协同模型深度设计](01-core-pipeline/02-扫描引擎与执行流水线职责边界与协同模型深度设计.md)**：“算法推演专家”与“生命周期流程管家”的正交解耦、6 大标准化流水线阶段与断点续跑归位。

### 2. 🤖 [02-scanner-engines: 智能扫描引擎集群](02-scanner-engines/README.md)
*   **[01-下一代AI扫描引擎与多Agent对抗辩论设计](02-scanner-engines/01-下一代AI扫描引擎与多Agent对抗辩论设计.md)**：语义感知分片、跨目录同名投影映射、三方交叉对抗辩论（猎手/辩护人/仲裁官）与确定性严重度校准矩阵。
*   **[02-原生LLM轻量执行引擎与动静分离混合调用设计](02-scanner-engines/02-原生LLM轻量执行引擎与动静分离混合调用设计.md)**：原生 HTTP REST API 直连引擎、6 大单轮场景以及“探索型 Agent + 原生 LLM”混合调用架构。
*   **[03-智能体协作与异构调度深度设计](02-scanner-engines/03-智能体协作与异构调度深度设计.md)**：猎手/辩护/法官三方交互协议、完整 Prompt 体系与 Go 语言实现方案。

### 3. ⚡ [03-dispatcher: 物理算力池与任务调度](03-dispatcher/README.md)
*   **[01-异构模型流水线与DAG调度器设计](03-dispatcher/01-异构模型流水线与DAG调度器设计.md)**：Tier 1/2/3 异构模型阶梯调度、异步背压流控、动态 DAG 任务拓扑与槽位租约管理。

### 4. 🧬 [04-fingerprint-governance: 确定性指纹与缺陷治理闭环](04-fingerprint-governance/README.md)
*   **[01-确定性源码指纹与抗抖动增量生命周期架构设计](04-fingerprint-governance/01-确定性源码指纹与抗抖动增量生命周期架构设计.md)**：`SourceEnricher` 物理源码真实源感知器、三级指纹分层体系与受控 Taxonomy 归一化。
*   **[02-下一代确定性缺陷生命周期与空间几何对齐架构设计](04-fingerprint-governance/02-下一代确定性缺陷生命周期与空间几何对齐架构设计.md)**：行号漂移几何对齐窗口、全状态流转机与代码变更守卫防假修复机制。
*   **[03-基于现网实测的确定性指纹演进与多任务通用分类收敛设计](04-fingerprint-governance/03-基于现网实测的确定性指纹演进与多任务通用分类收敛设计.md)**：通用任务分类矩阵、标准化规则映射与收敛算法。
*   **[04-多任务企业治理与历史记忆闭环深度设计](04-fingerprint-governance/04-多任务企业治理与历史记忆闭环深度设计.md)**：全任务通用指纹算法、增量 Diff 状态机、人机协同反馈与误报规则自进化。
*   **[05-同一代码仓多次扫描问题清单的系统化对账与增量治理架构设计](04-fingerprint-governance/05-同一代码仓多次扫描问题清单的系统化对账与增量治理架构设计.md)**：跨批次快照一致性核验、对账状态仲裁与增量交付机制。

### 5. 🏢 [05-multi-tenancy: 多租户与隔离治理体系](05-multi-tenancy/README.md)
*   **[01-GORM动态分库路由与连接池管理深度设计](05-multi-tenancy/01-GORM动态分库路由与连接池管理深度设计.md)**：多租户物理分库隔离、动态路由中间件与连接池生命周期管理。
*   **[02-租户模型服务资源隔离与动态调度设计](05-multi-tenancy/02-租户模型服务资源隔离与动态调度设计.md)**：租户资源配额、优先级队列调度与多租户动态限流。
*   **[03-代码仓凭据保险库与工作区安全沙箱](05-multi-tenancy/03-代码仓凭据保险库与工作区安全沙箱.md)**：凭据保险库（Vault）隔离机制与 Git 临时工作区沙箱安全加固。
*   **[04-租户分层配置与动态热加载深度设计](05-multi-tenancy/04-租户分层配置与动态热加载深度设计.md)**：系统、租户与项目三级配置覆盖模型与热加载机制。

### 6. 🗄️ [06-data-models: 数据资产与配置契约](06-data-models/README.md)
*   **[01-CodeShield-数据模型与系统配置变更设计](06-data-models/01-CodeShield-数据模型与系统配置变更设计.md)**：数据库 DDL 规范、GORM 模型定义与系统全局 `config.yaml` 配置字典。
