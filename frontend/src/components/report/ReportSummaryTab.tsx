import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { PrismLight as SyntaxHighlighter } from 'react-syntax-highlighter';
import { ghcolors } from 'react-syntax-highlighter/dist/esm/styles/prism';
import { TaskReportSummary } from '../../types/report';
import { formatDuration } from '../../utils/reportUtils';

// 常用语法高亮支持
import go from 'react-syntax-highlighter/dist/esm/languages/prism/go';
import python from 'react-syntax-highlighter/dist/esm/languages/prism/python';
import javascript from 'react-syntax-highlighter/dist/esm/languages/prism/javascript';
import typescript from 'react-syntax-highlighter/dist/esm/languages/prism/typescript';
import java from 'react-syntax-highlighter/dist/esm/languages/prism/java';
import c from 'react-syntax-highlighter/dist/esm/languages/prism/c';
import cpp from 'react-syntax-highlighter/dist/esm/languages/prism/cpp';
import json from 'react-syntax-highlighter/dist/esm/languages/prism/json';
import sql from 'react-syntax-highlighter/dist/esm/languages/prism/sql';

SyntaxHighlighter.registerLanguage('go', go);
SyntaxHighlighter.registerLanguage('python', python);
SyntaxHighlighter.registerLanguage('javascript', javascript);
SyntaxHighlighter.registerLanguage('typescript', typescript);
SyntaxHighlighter.registerLanguage('java', java);
SyntaxHighlighter.registerLanguage('c', c);
SyntaxHighlighter.registerLanguage('cpp', cpp);
SyntaxHighlighter.registerLanguage('json', json);
SyntaxHighlighter.registerLanguage('sql', sql);

interface ReportSummaryTabProps {
  summary: TaskReportSummary | null;
  loading: boolean;
}

export default function ReportSummaryTab({ summary, loading }: ReportSummaryTabProps) {
  if (loading && !summary) {
    return (
      <div style={{ textAlign: 'center', padding: '3rem', color: 'var(--text-secondary, #64748b)' }}>
        ⏳ 正在加载总结概览...
      </div>
    );
  }

  if (!summary) {
    return (
      <div style={{ textAlign: 'center', padding: '3rem', color: 'var(--text-secondary, #64748b)' }}>
        *暂无报告信息*
      </div>
    );
  }

  const { meta, metrics, markdown_content } = summary;
  const isEntityMode = meta.governance_mode === 'entity_assessment';

  const cardStyle: React.CSSProperties = {
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
  };

  return (
    <div>
      {/* KPI 指标卡片网格 (宽松四列/自适应网格) */}
      <div
        className="report-kpi-grid"
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fit, minmax(210px, 1fr))',
          gap: '1.25rem',
          marginBottom: '1.75rem',
        }}
      >
        <div className="report-kpi-card" style={cardStyle}>
          <span className="kpi-title" style={{ fontSize: '0.78rem', fontWeight: 600, color: '#64748b', textTransform: 'uppercase' }}>
            🎯 综合风险分
          </span>
          <span className="kpi-number" style={{ fontSize: '1.65rem', fontWeight: 700, color: '#d97706' }}>
            {meta.score} <span style={{ fontSize: '0.85rem', fontWeight: 500, color: '#64748b' }}>分 (估值)</span>
          </span>
        </div>

        <div className="report-kpi-card" style={cardStyle}>
          <span className="kpi-title" style={{ fontSize: '0.78rem', fontWeight: 600, color: '#64748b', textTransform: 'uppercase' }}>
            ⏳ 任务总耗时
          </span>
          <span className="kpi-number" style={{ fontSize: '1.65rem', fontWeight: 700, color: '#0f172a' }}>
            {formatDuration(meta.duration_seconds)}
          </span>
        </div>

        {isEntityMode ? (
          <>
            <div className="report-kpi-card" style={cardStyle}>
              <span className="kpi-title" style={{ fontSize: '0.78rem', fontWeight: 600, color: '#64748b', textTransform: 'uppercase' }}>
                📊 综合达标率
              </span>
              <span className="kpi-number" style={{ fontSize: '1.65rem', fontWeight: 700, color: (metrics.pass_rate || 0) >= 80 ? '#10b981' : '#eab308' }}>
                {(metrics.pass_rate || 0).toFixed(1)}%
              </span>
            </div>
            <div className="report-kpi-card" style={cardStyle}>
              <span className="kpi-title" style={{ fontSize: '0.78rem', fontWeight: 600, color: '#64748b', textTransform: 'uppercase' }}>
                📦 评估实体总数
              </span>
              <span className="kpi-number" style={{ fontSize: '1.65rem', fontWeight: 700, color: '#0f172a' }}>
                {metrics.total_findings} 个
              </span>
            </div>
          </>
        ) : (
          <>
            <div className="report-kpi-card" style={cardStyle}>
              <span className="kpi-title" style={{ fontSize: '0.78rem', fontWeight: 600, color: '#64748b', textTransform: 'uppercase' }}>
                ⚠️ 发现问题总数
              </span>
              <span className="kpi-number" style={{ fontSize: '1.65rem', fontWeight: 700, color: metrics.total_findings > 0 ? '#f97316' : '#10b981' }}>
                {metrics.total_findings} 个
              </span>
            </div>
            <div className="report-kpi-card" style={cardStyle}>
              <span className="kpi-title" style={{ fontSize: '0.78rem', fontWeight: 600, color: '#64748b', textTransform: 'uppercase' }}>
                🔴 高风险问题 (P0/P1)
              </span>
              <span className="kpi-number" style={{ fontSize: '1.65rem', fontWeight: 700, color: metrics.fatal_count + metrics.critical_count > 0 ? '#ef4444' : '#10b981' }}>
                {metrics.fatal_count + metrics.critical_count} 个
              </span>
            </div>
          </>
        )}
      </div>

      {/* Markdown 正文卡片容器 */}
      <div
        className="report-summary-markdown-card markdown-body"
        style={{
          background: '#ffffff',
          backgroundColor: '#ffffff',
          border: '1px solid #e2e8f0',
          borderRadius: '12px',
          padding: '2.25rem 2.75rem',
          boxShadow: '0 1px 3px rgba(0, 0, 0, 0.04)',
          lineHeight: 1.75,
          color: '#1e293b',
          boxSizing: 'border-box',
        }}
      >
        <ReactMarkdown
          remarkPlugins={[remarkGfm]}
          components={{
            code({ className, children, ...props }) {
              const match = /language-(\w+)/.exec(className || '');
              const codeString = String(children).replace(/\n$/, '');
              return match ? (
                <SyntaxHighlighter
                  style={ghcolors}
                  language={match[1]}
                  PreTag="div"
                  customStyle={{ borderRadius: '6px', fontSize: '85%', margin: '0.75rem 0' }}
                >
                  {codeString}
                </SyntaxHighlighter>
              ) : (
                <code className={className} {...props}>
                  {children}
                </code>
              );
            },
          }}
        >
          {markdown_content || '*暂无任何详细内容*'}
        </ReactMarkdown>
      </div>
    </div>
  );
}
