import React from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import ReviewOverviewTab from './ReviewOverviewTab';
import ExecutionLogs from './ExecutionLogs';

type ReviewTab = 'overview' | 'activity';

function CodeReviewManagement() {
  const { tab } = useParams<{ tab: ReviewTab }>();
  const navigate = useNavigate();

  const activeTab: ReviewTab = (tab as ReviewTab) || 'overview';

  const setActiveTab = (t: ReviewTab) => {
    navigate(`/reviews/${t}`, { replace: true });
  };

  const tabStyle = (t: ReviewTab) => ({
    background: 'transparent',
    border: 'none',
    padding: '0.75rem 0',
    fontWeight: 600,
    fontSize: '1rem',
    cursor: 'pointer',
    color: activeTab === t ? 'var(--primary-color)' : 'var(--text-color)',
    borderBottom: activeTab === t ? '2px solid var(--primary-color)' : '2px solid transparent',
    marginBottom: '-1px',
  } as React.CSSProperties);

  return (
    <div>
      <div style={{ display: 'flex', gap: '2rem', borderBottom: '1px solid var(--border-color)', marginBottom: '1.5rem' }}>
        <button onClick={() => setActiveTab('overview')} style={tabStyle('overview')}>
          检视概览
        </button>
        <button onClick={() => setActiveTab('activity')} style={tabStyle('activity')}>
          检视活动
        </button>
      </div>

      <div style={{ minHeight: '500px' }}>
        {activeTab === 'overview' && <ReviewOverviewTab />}
        {activeTab === 'activity' && <ExecutionLogs />}
      </div>
    </div>
  );
}

export default CodeReviewManagement;
