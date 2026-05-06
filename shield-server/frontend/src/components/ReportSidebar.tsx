import React, { useRef, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import html2pdf from 'html2pdf.js';

interface ReportSidebarProps {
  open: boolean;
  onClose: () => void;
  markdown: string;
  loading: boolean;
}

export default function ReportSidebar({ open, onClose, markdown, loading }: ReportSidebarProps) {
  const [savingPdf, setSavingPdf] = useState(false);
  const markdownRef = useRef<HTMLDivElement>(null);

  const handleDownloadPdf = async () => {
    if (!markdownRef.current || savingPdf) return;
    setSavingPdf(true);
    try {
      await html2pdf().set({
        margin: [10, 10, 10, 10],
        filename: `report-${Date.now()}.pdf`,
        image: { type: 'jpeg', quality: 0.98 },
        html2canvas: { scale: 2, useCORS: true },
        jsPDF: { unit: 'mm', format: 'a4', orientation: 'portrait' },
        pagebreak: { mode: ['avoid-all', 'css', 'legacy'] },
      } as any).from(markdownRef.current).save();
    } catch (err) {
      console.error('PDF generation failed:', err);
    } finally {
      setSavingPdf(false);
    }
  };

  const handleDownloadMd = () => {
    const blob = new Blob([markdown], { type: 'text/markdown;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `report-${Date.now()}.md`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const showActions = !loading && !!markdown;

  return (
    <>
      {/* Sidebar Drawer */}
      <div style={{ position: 'fixed', top: 0, right: open ? 0 : '-50vw', width: '50vw', height: '100vh', background: 'var(--bg-color)', boxShadow: '-4px 0 15px rgba(0,0,0,0.1)', transition: 'right 0.3s ease-in-out', zIndex: 1000, display: 'flex', flexDirection: 'column' }}>
        {/* Header */}
        <div style={{ padding: '1.5rem', borderBottom: '1px solid var(--border-color)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <h3 style={{ margin: 0 }}>任务报告详情</h3>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
            {showActions && (
              <>
                <button
                  onClick={handleDownloadPdf}
                  disabled={savingPdf}
                  style={{ background: 'transparent', border: '1px solid var(--primary-color)', cursor: savingPdf ? 'wait' : 'pointer', padding: '0.3rem 0.7rem', borderRadius: '4px', color: 'var(--primary-color)', fontSize: '0.825rem', display: 'flex', alignItems: 'center', gap: '0.35rem', opacity: savingPdf ? 0.6 : 1 }}
                  title="保存为 PDF 文件"
                >
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/>
                  </svg>
                  {savingPdf ? '生成中...' : '下载 PDF'}
                </button>
                <button
                  onClick={handleDownloadMd}
                  style={{ background: 'transparent', border: '1px solid #64748b', cursor: 'pointer', padding: '0.3rem 0.7rem', borderRadius: '4px', color: '#64748b', fontSize: '0.825rem', display: 'flex', alignItems: 'center', gap: '0.35rem' }}
                  title="下载原始 Markdown 文件"
                >
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/>
                  </svg>
                  下载 MD
                </button>
              </>
            )}
            <button onClick={onClose} style={{ background: 'transparent', border: 'none', cursor: 'pointer', opacity: 0.6, fontSize: '1.5rem', color: 'var(--text-color)' }}>&times;</button>
          </div>
        </div>

        {/* Content */}
        <div style={{ padding: '2rem', overflowY: 'auto', flex: 1, backgroundColor: '#ffffff' }}>
          {loading ? (
            <div style={{ textAlign: 'center', marginTop: '3rem', color: '#64748b' }}>
              <span className="report-sidebar-spinner" /> 正在渲染 Markdown...
            </div>
          ) : (
            <div className="markdown-body" ref={markdownRef}>
              <ReactMarkdown remarkPlugins={[remarkGfm]}>
                {markdown || '*暂无任何报告信息*'}
              </ReactMarkdown>
            </div>
          )}
        </div>
      </div>

      {/* Backdrop */}
      {open && (
        <div onClick={onClose} style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, background: 'rgba(0,0,0,0.5)', zIndex: 999 }} />
      )}

      <style>{`
        .report-sidebar-spinner { display: inline-block; width: 12px; height: 12px; border: 2px solid rgba(100, 116, 139, 0.3); border-radius: 50%; border-top-color: var(--primary-color); animation: report-sidebar-spin 1s ease-in-out infinite; vertical-align: middle; margin-right: 5px; }
        @keyframes report-sidebar-spin { to { transform: rotate(360deg); } }
        .markdown-body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif; line-height: 1.6; color: #24292f; }
        .markdown-body h1, .markdown-body h2, .markdown-body h3, .markdown-body h4 { margin-top: 24px; margin-bottom: 16px; font-weight: 600; line-height: 1.25; }
        .markdown-body h2 { border-bottom: 1px solid #d0d7de; padding-bottom: .3em; }
        .markdown-body blockquote { padding: 0 1em; color: #57606a; border-left: .25em solid #d0d7de; margin: 0 0 16px 0; }
        .markdown-body pre { padding: 16px; overflow: auto; font-size: 85%; line-height: 1.45; background-color: #f6f8fa; border-radius: 6px; }
        .markdown-body code { padding: .2em .4em; margin: 0; font-size: 85%; background-color: rgba(175, 184, 193, 0.2); border-radius: 6px; }
        .markdown-body pre > code { padding: 0; margin: 0; font-size: 100%; background-color: transparent; border: 0; }
        .markdown-body ul, .markdown-body ol { margin-top: 0; margin-bottom: 16px; padding-left: 2em; }
      `}</style>
    </>
  );
}
