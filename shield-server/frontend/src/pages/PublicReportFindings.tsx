import React, { useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';

interface Finding {
  id: number;
  task_report_id: number;
  task_type_id: number;
  repo_id: number;
  severity: string;
  category: string;
  file_path: string;
  line_number: string;
  code_snippet: string;
  title: string;
  detail: string;
  suggestion: string;
  status: string;
  assignee_id: string;
  feedback: string;
  feedback_at: string | null;
  created_at: string;
}

interface ReportDetails {
  id: number;
  repo_id: number;
  repo: {
    id: number;
    name: string;
    url: string;
    branch: string;
  };
  task_type_id: number;
  task_type: {
    id: number;
    name: string;
    display_name: string;
  };
  status: string;
  score: number;
  ai_summary: string;
  created_at: string;
}

const severityColors: Record<string, { color: string; bg: string }> = {
  '阻塞': { color: '#ef4444', bg: 'rgba(239, 68, 68, 0.1)' },
  'blocking': { color: '#ef4444', bg: 'rgba(239, 68, 68, 0.1)' },
  '严重': { color: '#f97316', bg: 'rgba(249, 115, 22, 0.1)' },
  'critical': { color: '#f97316', bg: 'rgba(249, 115, 22, 0.1)' },
  '高风险': { color: '#ef4444', bg: 'rgba(239, 68, 68, 0.1)' },
  'high': { color: '#ef4444', bg: 'rgba(239, 68, 68, 0.1)' },
  'high_risk': { color: '#ef4444', bg: 'rgba(239, 68, 68, 0.1)' },
  '主要': { color: '#eab308', bg: 'rgba(234, 179, 8, 0.1)' },
  'major': { color: '#eab308', bg: 'rgba(234, 179, 8, 0.1)' },
  '中风险': { color: '#f97316', bg: 'rgba(249, 115, 22, 0.1)' },
  'medium': { color: '#f97316', bg: 'rgba(249, 115, 22, 0.1)' },
  '提示': { color: '#3b82f6', bg: 'rgba(59, 130, 246, 0.1)' },
  'info': { color: '#3b82f6', bg: 'rgba(59, 130, 246, 0.1)' },
  '低风险': { color: '#3b82f6', bg: 'rgba(59, 130, 246, 0.1)' },
  'low': { color: '#3b82f6', bg: 'rgba(59, 130, 246, 0.1)' },
  '建议': { color: '#6b7280', bg: 'rgba(107, 114, 128, 0.1)' },
  'suggestion': { color: '#6b7280', bg: 'rgba(107, 114, 128, 0.1)' },
  '合格': { color: '#10b981', bg: 'rgba(16, 185, 129, 0.1)' },
  'pass': { color: '#10b981', bg: 'rgba(16, 185, 129, 0.1)' },
};

function PublicReportFindings() {
  const { reportId } = useParams<{ reportId: string }>();
  const [report, setReport] = useState<ReportDetails | null>(null);
  const [findings, setFindings] = useState<Finding[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const loadData = async () => {
      setLoading(true);
      setError(null);
      try {
        const reportRes = await fetch(`/api/public/tasks/${reportId}`);
        if (!reportRes.ok) {
          throw new Error('未找到该报告，或无访问权限');
        }
        const reportData = await reportRes.json();
        setReport(reportData);

        const findingsRes = await fetch(`/api/public/tasks/${reportId}/findings`);
        if (findingsRes.ok) {
          const findingsData = await findingsRes.json();
          setFindings(findingsData);
        }
      } catch (err: any) {
        setError(err.message || '加载报告数据失败');
      } finally {
        setLoading(false);
      }
    };
    loadData();
  }, [reportId]);

  const handlePrint = () => {
    window.print();
  };

  const getSeverityStyle = (severity: string) => {
    const sev = severity.toLowerCase();
    return severityColors[sev] || severityColors[severity] || { color: '#6b7280', bg: 'rgba(107, 114, 128, 0.1)' };
  };

  if (loading) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', minHeight: '100vh', background: '#f8fafc', fontFamily: 'system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif' }}>
        <div className="spinner" style={{ width: '40px', height: '40px', border: '4px solid #cbd5e1', borderTop: '4px solid #2563eb', borderRadius: '50%', animation: 'spin 1s linear infinite' }}></div>
        <p style={{ marginTop: '1rem', color: '#64748b', fontSize: '0.95rem' }}>正在为您加载完整的扫描分析发现清单...</p>
        <style>{`
          @keyframes spin { 0% { transform: rotate(0deg); } 100% { transform: rotate(360deg); } }
        `}</style>
      </div>
    );
  }

  if (error || !report) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', minHeight: '100vh', background: '#f8fafc', padding: '2rem', boxSizing: 'border-box', fontFamily: 'system-ui, -apple-system, sans-serif' }}>
        <div style={{ background: 'white', padding: '2.5rem', borderRadius: '12px', border: '1px solid #e2e8f0', boxShadow: '0 4px 6px -1px rgba(0,0,0,0.05)', textAlign: 'center', maxWidth: '450px' }}>
          <div style={{ width: '56px', height: '56px', borderRadius: '50%', background: '#fee2e2', color: '#ef4444', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: '1.75rem', margin: '0 auto 1.25rem' }}>✕</div>
          <h3 style={{ margin: '0 0 0.5rem', color: '#1e293b', fontSize: '1.25rem', fontWeight: 600 }}>加载失败</h3>
          <p style={{ color: '#64748b', fontSize: '0.9rem', lineHeight: 1.6, margin: '0 0 1.5rem' }}>{error || '无法获取报告详情。'}</p>
          <Link to="/" style={{ display: 'inline-block', background: '#2563eb', color: 'white', padding: '0.6rem 1.5rem', borderRadius: '6px', textDecoration: 'none', fontSize: '0.875rem', fontWeight: 500, boxShadow: '0 2px 4px rgba(37,99,235,0.2)' }}>返回主页</Link>
        </div>
      </div>
    );
  }

  // Count findings by severity
  const severityCounts: Record<string, number> = {};
  findings.forEach(f => {
    severityCounts[f.severity] = (severityCounts[f.severity] || 0) + 1;
  });

  return (
    <div style={{ background: '#f8fafc', minHeight: '100vh', padding: '2.5rem 2rem', boxSizing: 'border-box', fontFamily: 'system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif', color: '#1e293b' }}>
      
      {/* CSS Styles, including robust @media print configuration */}
      <style>{`
        .no-print {
          display: flex;
        }
        .public-container {
          max-width: 1100px;
          margin: 0 auto;
        }
        .findings-card {
          background: white;
          border: 1px solid #e2e8f0;
          border-radius: 12px;
          padding: 1.75rem;
          margin-bottom: 1.5rem;
          box-shadow: 0 1px 3px rgba(0, 0, 0, 0.02), 0 1px 2px rgba(0, 0, 0, 0.04);
          transition: transform 0.2s, box-shadow 0.2s;
        }
        .findings-card:hover {
          transform: translateY(-2px);
          box-shadow: 0 10px 15px -3px rgba(0, 0, 0, 0.03), 0 4px 6px -4px rgba(0, 0, 0, 0.03);
        }
        .code-container {
          background: #0f172a;
          color: #e2e8f0;
          padding: 1.25rem;
          border-radius: 8px;
          font-family: "JetBrains Mono", "Fira Code", Menlo, Monaco, Consolas, "Courier New", monospace;
          font-size: 0.825rem;
          line-height: 1.6;
          overflow-x: auto;
          margin: 1rem 0;
          border: 1px solid #1e293b;
        }
        .suggestion-box {
          background: #f0fdf4;
          border: 1px solid #bbf7d0;
          color: #15803d;
          padding: 1rem 1.25rem;
          border-radius: 8px;
          font-size: 0.875rem;
          line-height: 1.6;
          margin-top: 1rem;
        }
        .severity-badge {
          font-size: 0.75rem;
          padding: 0.25rem 0.6rem;
          border-radius: 6px;
          font-weight: 700;
          text-transform: uppercase;
          letter-spacing: 0.5px;
        }

        /* High-quality print output configuration */
        @media print {
          body {
            background: white !important;
            color: black !important;
            padding: 0 !important;
          }
          .no-print {
            display: none !important;
          }
          .public-container {
            max-width: 100% !important;
            margin: 0 !important;
            padding: 0 !important;
          }
          .findings-card {
            box-shadow: none !important;
            border: 1px solid #cbd5e1 !important;
            page-break-inside: avoid !important;
            margin-bottom: 2rem !important;
            padding: 1.5rem !important;
            background: white !important;
            -webkit-print-color-adjust: exact !important;
            print-color-adjust: exact !important;
          }
          .code-container {
            background: #f8fafc !important;
            color: #0f172a !important;
            border: 1px solid #cbd5e1 !important;
            white-space: pre-wrap !important;
            word-break: break-all !important;
            -webkit-print-color-adjust: exact !important;
            print-color-adjust: exact !important;
          }
          .suggestion-box {
            background: #f0fdf4 !important;
            border: 1px solid #86efac !important;
            color: #166534 !important;
            -webkit-print-color-adjust: exact !important;
            print-color-adjust: exact !important;
          }
          .severity-badge {
            border: 1px solid currentColor !important;
            -webkit-print-color-adjust: exact !important;
            print-color-adjust: exact !important;
          }
          @page {
            size: A4;
            margin: 1.6cm 1.2cm;
          }
        }
      `}</style>

      <div className="public-container">
        
        {/* Navigation Banner (Public View Indicator) */}
        <div className="no-print" style={{ justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            <span style={{ fontSize: '0.85rem', color: '#64748b', background: '#e2e8f0', padding: '0.25rem 0.6rem', borderRadius: '4px', fontWeight: 500 }}>
              公开报告只读视图
            </span>
          </div>
          <button 
            onClick={handlePrint}
            style={{ 
              display: 'flex', alignItems: 'center', gap: '0.5rem', background: '#2563eb', color: 'white',
              border: 'none', padding: '0.55rem 1.25rem', borderRadius: '8px', cursor: 'pointer',
              fontSize: '0.875rem', fontWeight: 600, boxShadow: '0 4px 6px -1px rgba(37,99,235,0.2)',
              transition: 'background-color 0.2s'
            }}
            onMouseEnter={e => e.currentTarget.style.backgroundColor = '#1d4ed8'}
            onMouseLeave={e => e.currentTarget.style.backgroundColor = '#2563eb'}
          >
            <svg width="16" height="16" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M17 17h2a2 2 0 002-2v-4a2 2 0 00-2-2H5a2 2 0 00-2 2v4a2 2 0 002 2h2m2 4h10a2 2 0 002-2v-4a2 2 0 00-2-2H9a2 2 0 00-2 2v4a2 2 0 002 2zm8-12V5a2 2 0 00-2-2H9a2 2 0 00-2 2v4h10z"></path>
            </svg>
            打印报告 / 导出 PDF
          </button>
        </div>

        {/* Premium Glassmorphic Header */}
        <div style={{ background: 'white', border: '1px solid #e2e8f0', borderRadius: '16px', padding: '2rem', marginBottom: '2rem', boxShadow: '0 4px 6px -1px rgba(0,0,0,0.02)' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', flexWrap: 'wrap', gap: '1.5rem' }}>
            <div style={{ flex: 1 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', marginBottom: '0.75rem' }}>
                <span style={{ fontSize: '0.75rem', background: 'rgba(37,99,235,0.08)', color: '#2563eb', padding: '0.25rem 0.5rem', borderRadius: '4px', fontWeight: 600 }}>
                  {report.task_type?.display_name || '代码检视'}
                </span>
                <span style={{ fontSize: '0.85rem', color: '#64748b' }}>
                  报告ID: #{report.id}
                </span>
              </div>
              <h1 style={{ margin: '0 0 0.5rem', fontSize: '1.5rem', fontWeight: 700, color: '#0f172a' }}>
                {report.repo?.name || '代码仓'}
              </h1>
              <div style={{ display: 'flex', gap: '1.5rem', color: '#64748b', fontSize: '0.875rem', flexWrap: 'wrap' }}>
                <span><strong>分支:</strong> {report.repo?.branch || 'main'}</span>
                <span><strong>时间:</strong> {report.created_at ? new Date(report.created_at).toLocaleString('zh-CN') : '-'}</span>
              </div>
            </div>

            {/* Score circle */}
            <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', background: '#f8fafc', padding: '1rem 1.5rem', borderRadius: '12px', border: '1px solid #f1f5f9' }}>
              <div style={{ textAlign: 'right' }}>
                <span style={{ display: 'block', fontSize: '0.75rem', color: '#64748b', fontWeight: 500, textTransform: 'uppercase' }}>质量评分</span>
                <span style={{ fontSize: '1.75rem', fontWeight: 800, color: report.score >= 80 ? '#10b981' : report.score >= 50 ? '#f59e0b' : '#ef4444' }}>{report.score}</span>
              </div>
              <div style={{ width: '4px', height: '36px', background: '#e2e8f0', borderRadius: '2px' }} />
              <div>
                <span style={{ display: 'block', fontSize: '0.75rem', color: '#64748b', fontWeight: 500 }}>全部问题</span>
                <span style={{ fontSize: '1.25rem', fontWeight: 700, color: '#0f172a' }}>{findings.length} 个</span>
              </div>
            </div>
          </div>

          {/* AI Summary */}
          {report.ai_summary && (
            <div style={{ marginTop: '1.5rem', paddingTop: '1.5rem', borderTop: '1px solid #f1f5f9' }}>
              <span style={{ display: 'block', fontSize: '0.8rem', fontWeight: 600, color: '#64748b', marginBottom: '0.5rem', textTransform: 'uppercase', letterSpacing: '0.5px' }}>检视摘要</span>
              <p style={{ margin: 0, fontSize: '0.9rem', color: '#334155', lineHeight: 1.7, background: '#f8fafc', padding: '1rem 1.25rem', borderRadius: '8px', border: '1px solid #f1f5f9' }}>
                {report.ai_summary}
              </p>
            </div>
          )}
        </div>

        {/* Severity Metrics Dashboard */}
        <div style={{ display: 'flex', gap: '1rem', marginBottom: '2rem', flexWrap: 'wrap' }}>
          {Object.entries(severityColors).filter(([k]) => !['blocking', 'critical', 'high', 'high_risk', 'major', 'medium', 'info', 'low', 'suggestion', 'pass'].includes(k)).map(([name, style]) => {
            const count = severityCounts[name] || 0;
            return (
              <div key={name} style={{ flex: '1 1 150px', background: 'white', border: '1px solid #e2e8f0', borderRadius: '12px', padding: '1rem 1.25rem', display: 'flex', flexDirection: 'column', gap: '0.25rem', boxShadow: '0 1px 2px rgba(0,0,0,0.01)' }}>
                <span style={{ fontSize: '0.8rem', color: '#64748b', fontWeight: 500 }}>{name}</span>
                <div style={{ display: 'flex', alignItems: 'baseline', gap: '0.5rem' }}>
                  <span style={{ fontSize: '1.5rem', fontWeight: 700, color: count > 0 ? style.color : '#94a3b8' }}>{count}</span>
                  <span style={{ fontSize: '0.75rem', color: '#94a3b8' }}>个</span>
                </div>
                <div style={{ height: '3px', background: count > 0 ? style.color : '#e2e8f0', borderRadius: '1.5px', marginTop: '0.25rem', opacity: count > 0 ? 1 : 0.4 }} />
              </div>
            );
          })}
        </div>

        {/* Section Heading */}
        <h2 style={{ fontSize: '1.1rem', fontWeight: 700, color: '#334155', margin: '0 0 1.25rem', display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <span style={{ width: '4px', height: '16px', background: '#2563eb', borderRadius: '2px', display: 'inline-block' }}></span>
          完整问题清单 ({findings.length})
        </h2>

        {/* Stacking Findings Cards */}
        {findings.length === 0 ? (
          <div style={{ background: 'white', border: '1px solid #e2e8f0', borderRadius: '12px', padding: '4rem 2rem', textAlign: 'center', color: '#64748b' }}>
            <div style={{ fontSize: '2.5rem', marginBottom: '1rem' }}>🎉</div>
            <h3 style={{ margin: '0 0 0.25rem', fontWeight: 600, color: '#1e293b' }}>未发现任何严重问题</h3>
            <p style={{ margin: 0, fontSize: '0.875rem' }}>本次代码检视结果良好，未检测出符合规则的问题。</p>
          </div>
        ) : (
          findings.map((f, index) => {
            const sevStyle = getSeverityStyle(f.severity);
            return (
              <div key={f.id} className="findings-card">
                {/* Header */}
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '1rem', borderBottom: '1px solid #f1f5f9', paddingBottom: '0.75rem', marginBottom: '1rem' }}>
                  <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center' }}>
                    <span style={{ fontSize: '0.8rem', color: '#64748b', fontWeight: 600 }}>
                      #{index + 1}
                    </span>
                    <span className="severity-badge" style={{ background: sevStyle.bg, color: sevStyle.color }}>
                      {f.severity}
                    </span>
                    {f.category && (
                      <span className="severity-badge" style={{ background: 'rgba(100,116,139,0.08)', color: '#64748b' }}>
                        {f.category}
                      </span>
                    )}
                  </div>
                  <span style={{ fontSize: '0.75rem', color: '#94a3b8', fontFamily: 'monospace' }}>
                    ID: {f.id}
                  </span>
                </div>

                {/* Title */}
                <h3 style={{ margin: '0 0 0.5rem', fontSize: '1.05rem', fontWeight: 700, color: '#0f172a', lineHeight: 1.4 }}>
                  {f.title}
                </h3>

                {/* Filepath and Line */}
                {f.file_path && (
                  <div style={{ fontSize: '0.8rem', color: '#64748b', fontFamily: 'monospace', background: '#f8fafc', padding: '0.4rem 0.75rem', borderRadius: '6px', border: '1px solid #f1f5f9', display: 'inline-block', width: '100%', boxSizing: 'border-box' }}>
                    📄 <strong>位置：</strong>{f.file_path}{f.line_number ? `:${f.line_number}` : ''}
                  </div>
                )}

                {/* Detail */}
                {f.detail && (
                  <div style={{ marginTop: '1rem' }}>
                    <div style={{ fontSize: '0.8rem', fontWeight: 600, color: '#64748b', marginBottom: '0.4rem', textTransform: 'uppercase', letterSpacing: '0.5px' }}>问题详情</div>
                    <div style={{ fontSize: '0.875rem', lineHeight: 1.7, color: '#334155', whiteSpace: 'pre-wrap' }}>
                      {f.detail}
                    </div>
                  </div>
                )}

                {/* Code Snippet */}
                {f.code_snippet && (
                  <div style={{ marginTop: '1rem' }}>
                    <div style={{ fontSize: '0.8rem', fontWeight: 600, color: '#64748b', marginBottom: '0.4rem', textTransform: 'uppercase', letterSpacing: '0.5px' }}>相关代码</div>
                    <pre className="code-container">
                      {f.code_snippet}
                    </pre>
                  </div>
                )}

                {/* Suggestion */}
                {f.suggestion && (
                  <div className="suggestion-box">
                    <strong style={{ display: 'block', marginBottom: '0.25rem', fontSize: '0.85rem' }}>💡 修复建议:</strong>
                    <div style={{ whiteSpace: 'pre-wrap' }}>{f.suggestion}</div>
                  </div>
                )}
              </div>
            );
          })
        )}

      </div>
    </div>
  );
}

export default PublicReportFindings;
