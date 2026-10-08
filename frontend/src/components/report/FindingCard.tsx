import React, { useEffect, useState } from 'react';
import { TaskFindingItem, GovernanceMode, ObservationProjection } from '../../types/report';
import { getSeverityMeta, getRepoSourceUrl, copyToClipboardWithFallback, extractFirstLineNumber } from '../../utils/reportUtils';
import { isAIFixURLValid, buildAIFixURL, getAIFixURL } from '../../utils/reportUtils';
import { useToast } from '../Toast';
import DebateVerdictView from './DebateVerdictView';
import SuggestionMarkdown from './SuggestionMarkdown';

interface FindingCardProps {
  finding: TaskFindingItem;
  governanceMode?: GovernanceMode;
  reconciliation?: ObservationProjection;
  repoUrl?: string;
  branch?: string;
  onFeedbackSubmit?: () => void;
}

export default function FindingCard({
  finding,
  governanceMode = 'full_ledger',
  reconciliation,
  repoUrl,
  branch,
  onFeedbackSubmit,
}: FindingCardProps) {
  const { showToast } = useToast();
  const [codeExpanded, setCodeExpanded] = useState(false);
  const [linkCopied, setLinkCopied] = useState(false);
  const [showFeedbackModal, setShowFeedbackModal] = useState(false);
  const [feedbackStatus, setFeedbackStatus] = useState('FALSE_POSITIVE');
  const [feedbackReason, setFeedbackReason] = useState('');
  const [submittingFeedback, setSubmittingFeedback] = useState(false);
  const [aiFixURL, setAIFixURL] = useState<string | null>(null);

  const isEntityMode = governanceMode === 'entity_assessment';
  const sevMeta = getSeverityMeta(finding.severity);
  const sourceUrl = getRepoSourceUrl(repoUrl, branch, finding.file_path, finding.line_number);
  const defectId = finding.defect_id ?? reconciliation?.defect_id;
  const verdictLabels: Record<string, { label: string; className: string }> = {
    NEW: { label: '本轮新增', className: 'verdict-new' },
    EXISTED: { label: '历史存量', className: 'verdict-existed' },
    REOPENED: { label: '修复后复发', className: 'verdict-reopened' },
    PROBABLE: { label: '疑似存量，待确认', className: 'verdict-probable' },
    RESOLVED: { label: '本轮判定修复', className: 'verdict-resolved' },
    COVERAGE_GAP: { label: '覆盖缺口，不判修复', className: 'verdict-coverage-gap' },
  };
  const matchTierLabels: Record<string, string> = {
    STRONG: '强身份匹配',
    STRUCT: '结构匹配',
    BUCKET: '弱候选匹配',
    ASSIGN: '全局指派',
    AI: 'AI 仲裁',
    AI_FALLBACK: 'AI 兜底裁决',
    HUMAN: '人工确认',
    UNCLAIMED: '未匹配',
  };
  const verdict = reconciliation ? verdictLabels[reconciliation.verdict] || null : null;
  const matchTierLabel = reconciliation ? matchTierLabels[reconciliation.match_tier] || reconciliation.match_tier : '';
  const confidenceLabel = reconciliation
    ? (Math.round(reconciliation.confidence * 100) / 100).toFixed(2)
    : '';

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

  const assessmentIssues = finding.assessment_artifact?.issues || [];
  const assessmentStatusLabels: Record<string, string> = {
    valid: '评估合格',
    invalid: '评估不合格',
    issue: '存在风险',
    not_thread_creation: '非目标命中',
    needs_human: '需人工复核',
  };
  const assessmentStatusLabel = finding.assessment_status
    ? assessmentStatusLabels[finding.assessment_status] || finding.assessment_status
    : '';
  const categoryNeedsReview = finding.category_status === 'REVIEW_REQUIRED' ||
    finding.category === '未分类' || !finding.category;

  useEffect(() => {
    if (isEntityMode) {
      return;
    }
    let active = true;
    getAIFixURL()
      .then((fixURL) => {
        if (active) setAIFixURL(fixURL || '');
      })
      .catch(() => {
        if (active) setAIFixURL('');
      });

    return () => {
      active = false;
    };
  }, [isEntityMode]);

  const aiFixConfigValid = isAIFixURLValid(aiFixURL);
  const canUseAIFix = Boolean(
    !isEntityMode &&
    defectId &&
    aiFixConfigValid &&
    (finding.status === 'open' || finding.status === 'analyzing')
  );
  const aiFixDisabledReason = !defectId
    ? '尚未关联缺陷台账，暂不支持 AI 修复'
    : !aiFixConfigValid
      ? 'AI 修复配置无效'
      : '当前缺陷状态不可修复';

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

          <span style={{ fontWeight: 700, fontSize: '1.02rem', color: 'var(--color-text-primary, #0f172a)', lineHeight: 1.45 }}>
            {finding.title}
          </span>

          {!isEntityMode && verdict && (
            <span className={`reconciliation-badge ${verdict.className}`}>
              {verdict.label}
              <small>{matchTierLabel}{reconciliation?.defect_id ? ` · #${reconciliation.defect_id}` : ''} · {confidenceLabel}</small>
            </span>
          )}

          {categoryNeedsReview && (
            <span
              style={{
                fontSize: '0.78rem',
                color: '#c2410c',
                background: '#fff7ed',
                padding: '0.2rem 0.6rem',
                borderRadius: '4px',
                border: '1px solid #fed7aa',
              }}
            >
              未分类 · 待人工复核
            </span>
          )}
          {!categoryNeedsReview && finding.category && (
            <span style={{ fontSize: '0.78rem', color: 'var(--color-text-secondary, #64748b)', background: 'var(--color-bg-muted, #f1f5f9)', padding: '0.2rem 0.6rem', borderRadius: '4px', border: '1px solid var(--color-border-primary, #e2e8f0)' }}>
              <span title={finding.category_code ? `短码: ${finding.category_code}` : undefined}>
                {finding.category}
              </span>
              {finding.category_status && <small style={{ marginLeft: '0.3rem' }}>{finding.category_status}</small>}
            </span>
          )}

          {assessmentStatusLabel && (
            <span style={{ fontSize: '0.78rem', color: '#0369a1', background: '#e0f2fe', padding: '0.2rem 0.6rem', borderRadius: '4px', border: '1px solid #bae6fd' }}>
              {assessmentStatusLabel}
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

          {!isEntityMode && (
            <a
              className={`nav-btn no-print ${canUseAIFix ? '' : 'ai-fix-disabled'}`}
              href={canUseAIFix && aiFixConfigValid && aiFixURL ? buildAIFixURL(aiFixURL, defectId!) : undefined}
              target="_blank"
              rel="noopener noreferrer"
              aria-disabled={!canUseAIFix}
              onClick={canUseAIFix ? undefined : (event) => event.preventDefault()}
              title={canUseAIFix ? '使用 AI 修复该缺陷' : aiFixDisabledReason}
              style={{ textDecoration: 'none', fontSize: '0.82rem', opacity: canUseAIFix ? 1 : 0.5, cursor: canUseAIFix ? 'pointer' : 'not-allowed' }}
            >
              🤖 AI修复
            </a>
          )}
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

      {/* 缺陷机理剖析与智能体存证 */}
      {(finding.detail || finding.hunter_claim || finding.judge_verdict) && (
        <div style={{ margin: '0.85rem 0' }}>
          <DebateVerdictView
            detail={finding.detail}
            title={finding.title}
            hunterClaim={finding.hunter_claim}
            challengerArg={finding.challenger_arg}
            judgeVerdict={finding.judge_verdict}
            triggerLine={finding.trigger_line}
            scopeSymbol={finding.scope_symbol}
          />
        </div>
      )}

      {finding.classification_rationale && (
        <div style={{ fontSize: '0.82rem', color: '#475569', marginTop: '0.85rem' }}>
          <strong style={{ color: '#334155' }}>分类依据：</strong>
          {finding.classification_rationale}
          {finding.taxonomy_hash && (
            <div style={{ marginTop: '0.25rem', color: '#64748b' }}>Taxonomy: {finding.taxonomy_hash}</div>
          )}
        </div>
      )}

      {assessmentIssues.length > 0 && (
        <div style={{ margin: '0.85rem 0', background: 'var(--color-bg-muted, #f8fafc)', borderRadius: '8px', border: '1px solid var(--color-border-primary, #e2e8f0)', padding: '0.75rem 1rem' }}>
          <div style={{ fontWeight: 700, fontSize: '0.86rem', color: '#334155', marginBottom: '0.6rem' }}>
            🧾 Primary Unit 结论清单
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.55rem' }}>
            {assessmentIssues.map((issue, index) => (
              <div key={`${issue.category}-${index}`} style={{ fontSize: '0.84rem', color: '#475569', borderLeft: '3px solid #f59e0b', paddingLeft: '0.65rem' }}>
                <div style={{ fontWeight: 600, color: '#0f172a' }}>{issue.severity} · {issue.category}</div>
                <div>{issue.detail}</div>
                {issue.suggestion && <div style={{ marginTop: '0.2rem' }}>建议：{issue.suggestion}</div>}
              </div>
            ))}
          </div>
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
          <SuggestionMarkdown content={finding.suggestion} />
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
