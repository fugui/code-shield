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
    <div className="finding-card" id={`finding-${finding.id}`}>
      {/* 头部信息 */}
      <div className="finding-card-header">
        <div className="finding-title-row">
          <span
            className="chip-btn"
            style={{ backgroundColor: sevMeta.bg, color: sevMeta.color, borderColor: 'transparent' }}
          >
            {finding.severity_display || sevMeta.label}
          </span>
          <span style={{ fontWeight: 600, fontSize: '0.95rem', color: 'var(--text-color, #0f172a)' }}>
            {finding.title}
          </span>
          {finding.category && (
            <span style={{ fontSize: '0.75rem', color: 'var(--text-secondary, #64748b)', background: 'var(--bg-color, #f1f5f9)', padding: '0.15rem 0.5rem', borderRadius: '4px' }}>
              {finding.category}
            </span>
          )}
        </div>

        {/* 状态展示 (只读 Badge) */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexShrink: 0 }}>
          <span className="finding-status-tag" title={`状态: ${finding.status_display || finding.status}`}>
            {finding.status_display || (finding.status === 'open' ? '待处理' : finding.status === 'pass' ? '合格' : finding.status)}
          </span>

          {finding.assignee_name && (
            <span style={{ fontSize: '0.75rem', color: 'var(--text-secondary, #64748b)' }}>
              @{finding.assignee_name}
            </span>
          )}
        </div>
      </div>

      {/* 位置栏与操作按钮 */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '0.5rem', flexWrap: 'wrap', margin: '0.5rem 0' }}>
        <div className="finding-location-bar">
          <span>📄</span>
          <span>{finding.file_path}{finding.line_number ? `:${finding.line_number}` : ''}</span>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '0.4rem' }}>
          <button className="nav-btn no-print" onClick={handleCopy} title="复制文件路径与行号">
            📋 复制定位
          </button>
          {sourceUrl && (
            <a
              href={sourceUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="nav-btn no-print"
              style={{ textDecoration: 'none' }}
              title="在代码仓中查看源码"
            >
              ↗ 源码
            </a>
          )}
        </div>
      </div>

      {/* 详细描述 */}
      {finding.detail && (
        <div style={{ fontSize: '0.85rem', color: 'var(--text-color, #334155)', margin: '0.5rem 0', lineHeight: 1.6 }}>
          {finding.detail}
        </div>
      )}

      {/* 代码片段 (带折叠) */}
      {finding.code_snippet && (
        <div style={{ margin: '0.5rem 0' }}>
          <div
            className="code-snippet-box"
            style={{
              maxHeight: codeExpanded ? 'none' : '120px',
              position: 'relative',
            }}
          >
            <pre style={{ margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
              {finding.code_snippet}
            </pre>
          </div>
          {finding.code_snippet.split('\n').length > 4 && (
            <button
              onClick={() => setCodeExpanded(!codeExpanded)}
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
              {codeExpanded ? '▲ 收起代码片段' : '▼ 展开完整代码片段'}
            </button>
          )}
        </div>
      )}

      {/* 修复建议 (仅缺陷模式展示) */}
      {!isEntityMode && finding.suggestion && (
        <div className="suggestion-box">
          <div style={{ fontWeight: 600, marginBottom: '0.2rem', display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
            💡 修复建议:
          </div>
          <div>{finding.suggestion}</div>
        </div>
      )}

      {/* 跟踪意见 */}
      {finding.latest_comment && (
        <div style={{ fontSize: '0.75rem', color: 'var(--text-secondary, #64748b)', marginTop: '0.5rem', background: 'var(--bg-color, #f8fafc)', padding: '0.35rem 0.65rem', borderRadius: '4px' }}>
          💬 最新跟踪意见: {finding.latest_comment}
        </div>
      )}
    </div>
  );
}
