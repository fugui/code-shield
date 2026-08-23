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
      <div style={{ textAlign: 'center', padding: '3rem', color: 'var(--text-secondary, #64748b)' }}>
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
      <div
        className="report-kpi-grid"
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(210px, 1fr))',
          gap: '1.25rem',
          marginBottom: '1.75rem',
        }}
      >
        <div
          className="report-kpi-card"
          style={{
            background: '#ffffff',
            backgroundColor: '#ffffff',
            border: '1px solid #e2e8f0',
            borderRadius: '12px',
            padding: '1.25rem 1.5rem',
            display: 'flex',
            flexDirection: 'column',
            gap: '0.45rem',
            boxShadow: '0 1px 3px rgba(0, 0, 0, 0.04)',
            boxSizing: 'border-box',
          }}
        >
          <span className="kpi-title" style={{ fontSize: '0.78rem', fontWeight: 600, color: '#64748b', textTransform: 'uppercase' }}>
            ⏳ 任务总耗时
          </span>
          <span className="kpi-number" style={{ fontSize: '1.65rem', fontWeight: 700, color: '#0f172a' }}>
            {formatDuration(diagnostics?.total_duration || meta?.duration_seconds)}
          </span>
        </div>

        <div
          className="report-kpi-card"
          style={{
            background: '#ffffff',
            backgroundColor: '#ffffff',
            border: '1px solid #e2e8f0',
            borderRadius: '12px',
            padding: '1.25rem 1.5rem',
            display: 'flex',
            flexDirection: 'column',
            gap: '0.45rem',
            boxShadow: '0 1px 3px rgba(0, 0, 0, 0.04)',
            boxSizing: 'border-box',
          }}
        >
          <span className="kpi-title" style={{ fontSize: '0.78rem', fontWeight: 600, color: '#64748b', textTransform: 'uppercase' }}>
            🎯 静态分析耗时
          </span>
          <span className="kpi-number" style={{ fontSize: '1.65rem', fontWeight: 700, color: '#0f172a' }}>
            {formatDuration(diagnostics?.analysis_duration)}
          </span>
        </div>

        <div
          className="report-kpi-card"
          style={{
            background: '#ffffff',
            backgroundColor: '#ffffff',
            border: '1px solid #e2e8f0',
            borderRadius: '12px',
            padding: '1.25rem 1.5rem',
            display: 'flex',
            flexDirection: 'column',
            gap: '0.45rem',
            boxShadow: '0 1px 3px rgba(0, 0, 0, 0.04)',
            boxSizing: 'border-box',
          }}
        >
          <span className="kpi-title" style={{ fontSize: '0.78rem', fontWeight: 600, color: '#64748b', textTransform: 'uppercase' }}>
            🧩 执行引擎模式
          </span>
          <span className="kpi-number" style={{ fontSize: '1.25rem', fontWeight: 700, color: '#2563eb' }}>
            {isSingleEngine ? '单仓全量分析 (Single)' : `分片并发分析 (${diagnostics?.chunks?.length || 0} 片)`}
          </span>
        </div>
      </div>

      {/* 1. 流水线阶段时序流 (现代化流程节点与卡片设计) */}
      {(() => {
        const steps = (diagnostics?.pipeline_steps && diagnostics.pipeline_steps.length > 0)
          ? diagnostics.pipeline_steps
          : [
              { name: '代码静态分析', status: 'success', duration_seconds: diagnostics?.analysis_duration || 0 },
              { name: '综合报告生成', status: 'success', duration_seconds: 0 },
            ];

        return (
          <div
            style={{
              background: '#ffffff',
              backgroundColor: '#ffffff',
              border: '1px solid #e2e8f0',
              borderRadius: '14px',
              padding: '1.5rem 1.85rem',
              marginBottom: '1.75rem',
              boxShadow: '0 1px 4px rgba(0, 0, 0, 0.04)',
              boxSizing: 'border-box',
            }}
          >
            {/* 卡片头部 */}
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.35rem' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                <span style={{ fontSize: '1.1rem' }}>🏃</span>
                <span style={{ fontSize: '0.98rem', fontWeight: 700, color: '#0f172a' }}>流水线阶段时序流</span>
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                <span
                  style={{
                    background: isFailed ? '#fef2f2' : '#f0fdf4',
                    color: isFailed ? '#dc2626' : '#16a34a',
                    border: `1px solid ${isFailed ? '#fecaca' : '#bbf7d0'}`,
                    padding: '0.25rem 0.75rem',
                    borderRadius: '9999px',
                    fontSize: '0.78rem',
                    fontWeight: 600,
                  }}
                >
                  {isFailed ? '执行中断' : `全部阶段完成 (${steps.length}/${steps.length})`}
                </span>
              </div>
            </div>

            {/* 流程节点横向网格 */}
            <div
              style={{
                display: 'grid',
                gridTemplateColumns: `repeat(${steps.length}, 1fr)`,
                gap: '1rem',
                alignItems: 'stretch',
              }}
            >
              {steps.map((step, idx) => {
                const isStepFailed = step.status === 'failed';
                const isStepRunning = step.status === 'running';

                return (
                  <div
                    key={idx}
                    style={{
                      background: isStepFailed ? '#fff1f2' : '#f8fafc',
                      border: `1.5px solid ${isStepFailed ? '#fecdd3' : '#e2e8f0'}`,
                      borderRadius: '12px',
                      padding: '1.15rem 1.25rem',
                      display: 'flex',
                      flexDirection: 'column',
                      justifyContent: 'space-between',
                      position: 'relative',
                      boxShadow: '0 1px 2px rgba(0, 0, 0, 0.02)',
                      boxSizing: 'border-box',
                      minHeight: '92px',
                    }}
                  >
                    {/* 节点序号与状态徽章 */}
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.65rem' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '0.45rem' }}>
                        <div
                          style={{
                            width: '20px',
                            height: '20px',
                            borderRadius: '50%',
                            background: isStepFailed ? '#ef4444' : isStepRunning ? '#3b82f6' : '#10b981',
                            color: '#ffffff',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            fontSize: '0.7rem',
                            fontWeight: 700,
                            boxShadow: isStepFailed ? '0 0 0 3px rgba(239, 68, 68, 0.15)' : '0 0 0 3px rgba(16, 185, 129, 0.15)',
                          }}
                        >
                          {isStepFailed ? '✕' : isStepRunning ? '●' : '✓'}
                        </div>
                        <span style={{ fontSize: '0.72rem', fontWeight: 700, color: '#64748b', letterSpacing: '0.04em' }}>
                          STAGE {String(idx + 1).padStart(2, '0')}
                        </span>
                      </div>

                      {/* 耗时胶囊 */}
                      <span
                        style={{
                          background: '#ffffff',
                          border: '1px solid #cbd5e1',
                          padding: '0.15rem 0.5rem',
                          borderRadius: '6px',
                          fontSize: '0.75rem',
                          fontFamily: '"JetBrains Mono", monospace',
                          color: '#334155',
                          fontWeight: 600,
                        }}
                      >
                        ⏱️ {formatDuration(step.duration_seconds)}
                      </span>
                    </div>

                    {/* 阶段名称 */}
                    <div style={{ fontSize: '0.92rem', fontWeight: 700, color: '#0f172a', lineHeight: 1.4 }}>
                      {step.name}
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        );
      })()}

      {/* 2. 分片执行矩阵 */}
      {!isSingleEngine && diagnostics?.chunks && diagnostics.chunks.length > 0 && (
        <div
          style={{
            background: '#ffffff',
            backgroundColor: '#ffffff',
            border: '1px solid #e2e8f0',
            borderRadius: '14px',
            padding: '1.5rem 1.85rem',
            marginBottom: '1.75rem',
            boxShadow: '0 1px 4px rgba(0, 0, 0, 0.04)',
            boxSizing: 'border-box',
          }}
        >
          {/* 矩阵标题栏 */}
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.25rem' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <span style={{ fontSize: '1.1rem' }}>📦</span>
              <span style={{ fontSize: '0.98rem', fontWeight: 700, color: '#0f172a' }}>
                分片执行矩阵
              </span>
            </div>
            <span
              style={{
                background: 'rgba(59, 130, 246, 0.08)',
                color: '#2563eb',
                border: '1px solid rgba(59, 130, 246, 0.25)',
                padding: '0.2rem 0.65rem',
                borderRadius: '9999px',
                fontSize: '0.78rem',
                fontWeight: 600,
              }}
            >
              共 {diagnostics.chunks.length} 个并发分片
            </span>
          </div>

          {/* 分片列表 */}
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.85rem' }}>
            {diagnostics.chunks.map((chunk) => {
              const isChunkFailed = chunk.status === 'failed';
              const expanded = !!expandedChunks[chunk.chunk_name];

              return (
                <div
                  key={chunk.chunk_name}
                  style={{
                    background: '#f8fafc',
                    border: `1px solid ${isChunkFailed ? '#fecdd3' : '#e2e8f0'}`,
                    borderRadius: '10px',
                    padding: '0.95rem 1.25rem',
                    boxSizing: 'border-box',
                    transition: 'all 0.15s ease-in-out',
                  }}
                >
                  <div
                    style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', cursor: 'pointer', userSelect: 'none' }}
                    onClick={() => toggleChunk(chunk.chunk_name)}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: '0.65rem' }}>
                      <span
                        style={{
                          width: '18px',
                          height: '18px',
                          borderRadius: '50%',
                          background: isChunkFailed ? '#ef4444' : '#10b981',
                          color: '#ffffff',
                          display: 'inline-flex',
                          alignItems: 'center',
                          justifyContent: 'center',
                          fontSize: '0.65rem',
                          fontWeight: 700,
                        }}
                      >
                        {isChunkFailed ? '✕' : '✓'}
                      </span>
                      <span style={{ fontWeight: 700, fontSize: '0.92rem', color: '#0f172a' }}>{chunk.chunk_name}</span>
                      {chunk.attempts > 1 && (
                        <span style={{ fontSize: '0.72rem', background: 'rgba(239, 68, 68, 0.1)', color: '#dc2626', padding: '0.12rem 0.45rem', borderRadius: '4px', fontWeight: 600 }}>
                          重试 {chunk.attempts} 次
                        </span>
                      )}
                    </div>

                    <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', fontSize: '0.82rem', color: '#64748b' }}>
                      <span style={{ background: '#ffffff', border: '1px solid #cbd5e1', padding: '0.2rem 0.55rem', borderRadius: '6px', fontFamily: 'monospace' }}>
                        ⏱️ {formatDuration(chunk.duration_seconds)}
                      </span>
                      <span style={{ background: '#ffffff', border: '1px solid #cbd5e1', padding: '0.2rem 0.55rem', borderRadius: '6px' }}>
                        📂 {chunk.files_count} 个文件
                      </span>
                      <span style={{ color: '#2563eb', fontWeight: 600, fontSize: '0.8rem' }}>
                        {expanded ? '收起 ▲' : '详情 ▼'}
                      </span>
                    </div>
                  </div>

                  {expanded && (
                    <div style={{ marginTop: '0.85rem', paddingTop: '0.85rem', borderTop: '1px solid #e2e8f0' }}>
                      {chunk.error_message && (
                        <div style={{ background: '#fff1f2', color: '#be123c', padding: '0.65rem 0.85rem', borderRadius: '6px', fontSize: '0.78rem', marginBottom: '0.65rem', fontFamily: 'monospace' }}>
                          错误信息: {chunk.error_message}
                        </div>
                      )}
                      {chunk.files && chunk.files.length > 0 && (
                        <div>
                          <div style={{ fontSize: '0.78rem', fontWeight: 600, color: '#475569', marginBottom: '0.35rem' }}>
                            该分片包含的文件清单 ({chunk.files.length} 个):
                          </div>
                          <div style={{ maxHeight: '150px', overflowY: 'auto', background: '#ffffff', border: '1px solid #e2e8f0', padding: '0.65rem', borderRadius: '6px', fontSize: '0.78rem', fontFamily: 'monospace' }}>
                            {chunk.files.map((f, i) => (
                              <div key={i} style={{ color: '#334155', padding: '0.15rem 0' }}>• {f}</div>
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
        <div
          style={{
            background: '#ffffff',
            backgroundColor: '#ffffff',
            border: '1px solid #e2e8f0',
            borderRadius: '14px',
            padding: '1.5rem 1.85rem',
            marginBottom: '1.75rem',
            boxShadow: '0 1px 4px rgba(0, 0, 0, 0.04)',
            boxSizing: 'border-box',
          }}
        >
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.95rem' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <span style={{ fontSize: '1.1rem' }}>📜</span>
              <span style={{ fontSize: '0.98rem', fontWeight: 700, color: '#0f172a' }}>
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
              background: '#0f172a',
              color: '#e2e8f0',
              padding: '1.25rem 1.5rem',
              borderRadius: '10px',
              fontFamily: '"JetBrains Mono", Consolas, monospace',
              fontSize: '0.8rem',
              lineHeight: 1.6,
              overflowX: 'auto',
              boxShadow: 'inset 0 1px 3px rgba(0,0,0,0.3)',
            }}
          >
            <pre style={{ margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-all', fontFamily: 'inherit' }}>
              {diagnostics.raw_output_log}
            </pre>
          </div>
          {diagnostics.total_log_lines > 20 && (
            <button
              onClick={() => setLogExpanded(!logExpanded)}
              style={{
                background: 'none',
                border: 'none',
                color: '#2563eb',
                fontSize: '0.8rem',
                cursor: 'pointer',
                padding: '0.4rem 0',
                fontWeight: 600,
                marginTop: '0.35rem',
                display: 'inline-flex',
                alignItems: 'center',
                gap: '0.25rem',
              }}
              className="no-print"
            >
              {logExpanded ? '▲ 收起日志' : '▼ 展开全部日志窗口'}
            </button>
          )}
        </div>
      )}
    </div>
  );
}
