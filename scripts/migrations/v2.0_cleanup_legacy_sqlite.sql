-- =============================================================================
-- Code-Shield v2.0 Release 历史技术债务与兼容表/列清理脚本
-- 适用环境: SQLite 3.35+ (本地开发机、单机私有化及轻量测试环境，如 shield.db)
-- 执行目标: 清除本地 SQLite 数据库中历史死表、废弃列与旧索引
-- 执行方式: sqlite3 shield.db < scripts/migrations/v2.0_cleanup_legacy_sqlite.sql
-- =============================================================================

BEGIN TRANSACTION;

-- 1. 物理删除 13 张历史已废弃表
DROP TABLE IF EXISTS key_issues;
DROP TABLE IF EXISTS defect_fingerprint_records;
DROP TABLE IF EXISTS scan_reconciliations;
DROP TABLE IF EXISTS reconciliation_links;
DROP TABLE IF EXISTS defect_entities;
DROP TABLE IF EXISTS defect_entity_aliases;
DROP TABLE IF EXISTS defect_entity_relations;
DROP TABLE IF EXISTS defect_occurrences;
DROP TABLE IF EXISTS report_file_coverages;
DROP TABLE IF EXISTS defect_anchor_keys;
DROP TABLE IF EXISTS defect_entity_state_versions;
DROP TABLE IF EXISTS defect_entity_events;
DROP TABLE IF EXISTS defect_assignments;

-- 2. 物理删除 14 个历史废弃列 (SQLite 3.35+ 支持单列逐一 DROP COLUMN)
-- analysis_findings 历史列清理 (7 列)
ALTER TABLE analysis_findings DROP COLUMN fingerprint;
ALTER TABLE analysis_findings DROP COLUMN diff_status;
ALTER TABLE analysis_findings DROP COLUMN anchor_state;
ALTER TABLE analysis_findings DROP COLUMN assignee_id;
ALTER TABLE analysis_findings DROP COLUMN assignee;
ALTER TABLE analysis_findings DROP COLUMN feedback;
ALTER TABLE analysis_findings DROP COLUMN feedback_at;

-- task_reports 历史列清理 (3 列)
ALTER TABLE task_reports DROP COLUMN new_defects_count;
ALTER TABLE task_reports DROP COLUMN existed_defects_count;
ALTER TABLE task_reports DROP COLUMN resolved_defects_count;

-- defects 历史几何指纹列清理 (4 列)
ALTER TABLE defects DROP COLUMN occurrence_index;
ALTER TABLE defects DROP COLUMN stmt_shape;
ALTER TABLE defects DROP COLUMN prev_shape;
ALTER TABLE defects DROP COLUMN next_shape;

-- 3. 确立槽位对账终态索引
CREATE UNIQUE INDEX IF NOT EXISTS uq_defect_active_slot_sqlite
ON defects (repo_id, task_type_id, slot_key)
WHERE status = 'ACTIVE';

CREATE INDEX IF NOT EXISTS idx_defect_core_slot_sqlite
ON defects (repo_id, task_type_id, core_slot_key, status);

CREATE INDEX IF NOT EXISTS idx_defect_slot_lookup_sqlite
ON defects (repo_id, task_type_id, slot_key, status);

-- 4. 历史存量数据分类与治理模式对齐
UPDATE analysis_findings 
SET category_status = 'LEGACY' 
WHERE category_status IS NULL OR category_status = '';

UPDATE task_types 
SET governance_mode = 'full_ledger' 
WHERE is_campaign = 1 
  AND (governance_mode IS NULL OR governance_mode = '' OR governance_mode = 'defect_tracking');

COMMIT;

-- 整理数据库碎片
VACUUM;
