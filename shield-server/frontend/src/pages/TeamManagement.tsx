import React from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import TeamsTab from './TeamsTab';
import MembersTab from './MembersTab';
import Repositories from './Repositories';

type TeamTab = 'departments' | 'members' | 'repositories';

function TeamManagement() {
  const { tab } = useParams<{ tab: TeamTab }>();
  const navigate = useNavigate();

  const activeTab: TeamTab = (tab as TeamTab) || 'departments';

  const setActiveTab = (t: TeamTab) => {
    navigate(`/teams/${t}`, { replace: true });
  };

  const tabStyle = (t: TeamTab) => ({
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
        <button onClick={() => setActiveTab('departments')} style={tabStyle('departments')}>
          部门管理
        </button>
        <button onClick={() => setActiveTab('members')} style={tabStyle('members')}>
          人员管理
        </button>
        <button onClick={() => setActiveTab('repositories')} style={tabStyle('repositories')}>
          代码仓管理
        </button>
      </div>

      <div style={{ minHeight: '500px' }}>
        {activeTab === 'departments' && <TeamsTab />}
        {activeTab === 'members' && <MembersTab />}
        {activeTab === 'repositories' && <Repositories />}
      </div>
    </div>
  );
}

export default TeamManagement;
