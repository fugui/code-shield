import React from 'react';
import { DefectDetailData, ReconciliationEvent } from '../../types/report';

interface DefectDetailPanelProps {
  defect: DefectDetailData;
  events?: ReconciliationEvent[];
  compact?: boolean;
  hidePath?: boolean;
}

const eventLabels: Record<string, string> = {
  CREATED: '缺陷创建',
  STATUS_CHANGED: '状态变化',
  REOPENED: '修复后复发',
  RESOLVED: '判定修复',
  IDENTITY_MOVED: '身份迁移',
  HUMAN_DECISION: '人工决策',
  RENAME_ALIAS: '重命名别名',
};

const statusLabels: Record<string, string> = {
  ACTIVE: '未命中',
  COVERAGE_GAP: '覆盖缺口',
  VERIFIED_PENDING: '待复核',
  DORMANT: '休眠',
};

function formatLocation(defect: DefectDetailData, hidePath = false) {
  const path = defect.norm_path || defect.file_path || '';
  if (!defect.line_start) return hidePath ? '行号未加载' : path || '行号未加载';
  if (defect.line_end && defect.line_end !== defect.line_start) {
    return hidePath ? `行 ${defect.line_start}-${defect.line_end}` : `${path}:${defect.line_start}-${defect.line_end}`;
  }
  return hidePath ? `行 ${defect.line_start}` : `${path}:${defect.line_start}`;
}

export default function DefectDetailPanel({ defect, events = [], compact = false, hidePath = false }: DefectDetailPanelProps) {
  const title = defect.title || `缺陷 #${defect.id}`;
  const location = formatLocation(defect, hidePath);
  const status = defect.status_label || statusLabels[defect.status || ''] || defect.status;

  return (
    <div className={`defect-detail-panel ${compact ? 'compact' : ''}`}>
      <div className="defect-detail-header">
        <strong>{title}</strong>
        <span>#{defect.id} · {location}</span>
      </div>

      <div className="defect-detail-meta">
        {defect.severity && <span>{defect.severity}</span>}
        {status && <span>{status}</span>}
        {defect.category && <span>{defect.category}</span>}
        {typeof defect.missed_count === 'number' && <span>未命中 {defect.missed_count} 次</span>}
        {typeof defect.dormant_rounds === 'number' && defect.dormant_rounds > 0 && <span>休眠 {defect.dormant_rounds} 轮</span>}
        {typeof defect.last_matched_report_id === 'number' && defect.last_matched_report_id > 0 && (
          <span>最后命中 #{defect.last_matched_report_id}</span>
        )}
      </div>

      {defect.status_reason && (
        <p className="defect-detail-reason">{defect.status_reason}</p>
      )}

      {defect.detail && (
        <p className="defect-detail-description">{defect.detail}</p>
      )}

      {defect.code_snippet && (
        <pre className="code-snippet-box">{defect.code_snippet}</pre>
      )}

      {defect.suggestion && (
        <div className="suggestion-box">{defect.suggestion}</div>
      )}

      {events.length > 0 && (
        <div className="defect-detail-events">
          <h5>生命周期事件</h5>
          <div className="event-timeline">
            {events.map(event => (
              <div key={event.id} className="event-row">
                <span className="event-dot" />
                <div>
                  <strong>{eventLabels[event.event_type] || event.event_type}</strong>
                  <span>
                    {event.from_status || '—'} → {event.to_status || '—'} · {event.actor_type} · {new Date(event.created_at).toLocaleString()}
                  </span>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
