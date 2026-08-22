import React from 'react';
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

  const getReportUrl = () => {
    if (!meta?.id) return '';
    return `${window.location.origin}${appNavigatePath(`/public/report/${meta.id}`)}`;
  };

  const handleCopyLink = async () => {
    const url = getReportUrl();
    if (!url) return;
    const ok = await copyToClipboardWithFallback(url);
    if (ok) {
      showToast('已复制报告快速链接到剪贴板！', 'success');
    } else {
      showToast('复制链接失败，请手动复制', 'error');
    }
  };

  const handleOpenExternal = () => {
    const url = getReportUrl();
    if (url) {
      window.open(url, '_blank');
    }
  };

  return (
    <div className="report-header-bar">
      {/* 左侧元信息 */}
      <div className="report-title-meta">
        <span>任务报告 #{meta?.id || ''}</span>
        {meta?.repo_name && (
          <span style={{ color: 'var(--text-secondary, #64748b)', fontWeight: 500 }}>
            · {meta.repo_name} {meta.branch ? `(${meta.branch})` : ''}
          </span>
        )}
        {meta?.score !== undefined && (
          <span className="report-rating-badge">
            风险分: {meta.score} 分
          </span>
        )}
      </div>

      {/* 右侧操作栏 */}
      <div className="report-header-actions">
        {/* 任务前后切换导航 */}
        {navigation && (
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.25rem' }}>
            <button
              className="nav-btn no-print"
              disabled={!navigation.prevTaskId}
              onClick={() => navigation.prevTaskId && navigation.onNavigate?.(navigation.prevTaskId)}
              title="查看上一个任务报告"
            >
              ‹ 上一个
            </button>
            <button
              className="nav-btn no-print"
              disabled={!navigation.nextTaskId}
              onClick={() => navigation.nextTaskId && navigation.onNavigate?.(navigation.nextTaskId)}
              title="查看下一个任务报告"
            >
              下一个 ›
            </button>
          </div>
        )}

        {/* 快速链接按钮 */}
        {meta?.id && (
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.25rem' }}>
            <button
              className="nav-btn no-print"
              onClick={handleCopyLink}
              title="复制报告专属直达链接 (URL)，便于在团队中快速分享"
            >
              🔗 复制链接
            </button>
            <button
              className="nav-btn no-print"
              onClick={handleOpenExternal}
              title="在新标签页中打开该报告独立视图"
            >
              ↗ 独立页面
            </button>
          </div>
        )}

        {/* 独立打印按钮 */}
        <button className="nav-btn no-print" onClick={onPrint} title="打印当前激活标签页内容或另存为 PDF">
          🖨️ 打印当前视图
        </button>

        {/* 导出下拉菜单 */}
        <ReportExportMenu
          taskId={meta?.id || 0}
          exporting={exporting}
          onExport={onExport}
          onPrint={onPrint}
        />

        {/* 全屏切换 */}
        {onToggleFullscreen && (
          <button
            className="nav-btn no-print"
            onClick={onToggleFullscreen}
            title={isFullscreen ? '退出全屏' : '全屏展开查看'}
          >
            {isFullscreen ? '⛶ 还原' : '⛶ 全屏'}
          </button>
        )}

        {/* 关闭按钮 */}
        {onClose && (
          <button
            className="nav-btn no-print"
            onClick={onClose}
            style={{ fontWeight: 700, padding: '0.35rem 0.5rem' }}
            title="关闭 (Esc)"
          >
            ✕
          </button>
        )}
      </div>
    </div>
  );
}
