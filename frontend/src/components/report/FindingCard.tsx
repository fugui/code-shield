import React, { useState } from 'react';
import { TaskFindingItem, GovernanceMode } from '../../types/report';
import { getSeverityMeta, getRepoSourceUrl, copyToClipboardWithFallback, extractFirstLineNumber } from '../../utils/reportUtils';
import { useToast } from '../Toast';
import DebateVerdictView from './DebateVerdictView';

interface FindingCardProps {
  finding: TaskFindingItem;
  governanceMode?: GovernanceMode;
  repoUrl?: string;
  branch?: string;
  onFeedbackSubmit?: () => void;
}

export default function FindingCard({
  finding,
  governanceMode = 'defect_tracking',
  repoUrl,
  branch,
  onFeedbackSubmit,
}: FindingCardProps) {
  const { showToast } = useToast();
  const [codeExpanded, setCodeExpanded] = useState(false);
  const [debateExpanded, setDebateExpanded] = useState(false);
  const [linkCopied, setLinkCopied] = useState(false);
  const [showFeedbackModal, setShowFeedbackModal] = useState(false);
  const [feedbackStatus, setFeedbackStatus] = useState('FALSE_POSITIVE');
  const [feedbackReason, setFeedbackReason] = useState('');
  const [submittingFeedback, setSubmittingFeedback] = useState(false);

  const isEntityMode = governanceMode === 'entity_assessment';
  const sevMeta = getSeverityMeta(finding.severity);
  const sourceUrl = getRepoSourceUrl(repoUrl, branch, finding.file_path, finding.line_number);

  const handleCopyLocation = async () => {
    const firstLine = extractFirstLineNumber(finding.line_number);
    const loc = firstLine ? `${finding.file_path}:${firstLine}` : finding.file_path;
    const ok = await copyToClipboardWithFallback(loc);
    if (ok) {
      showToast(`已复制定位: ${loc}`, 'success');
    } else {
      showToast('复制失败', 'error');
    }
  };

  const handleCopyLink = async () => {
    try {
      const url = new URL(window.location.href);
      url.searchParams.set('tab', 'findings');
      url.searchParams.set('findingId', finding.id.toString());
      url.hash = `finding-${finding.id}`;

      const shareUrl = url.toString();
      const ok = await copyToClipboardWithFallback(shareUrl);
      if (ok) {
        setLinkCopied(true);
        showToast('已复制问题直达链接到剪贴板！', 'success');
        setTimeout(() => setLinkCopied(false), 2000);
      } else {
        showToast('复制链接失败', 'error');
      }
    } catch {
      showToast('复制链接失败', 'error');
    }
  };

  const handleSubmitFeedback = async () => {
    if (!feedbackReason.trim()) {
      showToast('请填写排查与豁免说明理由', 'error');
      return;
    }
    setSubmittingFeedback(true);
    try {
      const res = await fetch(`/api/findings/${finding.id}/feedback`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          feedback_status: feedbackStatus,
          reason: feedbackReason,
        }),
      });
      if (!res.ok) {
        throw new Error('提交失败');
      }
      showToast('反馈已提交并沉淀为代码仓负样本规则！', 'success');
      setShowFeedbackModal(false);
      if (onFeedbackSubmit) {
        onFeedbackSubmit();
      }
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : '网络异常，提交失败';
      showToast(msg, 'error');
    } finally {
      setSubmittingFeedback(false);
    }
  };

  // 渲染增量状态徽标
  const renderDiffStatusBadge = () => {
    const status = finding.diff_status || finding.lifecycle_status;
    if (!status) return null;
    let bg = 'var(--color-bg-muted, #f1f5f9)';
    let color = 'var(--color-text-secondary, #475569)';
    let text = '存量';

    switch (status) {
      case 'NEW':
        bg = 'rgba(239, 68, 68, 0.12)';
        color = 'var(--color-danger, #dc2626)';
        text = '本次新增 (NEW)';
        break;
      case 'EXISTED':
        bg = 'rgba(100, 116, 139, 0.12)';
        color = 'var(--color-text-secondary, #475569)';
        text = '历史存量 (EXISTED)';
        break;
      case 'RESOLVED':
        bg = 'rgba(34, 197, 94, 0.12)';
        color = 'var(--color-success, #16a34a)';
        text = '已修复 (RESOLVED)';
        break;
      case 'REOPENED':
        bg = 'rgba(217, 119, 6, 0.12)';
        color = 'var(--color-warning, #d97706)';
        text = '复发激活 (REOPENED)';
        break;
      case 'GAP_FILLED':
        bg = 'rgba(6, 182, 212, 0.12)';
        color = '#0891b2';
        text = '漏扫回补 (GAP_FILLED)';
        break;
      case 'REGRESSED':
        bg = 'rgba(234, 88, 12, 0.12)';
        color = '#ea580c';
        text = '冷池复发 (REGRESSED)';
        break;
      case 'ARCHIVED':
        bg = 'rgba(148, 163, 184, 0.15)';
        color = 'var(--color-text-muted, #64748b)';
        text = '冷寂归档 (ARCHIVED)';
        break;
      case 'NEW_IN_DIFF':
        bg = 'rgba(220, 38, 38, 0.15)';
        color = 'var(--color-danger, #dc2626)';
        text = '变更引入 (NEW_IN_DIFF)';
        break;
      case 'RESOLVED_BY_CHANGE':
        bg = 'rgba(16, 185, 129, 0.15)';
        color = 'var(--color-success, #059669)';
        text = '变更顺带修复 (RESOLVED_BY_CHANGE)';
        break;
      case 'HISTORICAL_BASELINE':
        bg = 'rgba(148, 163, 184, 0.1)';
        color = 'var(--color-text-muted, #64748b)';
        text = '存量基线 (BASELINE)';
        break;
      default:
        text = status;
    }

    return (
      <span
        style={{
          fontSize: '0.75rem',
          padding: '0.2rem 0.5rem',
          borderRadius: '4px',
          backgroundColor: bg,
          color: color,
          fontWeight: 600,
          border: `1px solid ${color}40`,
        }}
        title={`缺陷生命周期状态: ${status}`}
      >
        {text}
      </span>
    );
  };

  const hasDebateInfo = Boolean(finding.hunter_claim || finding.challenger_arg || finding.judge_verdict);

  return (
    <div className="finding-card" id={`finding-${finding.id}`}>
      {/* 头部信息 (严重度徽章 + 增量状态 + 标题 + 分类 + R2R元数据) */}
      <div className="finding-card-header">
        <div className="finding-title-row" style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
          <span
            className="chip-btn"
            style={{
              backgroundColor: sevMeta.bg,
              color: sevMeta.color,
              borderColor: 'transparent',
              padding: '0.28rem 0.75rem',
              borderRadius: '6px',
              fontSize: '0.82rem',
              fontWeight: 700,
              display: 'inline-flex',
              alignItems: 'center',
            }}
          >
            {finding.severity_display || sevMeta.label}
          </span>

          {renderDiffStatusBadge()}

          {finding.item_uid && (
            <span
              style={{
                fontSize: '0.72rem',
                padding: '0.15rem 0.4rem',
                borderRadius: '4px',
                background: 'var(--color-bg-muted, #f1f5f9)',
                color: 'var(--color-text-muted, #64748b)',
                border: '1px solid var(--color-border-primary, #e2e8f0)',
                fontFamily: 'monospace',
              }}
              title={`对账条目唯一标识: ${finding.item_uid}`}
            >
              {finding.item_uid}
            </span>
          )}

          {finding.family_id && (
            <span
              style={{
                fontSize: '0.72rem',
                padding: '0.15rem 0.45rem',
                borderRadius: '4px',
                background: 'rgba(99, 102, 241, 0.1)',
                color: '#6366f1',
                border: '1px solid rgba(99, 102, 241, 0.25)',
                fontWeight: 600,
              }}
              title={`模板缺陷族标识: ${finding.family_id}`}
            >
              族: {finding.family_id.length > 18 ? `${finding.family_id.slice(0, 18)}...` : finding.family_id}
            </span>
          )}

          {finding.multi_view_count && finding.multi_view_count > 1 ? (
            <span
              style={{
                fontSize: '0.72rem',
                padding: '0.15rem 0.4rem',
                borderRadius: '4px',
                background: 'rgba(168, 85, 247, 0.1)',
                color: '#a855f7',
                border: '1px solid rgba(168, 85, 247, 0.25)',
                fontWeight: 600,
              }}
              title={`同位置多视角重叠合并，共聚合 ${finding.multi_view_count} 处切片检出`}
            >
              多视角 ×{finding.multi_view_count}
            </span>
          ) : null}

          {finding.rounds_seen && finding.rounds_seen > 1 ? (
            <span
              style={{
                fontSize: '0.72rem',
                padding: '0.15rem 0.4rem',
                borderRadius: '4px',
                background: 'var(--color-bg-muted, #f1f5f9)',
                color: 'var(--color-text-secondary, #475569)',
                border: '1px solid var(--color-border-primary, #e2e8f0)',
              }}
              title={`该缺陷在历史扫描中已累计检出 ${finding.rounds_seen} 轮`}
            >
              已见 {finding.rounds_seen} 轮
            </span>
          ) : null}

          <span style={{ fontWeight: 700, fontSize: '1.02rem', color: 'var(--color-text-primary, #0f172a)', lineHeight: 1.45 }}>
            {finding.title}
          </span>

          {finding.category && (
            <span style={{ fontSize: '0.78rem', color: 'var(--color-text-secondary, #64748b)', background: 'var(--color-bg-muted, #f1f5f9)', padding: '0.2rem 0.6rem', borderRadius: '4px', border: '1px solid var(--color-border-primary, #e2e8f0)' }}>
              {finding.category}
            </span>
          )}
        </div>

        {/* 状态展示与反馈操作 */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.65rem', flexShrink: 0 }}>
          <span className="finding-status-tag" title={`状态: ${finding.status_display || finding.status}`}>
            {finding.status_display || (finding.status === 'open' ? '待处理' : finding.status === 'pass' ? '合格' : finding.status)}
          </span>

          {finding.assignee_name && (
            <span style={{ fontSize: '0.78rem', color: '#64748b', background: '#f1f5f9', padding: '0.2rem 0.5rem', borderRadius: '4px' }}>
              @{finding.assignee_name}
            </span>
          )}

          <button
            className="nav-btn no-print"
            onClick={() => setShowFeedbackModal(true)}
            title="对该缺陷提交研发复核反馈（标记误报或不予修复并沉淀负样本规则）"
            style={{ fontSize: '0.82rem', background: 'var(--color-bg-surface, #fff)', border: '1px solid var(--color-border-primary, #cbd5e1)' }}
          >
            🛡️ 标记反馈
          </button>
        </div>
      </div>

      {/* 位置栏与操作按钮 */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '0.75rem', flexWrap: 'wrap', margin: '0.85rem 0 1rem 0' }}>
        <div className="finding-location-bar">
          <span>📄</span>
          <span>{finding.file_path}{finding.line_number ? `:${finding.line_number}` : ''}</span>
          {finding.scope_symbol && (
            <span style={{ fontSize: '0.8rem', color: '#64748b', marginLeft: '0.4rem' }}>
              (<code>{finding.scope_symbol}</code>)
            </span>
          )}
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <button
            className="nav-btn no-print"
            onClick={handleCopyLocation}
            title="复制文件路径与起始行号 (可用于 VSCode Ctrl+P 快捷跳转定位)"
            style={{ fontSize: '0.82rem' }}
          >
            📋 复制定位
          </button>
          <button
            className={`nav-btn no-print ${linkCopied ? 'btn-copied' : ''}`}
            onClick={handleCopyLink}
            title="复制该问题的专属直达链接"
            style={{ fontSize: '0.82rem' }}
          >
            {linkCopied ? '✓ 已复制链接' : '🔗 复制链接'}
          </button>
          {sourceUrl && (
            <a
              href={sourceUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="nav-btn no-print"
              style={{ textDecoration: 'none', fontSize: '0.82rem' }}
              title="在代码仓中查看源码"
            >
              ↗ 源码
            </a>
          )}
        </div>
      </div>

      {/* 详细描述 */}
      {finding.detail && (
        <div style={{ margin: '0.85rem 0' }}>
          <DebateVerdictView detail={finding.detail} title={finding.title} />
        </div>
      )}

      {/* 智能体三方对抗辩论事实链 (Hunter -> Challenger -> Judge) */}
      {hasDebateInfo && (
        <div style={{ margin: '0.85rem 0', background: 'var(--color-bg-muted, #f8fafc)', borderRadius: '8px', border: '1px solid var(--color-border-primary, #e2e8f0)', padding: '0.75rem 1rem' }}>
          <div
            style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', cursor: 'pointer', userSelect: 'none' }}
            onClick={() => setDebateExpanded(!debateExpanded)}
          >
            <span style={{ fontWeight: 700, fontSize: '0.86rem', color: '#334155', display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
              🤖 智能体三方对抗事实链 (Hunter ➜ Challenger ➜ Judge)
            </span>
            <span style={{ fontSize: '0.8rem', color: '#64748b' }}>
              {debateExpanded ? '▲ 收起' : '▼ 展开辩论'}
            </span>
          </div>

          {debateExpanded && (
            <div style={{ marginTop: '0.75rem', display: 'flex', flexDirection: 'column', gap: '0.65rem', fontSize: '0.84rem' }}>
              {finding.hunter_claim && (
                <div style={{ padding: '0.5rem 0.75rem', background: 'rgba(239, 68, 68, 0.05)', borderLeft: '3px solid #ef4444', borderRadius: '4px' }}>
                  <div style={{ fontWeight: 600, color: '#dc2626', marginBottom: '0.2rem' }}>🎯 Hunter (初筛猎手主张):</div>
                  <div style={{ color: '#475569' }}>{finding.hunter_claim}</div>
                </div>
              )}
              {finding.challenger_arg && (
                <div style={{ padding: '0.5rem 0.75rem', background: 'rgba(59, 130, 246, 0.05)', borderLeft: '3px solid #3b82f6', borderRadius: '4px' }}>
                  <div style={{ fontWeight: 600, color: '#2563eb', marginBottom: '0.2rem' }}>⚖️ Challenger (对抗辩护证据):</div>
                  <div style={{ color: '#475569' }}>{finding.challenger_arg}</div>
                </div>
              )}
              {finding.judge_verdict && (
                <div style={{ padding: '0.5rem 0.75rem', background: 'rgba(16, 185, 129, 0.05)', borderLeft: '3px solid #10b981', borderRadius: '4px' }}>
                  <div style={{ fontWeight: 600, color: '#059669', marginBottom: '0.2rem' }}>📜 Judge (终审法官裁决书):</div>
                  <div style={{ color: '#475569' }}>{finding.judge_verdict}</div>
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {/* 代码片段 */}
      {finding.code_snippet && (
        <div style={{ margin: '1rem 0' }}>
          <div
            className="code-snippet-box"
            style={{ maxHeight: codeExpanded ? 'none' : '150px' }}
          >
            <pre style={{ margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-word', fontFamily: 'inherit' }}>
              {finding.code_snippet}
            </pre>
          </div>
          {finding.code_snippet.split('\n').length > 4 && (
            <button
              onClick={() => setCodeExpanded(!codeExpanded)}
              className="expand-toggle-btn no-print"
            >
              {codeExpanded ? '▲ 收起代码片段' : '▼ 展开完整代码片段'}
            </button>
          )}
        </div>
      )}

      {/* 修复建议 */}
      {!isEntityMode && finding.suggestion && (
        <div className="suggestion-box">
          <div style={{ fontWeight: 700, marginBottom: '0.35rem', display: 'flex', alignItems: 'center', gap: '0.4rem', fontSize: '0.88rem' }}>
            💡 修复建议:
          </div>
          <div>{finding.suggestion}</div>
        </div>
      )}

      {/* 跟踪意见 */}
      {finding.latest_comment && (
        <div style={{ fontSize: '0.78rem', color: '#64748b', marginTop: '0.85rem', background: '#f8fafc', padding: '0.45rem 0.85rem', borderRadius: '6px', border: '1px solid #e2e8f0' }}>
          💬 最新跟踪意见: {finding.latest_comment}
        </div>
      )}

      {/* 标记反馈对话框 Modal */}
      {showFeedbackModal && (
        <div
          style={{
            position: 'fixed',
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            backgroundColor: 'rgba(15, 23, 42, 0.5)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            zIndex: 1000,
          }}
          onClick={() => setShowFeedbackModal(false)}
        >
          <div
            style={{
              background: 'var(--color-bg-surface, #ffffff)',
              borderRadius: '12px',
              padding: '1.5rem',
              width: '480px',
              maxWidth: '90vw',
              boxShadow: '0 20px 25px -5px rgba(0, 0, 0, 0.1), 0 10px 10px -5px rgba(0, 0, 0, 0.04)',
              border: '1px solid var(--color-border-primary, #e2e8f0)',
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <h3 style={{ margin: '0 0 1rem 0', fontSize: '1.1rem', color: '#0f172a' }}>
              🛡️ 提交研发排查反馈与沉淀例外规则
            </h3>
            <p style={{ fontSize: '0.85rem', color: '#64748b', marginBottom: '1rem' }}>
              标记为误报或不予修复后，系统将永久记住此负样本特征，并在下次扫描时自动规避，杜绝重复上报。
            </p>

            <div style={{ marginBottom: '1rem' }}>
              <label style={{ display: 'block', fontSize: '0.85rem', fontWeight: 600, color: '#334155', marginBottom: '0.35rem' }}>
                反馈处理类型:
              </label>
              <select
                value={feedbackStatus}
                onChange={(e) => setFeedbackStatus(e.target.value)}
                style={{ width: '100%', padding: '0.5rem', borderRadius: '6px', border: '1px solid #cbd5e1', fontSize: '0.88rem' }}
              >
                <option value="FALSE_POSITIVE">标记误报 (False Positive) - 自动沉淀过滤规则</option>
                <option value="WONT_FIX">不予修复 (Won't Fix) - 业务设计/已知豁免</option>
                <option value="CONFIRMED">确认真实缺陷 (Confirmed) - 纳入攻坚计划</option>
              </select>
            </div>

            <div style={{ marginBottom: '1.25rem' }}>
              <label style={{ display: 'block', fontSize: '0.85rem', fontWeight: 600, color: '#334155', marginBottom: '0.35rem' }}>
                排查与豁免说明理由 (必填):
              </label>
              <textarea
                value={feedbackReason}
                onChange={(e) => setFeedbackReason(e.target.value)}
                placeholder="例如：该函数仅在内部受限测试驱动中调用，外部入参已在上层网关做过非空校验..."
                rows={4}
                style={{ width: '100%', padding: '0.5rem', borderRadius: '6px', border: '1px solid #cbd5e1', fontSize: '0.88rem', resize: 'vertical' }}
              />
            </div>

            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '0.75rem' }}>
              <button
                className="btn btn-secondary"
                onClick={() => setShowFeedbackModal(false)}
                disabled={submittingFeedback}
              >
                取消
              </button>
              <button
                className="btn btn-primary"
                onClick={handleSubmitFeedback}
                disabled={submittingFeedback}
              >
                {submittingFeedback ? '提交中...' : '确认并沉淀知识'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
