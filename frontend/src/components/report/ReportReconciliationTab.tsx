import React, { useState, useMemo } from 'react';
import { ScanReconciliationInfo, TaskReportMeta } from '../../types/report';
import { apiUrl } from '../../config';

interface ReportReconciliationTabProps {
  meta?: TaskReportMeta;
  reconciliation: ScanReconciliationInfo | null;
  loading: boolean;
}

export default function ReportReconciliationTab({
  meta,
  reconciliation,
  loading,
}: ReportReconciliationTabProps) {
  const [selectedRelation, setSelectedRelation] = useState<string>('ALL');
  const [keyword, setKeyword] = useState<string>('');

  const links = useMemo(() => reconciliation?.reconciliation_links || [], [reconciliation]);
  const funnelStats = reconciliation?.funnel_stats || {};

  const filteredLinks = useMemo(() => {
    return links.filter(link => {
      if (selectedRelation !== 'ALL' && link.relation !== selectedRelation) {
        return false;
      }
      if (keyword.trim()) {
        const kw = keyword.trim().toLowerCase();
        const base = (link.base_record_id || '').toLowerCase();
        const curr = (link.curr_finding_id || '').toLowerCase();
        const rule = (link.match_rule || '').toLowerCase();
        const rationale = (link.rationale || '').toLowerCase();
        if (!base.includes(kw) && !curr.includes(kw) && !rule.includes(kw) && !rationale.includes(kw)) {
          return false;
        }
      }
      return true;
    });
  }, [links, selectedRelation, keyword]);

  if (loading && !reconciliation) {
    return (
      <div className="report-loading" style={{ textAlign: 'center', padding: '3rem', color: 'var(--color-text-muted, #64748b)' }}>
        ⏳ 正在加载跨轮对账与增量治理数据...
      </div>
    );
  }

  if (!reconciliation) {
    return (
      <div className="report-loading" style={{ textAlign: 'center', padding: '3rem', color: 'var(--color-text-muted, #64748b)' }}>
        ℹ️ 本次任务未检索到跨轮对账记录（单次独立扫描或尚未执行对账流程）
      </div>
    );
  }

  const modeDisplay = reconciliation.governance_mode === 'change_focus'
    ? '变更增量焦点模式 (Change Focus)'
    : '全量台账对账模式 (Full Ledger)';

  const modeBg = reconciliation.governance_mode === 'change_focus'
    ? 'rgba(16, 185, 129, 0.12)'
    : 'rgba(59, 130, 246, 0.12)';
  const modeColor = reconciliation.governance_mode === 'change_focus'
    ? 'var(--color-success, #10b981)'
    : 'var(--color-primary, #3b82f6)';

  // 漏斗关键阶梯定义
  const funnelSteps = [
    { key: 'R1_STRONG_PHYSICAL_FP', name: 'R1 强物理指纹', desc: '纳秒级哈希精确匹配' },
    { key: 'R2_DETERMINISTIC_FEATURE', name: 'R2 确定性特征几何', desc: '规则+文件+作用域锚定' },
    { key: 'R3_GEOMETRIC_SEMANTIC', name: 'R3 几何平移与语义', desc: 'L2 语义指纹 + 15行容差' },
    { key: 'R4_MULTI_VIEW_MERGE', name: 'R4 多视角诊断合并', desc: '同位置多切片聚合' },
    { key: 'R5_RESIDUAL_ALIGNMENT', name: 'R5 单文件残差对齐', desc: '重构重排相似度对齐' },
    { key: 'R6_TEMPLATE_FAMILY', name: 'R6 跨文件模板族', desc: '同源结构聚簇标记' },
  ];

  const totalMatched = reconciliation.matched_count || 1;

  // 导出对账文件链接
  const handleExport = (format: 'excel' | 'json') => {
    if (!meta?.id) return;
    const url = apiUrl(`/api/tasks/${meta.id}/report/export?scope=reconcile&format=${format}`);
    window.open(url, '_blank');
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1.25rem' }}>
      {/* 顶部对账概览卡片 */}
      <div
        style={{
          background: 'var(--color-bg-surface, #ffffff)',
          border: '1px solid var(--color-border-primary, #e2e8f0)',
          borderRadius: '8px',
          padding: '1.25rem 1.5rem',
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          flexWrap: 'wrap',
          gap: '1rem',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap' }}>
          <span
            style={{
              padding: '0.35rem 0.75rem',
              borderRadius: '6px',
              backgroundColor: modeBg,
              color: modeColor,
              fontWeight: 700,
              fontSize: '0.88rem',
              border: `1px solid ${modeColor}40`,
            }}
          >
            {modeDisplay}
          </span>
          <span style={{ fontSize: '0.85rem', color: 'var(--color-text-secondary, #64748b)' }}>
            当前任务: <strong>#{reconciliation.task_report_id}</strong>
            {reconciliation.baseline_report_id > 0 && (
              <>
                {' '}↔ 对比基线报告: <strong>#{reconciliation.baseline_report_id}</strong>
              </>
            )}
          </span>
        </div>

        {/* 导出操作栏 */}
        <div style={{ display: 'flex', gap: '0.65rem' }}>
          <button
            className="btn btn-secondary"
            onClick={() => handleExport('json')}
            style={{
              fontSize: '0.8rem',
              padding: '0.4rem 0.8rem',
              borderRadius: '6px',
              border: '1px solid var(--color-border-primary, #e2e8f0)',
              background: 'var(--color-bg-muted, #f1f5f9)',
              color: 'var(--color-text-secondary, #475569)',
              cursor: 'pointer',
            }}
          >
            📥 导出对账 JSON
          </button>
          <button
            className="btn btn-primary"
            onClick={() => handleExport('excel')}
            style={{
              fontSize: '0.8rem',
              padding: '0.4rem 0.8rem',
              borderRadius: '6px',
              background: 'var(--color-primary, #2563eb)',
              color: 'var(--color-text-white, #ffffff)',
              border: 'none',
              cursor: 'pointer',
              fontWeight: 600,
            }}
          >
            📊 导出对账 Excel
          </button>
        </div>
      </div>

      {/* 核心指标 KPI 栅格 */}
      <div className="report-kpi-grid">
        <div className="report-kpi-card">
          <span className="kpi-title">🔍 本次检出</span>
          <span className="kpi-number" style={{ color: 'var(--color-primary, #2563eb)' }}>
            {reconciliation.total_current} <span style={{ fontSize: '0.85rem', fontWeight: 500, color: 'var(--color-text-muted, #64748b)' }}>条</span>
          </span>
        </div>

        <div className="report-kpi-card">
          <span className="kpi-title">🆕 本次新增 (NEW)</span>
          <span className="kpi-number" style={{ color: 'var(--color-danger, #ef4444)' }}>
            {reconciliation.new_count} <span style={{ fontSize: '0.85rem', fontWeight: 500, color: 'var(--color-text-muted, #64748b)' }}>条</span>
          </span>
        </div>

        <div className="report-kpi-card">
          <span className="kpi-title">📦 历史存量 (EXISTED)</span>
          <span className="kpi-number" style={{ color: 'var(--color-text-secondary, #475569)' }}>
            {reconciliation.existed_count} <span style={{ fontSize: '0.85rem', fontWeight: 500, color: 'var(--color-text-muted, #64748b)' }}>条</span>
          </span>
        </div>

        <div className="report-kpi-card">
          <span className="kpi-title">✅ 修复核销 (RESOLVED)</span>
          <span className="kpi-number" style={{ color: 'var(--color-success, #10b981)' }}>
            {reconciliation.resolved_count} <span style={{ fontSize: '0.85rem', fontWeight: 500, color: 'var(--color-text-muted, #64748b)' }}>条</span>
          </span>
        </div>

        <div className="report-kpi-card">
          <span className="kpi-title">🔄 漏扫回补 (GAP_FILLED)</span>
          <span className="kpi-number" style={{ color: '#0891b2' }}>
            {reconciliation.gap_filled_count} <span style={{ fontSize: '0.85rem', fontWeight: 500, color: 'var(--color-text-muted, #64748b)' }}>条</span>
          </span>
        </div>

        <div className="report-kpi-card">
          <span className="kpi-title">❄️ 退火归档 (ARCHIVED)</span>
          <span className="kpi-number" style={{ color: 'var(--color-text-muted, #64748b)' }}>
            {reconciliation.archived_count} <span style={{ fontSize: '0.85rem', fontWeight: 500, color: 'var(--color-text-muted, #64748b)' }}>条</span>
          </span>
        </div>

        <div className="report-kpi-card">
          <span className="kpi-title">👥 模板族聚类</span>
          <span className="kpi-number" style={{ color: '#6366f1' }}>
            {reconciliation.family_count} <span style={{ fontSize: '0.85rem', fontWeight: 500, color: 'var(--color-text-muted, #64748b)' }}>族</span>
          </span>
        </div>

        <div className="report-kpi-card">
          <span className="kpi-title">👁️ 多视角聚合</span>
          <span className="kpi-number" style={{ color: '#a855f7' }}>
            {reconciliation.multi_view_count} <span style={{ fontSize: '0.85rem', fontWeight: 500, color: 'var(--color-text-muted, #64748b)' }}>处</span>
          </span>
        </div>
      </div>

      {/* 六级高精几何漏斗统计卡片 */}
      <div
        style={{
          background: 'var(--color-bg-surface, #ffffff)',
          border: '1px solid var(--color-border-primary, #e2e8f0)',
          borderRadius: '8px',
          padding: '1.25rem 1.5rem',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '1rem' }}>
          <h4 style={{ margin: 0, fontSize: '0.98rem', fontWeight: 700, color: 'var(--color-text-primary, #0f172a)' }}>
            🌪️ 六级几何漏斗匹配吞吐分析 (Funnel Stages)
          </h4>
          <span style={{ fontSize: '0.8rem', color: 'var(--color-text-muted, #64748b)' }}>
            累计自动认领成功: <strong>{reconciliation.matched_count}</strong> 条
          </span>
        </div>

        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '0.85rem' }}>
          {funnelSteps.map(step => {
            const count = funnelStats[step.key] || 0;
            const pct = Math.min(100, Math.round((count / totalMatched) * 100));
            return (
              <div
                key={step.key}
                style={{
                  background: 'var(--color-bg-muted, #f8fafc)',
                  border: '1px solid var(--color-border-primary, #e2e8f0)',
                  borderRadius: '6px',
                  padding: '0.85rem 1rem',
                  display: 'flex',
                  flexDirection: 'column',
                  gap: '0.4rem',
                }}
              >
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <span style={{ fontWeight: 600, fontSize: '0.85rem', color: 'var(--color-text-primary, #1e293b)' }}>
                    {step.name}
                  </span>
                  <span style={{ fontWeight: 700, fontSize: '0.92rem', color: 'var(--color-primary, #2563eb)' }}>
                    {count} 条
                  </span>
                </div>
                <div style={{ width: '100%', height: '6px', background: 'var(--color-border-primary, #e2e8f0)', borderRadius: '999px', overflow: 'hidden' }}>
                  <div
                    style={{
                      width: `${pct}%`,
                      height: '100%',
                      background: 'linear-gradient(90deg, var(--color-primary, #2563eb), #60a5fa)',
                      borderRadius: '999px',
                    }}
                  />
                </div>
                <span style={{ fontSize: '0.75rem', color: 'var(--color-text-muted, #64748b)' }}>
                  {step.desc} ({pct}%)
                </span>
              </div>
            );
          })}
        </div>
      </div>

      {/* 认领关系图谱表格 */}
      <div
        style={{
          background: 'var(--color-bg-surface, #ffffff)',
          border: '1px solid var(--color-border-primary, #e2e8f0)',
          borderRadius: '8px',
          padding: '1.25rem 1.5rem',
        }}
      >
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem', flexWrap: 'wrap', gap: '0.75rem' }}>
          <h4 style={{ margin: 0, fontSize: '0.98rem', fontWeight: 700, color: 'var(--color-text-primary, #0f172a)' }}>
            🔗 问题对账认领图谱清单 ({filteredLinks.length}/{links.length})
          </h4>

          {/* 筛选工具栏 */}
          <div style={{ display: 'flex', gap: '0.65rem', alignItems: 'center', flexWrap: 'wrap' }}>
            <input
              type="text"
              placeholder="搜索条目 UID / 规则 / 依据..."
              value={keyword}
              onChange={e => setKeyword(e.target.value)}
              style={{
                fontSize: '0.8rem',
                padding: '0.35rem 0.65rem',
                borderRadius: '6px',
                border: '1px solid var(--color-border-primary, #e2e8f0)',
                background: 'var(--color-bg-input, #ffffff)',
                color: 'var(--color-text-primary, #0f172a)',
                outline: 'none',
              }}
            />

            <select
              value={selectedRelation}
              onChange={e => setSelectedRelation(e.target.value)}
              style={{
                fontSize: '0.8rem',
                padding: '0.35rem 0.65rem',
                borderRadius: '6px',
                border: '1px solid var(--color-border-primary, #e2e8f0)',
                background: 'var(--color-bg-input, #ffffff)',
                color: 'var(--color-text-primary, #0f172a)',
                outline: 'none',
              }}
            >
              <option value="ALL">全部拓扑关系</option>
              <option value="SAME">SAME (相同继承)</option>
              <option value="SAME_MULTI_VIEW">SAME_MULTI_VIEW (多视角合并)</option>
              <option value="RESOLVED">RESOLVED (修复核销)</option>
              <option value="GAP_FILLED">GAP_FILLED (漏扫回补)</option>
              <option value="REGRESSED">REGRESSED (冷池复发)</option>
            </select>
          </div>
        </div>

        {/* 数据表格 */}
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: '0.83rem' }}>
            <thead>
              <tr style={{ borderBottom: '1px solid var(--color-border-primary, #e2e8f0)', background: 'var(--color-bg-muted, #f8fafc)' }}>
                <th style={{ padding: '0.65rem 0.75rem', textAlign: 'left', color: 'var(--color-text-secondary, #475569)' }}>基线条目 UID</th>
                <th style={{ padding: '0.65rem 0.75rem', textAlign: 'left', color: 'var(--color-text-secondary, #475569)' }}>当前条目 UID</th>
                <th style={{ padding: '0.65rem 0.75rem', textAlign: 'left', color: 'var(--color-text-secondary, #475569)' }}>匹配漏斗规则</th>
                <th style={{ padding: '0.65rem 0.75rem', textAlign: 'left', color: 'var(--color-text-secondary, #475569)' }}>拓扑关系</th>
                <th style={{ padding: '0.65rem 0.75rem', textAlign: 'center', color: 'var(--color-text-secondary, #475569)' }}>置信度</th>
                <th style={{ padding: '0.65rem 0.75rem', textAlign: 'left', color: 'var(--color-text-secondary, #475569)' }}>严重度区间</th>
                <th style={{ padding: '0.65rem 0.75rem', textAlign: 'left', color: 'var(--color-text-secondary, #475569)' }}>判定依据 / 备注</th>
              </tr>
            </thead>
            <tbody>
              {filteredLinks.length === 0 ? (
                <tr>
                  <td colSpan={7} style={{ padding: '2rem', textAlign: 'center', color: 'var(--color-text-muted, #94a3b8)' }}>
                    暂无符合条件的认领图谱记录
                  </td>
                </tr>
              ) : (
                filteredLinks.map(link => {
                  const isMultiView = link.relation === 'SAME_MULTI_VIEW';
                  return (
                    <tr
                      key={link.id || `${link.base_record_id}-${link.curr_finding_id}`}
                      style={{
                        borderBottom: '1px solid var(--color-border-primary, #e2e8f0)',
                        background: isMultiView ? 'rgba(168, 85, 247, 0.04)' : 'transparent',
                      }}
                    >
                      <td style={{ padding: '0.65rem 0.75rem', fontFamily: 'monospace', color: 'var(--color-text-secondary, #334155)' }}>
                        {link.base_record_id || '-'}
                      </td>
                      <td style={{ padding: '0.65rem 0.75rem', fontFamily: 'monospace', color: 'var(--color-primary, #2563eb)', fontWeight: 600 }}>
                        {link.curr_finding_id || '-'}
                      </td>
                      <td style={{ padding: '0.65rem 0.75rem' }}>
                        <span
                          style={{
                            fontSize: '0.72rem',
                            padding: '0.15rem 0.45rem',
                            borderRadius: '4px',
                            background: 'var(--color-bg-muted, #f1f5f9)',
                            color: 'var(--color-text-secondary, #475569)',
                            border: '1px solid var(--color-border-primary, #e2e8f0)',
                          }}
                        >
                          {link.match_rule}
                        </span>
                      </td>
                      <td style={{ padding: '0.65rem 0.75rem' }}>
                        <span
                          style={{
                            fontSize: '0.72rem',
                            padding: '0.15rem 0.45rem',
                            borderRadius: '4px',
                            fontWeight: 600,
                            background: link.relation === 'SAME'
                              ? 'rgba(59, 130, 246, 0.1)'
                              : link.relation === 'SAME_MULTI_VIEW'
                              ? 'rgba(168, 85, 247, 0.12)'
                              : link.relation === 'RESOLVED'
                              ? 'rgba(16, 185, 129, 0.1)'
                              : 'rgba(100, 116, 139, 0.1)',
                            color: link.relation === 'SAME'
                              ? 'var(--color-primary, #2563eb)'
                              : link.relation === 'SAME_MULTI_VIEW'
                              ? '#9333ea'
                              : link.relation === 'RESOLVED'
                              ? 'var(--color-success, #16a34a)'
                              : 'var(--color-text-secondary, #475569)',
                          }}
                        >
                          {link.relation}
                        </span>
                      </td>
                      <td style={{ padding: '0.65rem 0.75rem', textAlign: 'center', fontWeight: 600 }}>
                        <span style={{ color: link.confidence >= 0.9 ? 'var(--color-success, #16a34a)' : '#d97706' }}>
                          {Math.round(link.confidence * 100)}%
                        </span>
                      </td>
                      <td style={{ padding: '0.65rem 0.75rem', color: 'var(--color-text-muted, #64748b)' }}>
                        {link.severity_range || '-'}
                      </td>
                      <td style={{ padding: '0.65rem 0.75rem', color: 'var(--color-text-secondary, #475569)' }}>
                        {link.rationale || '-'}
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
