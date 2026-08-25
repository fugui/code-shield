import React, { useState, useEffect } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Drawer } from '@code/common';
import { useTaskReport } from '../../hooks/useTaskReport';
import { useReportExport } from '../../hooks/useReportExport';
import { TaskNavigationContext } from '../../types/report';
import ReportHeader from './ReportHeader';
import ReportSummaryTab from './ReportSummaryTab';
import ReportFindingsTab from './ReportFindingsTab';
import ReportDiagnosticsTab from './ReportDiagnosticsTab';
import './report.css';

export interface ReportViewerProps {
  taskId?: number | null;
  open?: boolean;
  onClose?: () => void;
  mode?: 'drawer' | 'fullpage' | 'readonly';
  navigation?: TaskNavigationContext;
  onResume?: (taskId: number) => void;
}

export default function ReportViewer({
  taskId,
  open = true,
  onClose,
  mode = 'drawer',
  navigation,
  onResume,
}: ReportViewerProps) {
  const [searchParams, setSearchParams] = useSearchParams();
  const rawTab = searchParams.get('tab');
  const urlTab = (rawTab === 'findings' || rawTab === 'diagnostics' || rawTab === 'summary')
    ? (rawTab as 'summary' | 'findings' | 'diagnostics')
    : undefined;

  const [activeTab, setActiveTab] = useState<'summary' | 'findings' | 'diagnostics'>(urlTab || 'summary');
  const [isFullscreen, setIsFullscreen] = useState(mode === 'fullpage');

  const {
    summary,
    loadingSummary,
    loadSummary,
    findingsPage,
    loadingFindings,
    loadFindings,
    diagnostics,
    loadingDiagnostics,
    loadDiagnostics,
    resetState,
  } = useTaskReport();

  const { exporting, exportReport, printReport } = useReportExport();

  // 当任务 ID 或打开状态改变时触发加载
  useEffect(() => {
    if (open && taskId) {
      resetState();
      const currentTab = urlTab || 'summary';
      setActiveTab(currentTab);
      loadSummary(taskId);
      if (currentTab === 'findings') {
        loadFindings(taskId);
      } else if (currentTab === 'diagnostics') {
        loadDiagnostics(taskId);
      }
    }
  }, [taskId, open, resetState, loadSummary, loadFindings, loadDiagnostics, urlTab]);

  // 当 summary 加载完成后，如果任务是失败状态且未显式指定 tab，自动定位到 diagnostics
  useEffect(() => {
    if (summary?.meta?.status === 'failed' && !urlTab) {
      setActiveTab('diagnostics');
      if (taskId) loadDiagnostics(taskId);
    }
  }, [summary?.meta?.status, taskId, loadDiagnostics, urlTab]);

  // Tab 切换时按需加载并同步 URL 参数
  const handleTabClick = (tab: 'summary' | 'findings' | 'diagnostics') => {
    setActiveTab(tab);
    setSearchParams(prev => {
      const next = new URLSearchParams(prev);
      if (tab === 'summary') {
        next.delete('tab');
      } else {
        next.set('tab', tab);
      }
      return next;
    }, { replace: true });

    if (!taskId) return;

    if (tab === 'findings' && !findingsPage) {
      loadFindings(taskId);
    } else if (tab === 'diagnostics' && !diagnostics) {
      loadDiagnostics(taskId);
    }
  };

  const handleFindingsFilter = (filters: Record<string, any>) => {
    if (taskId) {
      loadFindings(taskId, filters.page || 1, filters);
    }
  };

  const handleExport = (format: string, scope?: string) => {
    if (taskId) {
      exportReport(taskId, format, scope);
    }
  };

  const handleResumeClick = () => {
    if (taskId && onResume) {
      onResume(taskId);
    }
  };

  const meta = summary?.meta;
  const totalFindingsCount = summary?.metrics?.total_findings ?? findingsPage?.total ?? 0;

  const content = (
    <div className="report-viewer-container">
      {/* 顶部操作条 */}
      <ReportHeader
        meta={meta}
        activeTab={activeTab}
        isFullscreen={isFullscreen}
        onToggleFullscreen={mode === 'drawer' ? () => setIsFullscreen(!isFullscreen) : undefined}
        onClose={onClose}
        navigation={navigation}
        exporting={exporting}
        onExport={handleExport}
        onPrint={printReport}
      />

      {/* 选项卡栏 (样式固化在 report.css 中，杜绝微前端样式覆盖引起的随机跳跃) */}
      <div className="report-tab-bar cs-report-tab-bar no-print">
        <div
          className={`report-tab-item cs-report-tab-item ${activeTab === 'summary' ? 'active' : ''}`}
          onClick={() => handleTabClick('summary')}
        >
          <span>📑 审计总结报告</span>
        </div>

        <div
          className={`report-tab-item cs-report-tab-item ${activeTab === 'findings' ? 'active' : ''}`}
          onClick={() => handleTabClick('findings')}
        >
          <span>📋 详细问题清单</span>
          {totalFindingsCount > 0 && (
            <span className="tab-badge cs-tab-badge">
              {totalFindingsCount}
            </span>
          )}
        </div>

        <div
          className={`report-tab-item cs-report-tab-item ${activeTab === 'diagnostics' ? 'active' : ''}`}
          onClick={() => handleTabClick('diagnostics')}
        >
          <span>🔬 运行轨迹与诊断</span>
          {meta?.status === 'failed' && (
            <span className="tab-badge cs-tab-badge danger">
              异常
            </span>
          )}
        </div>
      </div>

      {/* 内容主体 (充沛页边距 + 浅灰底色衬托) */}
      <div className="report-content-body">
        <div className="report-tab-inner-container">
          {activeTab === 'summary' && (
            <ReportSummaryTab summary={summary} loading={loadingSummary} />
          )}
          {activeTab === 'findings' && (
            <ReportFindingsTab
              meta={meta}
              findingsPage={findingsPage}
              loading={loadingFindings}
              onFilterChange={handleFindingsFilter}
            />
          )}
          {activeTab === 'diagnostics' && (
            <ReportDiagnosticsTab
              meta={meta}
              diagnostics={diagnostics}
              loading={loadingDiagnostics}
              onResume={handleResumeClick}
            />
          )}
        </div>
      </div>
    </div>
  );

  if (mode === 'drawer') {
    return (
      <Drawer
        open={open}
        onClose={onClose || (() => {})}
        title={null}
        width={isFullscreen ? '100vw' : 'min(1200px, 95vw)'}
        bodyStyle={{ padding: 0, height: '100%', background: '#f1f5f9', backgroundColor: '#f1f5f9' }}
        headerStyle={{ display: 'none' }}
      >
        {content}
      </Drawer>
    );
  }

  return (
    <div className="report-fullpage-shell">
      {content}
    </div>
  );
}
