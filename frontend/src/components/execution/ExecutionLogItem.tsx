import React, { useState, useEffect, useRef } from 'react';
import { PipelineStepper } from './PipelineStepper';
import { sshToHttps } from '../../utils/urlUtils';

export interface ExecutionChunkDetail {
  chunk_name: string;
  status: string;
  duration_seconds?: number;
  retries?: number;
  files?: string[];
  error_message?: string;
}

export interface ExecutionActivityEvent {
  timestamp?: string;
  stage?: string;
  level?: string;
  message?: string;
}

export interface ExecutionDiagnosticsModel {
  is_running?: boolean;
  total_duration?: number;
  analysis_duration?: number;
  estimated_remaining?: number;
  phase_timers?: Record<string, {
    name?: string;
    duration_seconds?: number;
    status?: string;
  }>;
  recent_events?: ExecutionActivityEvent[];
  chunks?: ExecutionChunkDetail[];
}

export interface ExecutionLogItemProps {
  log: any;
  expanded: boolean;
  selected: boolean;
  canCancel: boolean;
  summary?: ExecutionDiagnosticsModel | null;
  loadingSummary: boolean;
  onToggleExpand: (id: number) => void;
  onToggleSelect: (id: number) => void;
  onDeletePending: (logId: number, isRunning: boolean) => void;
  onNotify: (reportId: number) => void;
  onOpenReport: (reportId: number) => void;
}

export const formatDuration = (seconds: number | null | undefined): string => {
  if (seconds == null || isNaN(seconds)) return '-';
  const s = Math.round(seconds);
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
};

export const formatDate = (dateStr: string | null | undefined): string => {
  if (!dateStr) return '-';
  const d = new Date(dateStr);
  return isNaN(d.getTime()) ? '-' : d.toLocaleString();
};

export const calcDuration = (startStr: string | null | undefined, endStr: string | null | undefined): string => {
  if (!startStr || !endStr) return '-';
  const diff = Math.floor((new Date(endStr).getTime() - new Date(startStr).getTime()) / 1000);
  if (isNaN(diff)) return '-';
  return diff < 60 ? `${diff}s` : `${Math.floor(diff / 60)}m ${diff % 60}s`;
};

export const useLiveTimer = (startStr: string | null | undefined, isRunning: boolean) => {
  const [now, setNow] = useState<number>(() => Date.now());

  useEffect(() => {
    if (!isRunning || !startStr) return;
    const interval = setInterval(() => {
      setNow(Date.now());
    }, 1000);
    return () => clearInterval(interval);
  }, [isRunning, startStr]);

  if (!startStr) return { durationText: '-', elapsedSeconds: 0 };
  const startTime = new Date(startStr).getTime();
  if (isNaN(startTime)) return { durationText: '-', elapsedSeconds: 0 };

  const diffSeconds = Math.max(0, Math.floor((now - startTime) / 1000));
  return {
    durationText: formatDuration(diffSeconds),
    elapsedSeconds: diffSeconds,
  };
};

export const ExecutionLogItem: React.FC<ExecutionLogItemProps> = ({
  log,
  expanded,
  selected,
  canCancel,
  summary,
  loadingSummary,
  onToggleExpand,
  onToggleSelect,
  onDeletePending,
  onNotify,
  onOpenReport,
}) => {
  const report = log.task_report;
  const hasReport = !!report;
  const isRunning = ['running', 'cloning', 'pre_processing', 'analyzing', 'synthesis', 'post_processing', 'merging'].includes(log.status);
  const isPending = log.status === 'pending' || log.status === 'queued';
  const liveTimer = useLiveTimer(log.start_time, isRunning);
  const [copiedLog, setCopiedLog] = useState(false);
  const eventLogRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (expanded && eventLogRef.current) {
      eventLogRef.current.scrollTop = eventLogRef.current.scrollHeight;
    }
  }, [expanded, summary?.recent_events]);

  const handleCopyLogs = (events: any[]) => {
    if (!events || events.length === 0) return;
    const text = events
      .map(e => `[${formatDate(e.timestamp)}] [${e.stage?.toUpperCase() || 'INFO'}] ${e.message}`)
      .join('\n');
    navigator.clipboard.writeText(text).then(() => {
      setCopiedLog(true);
      setTimeout(() => setCopiedLog(false), 2000);
    });
  };

  const renderStatusBadge = () => {
    const activeStatus = report?.status && report.status !== 'queued' && report.status !== 'pending'
      ? report.status
      : log.status;

    const isDebateMode = ['debate_full', 'debate_selective'].includes(log.engine_mode || '');
    const isChunkedMode = ['chunked', 'chunked_fast', 'debate_full', 'debate_selective'].includes(log.engine_mode || '') || (report?.total_chunks ?? 0) > 0;

    const map: Record<string, { cls: string; label: string }> = {
      success: { cls: 'success', label: '执行成功' },
      failed:  { cls: 'danger',  label: '执行失败' },
      running: { cls: 'primary', label: '运行中...' },
      skipped: { cls: 'info',    label: '已跳过' },
      pending: { cls: 'warning', label: '排队中' },
      cloning: { cls: 'primary', label: '代码克隆中...' },
      pre_processing: { cls: 'primary', label: '前置检查中...' },
      analyzing: { cls: 'primary', label: isDebateMode ? '对抗辩论中...' : '分片检视中...' },
      synthesis: { cls: 'primary', label: '全仓报告总结中 (AI 排版生成)...' },
      post_processing: { cls: 'primary', label: '缺陷状态同步中...' },
      merging: { cls: 'primary', label: '问题归并与闭环中...' },
    };
    const s = map[activeStatus] || { cls: 'warning', label: activeStatus };

    if (isChunkedMode && activeStatus === 'analyzing' && report) {
      const { processed_chunks, total_chunks } = report;
      if (total_chunks > 0) {
        if (processed_chunks >= total_chunks) {
          return <span className={`badge ${s.cls}`}>全仓报告总结中 (AI 排版生成)...</span>;
        }
        const actionText = isDebateMode ? '对抗辩论中' : '分片检视中';
        return <span className={`badge ${s.cls}`}>{`${actionText} (${processed_chunks}/${total_chunks})...`}</span>;
      }
    }

    return <span className={`badge ${s.cls}`}>{s.label}</span>;
  };

  const renderEngineBadge = () => {
    switch (log.engine_mode) {
      case 'debate_full':
        return (
          <span style={{ display: 'inline-block', padding: '0.1rem 0.4rem', borderRadius: '4px', background: 'rgba(124, 58, 237, 0.1)', border: '1px solid rgba(124, 58, 237, 0.25)', fontSize: '0.75rem', color: '#7c3aed', fontWeight: 600 }}>
            全量对抗辩论
          </span>
        );
      case 'debate_selective':
        return (
          <span style={{ display: 'inline-block', padding: '0.1rem 0.4rem', borderRadius: '4px', background: 'rgba(37, 99, 235, 0.1)', border: '1px solid rgba(37, 99, 235, 0.25)', fontSize: '0.75rem', color: '#2563eb', fontWeight: 600 }}>
            选择性辩论
          </span>
        );
      case 'chunked_fast':
        return (
          <span style={{ display: 'inline-block', padding: '0.1rem 0.4rem', borderRadius: '4px', background: 'rgba(13, 148, 136, 0.1)', border: '1px solid rgba(13, 148, 136, 0.25)', fontSize: '0.75rem', color: '#0d9488', fontWeight: 600 }}>
            分片快扫
          </span>
        );
      case 'chunked':
        return (
          <span style={{ display: 'inline-block', padding: '0.1rem 0.4rem', borderRadius: '4px', background: 'rgba(168, 85, 247, 0.08)', border: '1px solid rgba(168, 85, 247, 0.2)', fontSize: '0.75rem', color: '#7c3aed' }}>
            分片模式
          </span>
        );
      default:
        return (
          <span style={{ display: 'inline-block', padding: '0.1rem 0.4rem', borderRadius: '4px', background: 'var(--color-bg-muted, #f1f5f9)', border: '1px solid var(--color-border-primary, #e2e8f0)', fontSize: '0.75rem', color: 'var(--color-text-muted, #64748b)' }}>
            单次模式
          </span>
        );
    }
  };

  // 提取阶段耗时分布瀑布
  const timers = summary?.phase_timers || {};
  const prepDur = timers.prepare?.duration_seconds ?? 0;
  const anaDur = timers.analysis?.duration_seconds ?? summary?.analysis_duration ?? 0;
  const calDur = timers.calibration?.duration_seconds ?? 0;
  const synDur = timers.synthesis?.duration_seconds ?? 0;
  const arcDur = timers.archive?.duration_seconds ?? 0;
  const totalReportDuration = summary?.total_duration ?? (isRunning ? liveTimer.elapsedSeconds : 0);
  const maxWaterfall = Math.max(totalReportDuration, prepDur + anaDur + calDur + synDur + arcDur, 1);

  const waterfallStages = [
    { label: '代码拉取与预检', dur: prepDur, color: '#3b82f6' },
    { label: '分片检视与辩论', dur: anaDur, color: '#8b5cf6' },
    { label: '确定性严重度校准', dur: calDur, color: '#06b6d4' },
    { label: 'AI 全仓报告综合', dur: synDur, color: '#f59e0b' },
    { label: '缺陷归并与归档', dur: arcDur, color: '#10b981' },
  ];

  return (
    <>
      <tr
        style={{
          borderBottom: expanded ? 'none' : '1px solid var(--color-border-primary, #e2e8f0)',
          fontSize: '0.875rem',
          cursor: hasReport ? 'pointer' : 'default',
          background: expanded ? 'var(--color-bg-muted, rgba(248, 250, 252, 0.6))' : 'transparent',
        }}
        onClick={() => hasReport && onToggleExpand(log.id)}
      >
        <td style={{ padding: '1rem', textAlign: 'center' }} onClick={e => e.stopPropagation()}>
          {canCancel && (
            <input
              type="checkbox"
              checked={selected}
              onChange={() => onToggleSelect(log.id)}
              style={{ cursor: 'pointer', accentColor: 'var(--color-primary, #2563eb)' }}
            />
          )}
        </td>
        <td style={{ padding: '1rem', color: 'var(--color-text-muted, #94a3b8)', textAlign: 'center' }}>
          {hasReport ? (expanded ? '▼' : '▶') : ''}
        </td>
        <td style={{ padding: '1rem', color: 'var(--color-text-secondary, #64748b)' }}>#{log.id}</td>
        <td style={{ padding: '1rem', fontWeight: 500 }}>
          {log.repo_url ? (
            <a
              href={sshToHttps(log.repo_url)}
              target="_blank"
              rel="noreferrer"
              style={{ color: 'var(--color-primary, #2563eb)', textDecoration: 'none' }}
              onClick={e => e.stopPropagation()}
            >
              {log.repo_name || `Repo ${log.repo_id}`}
            </a>
          ) : (
            log.repo_name || `Repo ${log.repo_id}`
          )}
        </td>
        <td style={{ padding: '1rem' }}>
          <span
            style={{
              display: 'inline-block',
              padding: '0.15rem 0.5rem',
              borderRadius: '4px',
              background: 'rgba(37, 99, 235, 0.08)',
              color: 'var(--color-primary, #2563eb)',
              fontSize: '0.75rem',
              fontWeight: 500,
            }}
          >
            {log.task_type_name || '-'}
          </span>
        </td>
        <td style={{ padding: '1rem' }}>
          <span
            style={{
              textTransform: 'capitalize',
              display: 'inline-block',
              padding: '0.1rem 0.4rem',
              borderRadius: '4px',
              background: 'var(--color-bg-muted, #f1f5f9)',
              border: '1px solid var(--color-border-primary, #e2e8f0)',
              fontSize: '0.75rem',
              color: 'var(--color-text-secondary, #475569)',
            }}
          >
            {log.trigger_type}
          </span>
        </td>
        <td style={{ padding: '1rem' }}>{renderEngineBadge()}</td>
        <td style={{ padding: '1rem', color: 'var(--color-text-secondary, #64748b)' }}>{formatDate(log.start_time)}</td>
        <td style={{ padding: '1rem', color: 'var(--color-text-secondary, #64748b)' }}>
          {isRunning ? (
            <span
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: '0.35rem',
                color: 'var(--color-primary, #2563eb)',
                fontWeight: 600,
                fontVariantNumeric: 'tabular-nums',
              }}
            >
              <span
                style={{
                  width: '6px',
                  height: '6px',
                  borderRadius: '50%',
                  backgroundColor: 'var(--color-primary, #2563eb)',
                  boxShadow: '0 0 6px var(--color-primary, #2563eb)',
                }}
              />
              {liveTimer.durationText}
            </span>
          ) : (
            calcDuration(log.start_time, log.end_time)
          )}
        </td>
        <td style={{ padding: '1rem' }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.25rem' }}>
            {renderStatusBadge()}
            {log.status === 'failed' && log.error_message && (
              <span
                style={{
                  fontSize: '0.75rem',
                  color: 'var(--color-danger, #ef4444)',
                  maxWidth: '200px',
                  whiteSpace: 'nowrap',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                }}
                title={log.error_message}
              >
                {log.error_message}
              </span>
            )}
          </div>
        </td>
        <td style={{ padding: '1rem' }}>
          {canCancel && (
            <button
              className="btn"
              onClick={e => {
                e.stopPropagation();
                onDeletePending(log.id, isRunning);
              }}
              style={{
                background: 'transparent',
                color: 'var(--color-danger, #ef4444)',
                border: '1px solid var(--color-danger, #ef4444)',
                padding: '0.35rem',
                display: 'inline-flex',
                alignItems: 'center',
                justifyContent: 'center',
                borderRadius: '4px',
                cursor: 'pointer',
                transition: 'all 0.2s ease',
              }}
              title={isRunning ? '强杀并删除该运行中的任务' : '删除该排队任务'}
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                <polyline points="3 6 5 6 21 6"></polyline>
                <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
              </svg>
            </button>
          )}
        </td>
      </tr>

      {expanded && hasReport && (
        <tr style={{ borderBottom: '1px solid var(--color-border-primary, #e2e8f0)' }}>
          <td colSpan={11} style={{ padding: '1.25rem 1.5rem 1.5rem 3.5rem', background: 'var(--color-bg-muted, rgba(248, 250, 252, 0.5))' }}>
            <PipelineStepper log={log} report={report} summary={summary} liveSeconds={liveTimer.elapsedSeconds} />
            <div style={{ display: 'flex', gap: '1.25rem', flexWrap: 'wrap' }}>
              {/* Left Panel: Score, Chunk Matrix & Action Buttons */}
              <div style={{ flex: '1.1', minWidth: '340px', display: 'flex', flexDirection: 'column', gap: '0.85rem' }}>
                {/* 评分与摘要（成功后） */}
                {report.status === 'success' && (
                  <div style={{ display: 'flex', gap: '1rem', alignItems: 'center' }}>
                    <div
                      style={{
                        display: 'flex',
                        flexDirection: 'column',
                        alignItems: 'center',
                        background: 'var(--color-bg-surface, #ffffff)',
                        border: '1px solid var(--color-border-primary, #e2e8f0)',
                        borderRadius: '8px',
                        padding: '0.5rem 1rem',
                        minWidth: '80px',
                        boxShadow: 'var(--shadow-sm, 0 1px 2px rgba(0,0,0,0.04))',
                      }}
                    >
                      <span
                        style={{
                          fontSize: '1.25rem',
                          fontWeight: 700,
                          color: report.score >= 20 ? 'var(--color-danger, #ef4444)' : report.score >= 10 ? 'var(--color-warning, #f59e0b)' : 'var(--color-success, #10b981)',
                        }}
                      >
                        {report.score ?? 0}
                      </span>
                      <span style={{ fontSize: '0.7rem', color: 'var(--color-text-muted, #64748b)', marginTop: '0.1rem' }}>风险评分</span>
                    </div>

                    <div style={{ display: 'flex', gap: '0.5rem' }}>
                      <button
                        className="btn"
                        onClick={e => {
                          e.stopPropagation();
                          onNotify(report.id);
                        }}
                        style={{
                          background: 'transparent',
                          color: 'var(--color-primary, #2563eb)',
                          border: '1px solid var(--color-primary, #2563eb)',
                          fontSize: '0.85rem',
                        }}
                      >
                        通知责任人
                      </button>
                      <button
                        className="btn"
                        onClick={e => {
                          e.stopPropagation();
                          onOpenReport(report.id);
                        }}
                        style={{
                          background: 'var(--color-success, #10b981)',
                          borderColor: 'var(--color-success, #10b981)',
                          color: '#ffffff',
                          fontSize: '0.85rem',
                        }}
                      >
                        查看报告
                      </button>
                    </div>
                  </div>
                )}

                {/* AI Summary */}
                {report.ai_summary && (
                  <div
                    style={{
                      padding: '0.75rem 1rem',
                      background: 'rgba(37, 99, 235, 0.06)',
                      borderRadius: '6px',
                      border: '1px solid rgba(37, 99, 235, 0.2)',
                      color: 'var(--color-text-primary, #1e293b)',
                      fontSize: '0.85rem',
                      lineHeight: 1.6,
                    }}
                  >
                    <div style={{ fontWeight: 600, marginBottom: '0.2rem', fontSize: '0.8rem', color: 'var(--color-primary, #2563eb)' }}>
                      🤖 AI 审计摘要
                    </div>
                    {report.ai_summary}
                  </div>
                )}

                {/* 分片检视与对抗辩论透视看板 */}
                <div
                  style={{
                    background: 'var(--color-bg-surface, #ffffff)',
                    border: '1px solid var(--color-border-primary, #e2e8f0)',
                    borderRadius: '8px',
                    padding: '0.85rem 1rem',
                    boxShadow: 'var(--shadow-sm, 0 1px 2px rgba(0,0,0,0.02))',
                  }}
                >
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.5rem' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
                      <span style={{ fontSize: '0.85rem', fontWeight: 600, color: 'var(--color-text-primary, #0f172a)' }}>
                        🧩 分片检视与对抗辩论透视
                      </span>
                      {(summary?.estimated_remaining ?? 0) > 0 && isRunning && (
                        <span style={{ fontSize: '0.72rem', color: 'var(--color-text-muted, #64748b)', background: 'var(--color-bg-muted, #f1f5f9)', padding: '0.1rem 0.4rem', borderRadius: '4px' }}>
                          预计剩余: ~{formatDuration(summary?.estimated_remaining)}
                        </span>
                      )}
                    </div>
                    <span style={{ fontSize: '0.78rem', fontWeight: 600, color: 'var(--color-primary, #2563eb)' }}>
                      {report.processed_chunks || 0} / {report.total_chunks || 0} 完成
                      {report.total_chunks > 0 && ` (${Math.round(((report.processed_chunks || 0) / report.total_chunks) * 100)}%)`}
                    </span>
                  </div>

                  {/* 进度条 */}
                  <div style={{ width: '100%', height: '7px', background: 'var(--color-bg-muted, #f1f5f9)', borderRadius: '999px', overflow: 'hidden', border: '1px solid var(--color-border-primary, #e2e8f0)', marginBottom: '0.65rem' }}>
                    <div
                      style={{
                        width: report.total_chunks > 0 ? `${Math.min(100, ((report.processed_chunks || 0) / report.total_chunks) * 100)}%` : '0%',
                        height: '100%',
                        background: 'linear-gradient(90deg, var(--color-primary, #2563eb) 0%, #60a5fa 100%)',
                        borderRadius: '999px',
                        transition: 'width 0.5s ease',
                      }}
                    />
                  </div>

                  {/* 交互式分片方格矩阵 */}
                  {(() => {
                    const chunks = summary?.chunks || [];
                    const failedChunk = chunks.find((c: any) => c.status === 'failed');

                    if (chunks.length === 0 && (report.total_chunks ?? 0) === 0) {
                      return (
                        <div style={{ fontSize: '0.75rem', color: 'var(--color-text-muted, #94a3b8)', padding: '0.5rem 0', textAlign: 'center' }}>
                          {isRunning ? '正在拉取代码并进行 AST 语法规划拆解...' : '单仓模式或无分片明细'}
                        </div>
                      );
                    }

                    return (
                      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
                        <div
                          style={{
                            display: 'flex',
                            flexWrap: 'wrap',
                            gap: '5px',
                            background: 'var(--color-bg-muted, #f8fafc)',
                            padding: '0.5rem',
                            borderRadius: '6px',
                            border: '1px solid var(--color-border-primary, #e2e8f0)',
                            maxHeight: '120px',
                            overflowY: 'auto',
                          }}
                        >
                          {chunks.map((c: any, idx: number) => {
                            const isChunkFailed = c.status === 'failed';
                            const isChunkSuccess = c.status === 'success';
                            const bg = isChunkFailed
                              ? 'var(--color-danger, #ef4444)'
                              : isChunkSuccess
                              ? 'var(--color-success, #10b981)'
                              : 'var(--color-primary, #2563eb)';

                            return (
                              <div
                                key={c.chunk_name || idx}
                                style={{
                                  width: '16px',
                                  height: '16px',
                                  borderRadius: '3px',
                                  background: bg,
                                  cursor: 'pointer',
                                  transition: 'transform 0.1s ease',
                                }}
                                title={`分片: ${c.chunk_name}\n状态: ${c.status === 'success' ? '成功' : c.status === 'failed' ? '失败' : '处理中'}\n耗时: ${formatDuration(c.duration_seconds)}\n重试: ${c.retries || 0} 次\n文件: ${(c.files || []).join(', ') || '-'}`}
                              />
                            );
                          })}
                        </div>

                        {failedChunk && (
                          <div
                            style={{
                              background: 'rgba(239, 68, 68, 0.1)',
                              color: 'var(--color-danger, #ef4444)',
                              border: '1px solid rgba(239, 68, 68, 0.25)',
                              padding: '0.5rem 0.65rem',
                              borderRadius: '6px',
                              fontSize: '0.75rem',
                              lineHeight: '1.4',
                            }}
                          >
                            <span style={{ fontWeight: 600 }}>🚨 异常分片 [{failedChunk.chunk_name}]: </span>
                            <span style={{ fontFamily: 'monospace' }}>{failedChunk.error_message || '执行异常'}</span>
                          </div>
                        )}
                      </div>
                    );
                  })()}
                </div>
              </div>

              {/* Right Panel: Diagnostics Waterfall & Live Activity Stream */}
              <div
                style={{
                  flex: '1.2',
                  minWidth: '360px',
                  background: 'var(--color-bg-surface, #ffffff)',
                  border: '1px solid var(--color-border-primary, #e2e8f0)',
                  borderRadius: '8px',
                  padding: '1rem',
                  display: 'flex',
                  flexDirection: 'column',
                  gap: '0.75rem',
                  boxShadow: 'var(--shadow-sm, 0 1px 2px rgba(0,0,0,0.02))',
                }}
              >
                {/* 标题与计时 */}
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    borderBottom: '1px solid var(--color-border-primary, #e2e8f0)',
                    paddingBottom: '0.5rem',
                  }}
                >
                  <h4 style={{ margin: 0, fontSize: '0.85rem', color: 'var(--color-text-primary, #0f172a)', display: 'flex', alignItems: 'center', gap: '0.35rem', fontWeight: 600 }}>
                    🔬 运行轨迹与全流程耗时
                  </h4>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.65rem', fontSize: '0.78rem' }}>
                    <span style={{ color: 'var(--color-text-secondary, #64748b)' }}>总耗时:</span>
                    <strong style={{ color: isRunning ? 'var(--color-primary, #2563eb)' : 'var(--color-text-primary, #0f172a)', fontVariantNumeric: 'tabular-nums' }}>
                      {isRunning ? `⚡ ${liveTimer.durationText}` : formatDuration(summary?.total_duration ?? 0)}
                    </strong>
                  </div>
                </div>

                {loadingSummary && !summary ? (
                  <div style={{ padding: '1.5rem', textAlign: 'center', color: 'var(--color-text-muted, #64748b)', fontSize: '0.8rem' }}>
                    <span
                      style={{
                        display: 'inline-block',
                        width: '12px',
                        height: '12px',
                        border: '2px solid rgba(100,116,139,0.3)',
                        borderRadius: '50%',
                        borderTopColor: 'var(--color-primary, #2563eb)',
                        verticalAlign: 'middle',
                        marginRight: '6px',
                      }}
                    />
                    正在同步实时运行轨迹...
                  </div>
                ) : (
                  <>
                    {/* 阶段耗时瀑布分解 */}
                    <div>
                      <div style={{ fontSize: '0.75rem', fontWeight: 600, color: 'var(--color-text-secondary, #64748b)', marginBottom: '0.4rem' }}>
                        📊 各阶段耗时分解
                      </div>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: '0.35rem' }}>
                        {waterfallStages.map((st, i) => {
                          const pct = maxWaterfall > 0 && st.dur > 0 ? Math.min(100, Math.round((st.dur / maxWaterfall) * 100)) : 0;
                          return (
                            <div key={i} style={{ display: 'flex', alignItems: 'center', fontSize: '0.75rem', gap: '0.5rem' }}>
                              <span style={{ width: '110px', color: 'var(--color-text-secondary, #475569)', flexShrink: 0, textOverflow: 'ellipsis', overflow: 'hidden', whiteSpace: 'nowrap' }}>
                                {st.label}
                              </span>
                              <div style={{ flex: 1, height: '6px', background: 'var(--color-bg-muted, #f1f5f9)', borderRadius: '3px', overflow: 'hidden' }}>
                                <div
                                  style={{
                                    width: `${pct}%`,
                                    height: '100%',
                                    background: st.color,
                                    borderRadius: '3px',
                                    transition: 'width 0.4s ease',
                                  }}
                                />
                              </div>
                              <span style={{ width: '48px', textAlign: 'right', color: st.dur > 0 ? 'var(--color-text-primary, #0f172a)' : 'var(--color-text-muted, #94a3b8)', fontVariantNumeric: 'tabular-nums', fontWeight: st.dur > 0 ? 600 : 400 }}>
                                {st.dur > 0 ? formatDuration(st.dur) : '-'}
                              </span>
                            </div>
                          );
                        })}
                      </div>
                    </div>

                    {/* 实时活动日志流水 */}
                    <div>
                      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.35rem' }}>
                        <span style={{ fontSize: '0.75rem', fontWeight: 600, color: 'var(--color-text-secondary, #64748b)' }}>
                          📜 实时流水线活动日志
                        </span>
                        {Boolean(summary?.recent_events && summary.recent_events.length > 0) && (
                          <button
                            type="button"
                            onClick={() => handleCopyLogs(summary?.recent_events || [])}
                            style={{
                              background: 'transparent',
                              border: 'none',
                              color: 'var(--color-primary, #2563eb)',
                              fontSize: '0.7rem',
                              cursor: 'pointer',
                              padding: 0,
                            }}
                          >
                            {copiedLog ? '✓ 已复制' : '复制日志'}
                          </button>
                        )}
                      </div>
                      <div
                        ref={eventLogRef}
                        style={{
                          background: 'var(--color-bg-muted, #f8fafc)',
                          border: '1px solid var(--color-border-primary, #e2e8f0)',
                          borderRadius: '6px',
                          padding: '0.5rem 0.65rem',
                          maxHeight: '140px',
                          overflowY: 'auto',
                          fontFamily: 'monospace',
                          fontSize: '0.72rem',
                          lineHeight: '1.5',
                          display: 'flex',
                          flexDirection: 'column',
                          gap: '0.2rem',
                        }}
                      >
                        {summary?.recent_events && summary.recent_events.length > 0 ? (
                          summary.recent_events.map((ev: any, idx: number) => {
                            const timeStr = ev.timestamp ? new Date(ev.timestamp).toLocaleTimeString() : '';
                            const isErr = ev.level === 'error';
                            const isSucc = ev.level === 'success';
                            const color = isErr ? 'var(--color-danger, #ef4444)' : isSucc ? 'var(--color-success, #10b981)' : 'var(--color-text-secondary, #334155)';

                            return (
                              <div key={idx} style={{ color, wordBreak: 'break-all' }}>
                                <span style={{ color: 'var(--color-text-muted, #94a3b8)', marginRight: '6px' }}>[{timeStr}]</span>
                                <span style={{ fontWeight: 600, marginRight: '6px' }}>[{ev.stage?.toUpperCase() || 'INFO'}]</span>
                                <span>{ev.message}</span>
                              </div>
                            );
                          })
                        ) : (
                          <div style={{ color: 'var(--color-text-muted, #94a3b8)', fontStyle: 'italic', textAlign: 'center', padding: '0.5rem 0' }}>
                            {isRunning ? '任务执行中，等待新事件派发...' : '暂无详细事件流记录'}
                          </div>
                        )}
                      </div>
                    </div>
                  </>
                )}
              </div>
            </div>
          </td>
        </tr>
      )}
    </>
  );
};
