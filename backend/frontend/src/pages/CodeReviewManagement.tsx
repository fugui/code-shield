import React, { useState } from 'react';
import ReviewOverviewTab from './ReviewOverviewTab';
import ReviewReports from './ReviewReports';

function CodeReviewManagement() {
  const [activeTab, setActiveTab] = useState<'overview' | 'tasks'>('overview');

  return (
    <div>
      <div style={{ display: 'flex', gap: '2rem', borderBottom: '1px solid var(--border-color)', marginBottom: '1.5rem' }}>
        <button 
          onClick={() => setActiveTab('overview')}
          style={{ 
            background: 'transparent', border: 'none', padding: '0.75rem 0', fontWeight: 600, fontSize: '1rem',
            cursor: 'pointer', color: activeTab === 'overview' ? 'var(--primary-color)' : 'var(--text-color)',
            borderBottom: activeTab === 'overview' ? '2px solid var(--primary-color)' : '2px solid transparent',
            marginBottom: '-1px'
          }}
        >
          检视概览
        </button>
        <button 
          onClick={() => setActiveTab('tasks')}
          style={{ 
            background: 'transparent', border: 'none', padding: '0.75rem 0', fontWeight: 600, fontSize: '1rem',
            cursor: 'pointer', color: activeTab === 'tasks' ? 'var(--primary-color)' : 'var(--text-color)',
            borderBottom: activeTab === 'tasks' ? '2px solid var(--primary-color)' : '2px solid transparent',
            marginBottom: '-1px'
          }}
        >
          检视任务
        </button>
      </div>

      <div style={{ minHeight: '500px' }}>
        {activeTab === 'overview' && <ReviewOverviewTab setActiveTab={setActiveTab} />}
        {activeTab === 'tasks' && <ReviewReports />}
      </div>
    </div>
  );
}

export default CodeReviewManagement;
