import React, { useState } from 'react';
import { TaskFindingItem, GovernanceMode } from '../../types/report';
import { getSeverityMeta, getRepoSourceUrl, copyToClipboardWithFallback } from '../../utils/reportUtils';
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
  const isEntityMode = governanceMode === 'entity_assessment';
  const sevMeta = getSeverityMeta(finding.severity);
  const sourceUrl = getRepoSourceUrl(repoUrl, branch, finding.file_path, finding.line_number);

  const handleCopy = async () => {
    const loc = finding.line_number ? `${finding.file_path}:${finding.line_number}` : finding.file_path;
    const ok = await copyToClipboardWithFallback(loc);
    if (ok) {
      showToast(`已复制: ${loc}`, 'success');
    } else {
      showToast('复制失败', 'error');
    }
  };

  return (
    <div
      className="finding-card"
      id={`finding-${finding.id}`}
      style={{
        background: '#ffffff',
        backgroundColor: '#ffffff',
        border: '1px solid #e2e8f0',
        borderRadius: '14px',
        padding: '1.6rem 1.85rem',
        marginBottom: '1.5rem',
        boxShadow: '0 1px 4px rgba(0, 0, 0, 0.04)',
        boxSizing: 'border-box',
      }}
    >
      {/* 头部信息 (严重度徽章 + 标题 + 分类 + 只读状态) */}
      <div className="finding-card-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '1.25rem', marginBottom: '0.95rem' }}>
        <div className="finding-title-row" style={{ display: 'flex', alignItems: 'center', gap: '0.65rem', flexWrap: 'wrap', flex: 1 }}>
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
          <span
            className="finding-status-tag"
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              padding: '0.28rem 0.75rem',
              borderRadius: '6px',
              fontSize: '0.8rem',
              fontWeight: 600,
              background: '#f8fafc',
              color: '#475569',
              border: '1px solid #cbd5e1',
            }}
            title={`状态: ${finding.status_display || finding.status}`}
          >
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
        <div
          className="finding-location-bar"
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: '0.55rem',
            background: '#f8fafc',
            padding: '0.45rem 0.85rem',
            borderRadius: '6px',
            border: '1px solid #e2e8f0',
            fontFamily: '"JetBrains Mono", Consolas, monospace',
            fontSize: '0.82rem',
            color: '#475569',
          }}
        >
          <span>📄</span>
          <span>{finding.file_path}{finding.line_number ? `:${finding.line_number}` : ''}</span>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <button
            className="nav-btn no-print"
            onClick={handleCopy}
            title="复制文件路径与行号"
            style={{ padding: '0.4rem 0.85rem', fontSize: '0.82rem' }}
          >
            📋 复制定位
          </button>
          {sourceUrl && (
            <a
              href={sourceUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="nav-btn no-print"
              style={{ textDecoration: 'none', padding: '0.4rem 0.85rem', fontSize: '0.82rem' }}
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
            style={{
              maxHeight: codeExpanded ? 'none' : '150px',
              position: 'relative',
              background: '#0f172a',
              color: '#e2e8f0',
              padding: '1.25rem 1.5rem',
              borderRadius: '10px',
              fontFamily: '"JetBrains Mono", Consolas, "Fira Code", monospace',
              fontSize: '0.84rem',
              lineHeight: 1.65,
              overflowX: 'auto',
              boxShadow: 'inset 0 1px 3px rgba(0,0,0,0.3)',
            }}
          >
            <pre style={{ margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-word', fontFamily: 'inherit' }}>
              {finding.code_snippet}
            </pre>
          </div>
          {finding.code_snippet.split('\n').length > 4 && (
            <button
              onClick={() => setCodeExpanded(!codeExpanded)}
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
              {codeExpanded ? '▲ 收起代码片段' : '▼ 展开完整代码片段'}
            </button>
          )}
        </div>
      )}

      {/* 修复建议 (仅缺陷模式展示) */}
      {!isEntityMode && finding.suggestion && (
        <div
          className="suggestion-box"
          style={{
            background: '#f0fdf4',
            border: '1px solid #bbf7d0',
            color: '#15803d',
            padding: '1rem 1.35rem',
            borderRadius: '10px',
            fontSize: '0.85rem',
            lineHeight: 1.65,
            marginTop: '1.15rem',
          }}
        >
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
