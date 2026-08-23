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

      {/* KPI 卡片 */}
      <div className="report-kpi-grid">
        <div className="report-kpi-card">
          <span className="kpi-title">⏳ 任务总耗时</span>
          <span className="kpi-number">{formatDuration(diagnostics?.total_duration || meta?.duration_seconds)}</span>
        </div>
        <div className="report-kpi-card">
          <span className="kpi-title">🎯 静态分析耗时</span>
          <span className="kpi-number">{formatDuration(diagnostics?.analysis_duration)}</span>
        </div>
        <div className="report-kpi-card">
          <span className="kpi-title">🧩 执行引擎模式</span>
          <span className="kpi-number" style={{ fontSize: '1.1rem' }}>
            {isSingleEngine ? '单仓分析 (Single)' : `分片并发 (${diagnostics?.chunks?.length || 0} 片)`}
          </span>
        </div>
      </div>

      {/* 1. 时序阶段流 */}
      <h4 style={{ margin: '1.25rem 0 0.5rem 0', fontSize: '0.9rem', color: 'var(--text-color, #0f172a)' }}>
        🏃 流水线阶段时序流
      </h4>
      <div className="pipeline-step-flow">
        {(diagnostics?.pipeline_steps && diagnostics.pipeline_steps.length > 0 ? diagnostics.pipeline_steps : [
          { name: '代码静态分析', status: 'success', duration_seconds: diagnostics?.analysis_duration || 0 },
          { name: '综合报告生成', status: 'success', duration_seconds: 0 },
        ]).map((step, idx) => (
          <div key={idx} className={`step-card step-${step.status}`}>
            <div style={{ fontSize: '0.8rem', fontWeight: 600, color: 'var(--text-color, #0f172a)' }}>
              {step.status === 'success' ? '✓' : step.status === 'failed' ? '✗' : '●'} {step.name}
            </div>
            <div style={{ fontSize: '0.75rem', color: 'var(--text-secondary, #64748b)' }}>
              耗时: {formatDuration(step.duration_seconds)}
            </div>
          </div>
        ))}
      </div>

      {/* 2. 分片执行矩阵 */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', margin: '1.5rem 0 0.75rem 0' }}>
        <h4 style={{ margin: 0, fontSize: '0.9rem', color: 'var(--text-color, #0f172a)' }}>
          📦 分片执行矩阵 ({diagnostics?.chunks?.length || 0} 个分片)
        </h4>
      </div>

      {isSingleEngine || !diagnostics?.chunks || diagnostics.chunks.length === 0 ? (
        <ReportEmptyState type="no-chunks" />
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
          {diagnostics.chunks.map((chunk) => {
            const isChunkFailed = chunk.status === 'failed';
            const expanded = !!expandedChunks[chunk.chunk_name];

            return (
              <div
                key={chunk.chunk_name}
                style={{
                  background: 'var(--card-bg, #ffffff)',
                  border: `1px solid ${isChunkFailed ? '#fecdd3' : '#e2e8f0'}`,
                  borderRadius: '8px',
                  padding: '0.75rem 1rem',
                }}
              >
                <div
                  style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', cursor: 'pointer' }}
                  onClick={() => toggleChunk(chunk.chunk_name)}
                >
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
                    <span style={{ color: isChunkFailed ? '#ef4444' : '#10b981', fontWeight: 700 }}>
                      {isChunkFailed ? '✗' : '✓'}
                    </span>
                    <span style={{ fontWeight: 600, fontSize: '0.875rem' }}>{chunk.chunk_name}</span>
                    {chunk.attempts > 1 && (
                      <span style={{ fontSize: '0.7rem', background: 'rgba(239, 68, 68, 0.1)', color: '#dc2626', padding: '0.1rem 0.4rem', borderRadius: '4px', fontWeight: 600 }}>
                        重试 {chunk.attempts} 次
                      </span>
                    )}
                  </div>

                  <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', fontSize: '0.8rem', color: 'var(--text-secondary, #64748b)' }}>
                    <span>⏱️ {formatDuration(chunk.duration_seconds)}</span>
                    <span>📂 {chunk.files_count} 个文件</span>
                    <span>{expanded ? '▲' : '▼'}</span>
                  </div>
                </div>

                {expanded && (
                  <div style={{ marginTop: '0.75rem', paddingTop: '0.75rem', borderTop: '1px solid var(--border-color, #e2e8f0)' }}>
                    {chunk.error_message && (
                      <div style={{ background: '#fff1f2', color: '#be123c', padding: '0.5rem 0.75rem', borderRadius: '6px', fontSize: '0.75rem', marginBottom: '0.5rem', fontFamily: 'monospace' }}>
                        错误: {chunk.error_message}
                      </div>
                    )}
                    {chunk.files && chunk.files.length > 0 && (
                      <div>
                        <div style={{ fontSize: '0.75rem', fontWeight: 600, color: 'var(--text-secondary, #64748b)', marginBottom: '0.25rem' }}>
                          包含的文件列表:
                        </div>
                        <div style={{ maxHeight: '120px', overflowY: 'auto', background: 'var(--bg-color, #f8fafc)', padding: '0.5rem', borderRadius: '4px', fontSize: '0.75rem', fontFamily: 'monospace' }}>
                          {chunk.files.map((f, i) => (
                            <div key={i} style={{ color: 'var(--text-color, #334155)', padding: '0.1rem 0' }}>• {f}</div>
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
      )}

      {/* 3. 执行输出日志查看器 */}
      {diagnostics?.raw_output_log && (
        <div style={{ marginTop: '1.5rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.5rem' }}>
            <h4 style={{ margin: 0, fontSize: '0.9rem', color: 'var(--text-color, #0f172a)' }}>
              📜 终端执行输出日志 {diagnostics.log_truncated ? `(展示最新 200 行，共 ${diagnostics.total_log_lines} 行)` : ''}
            </h4>
            <div style={{ display: 'flex', gap: '0.4rem' }}>
              <button className="nav-btn no-print" onClick={handleCopyLog}>
                📋 复制日志
              </button>
            </div>
          </div>

          <div
            className="code-snippet-box"
            style={{
              maxHeight: logExpanded ? 'none' : '240px',
            }}
          >
            <pre style={{ margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-all', fontSize: '0.75rem' }}>
              {diagnostics.raw_output_log}
            </pre>
          </div>
          {diagnostics.total_log_lines > 20 && (
            <button
              onClick={() => setLogExpanded(!logExpanded)}
              style={{
                background: 'none',
                border: 'none',
                color: 'var(--primary-color, #2563eb)',
                fontSize: '0.75rem',
                cursor: 'pointer',
                padding: '0.2rem 0',
                fontWeight: 500,
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
