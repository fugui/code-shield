import React, { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import TaskOverviewTab from './ReviewOverviewTab';
import ExecutionLogs from './ExecutionLogs';
import { useToast } from '../components/Toast';

type TaskTab = 'overview' | 'activity';

function TaskManagement() {
  const { tab } = useParams<{ tab: TaskTab }>();
  const navigate = useNavigate();
  const { showToast } = useToast();
  const [isAdmin, setIsAdmin] = useState(false);

  useEffect(() => {
    fetch('/api/me')
      .then(res => res.ok ? res.json() : null)
      .then(data => { if (data) setIsAdmin(!!data.is_admin); })
      .catch(() => {});
  }, []);

  const activeTab: TaskTab = (tab as TaskTab) || 'overview';

  const setActiveTab = (t: TaskTab) => {
    navigate(`/tasks/${t}`, { replace: true });
  };

  const handleClearInvalidReports = async () => {
    if (!window.confirm('确认清除所有不是“完成”状态的无效报告记录吗？进行中的任务可能会受影响。')) return;
    try {
      const res = await fetch('/api/tasks/invalid-reports', { method: 'DELETE' });
      if (res.ok) {
        const data = await res.json();
        showToast(`成功清除 ${data.deleted} 条无效报告记录`, 'success');
        setTimeout(() => window.location.reload(), 1000);
      } else {
        const err = await res.json();
        showToast(`清除失败: ${err.error || '未知错误'}`, 'error');
      }
    } catch (err) {
      showToast('请求失败，请检查网络连接', 'error');
    }
  };

  const tabStyle = (t: TaskTab) => ({
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
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', borderBottom: '1px solid var(--border-color)', marginBottom: '1.5rem' }}>
        <div style={{ display: 'flex', gap: '2rem' }}>
          <button onClick={() => setActiveTab('overview')} style={tabStyle('overview')}>
            任务概览
          </button>
          <button onClick={() => setActiveTab('activity')} style={tabStyle('activity')}>
            执行活动
          </button>
        </div>
        {isAdmin && (
          <button 
            className="btn"
            onClick={handleClearInvalidReports}
            style={{ 
              background: 'transparent', 
              color: 'var(--danger-color)', 
              border: '1px solid var(--danger-color)', 
              padding: '0.4rem 0.8rem', 
              borderRadius: '4px', 
              cursor: 'pointer',
              fontSize: '0.875rem',
              marginBottom: '0.5rem'
            }}
            onMouseEnter={e => e.currentTarget.style.backgroundColor = 'rgba(239, 68, 68, 0.1)'}
            onMouseLeave={e => e.currentTarget.style.backgroundColor = 'transparent'}
          >
            清除无效报告
          </button>
        )}
      </div>

      <div style={{ minHeight: '500px' }}>
        {activeTab === 'overview' && <TaskOverviewTab />}
        {activeTab === 'activity' && <ExecutionLogs />}
      </div>
    </div>
  );
}

export default TaskManagement;
