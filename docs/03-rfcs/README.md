# 📜 03-rfcs: 架构决策记录与演进提案 (ADRs / RFCs)

本目录收录 Code-Shield 系统的**重大架构决策记录 (Architecture Decision Records, ADR)** 与 **重大技术演进提案 (Request for Comments, RFC)**。

---

## 🎯 RFC / ADR 机制与生命周期

任何对系统拓扑、核心算法、数据模型或协议接口产生破坏性或深远影响的变更，均须通过 RFC 进行同行评审：

```text
Draft (草案撰写) ──▶ In-Review (同行评审) ──▶ Accepted (评审通过/排期) ──▶ Implemented (研发交付/沉淀规范)
                                          └──▶ Rejected (驳回废弃)
```

---

## 📚 提案与决策归档列表

*   **[RFC-001-多租户隔离方案评估与分库架构选型.md](RFC-001-多租户隔离方案评估与分库架构选型.md)**  
    `Status: Implemented` | 核心决策：在“共享数据库共享 Schema (租户字段隔离)”、“共享数据库独立 Schema”与“独立数据库 (物理分库)”三类方案中，基于金融级数据隔离与故障爆炸半径评估，选定基于 GORM 动态路由的**物理分库方案**。
*   **[RFC-002-AI执行引擎与企业治理配置架构优化设计.md](RFC-002-AI执行引擎与企业治理配置架构优化设计.md)**  
    `Status: Implemented` | 核心决策：针对轻量扫描场景，论证了引入 Native HTTP 直连驱动替代重型 CLI 包装器的技术必要性，确立动静分离的双引擎模式。
*   **[RFC-003-租户自定义任务类型与动态规则扩展设计.md](RFC-003-租户自定义任务类型与动态规则扩展设计.md)**  
    `Status: Accepted` | 演进提案：探讨支持多租户自定义 Prompt 模版、私有规则白名单与动态插件执行器的安全隔离与沙箱执行方案。
