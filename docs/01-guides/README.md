# 🛠️ 01-guides: 使用与运维指南 (User & Ops Guides)

本目录收录面向 **Code-Shield 最终使用者、接入方团队以及系统运维人员** 的核心操作与配置指南。

---

## 📚 文档目录

*   **[01-智能扫描引擎与任务配置使用手册.md](01-智能扫描引擎与任务配置使用手册.md)**  
    系统核心操作宝典：涵盖 5 大执行模式（`debate_full`、`debate_selective`、`chunked_fast`、`chunked`、`single`）的选型决策树、`engine_config` 差异化配置模版库、Tier 模型分层调度参数以及常见问题解答（FAQ）。
*   **[02-任务类型领域语义与受控分类配置指南.md](02-任务类型领域语义与受控分类配置指南.md)**  
    任务类型管理深度指南：详解领域语义（`domain_label`、`target_semantics`、`display_semantics`）与受控分类白名单（Categories SSOT）的设计机理、LLM 契约约束、增量指纹协同与四大典型场景配置模板。

---

## 🎯 规划中的扩展指南 (Roadmap)

*   `03-系统配置项全景参考规范.md`：`config.yaml` 各字段类型、默认值、生产环境推荐配置及热加载规则。
*   `04-私有化部署与日常运维排障指南.md`：多容器编排、数据库迁移、探活监控与常见故障排查流水账。
