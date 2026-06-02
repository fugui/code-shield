import React, { useRef, useState, useEffect } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { PrismLight as SyntaxHighlighter } from 'react-syntax-highlighter';
import { ghcolors } from 'react-syntax-highlighter/dist/esm/styles/prism';
import { BASE_PATH } from '../config';

// 仅注册报告中常用的语言，避免打包全部 ~290 种语言定义
import go from 'react-syntax-highlighter/dist/esm/languages/prism/go';
import python from 'react-syntax-highlighter/dist/esm/languages/prism/python';
import javascript from 'react-syntax-highlighter/dist/esm/languages/prism/javascript';
import typescript from 'react-syntax-highlighter/dist/esm/languages/prism/typescript';
import java from 'react-syntax-highlighter/dist/esm/languages/prism/java';
import c from 'react-syntax-highlighter/dist/esm/languages/prism/c';
import cpp from 'react-syntax-highlighter/dist/esm/languages/prism/cpp';
import bash from 'react-syntax-highlighter/dist/esm/languages/prism/bash';
import json from 'react-syntax-highlighter/dist/esm/languages/prism/json';
import yaml from 'react-syntax-highlighter/dist/esm/languages/prism/yaml';
import sql from 'react-syntax-highlighter/dist/esm/languages/prism/sql';
import markdown from 'react-syntax-highlighter/dist/esm/languages/prism/markdown';

SyntaxHighlighter.registerLanguage('go', go);
SyntaxHighlighter.registerLanguage('python', python);
SyntaxHighlighter.registerLanguage('javascript', javascript);
SyntaxHighlighter.registerLanguage('typescript', typescript);
SyntaxHighlighter.registerLanguage('java', java);
SyntaxHighlighter.registerLanguage('c', c);
SyntaxHighlighter.registerLanguage('cpp', cpp);
SyntaxHighlighter.registerLanguage('bash', bash);
SyntaxHighlighter.registerLanguage('json', json);
SyntaxHighlighter.registerLanguage('yaml', yaml);
SyntaxHighlighter.registerLanguage('sql', sql);
SyntaxHighlighter.registerLanguage('markdown', markdown);

interface ReportSidebarProps {
  open: boolean;
  onClose: () => void;
  markdown: string;
  loading: boolean;
  reportId?: number;
}

export default function ReportSidebar({ open, onClose, markdown, loading, reportId }: ReportSidebarProps) {
  const markdownRef = useRef<HTMLDivElement>(null);
  
  const [activeTab, setActiveTab] = useState<'report' | 'diagnostic'>('report');
  const [summary, setSummary] = useState<any>(null);
  const [loadingSummary, setLoadingSummary] = useState(false);
  const [summaryError, setSummaryError] = useState<string | null>(null);
  const [expandedChunks, setExpandedChunks] = useState<Record<string, boolean>>({});

  useEffect(() => {
    if (!open) {
      setActiveTab('report');
      return;
    }
    if (reportId) {
      setLoadingSummary(true);
      setSummary(null);
      setSummaryError(null);
      setExpandedChunks({});
      fetch(`/api/tasks/${reportId}/summary`)
        .then(res => {
          if (!res.ok) throw new Error('未发现详细诊断数据（分片任务才会生成完整的 Summary 日志）');
          return res.json();
        })
        .then(data => {
          setSummary(data);
        })
        .catch(err => {
          console.error(err);
          setSummaryError(err.message || '加载任务诊断摘要失败');
        })
        .finally(() => {
          setLoadingSummary(false);
        });
    }
  }, [reportId, open]);

  const toggleChunk = (name: string) => {
    setExpandedChunks(prev => ({ ...prev, [name]: !prev[name] }));
  };

  const formatDuration = (seconds: number) => {
    if (seconds == null) return '-';
    const s = Math.round(seconds);
    return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
  };


  const handlePrint = () => {
    if (!markdownRef.current) return;
    const printWindow = window.open('', '_blank');
    if (!printWindow) return;
    printWindow.document.write(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>任务报告</title>
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif; line-height: 1.6; color: #24292f; max-width: 900px; margin: 0 auto; padding: 20px 40px; }
  h1, h2, h3, h4 { margin-top: 24px; margin-bottom: 16px; font-weight: 600; line-height: 1.25; }
  h2 { border-bottom: 1px solid #d0d7de; padding-bottom: .3em; }
  blockquote { padding: 0 1em; color: #57606a; border-left: .25em solid #d0d7de; margin: 0 0 16px 0; }
  pre { padding: 16px; overflow: auto; font-size: 85%; line-height: 1.45; background-color: #f6f8fa; border-radius: 6px; white-space: pre-wrap; word-wrap: break-word; }
  code { padding: .2em .4em; font-size: 85%; background-color: rgba(175,184,193,0.2); border-radius: 6px; }
  pre > code { padding: 0; font-size: 100%; background-color: transparent; }
  ul, ol { margin-top: 0; margin-bottom: 16px; padding-left: 2em; }
  table { border-collapse: collapse; width: 100%; margin-bottom: 16px; page-break-inside: avoid; }
  th, td { border: 1px solid #d0d7de; padding: 6px 13px; }
  th { background-color: #f6f8fa; font-weight: 600; }
</style>
</head><body>${markdownRef.current.innerHTML}</body></html>`);
    printWindow.document.close();
    printWindow.onafterprint = () => printWindow.close();
    printWindow.print();
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

  const handleDownloadJson = async () => {
    if (!reportId) return;
    try {
      const res = await fetch(`/api/tasks/${reportId}/synthesis`);
      if (!res.ok) {
        alert('无法获取问题记录文件，请确认该文件是否存在。');
        return;
      }
      const blob = await res.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `report-${reportId}-synthesis.json`;
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      console.error('Failed to download synthesis json:', err);
      alert('下载文件时发生错误。');
    }
  };

  const handleOpenPublicDetails = () => {
    if (!reportId) return;
    window.open(`${BASE_PATH}/public/reports/${reportId}`, '_blank');
  };

  const handleCopyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text);
    alert('已复制报错日志到剪贴板');
  };

  const showActions = !loading && !!markdown;

  return (
    <>
      {/* Sidebar Drawer */}
      <div className="report-sidebar-drawer" style={{ position: 'fixed', top: 0, right: open ? 0 : '-50vw', width: '50vw', height: '100vh', background: 'var(--bg-color)', boxShadow: '-4px 0 15px rgba(0,0,0,0.1)', transition: 'right 0.3s ease-in-out', zIndex: 1000, display: 'flex', flexDirection: 'column' }}>
        
        {/* Header with Tab Navigation */}
        <div style={{ borderBottom: '1px solid var(--border-color)', background: 'var(--bg-color)' }}>
          <div className="report-sidebar-header" style={{ padding: '1.25rem 1.5rem 0.75rem 1.5rem', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <h3 style={{ margin: 0, fontSize: '1.15rem', color: 'var(--text-color)' }}>任务报告详情</h3>
            <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              {showActions && (
                <>
                  <button
                    onClick={handlePrint}
                    style={{ background: 'transparent', border: '1px solid var(--primary-color)', cursor: 'pointer', padding: '0.3rem 0.7rem', borderRadius: '4px', color: 'var(--primary-color)', fontSize: '0.825rem', display: 'flex', alignItems: 'center', gap: '0.35rem' }}
                    title={'打印或保存为 PDF（在打印对话框中选择"另存为 PDF"）'}
                  >
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                      <polyline points="6 9 6 2 18 2 18 9"/><path d="M6 18H4a2 2 0 01-2-2v-5a2 2 0 012-2h16a2 2 0 012 2v5a2 2 0 01-2 2h-2"/><rect x="6" y="14" width="12" height="8"/>
                    </svg>
                    打印 / PDF
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
                  {reportId && (
                    <button
                      onClick={handleDownloadJson}
                      style={{ background: 'transparent', border: '1px solid #0284c7', cursor: 'pointer', padding: '0.3rem 0.7rem', borderRadius: '4px', color: '#0284c7', fontSize: '0.825rem', display: 'flex', alignItems: 'center', gap: '0.35rem' }}
                      title="下载全部问题记录 (JSON)"
                    >
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <rect x="3" y="3" width="18" height="18" rx="2" ry="2"/><line x1="9" y1="9" x2="15" y2="9"/><line x1="9" y1="13" x2="15" y2="13"/><line x1="9" y1="17" x2="15" y2="17"/>
                      </svg>
                      下载 JSON
                    </button>
                  )}
                  {reportId && (
                    <button
                      onClick={handleOpenPublicDetails}
                      style={{ 
                        background: 'var(--primary-color)', 
                        border: 'none', 
                        cursor: 'pointer', 
                        padding: '0.3rem 0.8rem', 
                        borderRadius: '4px', 
                        color: 'white', 
                        fontSize: '0.825rem', 
                        display: 'flex', 
                        alignItems: 'center', 
                        gap: '0.35rem',
                        fontWeight: 600,
                        boxShadow: '0 2px 4px rgba(37,99,235,0.1)'
                      }}
                      title="在新窗口查看全部问题详情及打印导出排版"
                    >
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                        <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"></path>
                        <polyline points="15 3 21 3 21 9"></polyline>
                        <line x1="10" y1="14" x2="21" y2="3"></line>
                      </svg>
                      查看详情
                    </button>
                  )}
                </>
              )}
              <button onClick={onClose} style={{ background: 'transparent', border: 'none', cursor: 'pointer', opacity: 0.6, fontSize: '1.5rem', color: 'var(--text-color)', display: 'flex', alignItems: 'center', justifyContent: 'center', width: '28px', height: '28px' }}>&times;</button>
            </div>
          </div>

          {/* Navigation Tabs */}
          <div className="diag-tabs">
            <button className={`diag-tab-btn ${activeTab === 'report' ? 'active' : ''}`} onClick={() => setActiveTab('report')}>
              📑 审计报告正文
            </button>
            <button className={`diag-tab-btn ${activeTab === 'diagnostic' ? 'active' : ''}`} onClick={() => setActiveTab('diagnostic')}>
              🔬 运行轨迹与诊断
            </button>
          </div>
        </div>

        {/* Content Body */}
        <div style={{ padding: '1.5rem', overflowY: 'auto', flex: 1, backgroundColor: activeTab === 'diagnostic' ? '#f8fafc' : '#ffffff' }}>
          
          {activeTab === 'report' ? (
            /* Tab 1: Markdown Report */
            loading ? (
              <div style={{ textAlign: 'center', marginTop: '3rem', color: '#64748b' }}>
                <span className="report-sidebar-spinner" /> 正在渲染 Markdown...
              </div>
            ) : (
              <div className="markdown-body" ref={markdownRef}>
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
                          customStyle={{ borderRadius: '6px', fontSize: '85%', margin: '0' }}
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
                  {markdown || '*暂无任何报告信息*'}
                </ReactMarkdown>
              </div>
            )
          ) : (
            /* Tab 2: Diagnostic & Trace Logs */
            loadingSummary ? (
              <div style={{ textAlign: 'center', marginTop: '3rem', color: '#64748b' }}>
                <span className="report-sidebar-spinner" /> 正在获取执行轨迹数据...
              </div>
            ) : summaryError ? (
              <div style={{ padding: '2rem', textAlign: 'center', background: '#fff', borderRadius: '8px', border: '1px solid #fee2e2', color: '#b91c1c' }}>
                <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" style={{ marginBottom: '0.5rem' }}>
                  <circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>
                </svg>
                <div style={{ fontWeight: 600 }}>无法获取执行轨迹数据</div>
                <div style={{ fontSize: '0.8rem', opacity: 0.8, marginTop: '0.25rem' }}>{summaryError}</div>
              </div>
            ) : summary ? (
              <div>
                
                {/* 1. KPI Cards */}
                <div className="kpi-container">
                  <div className="kpi-card">
                    <span className="kpi-label">⏳ 任务总耗时</span>
                    <span className="kpi-val">{formatDuration(summary.duration_seconds)}</span>
                  </div>
                  <div className="kpi-card">
                    <span className="kpi-label">🎯 静态分析耗时</span>
                    <span className="kpi-val">{formatDuration(summary.analysis?.duration_seconds)}</span>
                  </div>
                  <div className="kpi-card">
                    <span className="kpi-label">🧩 分片分析进度</span>
                    <span className="kpi-val">
                      {summary.analysis?.success_chunks} / {summary.analysis?.total_chunks}
                      {summary.analysis?.failed_chunks > 0 && (
                        <span style={{ color: '#ef4444', fontSize: '0.75rem', marginLeft: '0.25rem' }}>
                          ({summary.analysis.failed_chunks} 失败)
                        </span>
                      )}
                    </span>
                  </div>
                  <div className="kpi-card">
                    <span className="kpi-label">⚠️ 发现问题总数</span>
                    <span className="kpi-val" style={{ color: summary.analysis?.total_findings > 0 ? '#ea580c' : '#10b981' }}>
                      {summary.analysis?.total_findings ?? 0} 个
                    </span>
                  </div>
                </div>

                {/* 2. Visual Pipeline Timeline */}
                <h4 style={{ margin: '0 0 0.75rem 0', color: '#334155', fontSize: '0.9rem' }}>🏃 执行阶段时序流</h4>
                <div className="diag-pipeline">
                  <div className={`pipeline-step success`}>
                    <div className="pipeline-dot success">✓</div>
                    <span className="pipeline-label">初始化克隆</span>
                    <span className="pipeline-sub">已完成</span>
                  </div>
                  <div className={`pipeline-step success`}>
                    <div className="pipeline-dot success">✓</div>
                    <span className="pipeline-label">前置预检查</span>
                    <span className="pipeline-sub">已跳过/完成</span>
                  </div>
                  <div className={`pipeline-step ${summary.analysis?.status === 'success' ? 'success' : summary.analysis?.status === 'failed' ? 'failed' : 'active'}`}>
                    <div className={`pipeline-dot ${summary.analysis?.status === 'success' ? 'success' : summary.analysis?.status === 'failed' ? 'failed' : 'active'}`}>
                      {summary.analysis?.status === 'success' ? '✓' : summary.analysis?.status === 'failed' ? '✗' : '...'}
                    </div>
                    <span className="pipeline-label">分片静态分析</span>
                    <span className="pipeline-sub">{formatDuration(summary.analysis?.duration_seconds)}</span>
                  </div>
                  <div className={`pipeline-step ${summary.synthesis?.status === 'success' ? 'success' : summary.synthesis?.status === 'failed' ? 'failed' : ''}`}>
                    <div className={`pipeline-dot ${summary.synthesis?.status === 'success' ? 'success' : summary.synthesis?.status === 'failed' ? 'failed' : ''}`}>
                      {summary.synthesis?.status === 'success' ? '✓' : summary.synthesis?.status === 'failed' ? '✗' : '-'}
                    </div>
                    <span className="pipeline-label">综合报告生成</span>
                    <span className="pipeline-sub">{formatDuration(summary.synthesis?.duration_seconds)}</span>
                  </div>
                </div>

                {/* 3. Chunk details browser */}
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', margin: '1rem 0 0.75rem 0' }}>
                  <h4 style={{ margin: 0, color: '#334155', fontSize: '0.9rem' }}>📦 静态扫描分片详情 ({summary.analysis?.chunks?.length ?? 0} 个)</h4>
                  <span style={{ fontSize: '0.75rem', color: '#64748b' }}>模式: {summary.engine_mode === 'chunked' ? '分片扫描 (Chunked)' : '单次扫描 (Single)'}</span>
                </div>

                {(!summary.analysis?.chunks || summary.analysis.chunks.length === 0) ? (
                  <div style={{ padding: '2rem', textAlign: 'center', background: '#fff', borderRadius: '8px', border: '1px solid #e2e8f0', color: '#64748b', fontSize: '0.875rem' }}>
                    该任务未使用分片分析引擎运行。
                  </div>
                ) : (
                  summary.analysis.chunks.map((chunk: any) => {
                    const isFailed = chunk.status === 'failed';
                    const expanded = !!expandedChunks[chunk.chunk_name];
                    return (
                      <div key={chunk.chunk_name} className={`chunk-card ${isFailed ? 'failed' : 'success'}`}>
                        <div className="chunk-header" onClick={() => toggleChunk(chunk.chunk_name)}>
                          <div className="chunk-title">
                            <span style={{ color: isFailed ? '#ef4444' : '#10b981', display: 'flex', alignItems: 'center' }}>
                              {isFailed ? (
                                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
                              ) : (
                                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5"><polyline points="20 6 9 17 4 12"/></svg>
                              )}
                            </span>
                            <span>{chunk.chunk_name || '未命名分片'}</span>
                          </div>
                          <div className="chunk-meta">
                            <span>⏱️ {formatDuration(chunk.duration_seconds)}</span>
                            {chunk.attempts > 1 && <span style={{ background: '#fee2e2', color: '#b91c1c', padding: '0.05rem 0.3rem', borderRadius: '4px', fontSize: '0.7rem', fontWeight: 600 }}>尝试: {chunk.attempts}</span>}
                            <span>{expanded ? '▼' : '▶'}</span>
                          </div>
                        </div>
                        {expanded && (
                          <div className="chunk-body">
                            <div style={{ fontWeight: 600, color: '#475569', marginBottom: '0.25rem', fontSize: '0.75rem' }}>📂 处理的文件列表 ({chunk.files?.length ?? 0}):</div>
                            <ul className="chunk-files-list">
                              {chunk.files && chunk.files.length > 0 ? (
                                chunk.files.map((file: string, idx: number) => (
                                  <li key={idx} className="chunk-file-item">
                                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" style={{ color: '#94a3b8' }}><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><polyline points="13 2 13 9 20 9"/></svg>
                                    {file}
                                  </li>
                                ))
                              ) : (
                                <li style={{ color: '#94a3b8', fontSize: '0.75rem', fontStyle: 'italic' }}>无文件</li>
                              )}
                            </ul>
                            
                            {isFailed && chunk.error_message && (
                              <div style={{ marginTop: '0.75rem' }}>
                                <div style={{ fontWeight: 600, color: '#b91c1c', marginBottom: '0.25rem', fontSize: '0.75rem' }}>🚨 报错信息诊断:</div>
                                <div className="chunk-error-msg">
                                  <button className="copy-btn" onClick={(e) => { e.stopPropagation(); handleCopyToClipboard(chunk.error_message); }}>
                                    复制日志
                                  </button>
                                  {chunk.error_message}
                                </div>
                              </div>
                            )}
                          </div>
                        )}
                      </div>
                    );
                  })
                )}
              </div>
            ) : (
              <div style={{ padding: '2rem', textAlign: 'center', background: '#fff', borderRadius: '8px', color: '#64748b', fontSize: '0.875rem' }}>
                暂无诊断信息。
              </div>
            )
          )}
        </div>
      </div>

      {/* Backdrop */}
      {open && (
        <div className="report-sidebar-backdrop" onClick={onClose} style={{ position: 'fixed', top: 0, left: 0, right: 0, bottom: 0, background: 'rgba(0,0,0,0.5)', zIndex: 999 }} />
      )}

      <style>{`
        .report-sidebar-spinner { display: inline-block; width: 12px; height: 12px; border: 2px solid rgba(100, 116, 139, 0.3); border-radius: 50%; border-top-color: var(--primary-color); animation: report-sidebar-spin 1s ease-in-out infinite; vertical-align: middle; margin-right: 5px; }
        @keyframes report-sidebar-spin { to { transform: rotate(360deg); } }
        
        /* Premium Tab Styles */
        .diag-tabs { display: flex; border-bottom: 1px solid var(--border-color); background: var(--bg-color); padding: 0 1.5rem; }
        .diag-tab-btn { background: transparent; border: none; padding: 0.75rem 1.25rem; font-size: 0.875rem; font-weight: 500; color: #64748b; cursor: pointer; border-bottom: 2px solid transparent; transition: all 0.2s ease; outline: none; margin-bottom: -1px; }
        .diag-tab-btn:hover { color: var(--primary-color); }
        .diag-tab-btn.active { color: var(--primary-color); border-bottom-color: var(--primary-color); font-weight: 600; }
        
        /* KPI Cards Styles */
        .kpi-container { display: flex; gap: 0.75rem; margin-bottom: 1.25rem; flex-wrap: wrap; }
        .kpi-card { background: #fff; border: 1px solid #e2e8f0; border-radius: 8px; padding: 0.75rem; display: flex; flex-direction: column; gap: 0.2rem; flex: 1; min-width: 120px; box-shadow: 0 1px 2px 0 rgba(0, 0, 0, 0.03); transition: all 0.2s ease; }
        .kpi-card:hover { transform: translateY(-2px); box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.05); border-color: #cbd5e1; }
        .kpi-label { font-size: 0.7rem; color: #64748b; font-weight: 500; display: flex; align-items: center; gap: 0.25rem; }
        .kpi-val { font-size: 1.1rem; font-weight: 700; color: #0f172a; }
        
        /* Workflow Timeline Styles */
        .diag-pipeline { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; margin-bottom: 1.5rem; padding: 1rem 0.5rem; background: #fff; border-radius: 8px; border: 1px solid #e2e8f0; }
        .pipeline-step { display: flex; flex-direction: column; align-items: center; flex: 1; position: relative; min-width: 70px; }
        .pipeline-step:not(:last-child)::after { content: ''; position: absolute; top: 12px; left: calc(50% + 15px); width: calc(100% - 30px); height: 2px; background: #e2e8f0; z-index: 1; }
        .pipeline-step.success:not(:last-child)::after { background: #10b981; }
        .pipeline-dot { width: 24px; height: 24px; border-radius: 50%; background: #e2e8f0; color: #64748b; display: flex; align-items: center; justify-content: center; font-size: 0.7rem; font-weight: bold; z-index: 2; border: 2px solid #fff; box-shadow: 0 0 0 1px #cbd5e1; }
        .pipeline-dot.success { background: #d1fae5; color: #065f46; box-shadow: 0 0 0 1px #10b981; }
        .pipeline-dot.failed { background: #fee2e2; color: #991b1b; box-shadow: 0 0 0 1px #ef4444; }
        .pipeline-dot.active { background: #fef3c7; color: #92400e; box-shadow: 0 0 0 1px #f59e0b; animation: pipeline-pulse 1.5s infinite; }
        .pipeline-label { font-size: 0.7rem; font-weight: 600; margin-top: 0.4rem; color: #334155; text-align: center; }
        .pipeline-sub { font-size: 0.6rem; color: #64748b; margin-top: 0.05rem; }
        @keyframes pipeline-pulse { 0% { transform: scale(1); } 50% { transform: scale(1.08); } 100% { transform: scale(1); } }
        
        /* Chunk Cards Styles */
        .chunk-card { background: #fff; border: 1px solid #e2e8f0; border-radius: 8px; margin-bottom: 0.6rem; overflow: hidden; transition: all 0.2s ease; box-shadow: 0 1px 2px 0 rgba(0, 0, 0, 0.02); }
        .chunk-card:hover { border-color: #cbd5e1; box-shadow: 0 3px 6px -1px rgba(0,0,0,0.05); }
        .chunk-card.failed { border-left: 3px solid #ef4444; }
        .chunk-card.success { border-left: 3px solid #10b981; }
        .chunk-header { display: flex; align-items: center; justify-content: space-between; padding: 0.6rem 0.85rem; cursor: pointer; user-select: none; background: #fff; }
        .chunk-header:hover { background: #f8fafc; }
        .chunk-title { font-size: 0.8rem; font-weight: 600; color: var(--text-color); display: flex; align-items: center; gap: 0.4rem; }
        .chunk-meta { display: flex; align-items: center; gap: 0.6rem; font-size: 0.7rem; color: #64748b; }
        .chunk-body { padding: 0.85rem; background: #f8fafc; border-top: 1px solid #f1f5f9; font-size: 0.775rem; }
        .chunk-files-list { list-style: none; padding: 0; margin: 0; }
        .chunk-file-item { display: flex; align-items: center; gap: 0.35rem; padding: 0.2rem 0; color: #475569; font-family: monospace; font-size: 0.7rem; word-break: break-all; }
        .chunk-error-msg { background: #0f172a; color: #f87171; font-family: monospace; font-size: 0.7rem; padding: 0.6rem 0.85rem; border-radius: 6px; margin-top: 0.4rem; overflow-x: auto; white-space: pre-wrap; word-break: break-all; position: relative; }
        .copy-btn { position: absolute; top: 0.4rem; right: 0.4rem; background: rgba(255,255,255,0.1); border: none; color: #fff; padding: 0.15rem 0.35rem; border-radius: 4px; font-size: 0.6rem; cursor: pointer; transition: all 0.2s; }
        .copy-btn:hover { background: rgba(255,255,255,0.25); }

        .markdown-body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif; line-height: 1.6; color: #24292f; }
        .markdown-body h1, .markdown-body h2, .markdown-body h3, .markdown-body h4 { margin-top: 24px; margin-bottom: 16px; font-weight: 600; line-height: 1.25; }
        .markdown-body h2 { border-bottom: 1px solid #d0d7de; padding-bottom: .3em; }
        .markdown-body blockquote { padding: 0 1em; color: #57606a; border-left: .25em solid #d0d7de; margin: 0 0 16px 0; }
        .markdown-body pre { padding: 16px; overflow: auto; font-size: 85%; line-height: 1.45; background-color: #f6f8fa; border-radius: 6px; }
        .markdown-body code { padding: .2em .4em; margin: 0; font-size: 85%; background-color: rgba(175, 184, 193, 0.2); border-radius: 6px; }
        .markdown-body pre > code { padding: 0; margin: 0; font-size: 100%; background-color: transparent; border: 0; }
        .markdown-body ul, .markdown-body ol { margin-top: 0; margin-bottom: 16px; padding-left: 2em; }
        .markdown-body table { border-collapse: collapse; width: 100%; margin-bottom: 16px; }
        .markdown-body th, .markdown-body td { border: 1px solid #d0d7de; padding: 6px 13px; }
        .markdown-body th { background-color: #f6f8fa; font-weight: 600; }
      `}</style>
    </>
  );
}

