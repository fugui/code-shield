import React, { useMemo, useState } from 'react';
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

type ChunkHealth = 'failed' | 'degraded' | 'recovered' | 'success';

const errorClassLabels: Record<string, string> = {
  canceled: '已取消',
  timeout: '执行超时',
  idle_timeout: '模型响应空闲超时',
  contract_mismatch: '输出契约不匹配',
  rate_limited: '模型调用限流',
  network_transient: '网络瞬时异常',
  resource_busy: '算力资源繁忙',
  auth: '模型凭证/鉴权失败',
  config: '调用配置错误',
  content_filtered: '内容被安全策略拦截',
  output_missing: '输出产物缺失',
  output_invalid: '输出内容无效',
  json_invalid: '输出不是有效 JSON',
  unknown: '未分类错误',
};

const errorClassSuggestions: Record<string, string> = {
  timeout: '通常是分片过大或模型响应过慢，可恢复任务重试；系统可能会自动拆分。',
  idle_timeout: '模型连接建立后长时间没有返回内容，建议重试或检查模型端负载。',
  contract_mismatch: '模型输出不符合报告契约，通常已尝试修复；若持续出现，可查看原始日志中的 JSON 片段。',
  output_missing: '执行器没有找到预期输出文件，建议重试；持续失败时检查终端日志中的输出路径。',
  output_invalid: '模型返回内容无法解析，建议重试；持续失败时结合原始日志定位。',
  json_invalid: '模型输出不是有效 JSON，建议重试；持续失败时结合原始日志定位。',
  rate_limited: '模型端触发限流，稍后恢复任务或降低并发后重试。',
  resource_busy: '模型资源繁忙，稍后恢复任务或降低并发后重试。',
  network_transient: '网络连接瞬时中断，通常恢复任务重试即可。',
  auth: '检查模型账号、Token 或接入配置。',
  config: '检查模型调用配置和任务模板配置。',
  content_filtered: '内容触发模型安全策略，需要评估提示词或目标代码上下文。',
  canceled: '任务被用户或系统取消，不是模型执行本身的错误。',
  unknown: '缺少稳定错误分类，建议展开错误信息并检查原始日志。',
};

function chunkHealth(chunk: NonNullable<TaskDiagnostics['chunks']>[number]): ChunkHealth {
  if (chunk.status === 'failed') return 'failed';
  if (chunk.artifact_quality_degraded || chunk.artifact_complete === false) return 'degraded';
  if (chunk.resumed) return 'recovered';
  return 'success';
}

interface ChunkTitleInfo {
  primary: string;
  qualifier?: string;
  secondary?: string;
  tooltip: string;
}

function chunkTitle(chunkName: string): ChunkTitleInfo {
  if (chunkName === 'root') {
    return { primary: '根目录分片', tooltip: '文件位于仓库根目录附近，无法按子目录进一步命名。' };
  }

  const changeMatch = chunkName.match(/^change-(\d+)(?:-(.*))?$/);
  if (changeMatch) {
    const changePath = changeMatch[2] || '';
    return {
      primary: `Change ${changeMatch[1]}`,
      qualifier: '变更分片',
      secondary: changePath,
      tooltip: changePath || '按变更语义生成的分片。',
    };
  }

  const splitMatch = chunkName.match(/^(.*)-split(\d+)$/);
  if (splitMatch) {
    return {
      primary: splitMatch[1] || '语义分片',
      qualifier: `动态拆分 ${splitMatch[2]}`,
      tooltip: '目录语义组过大或执行失败后，运行时按语义边界继续拆出的子分片。',
    };
  }

  const groupMatch = chunkName.match(/^(.*)-(\d+)$/);
  if (groupMatch && groupMatch[1]) {
    return {
      primary: groupMatch[1],
      qualifier: `组 ${groupMatch[2]}`,
      tooltip: '同名目录包含的文件超过单分片上限，编号表示该目录下的顺序分组。',
    };
  }

  return { primary: `目录 ${chunkName}`, tooltip: '按目录语义聚合后的分片名。' };
}

function getStageLabel(index: number, total: number, retryCount: number): string {
  if (retryCount > 0) {
    return index === 0 ? '初次调用' : `第 ${index} 次重试`;
  }
  if (total === 3) {
    const debateStages = ['Stage 1 (Hunter 猎手初检)', 'Stage 2 (Challenger 辩驳质询)', 'Stage 3 (Judge 终审裁决)'];
    return debateStages[index] || `阶段 ${index + 1}`;
  }
  if (total > 1) {
    return `阶段 ${index + 1}`;
  }
  return '初次调用';
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
  const [chunkQuery, setChunkQuery] = useState('');
  const [chunkFilter, setChunkFilter] = useState<'all' | 'problems' | 'actions' | 'success'>('all');

  const chunkStats = useMemo(() => {
    const chunks = diagnostics?.chunks || [];
    return {
      total: chunks.length,
      failed: chunks.filter(chunk => chunkHealth(chunk) === 'failed').length,
      degraded: chunks.filter(chunk => chunkHealth(chunk) === 'degraded').length,
      recovered: chunks.filter(chunk => chunkHealth(chunk) === 'recovered').length,
      success: chunks.filter(chunk => chunkHealth(chunk) === 'success').length,
    };
  }, [diagnostics?.chunks]);

  const visibleChunks = useMemo(() => {
    const query = chunkQuery.trim().toLowerCase();
    const rank: Record<ChunkHealth, number> = { failed: 0, degraded: 1, recovered: 2, success: 3 };

    return (diagnostics?.chunks || [])
      .filter(chunk => {
        const health = chunkHealth(chunk);
        if (chunkFilter === 'problems') return health === 'failed' || health === 'degraded';
        if (chunkFilter === 'actions') return chunk.attempts > 1 || chunk.split_depth > 0 || chunk.contract_repairs > 0 || (chunk.resource_failovers ?? 0) > 0 || chunk.driver_failovers > 0 || (chunk.schema_repair_attempts ?? 0) > 0 || (chunk.candidate_quarantine_count ?? 0) > 0;
        if (chunkFilter === 'success') return health === 'success' || health === 'recovered';
        return true;
      })
      .filter(chunk => {
        if (!query) return true;
        return chunk.chunk_name.toLowerCase().includes(query) || (chunk.files || []).some(file => file.toLowerCase().includes(query));
      })
      .sort((a, b) => {
        const healthDiff = rank[chunkHealth(a)] - rank[chunkHealth(b)];
        if (healthDiff !== 0) return healthDiff;
        return a.chunk_name.localeCompare(b.chunk_name, 'zh-Hans-CN');
      });
  }, [diagnostics?.chunks, chunkFilter, chunkQuery]);

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

  const isDegraded = meta?.status === 'degraded';
  const isFailed = meta?.status === 'failed' || diagnostics?.error_message;
  const degradedReasons = diagnostics?.degraded_reasons || [];
  const hasQuarantine = degradedReasons.includes('CANDIDATE_QUARANTINED') ||
    (diagnostics?.candidate_quarantine_count ?? 0) > 0;
  const hasArtifactIncomplete = degradedReasons.includes('ARTIFACT_INCOMPLETE') ||
    (!hasQuarantine && diagnostics?.artifact_complete === false);
  const hasCoverageReason = isDegraded && !hasArtifactIncomplete && !hasQuarantine;

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

      {isDegraded && !isFailed && (
        <div
          style={{
            background: '#fff7ed',
            border: '1px solid #fed7aa',
            borderRadius: '8px',
            padding: '1rem',
            margin: '1rem 0',
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', color: '#c2410c', fontWeight: 700, fontSize: '1.05rem', marginBottom: '0.5rem' }}>
            <span>⚠️</span>
            <span>
              {hasCoverageReason
                ? '扫描覆盖率降级'
                : hasQuarantine
                ? 'AI 产物候选隔离'
                : hasArtifactIncomplete
                ? 'AI 产物完整性降级'
                : '任务降级'}
            </span>
          </div>
          <p style={{ color: '#9a3412', fontSize: '0.875rem', lineHeight: 1.6, margin: 0 }}>
            {hasCoverageReason
              ? '报告已生成，但存在失败分片或未覆盖文件，结论仅代表已扫描部分。'
              : hasQuarantine
              ? '部分 AI 候选因契约或锚点问题被隔离，结果可能不完整。'
              : 'AI 产物完整性未达标，结论可能不完整。'}
            {meta?.success_chunks != null && meta?.total_chunks ? ` 分片完成：${meta.success_chunks}/${meta.total_chunks}。` : ''}
          </p>
          {onResume && (
            <button
              onClick={onResume}
              className="nav-btn no-print"
              style={{ background: '#ea580c', color: 'white', borderColor: 'transparent', fontWeight: 600, padding: '0.4rem 1rem', marginTop: '0.75rem' }}
            >
              🔄 恢复失败分片并重新汇总
            </button>
          )}
        </div>
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
              ? '🤖 全量对抗辩论'
              : '📦 历史执行快照'}
          </span>
          <span className="kpi-meta">
            {diagnostics?.chunks?.length || meta?.total_chunks || 0} 个语义分片
          </span>
        </div>

        <div className="report-kpi-card">
          <span className="kpi-title">
            {(diagnostics?.retries ?? 0) > 0 ? '♻️ 故障恢复画像' : '⚡ 调度与执行画像'}
          </span>
          <span className="kpi-number" style={{ fontSize: '0.95rem', color: '#64748b' }}>
            {diagnostics?.attempts || 0} 次调用 · {diagnostics?.retries || 0} 次重试
          </span>
          <div className="kpi-chip-row">
            {diagnostics?.contract_repairs ? <span className="kpi-chip">修复 {diagnostics.contract_repairs}</span> : null}
            <span className="kpi-chip">资源切换 {diagnostics?.resource_failovers ?? 0}</span>
            {diagnostics?.driver_failovers ? <span className="kpi-chip">切换 {diagnostics.driver_failovers}</span> : null}
            {diagnostics?.split_invocations ? <span className="kpi-chip">拆分 {diagnostics.split_invocations}</span> : null}
            {diagnostics?.recovered_chunks ? <span className="kpi-chip">复用 {diagnostics.recovered_chunks}</span> : null}
            {diagnostics?.normalized_issue_count ? <span className="kpi-chip">归一 {diagnostics.normalized_issue_count}</span> : null}
            {diagnostics?.schema_repair_attempts ? (
              <span className="kpi-chip">
                Schema {diagnostics.schema_repair_successes ?? 0}/{diagnostics.schema_repair_attempts}
              </span>
            ) : null}
            {diagnostics?.candidate_quarantine_count ? <span className="kpi-chip">隔离 {diagnostics.candidate_quarantine_count}</span> : null}
            {diagnostics?.artifact_quality_degraded ? (
              <span className="kpi-chip warning">质量告警 {diagnostics.unresolved_issue_count || 0}</span>
            ) : null}
          </div>
        </div>
      </div>

      {diagnostics?.category && (
        <div className="diagnostics-panel">
          <div className="panel-header-row">
            <div className="panel-title-row">
              <span className="panel-emoji">🏷️</span>
              <span className="panel-title">分类守卫诊断</span>
            </div>
          </div>
          <div className="report-kpi-grid" style={{ marginTop: '0.75rem' }}>
            <div className="report-kpi-card">
              <span className="kpi-title">🎯 Hunter 短码覆盖</span>
              <span className="kpi-number" style={{ fontSize: '1.1rem', color: '#2563eb' }}>
                {diagnostics.category.hunter.model_candidates
                  ? `${Math.round((diagnostics.category.hunter.model_code_valid / diagnostics.category.hunter.model_candidates) * 100)}%`
                  : 'N/A'}
              </span>
            </div>
            <div className="report-kpi-card">
              <span className="kpi-title">🧑‍⚖️ Judge 短码覆盖</span>
              <span className="kpi-number" style={{ fontSize: '1.1rem', color: '#2563eb' }}>
                {diagnostics.category.judge.model_candidates
                  ? `${Math.round((diagnostics.category.judge.model_code_valid / diagnostics.category.judge.model_candidates) * 100)}%`
                  : 'N/A'}
              </span>
            </div>
            <div className="report-kpi-card">
              <span className="kpi-title">🎯 Hunter 自动修复</span>
              <span
                className="kpi-number"
                style={{
                  fontSize: '1.1rem',
                  color:
                    diagnostics.category.hunter.model_code_invalid ||
                    diagnostics.category.hunter.model_code_absent
                      ? '#c2410c'
                      : '#2563eb',
                }}
              >
                {diagnostics.category.hunter.llm_repairs ?? 0}/{diagnostics.category.hunter.llm_repair_attempts ?? 0}
              </span>
            </div>
            <div className="report-kpi-card">
              <span className="kpi-title">🧑‍⚖️ Judge 自动修复</span>
              <span
                className="kpi-number"
                style={{
                  fontSize: '1.1rem',
                  color:
                    diagnostics.category.judge.model_code_invalid ||
                    diagnostics.category.judge.model_code_absent
                      ? '#c2410c'
                      : '#2563eb',
                }}
              >
                {diagnostics.category.judge.llm_repairs ?? 0}/{diagnostics.category.judge.llm_repair_attempts ?? 0}
              </span>
            </div>
            <div className="report-kpi-card">
              <span className="kpi-title">⏳ Hunter 待复核 / 未分类</span>
              <span className="kpi-number" style={{ fontSize: '1.1rem', color: '#c2410c' }}>
                {diagnostics.category.hunter.review_required}
              </span>
            </div>
            <div className="report-kpi-card">
              <span className="kpi-title">⏳ Judge 待复核 / 未分类</span>
              <span className="kpi-number" style={{ fontSize: '1.1rem', color: '#059669' }}>
                {diagnostics.category.judge.review_required}
              </span>
            </div>
            <div className="report-kpi-card">
              <span className="kpi-title">🚨 Hunter Category 隔离</span>
              <span
                className="kpi-number"
                style={{
                  fontSize: '1.1rem',
                  color: diagnostics.category.hunter.category_only_quarantined ? '#dc2626' : '#16a34a',
                }}
              >
                {diagnostics.category.hunter.category_only_quarantined}
              </span>
            </div>
            <div className="report-kpi-card">
              <span className="kpi-title">🚨 Judge Category 隔离</span>
              <span
                className="kpi-number"
                style={{
                  fontSize: '1.1rem',
                  color: diagnostics.category.judge.category_only_quarantined ? '#dc2626' : '#16a34a',
                }}
              >
                {diagnostics.category.judge.category_only_quarantined}
              </span>
            </div>
            <div className="report-kpi-card" style={{ gridColumn: '1 / -1' }}>
              <span className="kpi-title">🧬 Taxonomy</span>
              <span className="kpi-number" style={{ fontSize: '0.9rem', color: '#64748b' }}>
                {meta?.taxonomy_schema_version ? `v${meta.taxonomy_schema_version} · ` : ''}
                {meta?.taxonomy_hash || meta?.category_schema_hash || 'legacy'}
              </span>
            </div>
          </div>
        </div>
      )}

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
                    '--pill-bg': isFailed ? '#fef2f2' : isDegraded ? '#fff7ed' : '#f0fdf4',
                    '--pill-color': isFailed ? '#dc2626' : isDegraded ? '#c2410c' : '#16a34a',
                    '--pill-border': isFailed ? '#fecaca' : isDegraded ? '#fed7aa' : '#bbf7d0',
                  } as React.CSSProperties}
                >
                  {isFailed
                    ? '执行中断'
                    : isDegraded
                    ? `任务降级 (${steps.length}/${steps.length})`
                    : `全部阶段完成 (${steps.length}/${steps.length})`}
                </span>
              )}
            </div>

            {steps.length > 0 ? (
              /* 流程节点横向网格 */
              <div className="step-grid">
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

      {diagnostics?.synthesis && (() => {
        const synthesis = diagnostics.synthesis;
        return (
          <div className="diagnostics-panel">
            <div className="panel-header-row" style={{ marginBottom: '1.25rem' }}>
              <div className="panel-title-row">
                <span className="panel-emoji">🧾</span>
                <span className="panel-title">Synthesis 恢复观测</span>
              </div>
              <span
                className="status-pill pill-sm"
                style={{
                  '--pill-bg': synthesis.status === 'failed' ? '#fef2f2' : 'rgba(22,163,74,0.08)',
                  '--pill-color': synthesis.status === 'failed' ? '#dc2626' : '#16a34a',
                  '--pill-border': synthesis.status === 'failed' ? '#fecaca' : 'rgba(22,163,74,0.25)',
                } as React.CSSProperties}
              >
                {synthesis.status || 'unknown'}
              </span>
            </div>

            <div className="chunk-health-summary">
              <span className="health-chip success">Attempts {synthesis.attempts}</span>
              <span className="health-chip degraded">资源切换 {synthesis.resource_failovers}</span>
              <span className="health-chip failed">Driver 切换 {synthesis.driver_failovers}</span>
              {synthesis.resource_id && <span className="health-chip success">{synthesis.resource_id}</span>}
            </div>

            <div style={{ marginTop: '0.9rem', fontSize: '0.85rem', color: 'var(--color-text-secondary, #475569)' }}>
              {synthesis.resource_chain && synthesis.resource_chain.length > 0 && (
                <div className="chunk-trace-info">
                  <div className="chunk-trace-title">
                    {synthesis.attempts > 1 ? '模型资源调度链 (Resource Chain):' : '执行资源 (Resource):'}
                  </div>
                  {synthesis.resource_chain.map((resourceID, index) => (
                    <div key={`${resourceID}-${index}`} className="file-line">
                      • {synthesis.attempts > 1 ? `第 ${index + 1} 次调用: ` : ''}{resourceID}
                    </div>
                  ))}
                </div>
              )}
              {synthesis.error_classes && synthesis.error_classes.some(errorClass => errorClass && errorClass !== 'none') && (
                <div className="chunk-error">
                  <div className="chunk-trace-title">错误分类:</div>
                  {synthesis.error_classes.map((errorClass, index) => (
                    <div key={`${errorClass}-${index}`} className="file-line">• attempt {index + 1}: {errorClass}</div>
                  ))}
                </div>
              )}
              {synthesis.queue_wait_ms && synthesis.queue_wait_ms.some(wait => wait > 0) && (
                <div className="chunk-trace-info">
                  <div className="chunk-trace-title">每次尝试队列等待:</div>
                  {synthesis.queue_wait_ms.map((wait, index) => (
                    <div key={`synthesis-queue-${index}`} className="file-line">• attempt {index + 1}: {wait} ms</div>
                  ))}
                </div>
              )}
              {synthesis.attempt_duration_seconds && synthesis.attempt_duration_seconds.length > 0 && (
                <div className="chunk-trace-info">
                  <div className="chunk-trace-title">
                    {synthesis.attempts > 1 ? '各次调用耗时:' : '执行耗时:'}
                  </div>
                  {synthesis.attempt_duration_seconds.map((duration, index) => (
                    <div key={`synthesis-duration-${index}`} className="file-line">
                      • {synthesis.attempts > 1 ? `第 ${index + 1} 次: ` : ''}{duration.toFixed(2)} s
                    </div>
                  ))}
                </div>
              )}
              {synthesis.error_message && (
                <div className="chunk-error">
                  <div className="chunk-trace-title">错误信息:</div>
                  {synthesis.error_message}
                </div>
              )}
            </div>
          </div>
        );
      })()}

      {/* 2. 分片执行矩阵 */}
      {diagnostics?.chunks && diagnostics.chunks.length > 0 && (
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

          <div className="chunk-matrix-toolbar">
            <div className="chunk-health-summary">
              <span className="health-chip success">成功 {chunkStats.success}</span>
              <span className="health-chip recovered">复用成功 {chunkStats.recovered}</span>
              <span className="health-chip degraded">质量降级 {chunkStats.degraded}</span>
              <span className="health-chip failed">失败 {chunkStats.failed}</span>
            </div>
            <div className="chunk-matrix-controls">
              <input
                className="chunk-search"
                placeholder="搜索分片名或文件路径"
                value={chunkQuery}
                onChange={(event) => setChunkQuery(event.target.value)}
              />
              <select
                className="chunk-filter"
                value={chunkFilter}
                onChange={(event) => setChunkFilter(event.target.value as typeof chunkFilter)}
              >
                <option value="all">全部分片</option>
                <option value="problems">失败 / 降级</option>
                <option value="actions">有重试 / 修复动作</option>
                <option value="success">正常成功</option>
              </select>
            </div>
          </div>

          {/* 分片列表 */}
          <div className="chunk-list">
            {visibleChunks.map((chunk) => {
              const health = chunkHealth(chunk);
              const isChunkFailed = health === 'failed';
              const isChunkDegraded = health === 'degraded';
              const chunkTitleInfo = chunkTitle(chunk.chunk_name);
              const errorLabel = chunk.error_class ? (errorClassLabels[chunk.error_class] || chunk.error_class) : '';
              const errorSuggestion = chunk.error_class ? (errorClassSuggestions[chunk.error_class] || '展开错误信息并检查原始日志。') : '';
              const expanded = !!expandedChunks[chunk.chunk_name];
              const retryCount = typeof chunk.retries === 'number'
                ? chunk.retries
                : (chunk.attempts > 1 && ((chunk.resource_failovers ?? 0) > 0 || (chunk.error_classes ?? []).some(c => c && c !== 'none'))
                    ? chunk.attempts - 1
                    : 0);

              return (
                <div
                  key={chunk.chunk_name}
                  className="chunk-row"
                  style={{
                    '--chunk-border': isChunkFailed ? '#fecdd3' : isChunkDegraded ? '#fed7aa' : '#e2e8f0',
                    '--chunk-bg': isChunkFailed ? '#fff7f7' : isChunkDegraded ? '#fffdf8' : '#f8fafc',
                  } as React.CSSProperties}
                >
                  <div
                    className="chunk-row-header"
                    role="button"
                    tabIndex={0}
                    aria-expanded={expanded}
                    onClick={() => toggleChunk(chunk.chunk_name)}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter' || event.key === ' ') {
                        event.preventDefault();
                        toggleChunk(chunk.chunk_name);
                      }
                    }}
                  >
                    <span
                      className="chunk-badge"
                      style={{
                        '--chunk-badge-bg': isChunkFailed ? '#ef4444' : isChunkDegraded ? '#f59e0b' : '#10b981',
                      } as React.CSSProperties}
                    >
                      {isChunkFailed ? '✕' : isChunkDegraded ? '!' : '✓'}
                    </span>
                    <div className="chunk-main">
                      <div className="chunk-title-row">
                        <span className="chunk-title-wrap">
                          <span className="chunk-name" title={chunkTitleInfo.tooltip}>
                            {chunkTitleInfo.primary}
                          </span>
                          {chunkTitleInfo.qualifier && (
                            <span className="chunk-name-qualifier">{chunkTitleInfo.qualifier}</span>
                          )}
                          <span className={`chunk-state-pill ${health}`}>
                            {health === 'failed' ? '失败' : health === 'degraded' ? '成功但降级' : health === 'recovered' ? '成功（复用）' : '成功'}
                          </span>
                        </span>
                        <div className="chunk-action-badges">
                          {isChunkFailed && errorLabel && (
                            <span className="retry-badge">{errorLabel}</span>
                          )}
                          {retryCount > 0 ? (
                            <span className="retry-badge">
                              重试 {retryCount} 次
                            </span>
                          ) : chunk.attempts > 1 ? (
                            <span className="retry-badge info">
                              {chunk.attempts} 阶段调用
                            </span>
                          ) : null}
                          {chunk.split_depth > 0 && (
                            <span className="retry-badge">
                              拆分 {chunk.split_count > 0 ? `${chunk.split_count} 次` : `${chunk.split_depth} 层`}
                            </span>
                          )}
                          {chunk.contract_repairs > 0 && (
                            <span className="retry-badge">修复 {chunk.contract_repairs} 次</span>
                          )}
                          {(chunk.normalized_issue_count ?? 0) > 0 && (
                            <span className="retry-badge">归一 {chunk.normalized_issue_count} 项</span>
                          )}
                          {(chunk.schema_repair_attempts ?? 0) > 0 && (
                            <span className="retry-badge">
                              Schema 修复 {chunk.schema_repair_successes ?? 0}/{chunk.schema_repair_attempts}
                            </span>
                          )}
                          {(chunk.candidate_quarantine_count ?? 0) > 0 && (
                            <span className="retry-badge">隔离 {chunk.candidate_quarantine_count} 个</span>
                          )}
                          {(chunk.resource_failovers ?? 0) > 0 && (
                            <span className="retry-badge">资源池切换 {chunk.resource_failovers} 次</span>
                          )}
                          {chunk.artifact_quality_degraded && (
                            <span className="retry-badge">质量告警 {chunk.unresolved_issue_count ?? 0} 项</span>
                          )}
                          {chunk.driver_failovers > 0 && (
                            <span className="retry-badge">切换 {chunk.driver_failovers} 次</span>
                          )}
                        </div>
                      </div>
                      {chunkTitleInfo.secondary && (
                        <span className="chunk-secondary-name" title={chunkTitleInfo.secondary}>
                          {chunkTitleInfo.secondary}
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
                      {errorLabel && (
                        <div className="chunk-error-summary">
                          <strong>{errorLabel}</strong>
                          <span>{errorSuggestion}</span>
                        </div>
                      )}
                      {chunk.error_message && (
                        <div className="chunk-error">
                          <div className="chunk-trace-title">错误信息:</div>
                          {chunk.error_message}
                        </div>
                      )}
                      {chunk.resource_chain && chunk.resource_chain.length > 0 && (
                        <div className="chunk-trace-info">
                          <div className="chunk-trace-title">
                            {retryCount > 0 ? '故障重试链路 (Retry Chain):' : (chunk.attempts > 1 ? '多阶段调度链路 (Stage Chain):' : '执行资源 (Resource):')}
                          </div>
                          {chunk.resource_chain.map((resourceID, i) => (
                            <div key={`${resourceID}-${i}`} className="file-line">
                              • {getStageLabel(i, chunk.resource_chain!.length, retryCount)}: {resourceID}
                            </div>
                          ))}
                        </div>
                      )}
                      {chunk.error_classes && chunk.error_classes.some(c => c && c !== 'none') && (
                        <div className="chunk-error">
                          <div className="chunk-trace-title">错误分类:</div>
                          {chunk.error_classes.map((errorClass, i) => (
                            <div key={`${errorClass}-${i}`} className="file-line">
                              • {getStageLabel(i, chunk.error_classes!.length, retryCount)}: {errorClass}
                            </div>
                          ))}
                        </div>
                      )}
                      {(chunk.queue_wait_ms?.length ?? 0) > 0 && chunk.queue_wait_ms!.some(wait => wait > 0) && (
                        <div className="chunk-trace-info">
                          <div className="chunk-trace-title">队列等待:</div>
                          {chunk.queue_wait_ms!.map((wait, i) => (
                            <div key={`queue-${i}`} className="file-line">
                              • {getStageLabel(i, chunk.queue_wait_ms!.length, retryCount)}: {wait} ms
                            </div>
                          ))}
                        </div>
                      )}
                      {(chunk.attempt_duration_seconds?.length ?? 0) > 0 && (
                        <div className="chunk-trace-info">
                          <div className="chunk-trace-title">
                            {retryCount > 0 ? '各次尝试耗时:' : (chunk.attempts > 1 ? '各阶段执行耗时:' : '执行耗时:')}
                          </div>
                          {chunk.attempt_duration_seconds!.map((duration, i) => (
                            <div key={`duration-${i}`} className="file-line">
                              • {getStageLabel(i, chunk.attempt_duration_seconds!.length, retryCount)}: {duration.toFixed(2)} s
                            </div>
                          ))}
                        </div>
                      )}
                      {(chunk.schema_repair_issues?.length ?? 0) > 0 && (
                        <div className="chunk-error">
                          <div className="chunk-trace-title">Schema 修复残余问题:</div>
                          {chunk.schema_repair_issues!.map((issue, i) => (
                            <div key={i} className="file-line">• {issue}</div>
                          ))}
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
          {visibleChunks.length === 0 && (
            <div className="chunk-empty-filter">没有匹配当前筛选条件的分片。</div>
          )}
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
