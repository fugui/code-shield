import React, { useState } from 'react';
import { TaskReportMeta, TaskNavigationContext } from '../../types/report';
import ReportExportMenu from './ReportExportMenu';
import { appNavigatePath } from '../../config';
import { copyToClipboardWithFallback } from '../../utils/reportUtils';
import { useToast } from '../Toast';

interface ReportHeaderProps {
  meta?: TaskReportMeta;
  isFullscreen: boolean;
  onToggleFullscreen?: () => void;
  onClose?: () => void;
  navigation?: TaskNavigationContext;
  exporting: boolean;
  onExport: (format: string, scope?: string) => void;
  onPrint: () => void;
}

export default function ReportHeader({
  meta,
  isFullscreen,
  onToggleFullscreen,
  onClose,
  navigation,
  exporting,
  onExport,
  onPrint,
}: ReportHeaderProps) {
  const { showToast } = useToast();
  const [copied, setCopied] = useState(false);

  const getReportUrl = () => {
    if (!meta?.id) return '';
    return `${window.location.origin}${appNavigatePath(`/public/report/${meta.id}`)}`;
  };

  const handleOpenExternal = () => {
    const url = getReportUrl();
    if (url) {
      window.open(url, '_blank');
    }
  };

  const handleCopyLink = async () => {
    const url = getReportUrl();
    if (!url) return;
    const ok = await copyToClipboardWithFallback(url);
    if (ok) {
      setCopied(true);
      showToast('已复制报告快速直达链接 (URL) 到剪贴板！', 'success');
      setTimeout(() => setCopied(false), 2000);
    } else {
      showToast('复制链接失败，请手动复制', 'error');
    }
  };

  return (
    <div className="report-header-bar">
      {/* 左侧元信息 */}
      <div className="report-title-meta">
        <span className="report-main-title">任务报告 #{meta?.id || ''}</span>
        {meta?.repo_name && (
          <span className="report-repo-tag" title={meta.repo_name}>
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ opacity: 0.7 }}>
              <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20" />
              <path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z" />
            </svg>
            <span>{meta.repo_name}</span>
            {meta.branch && <span style={{ opacity: 0.6 }}>({meta.branch})</span>}
          </span>
        )}
        {meta?.score !== undefined && (
          <span className="report-rating-badge">
            <span className="rating-dot" />
            风险分: {meta.score} 分 (估值)
          </span>
        )}
      </div>

      {/* 右侧操作栏 */}
      <div className="report-header-actions">
        {/* 任务前后切换导航 (Segmented 对) */}
        {navigation && (
          <div className="nav-segmented-group no-print">
            <button
              className="nav-btn segment-btn"
              disabled={!navigation.prevTaskId}
              onClick={() => navigation.prevTaskId && navigation.onNavigate?.(navigation.prevTaskId)}
              title="查看上一个任务报告"
            >
              ‹ 上一个
            </button>
            <button
              className="nav-btn segment-btn"
              disabled={!navigation.nextTaskId}
              onClick={() => navigation.nextTaskId && navigation.onNavigate?.(navigation.nextTaskId)}
              title="查看下一个任务报告"
            >
              下一个 ›
            </button>
          </div>
        )}

        {/* 页面直达与复制链接组 (独立页面在前，复制链接在后，纯图标呈现) */}
        {meta?.id && (
          <div className="header-action-group no-print">
            <button
              className="icon-action-btn"
              onClick={handleOpenExternal}
              title="在新标签页中打开该报告独立全屏页面"
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
                <polyline points="15 3 21 3 21 9" />
                <line x1="10" y1="14" x2="21" y2="3" />
              </svg>
            </button>
            <button
              className={`icon-action-btn ${copied ? 'btn-copied' : ''}`}
              onClick={handleCopyLink}
              title={copied ? "已复制直达链接！" : "复制报告专属直达链接 (URL)"}
            >
              {copied ? (
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#16a34a" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                  <polyline points="20 6 9 17 4 12" />
                </svg>
              ) : (
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
                  <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
                </svg>
              )}
            </button>
          </div>
        )}

        {/* 导出多格式报告下拉菜单 (核心 Primary Action) */}
        <ReportExportMenu
          taskId={meta?.id || 0}
          exporting={exporting}
          onExport={onExport}
          onPrint={onPrint}
        />

        {/* 全屏切换与关闭控制 */}
        <div className="header-action-group no-print" style={{ marginLeft: '0.25rem' }}>
          {onToggleFullscreen && (
            <button
              className="icon-tool-btn"
              onClick={onToggleFullscreen}
              title={isFullscreen ? '退出全屏 (还原抽屉)' : '全屏展开查看'}
            >
              {isFullscreen ? (
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <polyline points="4 14 10 14 10 20" />
                  <polyline points="20 10 14 10 14 4" />
                  <line x1="14" y1="10" x2="21" y2="3" />
                  <line x1="3" y1="21" x2="10" y2="14" />
                </svg>
              ) : (
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <polyline points="15 3 21 3 21 9" />
                  <polyline points="9 21 3 21 3 15" />
                  <line x1="21" y1="3" x2="14" y2="10" />
                  <line x1="3" y1="21" x2="10" y2="14" />
                </svg>
              )}
            </button>
          )}

          {onClose && (
            <button
              className="icon-tool-btn btn-close"
              onClick={onClose}
              title="关闭抽屉 (Esc)"
            >
              <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                <line x1="18" y1="6" x2="6" y2="18" />
                <line x1="6" y1="6" x2="18" y2="18" />
              </svg>
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

