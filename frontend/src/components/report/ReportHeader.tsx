import React from 'react';
import { TaskReportMeta, TaskNavigationContext } from '../../types/report';
import ReportExportMenu from './ReportExportMenu';

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
        {meta?.rating && (
          <span className={`report-rating-badge rating-${meta.rating}`}>
            {meta.score} 分 · {meta.rating}
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
