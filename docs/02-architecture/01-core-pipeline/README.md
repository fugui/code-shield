# 🔄 01-core-pipeline: 流水线与核心服务层架构

本领域聚焦 Code-Shield 核心服务层（`services/`）的领域模型、子包划分、执行流水线（Pipeline）与引擎协作契约。

---

## 📚 现行设计规范

*   **[01-Services领域驱动子包化重构设计与契约规范.md](01-Services领域驱动子包化重构设计与契约规范.md)**  
    核心拓扑架构：记录服务层 7 大领域子包（`invoker/`, `dispatcher/`, `engines/`, `defects/`, `governance/`, `queue/`, `runner/`）划分契约、`EngineContext` / `EngineResult` 纯内存计算模型、以及顶层 `services.go` 门面设计。
*   **[02-扫描引擎与执行流水线职责边界与协同模型深度设计.md](02-扫描引擎与执行流水线职责边界与协同模型深度设计.md)**  
    职责边界与协同协议：“算法推演专家（Engines）”与“生命周期流程管家（Runner）”的正交解耦、6 大标准化流水线执行阶段、断点续跑归位与事务治理防线。
