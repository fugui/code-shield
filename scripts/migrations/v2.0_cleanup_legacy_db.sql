-- =============================================================================
-- Code-Shield v2.0 Release 历史技术债务与兼容表/列清理脚本
-- 适用环境: PostgreSQL 12+ (生产与预发环境)
-- 执行目标: 彻底清除早期开发阶段遗留的历史死表、废弃列与旧索引，对齐 09 号缺陷槽位终态
-- 执行方式: psql -U <user> -d <database> -f v2.0_cleanup_legacy_db.sql
-- 特性说明: 脚本具备完全幂等性 (Idempotent)，支持在事务中安全重复执行
-- =============================================================================

\echo '========================================================================'
\echo '[1/5] 开始执行 Code-Shield v2.0 数据库纯净化升级与历史废弃数据清理...'
\echo '========================================================================'

BEGIN;

-- -----------------------------------------------------------------------------
-- 第一步: 安全前置检查 (Pre-Flight Health Check)
-- 检查是否存在尚未通过新扫描自愈补齐 slot_key 的存量活跃缺陷
-- -----------------------------------------------------------------------------
DO $$
DECLARE
    unhealed_count INTEGER := 0;
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.tables 
        WHERE table_schema = CURRENT_SCHEMA() AND table_name = 'defects'
    ) THEN
        SELECT COUNT(*) INTO unhealed_count
        FROM defects
        WHERE status = 'ACTIVE' AND (slot_key IS NULL OR slot_key = '');

        IF unhealed_count > 0 THEN
            RAISE WARNING '检测到存在 % 条未补齐 slot_key 的存量活跃缺陷！建议在彻底废除旧指纹前确保新扫描已充分完成双轨自愈。', unhealed_count;
        ELSE
            RAISE NOTICE '前置检查通过: 所有存量活跃缺陷已 100%% 具备有效 slot_key。';
        END IF;
    END IF;
END $$;

-- -----------------------------------------------------------------------------
-- 第二步: 物理删除 13 张历史已废弃业务表与过程性临时表
-- (包含早期 KeyIssue 模型及旧版多表对账体系)
-- -----------------------------------------------------------------------------
\echo '[2/5] 物理删除 13 张历史废弃表与过程性临时表...'

DROP TABLE IF EXISTS key_issues CASCADE;
DROP TABLE IF EXISTS defect_fingerprint_records CASCADE;
DROP TABLE IF EXISTS scan_reconciliations CASCADE;
DROP TABLE IF EXISTS reconciliation_links CASCADE;
DROP TABLE IF EXISTS defect_entities CASCADE;
DROP TABLE IF EXISTS defect_entity_aliases CASCADE;
DROP TABLE IF EXISTS defect_entity_relations CASCADE;
DROP TABLE IF EXISTS defect_occurrences CASCADE;
DROP TABLE IF EXISTS report_file_coverages CASCADE;
DROP TABLE IF EXISTS defect_anchor_keys CASCADE;
DROP TABLE IF EXISTS defect_entity_state_versions CASCADE;
DROP TABLE IF EXISTS defect_entity_events CASCADE;
DROP TABLE IF EXISTS defect_assignments CASCADE;

-- -----------------------------------------------------------------------------
-- 第三步: 物理删除 14 个历史废弃列 (消除冗余存储与反序列化污染)
-- -----------------------------------------------------------------------------
\echo '[3/5] 物理删除 14 个历史废弃字段...'

-- 1. analysis_findings 历史列清理 (7 列)
ALTER TABLE analysis_findings 
    DROP COLUMN IF EXISTS fingerprint,
    DROP COLUMN IF EXISTS diff_status,
    DROP COLUMN IF EXISTS anchor_state,
    DROP COLUMN IF EXISTS assignee_id,
    DROP COLUMN IF EXISTS assignee,
    DROP COLUMN IF EXISTS feedback,
    DROP COLUMN IF EXISTS feedback_at;

-- 2. task_reports 历史列清理 (3 列)
ALTER TABLE task_reports 
    DROP COLUMN IF EXISTS new_defects_count,
    DROP COLUMN IF EXISTS existed_defects_count,
    DROP COLUMN IF EXISTS resolved_defects_count;

-- 3. defects 历史几何指纹列清理 (4 列，对齐 09 号设计 Phase 3)
ALTER TABLE defects 
    DROP COLUMN IF EXISTS occurrence_index,
    DROP COLUMN IF EXISTS stmt_shape,
    DROP COLUMN IF EXISTS prev_shape,
    DROP COLUMN IF EXISTS next_shape;

-- -----------------------------------------------------------------------------
-- 第四步: 废除历史索引，确立基于 ACTIVE 状态 SlotKey 的强唯一约束与加速索引
-- -----------------------------------------------------------------------------
\echo '[4/5] 规范缺陷台账索引体系 (对齐槽位对账终态)...'

-- 废除旧版复合唯一约束
DROP INDEX IF EXISTS uq_defects_current_identity;

-- 确立基于 ACTIVE 状态精准槽位的条件强唯一索引 (杜绝重复创建)
CREATE UNIQUE INDEX IF NOT EXISTS uq_defect_active_slot_idx
ON defects (repo_id, task_type_id, slot_key)
WHERE status = 'ACTIVE';

-- 确立一级主槽位单实例秒配加速索引
CREATE INDEX IF NOT EXISTS idx_defect_core_slot_key
ON defects (repo_id, task_type_id, core_slot_key)
WHERE status = 'ACTIVE';

-- 确立二级精准槽位检索索引
CREATE INDEX IF NOT EXISTS idx_defect_slot_key
ON defects (repo_id, task_type_id, slot_key)
WHERE status = 'ACTIVE';

-- -----------------------------------------------------------------------------
-- 第五步: 历史存量数据分类与治理模式状态对齐 (DML)
-- -----------------------------------------------------------------------------
\echo '[5/5] 对齐历史 Finding 分类状态与专项任务治理模式...'

UPDATE analysis_findings 
SET category_status = 'LEGACY' 
WHERE category_status IS NULL OR category_status = '';

UPDATE task_types 
SET governance_mode = 'full_ledger' 
WHERE is_campaign = true 
  AND (governance_mode IS NULL OR governance_mode = '' OR governance_mode = 'defect_tracking');

COMMIT;

\echo '========================================================================'
\echo 'Code-Shield v2.0 数据库历史数据纯净化执行完毕！'
\echo '数据库已达到零过渡死表、零废弃列、槽位索引强化的终态标准。'
\echo '========================================================================'
