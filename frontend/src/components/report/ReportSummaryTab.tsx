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
      <div className="report-loading">
        ⏳ 正在加载总结概览...
      </div>
    );
  }

  if (!summary) {
    return (
      <div className="report-loading">
        *暂无报告信息*
      </div>
    );
  }

  const { meta, metrics, markdown_content } = summary;
  const isEntityMode = meta.governance_mode === 'entity_assessment';

  return (
    <div>
      {/* KPI 指标卡片网格 (宽松四列/自适应网格) */}
      <div className="report-kpi-grid">
        <div className="report-kpi-card">
          <span className="kpi-title">
            🎯 综合风险分
          </span>
          <span className="kpi-number" style={{ color: '#d97706' }}>
            {meta.score} <span style={{ fontSize: '0.85rem', fontWeight: 500, color: '#64748b' }}>分 (估值)</span>
          </span>
        </div>

        <div className="report-kpi-card">
          <span className="kpi-title">
            ⏳ 任务总耗时
          </span>
          <span className="kpi-number">
            {formatDuration(meta.duration_seconds)}
          </span>
        </div>

        {isEntityMode ? (
          <>
            <div className="report-kpi-card">
              <span className="kpi-title">
                📊 综合达标率
              </span>
              <span className="kpi-number" style={{ color: (metrics.pass_rate || 0) >= 80 ? '#10b981' : '#eab308' }}>
                {(metrics.pass_rate || 0).toFixed(1)}%
              </span>
            </div>
            <div className="report-kpi-card">
              <span className="kpi-title">
                📦 评估实体总数
              </span>
              <span className="kpi-number">
                {metrics.total_findings} 个
              </span>
            </div>
          </>
        ) : (
          <>
            <div className="report-kpi-card">
              <span className="kpi-title">
                ⚠️ 发现问题总数
              </span>
              <span className="kpi-number" style={{ color: metrics.total_findings > 0 ? '#f97316' : '#10b981' }}>
                {metrics.total_findings} 个
              </span>
            </div>
            <div className="report-kpi-card">
              <span className="kpi-title">
                🔴 高风险问题 (P0/P1)
              </span>
              <span className="kpi-number" style={{ color: metrics.fatal_count + metrics.critical_count > 0 ? '#ef4444' : '#10b981' }}>
                {metrics.fatal_count + metrics.critical_count} 个
              </span>
            </div>
          </>
        )}
      </div>

      {/* Markdown 正文卡片容器 */}
      <div className="report-summary-markdown-card markdown-body">
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
