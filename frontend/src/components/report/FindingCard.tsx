import React, { useState } from 'react';
import { TaskFindingItem, GovernanceMode } from '../../types/report';
import { getSeverityMeta, getRepoSourceUrl, copyToClipboardWithFallback, extractFirstLineNumber } from '../../utils/reportUtils';
import { useToast } from '../Toast';

interface FindingCardProps {
  finding: TaskFindingItem;
  governanceMode?: GovernanceMode;
  repoUrl?: string;
  branch?: string;
}

export default function FindingCard({
  finding,
  governanceMode = 'defect_tracking',
  repoUrl,
  branch,
}: FindingCardProps) {
  const { showToast } = useToast();
  const [codeExpanded, setCodeExpanded] = useState(false);
  const [linkCopied, setLinkCopied] = useState(false);
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

  return (
    <div className="finding-card" id={`finding-${finding.id}`}>
      {/* 头部信息 (严重度徽章 + 标题 + 分类 + 只读状态) */}
      <div className="finding-card-header">
        <div className="finding-title-row">
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
          <span style={{ fontWeight: 700, fontSize: '1.02rem', color: '#0f172a', lineHeight: 1.45 }}>
            {finding.title}
          </span>
          {finding.category && (
            <span style={{ fontSize: '0.78rem', color: '#64748b', background: '#f1f5f9', padding: '0.2rem 0.6rem', borderRadius: '4px', border: '1px solid #e2e8f0' }}>
              {finding.category}
            </span>
          )}
        </div>

        {/* 状态展示 (只读 Badge) */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.65rem', flexShrink: 0 }}>
          <span className="finding-status-tag" title={`状态: ${finding.status_display || finding.status}`}>
            {finding.status_display || (finding.status === 'open' ? '待处理' : finding.status === 'pass' ? '合格' : finding.status)}
          </span>

          {finding.assignee_name && (
            <span style={{ fontSize: '0.78rem', color: '#64748b', background: '#f1f5f9', padding: '0.2rem 0.5rem', borderRadius: '4px' }}>
              @{finding.assignee_name}
            </span>
          )}
        </div>
      </div>

      {/* 位置栏与操作按钮 */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '0.75rem', flexWrap: 'wrap', margin: '0.85rem 0 1rem 0' }}>
        <div className="finding-location-bar">
          <span>📄</span>
          <span>{finding.file_path}{finding.line_number ? `:${finding.line_number}` : ''}</span>
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
            title="复制该问题的专属直达链接 (其他人打开将直接定位并高亮此问题)"
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
        <div style={{ fontSize: '0.9rem', color: '#334155', margin: '0.85rem 0', lineHeight: 1.65 }}>
          {finding.detail}
        </div>
      )}

      {/* 代码片段 (带折叠，充沛内衬) */}
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

      {/* 修复建议 (仅缺陷模式展示) */}
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
    </div>
  );
}
