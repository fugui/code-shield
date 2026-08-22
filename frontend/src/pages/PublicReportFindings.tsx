import React from 'react';
import { useParams } from 'react-router-dom';
import ReportViewer from '../components/report/ReportViewer';

export default function PublicReportFindings() {
  const { reportId } = useParams<{ reportId: string }>();
  const idNum = reportId ? parseInt(reportId, 10) : 0;

  if (!idNum) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100vh', background: 'var(--bg-color, #f8fafc)', color: 'var(--text-secondary, #64748b)' }}>
        未指定有效的任务报告 ID
      </div>
    );
  }

  return (
    <div style={{ minHeight: '100vh', background: 'var(--bg-color, #f8fafc)' }}>
      <ReportViewer taskId={idNum} mode="fullpage" />
    </div>
  );
}
