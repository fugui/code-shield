import React, { useState, useEffect } from 'react';
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
  const [activeTab, setActiveTab] = useState<'summary' | 'findings' | 'diagnostics'>('summary');
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
      setActiveTab('summary');
      loadSummary(taskId);
    }
  }, [taskId, open, resetState, loadSummary]);

  // 当 summary 加载完成后，如果任务是失败状态，自动定位到 diagnostics
  useEffect(() => {
    if (summary?.meta?.status === 'failed') {
      setActiveTab('diagnostics');
      if (taskId) loadDiagnostics(taskId);
    }
  }, [summary?.meta?.status, taskId, loadDiagnostics]);

  // Tab 切换时按需加载
  const handleTabClick = (tab: 'summary' | 'findings' | 'diagnostics') => {
    setActiveTab(tab);
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
        isFullscreen={isFullscreen}
        onToggleFullscreen={mode === 'drawer' ? () => setIsFullscreen(!isFullscreen) : undefined}
        onClose={onClose}
        navigation={navigation}
        exporting={exporting}
        onExport={handleExport}
        onPrint={printReport}
      />

      {/* 选项卡栏 */}
      <div className="report-tab-bar no-print">
        <div
          className={`report-tab-item ${activeTab === 'summary' ? 'active' : ''}`}
          onClick={() => handleTabClick('summary')}
        >
          <span>📑 审计总结报告</span>
        </div>
        <div
          className={`report-tab-item ${activeTab === 'findings' ? 'active' : ''}`}
          onClick={() => handleTabClick('findings')}
        >
          <span>📋 详细问题清单</span>
          {totalFindingsCount > 0 && <span className="tab-badge">{totalFindingsCount}</span>}
        </div>
        <div
          className={`report-tab-item ${activeTab === 'diagnostics' ? 'active' : ''}`}
          onClick={() => handleTabClick('diagnostics')}
        >
          <span>🔬 运行轨迹与诊断</span>
          {meta?.status === 'failed' && (
            <span className="tab-badge" style={{ background: '#fee2e2', color: '#dc2626' }}>
              异常
            </span>
          )}
        </div>
      </div>

      {/* 内容主体 (充沛页边距 + 浅灰底色衬托) */}
      <div
        className="report-content-body"
        style={{
          flex: 1,
          overflowY: 'auto',
          padding: '2rem 2.25rem 3.5rem 2.25rem',
          backgroundColor: '#f1f5f9',
          boxSizing: 'border-box',
        }}
      >
        <div className="report-tab-inner-container" style={{ maxWidth: '1320px', margin: '0 auto', width: '100%', boxSizing: 'border-box' }}>
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
    <div style={{ minHeight: '100vh', background: '#f1f5f9', backgroundColor: '#f1f5f9' }}>
      {content}
    </div>
  );
}
