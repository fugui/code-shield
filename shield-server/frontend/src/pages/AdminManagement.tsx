import React, { useState } from 'react';
import ScanManagement from './ScanManagement';
import TaskTypeManagement from './TaskTypeManagement';
import UserManagement from './UserManagement';
import ExecutionLogs from './ExecutionLogs';

type AdminTab = 'scan' | 'taskTypes' | 'users' | 'activity';

function AdminManagement() {
  const [activeTab, setActiveTab] = useState<AdminTab>('scan');

  const tabStyle = (t: AdminTab) => ({
    background: 'transparent',
    border: 'none',
    padding: '0.75rem 0',
    fontWeight: 600,
    fontSize: '1rem',
    cursor: 'pointer',
    color: activeTab === t ? 'var(--primary-color)' : 'var(--text-color)',
    borderBottom: activeTab === t ? '2px solid var(--primary-color)' : '2px solid transparent',
    marginBottom: '-1px',
    outline: 'none',
    transition: 'all 0.2s',
  } as React.CSSProperties);

  return (
    <div>
      <div style={{ display: 'flex', gap: '2rem', borderBottom: '1px solid var(--border-color)', marginBottom: '1.5rem' }}>
        <button onClick={() => setActiveTab('scan')} style={tabStyle('scan')}>扫描任务</button>
        <button onClick={() => setActiveTab('taskTypes')} style={tabStyle('taskTypes')}>任务类型</button>
        <button onClick={() => setActiveTab('users')} style={tabStyle('users')}>用户管理</button>
        <button onClick={() => setActiveTab('activity')} style={tabStyle('activity')}>执行日志</button>
      </div>

      <div style={{ marginTop: '1rem' }}>
        {activeTab === 'scan' && <ScanManagement />}
        {activeTab === 'taskTypes' && <TaskTypeManagement />}
        {activeTab === 'users' && <UserManagement />}
        {activeTab === 'activity' && <ExecutionLogs />}
      </div>
    </div>
  );
}

export default AdminManagement;
