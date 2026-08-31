import React, { useState } from 'react';
import { TaskDiagnostics, TaskReportMeta } from '../../types/report';
import { formatDuration, copyToClipboardWithFallback } from '../../utils/reportUtils';
import { useToast } from '../Toast';
import ReportEmptyState from './ReportEmptyState';

interface ReportDiagnosticsTabProps {
  meta?: TaskReportMeta;
  diagnostics: TaskDiagnostics | null;
  loading: boolean;
  onResume?: () => void;
}

export default function ReportDiagnosticsTab({
  meta,
  diagnostics,
  loading,
  onResume,
}: ReportDiagnosticsTabProps) {
  const { showToast } = useToast();
  const [expandedChunks, setExpandedChunks] = useState<Record<string, boolean>>({});
  const [logExpanded, setLogExpanded] = useState(false);

  const toggleChunk = (name: string) => {
    setExpandedChunks(prev => ({ ...prev, [name]: !prev[name] }));
  };

  const handleCopyLog = async () => {
    if (!diagnostics?.raw_output_log) return;
    const ok = await copyToClipboardWithFallback(diagnostics.raw_output_log);
    if (ok) {
      showToast('已复制执行输出日志到剪贴板', 'success');
    } else {
      showToast('复制日志失败', 'error');
    }
  };

  if (loading && !diagnostics) {
    return (
      <div className="report-loading">
        ⏳ 正在获取运行轨迹与诊断数据...
      </div>
    );
  }

  const isFailed = meta?.status === 'failed' || diagnostics?.error_message;
  const isSingleEngine = meta?.engine_mode === 'single';

  return (
    <div>
      {/* 失败状态警告横幅 */}
      {isFailed && (
        <ReportEmptyState
          type="failed"
          title="任务执行异常中断"
          description="该任务在执行过程中遭遇错误未能全部完成，以下为异常诊断信息："
          errorMessage={diagnostics?.error_message}
          onResume={onResume}
        />
      )}

      {/* KPI 卡片区 */}
      <div className="report-kpi-grid">
        <div className="report-kpi-card">
          <span className="kpi-title">
            ⏳ 任务总耗时
          </span>
          <span className="kpi-number">
            {formatDuration(diagnostics?.total_duration || meta?.duration_seconds)}
          </span>
        </div>

        <div className="report-kpi-card">
          <span className="kpi-title">
            🎯 静态分析耗时
          </span>
          <span className="kpi-number">
            {formatDuration(diagnostics?.analysis_duration)}
          </span>
        </div>

        <div className="report-kpi-card">
          <span className="kpi-title">
            🧩 执行引擎模式
          </span>
          <span className="kpi-number" style={{ fontSize: '1.15rem', color: '#2563eb' }}>
            {meta?.engine_mode === 'debate_full'
              ? `🤖 全量对抗辩论 (${diagnostics?.chunks?.length || meta?.total_chunks || 0} 个语义分片)`
              : meta?.engine_mode === 'debate_selective'
              ? `⚖️ 选择性辩论 (${diagnostics?.chunks?.length || meta?.total_chunks || 0} 个语义分片)`
              : meta?.engine_mode === 'chunked_fast'
              ? `⚡ 语义分片快扫 (${diagnostics?.chunks?.length || meta?.total_chunks || 0} 片)`
              : isSingleEngine
              ? '📄 单仓全量分析 (Single)'
              : `📦 经典分片分析 (${diagnostics?.chunks?.length || meta?.total_chunks || 0} 片)`}
          </span>
        </div>
      </div>

      {/* 1. 流水线阶段时序流 (现代化流程节点与卡片设计) */}
      {(() => {
        const steps = diagnostics?.pipeline_steps || [];

        return (
          <div className="diagnostics-panel">
            {/* 卡片头部 */}
            <div className="panel-header-row" style={{ marginBottom: steps.length > 0 ? '1.35rem' : 0 }}>
              <div className="panel-title-row">
                <span className="panel-emoji">🏃</span>
                <span className="panel-title">流水线阶段时序流</span>
              </div>
              {steps.length > 0 && (
                <span
                  className="status-pill pill-lg"
                  style={{
                    '--pill-bg': isFailed ? '#fef2f2' : '#f0fdf4',
                    '--pill-color': isFailed ? '#dc2626' : '#16a34a',
                    '--pill-border': isFailed ? '#fecaca' : '#bbf7d0',
                  } as React.CSSProperties}
                >
                  {isFailed ? '执行中断' : `全部阶段完成 (${steps.length}/${steps.length})`}
                </span>
              )}
            </div>

            {steps.length > 0 ? (
              /* 流程节点横向网格 */
              <div className="step-grid" style={{ gridTemplateColumns: `repeat(${steps.length}, 1fr)` }}>
                {steps.map((step, idx) => {
                  const isStepFailed = step.status === 'failed';
                  const isStepRunning = step.status === 'running';

                  return (
                    <div
                      key={idx}
                      className="step-node"
                      style={
                        isStepFailed
                          ? { '--step-bg': '#fff1f2', '--step-border': '#fecdd3' } as React.CSSProperties
                          : undefined
                      }
                    >
                      {/* 节点序号与状态徽章 */}
                      <div className="step-node-header">
                        <div style={{ display: 'flex', alignItems: 'center', gap: '0.45rem' }}>
                          <div
                            className="step-badge"
                            style={
                              isStepFailed
                                ? { '--badge-bg': '#ef4444', '--badge-ring': 'rgba(239, 68, 68, 0.15)' } as React.CSSProperties
                                : isStepRunning
                                  ? { '--badge-bg': '#3b82f6' } as React.CSSProperties
                                  : undefined
                            }
                          >
                            {isStepFailed ? '✕' : isStepRunning ? '●' : '✓'}
                          </div>
                          <span className="step-label">
                            STAGE {String(idx + 1).padStart(2, '0')}
                          </span>
                        </div>

                        {/* 耗时胶囊 */}
                        <span className="duration-pill">
                          ⏱️ {formatDuration(step.duration_seconds)}
                        </span>
                      </div>

                      {/* 阶段名称 */}
                      <div className="step-name">
                        {step.name}
                      </div>
                    </div>
                  );
                })}
              </div>
            ) : (
              <div style={{ fontSize: '0.85rem', color: '#64748b', padding: '0.5rem 0' }}>
                该任务未记录流水线阶段时序数据。
              </div>
            )}
          </div>
        );
      })()}

      {/* 2. 分片执行矩阵 */}
      {!isSingleEngine && diagnostics?.chunks && diagnostics.chunks.length > 0 && (
        <div className="diagnostics-panel">
          {/* 矩阵标题栏 */}
          <div className="panel-header-row" style={{ marginBottom: '1.25rem' }}>
            <div className="panel-title-row">
              <span className="panel-emoji">📦</span>
              <span className="panel-title">分片执行矩阵</span>
            </div>
            <span
              className="status-pill pill-sm"
              style={{
                '--pill-bg': 'rgba(59, 130, 246, 0.08)',
                '--pill-color': '#2563eb',
                '--pill-border': 'rgba(59, 130, 246, 0.25)',
              } as React.CSSProperties}
            >
              共 {diagnostics.chunks.length} 个并发分片
            </span>
          </div>

          {/* 分片列表 */}
          <div className="chunk-list">
            {diagnostics.chunks.map((chunk) => {
              const isChunkFailed = chunk.status === 'failed';
              const expanded = !!expandedChunks[chunk.chunk_name];

              return (
                <div
                  key={chunk.chunk_name}
                  className="chunk-row"
                  style={{ '--chunk-border': isChunkFailed ? '#fecdd3' : '#e2e8f0' } as React.CSSProperties}
                >
                  <div
                    className="chunk-row-header"
                    onClick={() => toggleChunk(chunk.chunk_name)}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.65rem' }}>
                      <span
                        className="chunk-badge"
                        style={isChunkFailed ? { '--chunk-badge-bg': '#ef4444' } as React.CSSProperties : undefined}
                      >
                        {isChunkFailed ? '✕' : '✓'}
                      </span>
                      <span className="chunk-name">{chunk.chunk_name}</span>
                      {chunk.attempts > 1 && (
                        <span className="retry-badge">
                          重试 {chunk.attempts} 次
                        </span>
                      )}
                    </div>

                    <div className="chunk-meta">
                      <span className="meta-pill mono">
                        ⏱️ {formatDuration(chunk.duration_seconds)}
                      </span>
                      <span className="meta-pill">
                        📂 {chunk.files_count} 个文件
                      </span>
                      <span className="toggle-hint">
                        {expanded ? '收起 ▲' : '详情 ▼'}
                      </span>
                    </div>
                  </div>

                  {expanded && (
                    <div className="chunk-expanded">
                      {chunk.error_message && (
                        <div className="chunk-error">
                          错误信息: {chunk.error_message}
                        </div>
                      )}
                      {chunk.files && chunk.files.length > 0 && (
                        <div>
                          <div className="file-list-label">
                            该分片包含的文件清单 ({chunk.files.length} 个):
                          </div>
                          <div className="file-list-box">
                            {chunk.files.map((f, i) => (
                              <div key={i} className="file-line">• {f}</div>
                            ))}
                          </div>
                        </div>
                      )}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      )}

      {/* 3. 执行输出日志查看器 */}
      {diagnostics?.raw_output_log && (
        <div className="diagnostics-panel">
          <div className="panel-header-row" style={{ marginBottom: '0.95rem' }}>
            <div className="panel-title-row">
              <span className="panel-emoji">📜</span>
              <span className="panel-title">
                终端执行输出日志 {diagnostics.log_truncated ? `(展示最新 200 行，共 ${diagnostics.total_log_lines} 行)` : ''}
              </span>
            </div>
            <button
              className="nav-btn no-print"
              onClick={handleCopyLog}
              style={{ padding: '0.35rem 0.85rem', fontSize: '0.82rem' }}
            >
              📋 复制日志
            </button>
          </div>

          <div
            className="code-snippet-box"
            style={{
              maxHeight: logExpanded ? 'none' : '260px',
              fontSize: '0.8rem',
              lineHeight: 1.6,
            }}
          >
            <pre style={{ margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-all', fontFamily: 'inherit' }}>
              {diagnostics.raw_output_log}
            </pre>
          </div>
          {diagnostics.total_log_lines > 20 && (
            <button
              onClick={() => setLogExpanded(!logExpanded)}
              className="expand-toggle-btn no-print"
            >
              {logExpanded ? '▲ 收起日志' : '▼ 展开全部日志窗口'}
            </button>
          )}
        </div>
      )}
    </div>
  );
}
