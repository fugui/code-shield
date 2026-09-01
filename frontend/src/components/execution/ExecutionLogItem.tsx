import React from 'react';
import { PipelineStepper } from './PipelineStepper';
import { sshToHttps } from '../../utils/urlUtils';

export interface ExecutionLogItemProps {
  log: any;
  expanded: boolean;
  selected: boolean;
  canCancel: boolean;
  summary: any;
  loadingSummary: boolean;
  onToggleExpand: (id: number) => void;
  onToggleSelect: (id: number) => void;
  onDeletePending: (logId: number, isRunning: boolean) => void;
  onNotify: (reportId: number) => void;
  onOpenReport: (reportId: number) => void;
}

export const formatDuration = (seconds: number | null | undefined): string => {
  if (seconds == null) return '-';
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
  const isRunning = ['running', 'cloning', 'pre_processing', 'analyzing', 'post_processing', 'merging'].includes(log.status);
  const isPending = log.status === 'pending' || log.status === 'queued';

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
        <td style={{ padding: '1rem', color: 'var(--color-text-secondary, #64748b)' }}>{calcDuration(log.start_time, log.end_time)}</td>
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
            <PipelineStepper log={log} report={report} />
            <div style={{ display: 'flex', gap: '1.5rem', flexWrap: 'wrap' }}>
              {/* Left Panel: Score, Summary & Buttons */}
              <div style={{ flex: '1.2', minWidth: '320px', display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
                {/* Score */}
                {report.status === 'success' && (
                  <div style={{ display: 'flex', gap: '1rem', flexWrap: 'wrap' }}>
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
                      fontSize: '0.875rem',
                      lineHeight: 1.6,
                    }}
                  >
                    <div style={{ fontWeight: 600, marginBottom: '0.2rem', fontSize: '0.8rem', color: 'var(--color-primary, #2563eb)' }}>
                      🤖 AI 审计摘要
                    </div>
                    {report.ai_summary}
                  </div>
                )}

                {/* Action buttons */}
                {report.status === 'success' && (
                  <div style={{ display: 'flex', gap: '0.5rem', marginTop: '0.25rem' }}>
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
                )}
              </div>

              {/* Right Panel: Diagnostics Snapshot */}
              <div
                style={{
                  flex: '1',
                  minWidth: '300px',
                  background: 'var(--color-bg-surface, #ffffff)',
                  border: '1px solid var(--color-border-primary, #e2e8f0)',
                  borderRadius: '8px',
                  padding: '1rem',
                  display: 'flex',
                  flexDirection: 'column',
                  gap: '0.5rem',
                  boxShadow: 'var(--shadow-sm, 0 1px 2px rgba(0,0,0,0.02))',
                }}
              >
                <h4
                  style={{
                    margin: 0,
                    fontSize: '0.85rem',
                    color: 'var(--color-text-primary, #0f172a)',
                    display: 'flex',
                    alignItems: 'center',
                    gap: '0.35rem',
                    borderBottom: '1px solid var(--color-border-primary, #e2e8f0)',
                    paddingBottom: '0.5rem',
                    fontWeight: 600,
                  }}
                >
                  🔬 运行轨迹与诊断快照
                </h4>
                {loadingSummary ? (
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
                    正在获取运行轨迹...
                  </div>
                ) : summary ? (
                  (() => {
                    const s = summary;
                    const chunks = s.chunks || s.analysis?.chunks || [];
                    const failedChunk = chunks.find((c: any) => c.status === 'failed');
                    const analysisDur = s.analysis_duration ?? s.analysis?.duration_seconds ?? 0;
                    const totalDur = s.total_duration ?? s.duration_seconds ?? 0;
                    const successChunks = chunks.filter((c: any) => c.status === 'success').length;
                    return (
                      <div style={{ fontSize: '0.8rem', display: 'flex', flexDirection: 'column', gap: '0.5rem', color: 'var(--color-text-secondary, #475569)' }}>
                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                          <span>⏱️ 静态分析耗时:</span>
                          <strong style={{ color: 'var(--color-text-primary, #0f172a)' }}>{formatDuration(analysisDur)}</strong>
                        </div>
                        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
                          <span>⌛ 任务总体耗时:</span>
                          <strong style={{ color: 'var(--color-text-primary, #0f172a)' }}>{formatDuration(totalDur)}</strong>
                        </div>
                        {chunks.length > 0 && (
                          <div>
                            <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '0.35rem' }}>
                              <span>🧩 分片扫描进度:</span>
                              <strong>
                                {successChunks} / {chunks.length} 成功
                              </strong>
                            </div>

                            {/* Mini Chunk Color Grid */}
                            <div
                              style={{
                                display: 'flex',
                                flexWrap: 'wrap',
                                gap: '4px',
                                background: 'var(--color-bg-muted, #f8fafc)',
                                padding: '0.5rem',
                                borderRadius: '6px',
                                border: '1px solid var(--color-border-primary, #e2e8f0)',
                              }}
                            >
                              {chunks.map((c: any, idx: number) => {
                                const isChunkFailed = c.status === 'failed';
                                return (
                                  <div
                                    key={c.chunk_name || idx}
                                    className="code-chunk-dot"
                                    style={{
                                      width: '14px',
                                      height: '14px',
                                      borderRadius: '3px',
                                      background: isChunkFailed ? 'var(--color-danger, #ef4444)' : 'var(--color-success, #10b981)',
                                      border: `1px solid ${isChunkFailed ? '#dc2626' : '#059669'}`,
                                      cursor: 'pointer',
                                    }}
                                    title={`${c.chunk_name} (耗时: ${formatDuration(c.duration_seconds)}, 状态: ${c.status === 'success' ? '成功' : '失败'})`}
                                  />
                                );
                              })}
                            </div>
                          </div>
                        )}

                        {/* Error diagnosis box */}
                        {failedChunk && (
                          <div
                            style={{
                              marginTop: '0.25rem',
                              background: 'rgba(239, 68, 68, 0.12)',
                              color: 'var(--color-danger, #991b1b)',
                              border: '1px solid rgba(239, 68, 68, 0.3)',
                              padding: '0.6rem 0.75rem',
                              borderRadius: '6px',
                              fontSize: '0.75rem',
                              lineHeight: '1.4',
                            }}
                          >
                            <div style={{ fontWeight: 600, display: 'flex', alignItems: 'center', gap: '0.25rem', marginBottom: '0.2rem' }}>
                              <span>🚨 故障分片:</span>
                              <span style={{ fontFamily: 'monospace' }}>{failedChunk.chunk_name}</span>
                            </div>
                            <div
                              style={{
                                fontFamily: 'monospace',
                                wordBreak: 'break-all',
                                maxHeight: '60px',
                                overflowY: 'auto',
                                background: 'rgba(255,255,255,0.5)',
                                padding: '0.3rem',
                                borderRadius: '4px',
                              }}
                            >
                              {failedChunk.error_message}
                            </div>
                          </div>
                        )}
                      </div>
                    );
                  })()
                ) : (['chunked', 'chunked_fast', 'debate_full', 'debate_selective'].includes(log.engine_mode || '') || (report?.total_chunks ?? 0) > 0) && (isRunning || isPending) ? (
                  <div style={{ fontSize: '0.8rem', display: 'flex', flexDirection: 'column', gap: '0.75rem', color: 'var(--color-text-secondary, #475569)', padding: '0.5rem 0' }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                      <span style={{ fontWeight: 500, color: 'var(--color-text-secondary, #475569)', display: 'flex', alignItems: 'center', gap: '0.25rem' }}>
                        🧩 分片扫描进度
                      </span>
                      {report.total_chunks > 0 ? (
                        <strong style={{ color: 'var(--color-primary, #2563eb)', fontSize: '0.875rem' }}>
                          {report.processed_chunks} / {report.total_chunks} 已处理
                        </strong>
                      ) : (
                        <span style={{ color: 'var(--color-text-muted, #94a3b8)', fontStyle: 'italic' }}>准备中...</span>
                      )}
                    </div>

                    <div style={{ width: '100%', height: '8px', background: 'var(--color-bg-muted, #f1f5f9)', borderRadius: '999px', overflow: 'hidden', border: '1px solid var(--color-border-primary, #e2e8f0)' }}>
                      <div
                        style={{
                          width: report.total_chunks > 0 ? `${Math.min(100, (report.processed_chunks / report.total_chunks) * 100)}%` : '0%',
                          height: '100%',
                          background: 'linear-gradient(90deg, var(--color-primary, #2563eb) 0%, #60a5fa 100%)',
                          borderRadius: '999px',
                          transition: 'width 0.8s cubic-bezier(0.4, 0, 0.2, 1)',
                        }}
                      />
                    </div>

                    <div style={{ display: 'flex', justifyContent: 'space-between', color: 'var(--color-text-muted, #64748b)', fontSize: '0.75rem', marginTop: '0.25rem' }}>
                      <span>
                        {report.total_chunks > 0 ? '正在执行分片并发扫描...' : '正在初始化代码仓或分析范围...'}
                      </span>
                      <span>
                        {report.total_chunks > 0 && `${Math.round((report.processed_chunks / report.total_chunks) * 100)}%`}
                      </span>
                    </div>
                  </div>
                ) : (
                  <div style={{ color: 'var(--color-text-muted, #94a3b8)', fontSize: '0.75rem', fontStyle: 'italic', textAlign: 'center', padding: '1rem 0' }}>
                    无分片诊断数据 (单次任务模式)
                  </div>
                )}
              </div>
            </div>
          </td>
        </tr>
      )}
    </>
  );
};
