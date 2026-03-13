import React, { useState } from 'react';
import TeamsTab from './TeamsTab';
import MembersTab from './MembersTab';

function TeamManagement() {
  const [activeTab, setActiveTab] = useState<'departments' | 'members'>('departments');

  return (
    <div>
      <div style={{ display: 'flex', gap: '2rem', borderBottom: '1px solid var(--border-color)', marginBottom: '1.5rem' }}>
        <button 
          onClick={() => setActiveTab('departments')}
          style={{ 
            background: 'transparent', border: 'none', padding: '0.75rem 0', fontWeight: 600, fontSize: '1rem',
            cursor: 'pointer', color: activeTab === 'departments' ? 'var(--primary-color)' : 'var(--text-color)',
            borderBottom: activeTab === 'departments' ? '2px solid var(--primary-color)' : '2px solid transparent',
            marginBottom: '-1px'
          }}
        >
          部门管理
        </button>
        <button 
          onClick={() => setActiveTab('members')}
          style={{ 
            background: 'transparent', border: 'none', padding: '0.75rem 0', fontWeight: 600, fontSize: '1rem',
            cursor: 'pointer', color: activeTab === 'members' ? 'var(--primary-color)' : 'var(--text-color)',
            borderBottom: activeTab === 'members' ? '2px solid var(--primary-color)' : '2px solid transparent',
            marginBottom: '-1px'
          }}
        >
          人员名册管理
        </button>
      </div>

      <div style={{ minHeight: '500px' }}>
        {activeTab === 'departments' ? <TeamsTab /> : <MembersTab />}
      </div>
    </div>
  );
}

export default TeamManagement;
