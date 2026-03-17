import React, { useEffect, useState } from 'react';

function KeyIssues() {
  const [issues, setIssues] = useState<any[]>([]);
  const [members, setMembers] = useState<any[]>([]);

  useEffect(() => {
    fetch('/api/issues')
      .then(res => res.json())
      .then(data => setIssues(data || []))
      .catch(console.error);

    fetch('/api/members')
      .then(res => res.json())
      .then(data => setMembers(data || []))
      .catch(console.error);
  }, []);

  const updateStatus = (id: number, status: string) => {
    fetch(`/api/issues/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ status })
    }).then(res => res.json())
      .then(updated => {
        setIssues(issues.map(i => i.id === id ? updated : i));
      }).catch(console.error);
  };

  const updateAssignee = (id: number, assignee_id: string) => {
    fetch(`/api/issues/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ assignee_id })
    }).then(res => res.json())
      .then(updated => {
        setIssues(issues.map(i => i.id === id ? updated : i)); // Wait: we need to manually update the .assignee object. But reloading issues is easier.
        fetchIssues();
      }).catch(console.error);
  };

  const fetchIssues = () => {
    fetch('/api/issues')
      .then(res => res.json())
      .then(data => setIssues(data || []))
      .catch(console.error);
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
        <h2>核心重点问题拦截</h2>
        <button className="btn" style={{ background: 'var(--border-color)', color: 'white' }}>过滤选项</button>
      </div>
      <div className="card" style={{ padding: 0, overflow: 'hidden' }}>
        <table className="table">
          <thead>
            <tr>
              <th>问题描述</th>
              <th>所属代码仓</th>
              <th>问题类别</th>
              <th>处理进度</th>
              <th>责任人</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {issues.length === 0 ? (
              <tr><td colSpan={6} style={{ textAlign: 'center', padding: '3rem', color: '#94a3b8' }}>暂未发现任何核心问题</td></tr>
            ) : issues.map(issue => (
              <tr key={issue.id}>
                <td>
                  <div style={{ fontWeight: 500 }}>{issue.title}</div>
                  <div style={{ fontSize: '0.8rem', color: '#94a3b8', marginTop: '0.4rem', fontFamily: 'monospace' }}>
                    {issue.file_path && `${issue.file_path}:${issue.line_number}`}
                  </div>
                </td>
                <td>{issue.repo?.name}</td>
                <td><span className="badge" style={{ background: '#3b82f633', color: '#60a5fa' }}>{issue.issue_type}</span></td>
                <td>
                   <select 
                     value={issue.status} 
                     onChange={e => updateStatus(issue.id, e.target.value)}
                     style={{ background: 'var(--bg-color)', color: 'white', border: '1px solid var(--border-color)', padding: '0.4rem 0.5rem', borderRadius: '4px', outline: 'none' }}
                   >
                     <option value="open">待解决 (Open)</option>
                     <option value="in_progress">处理中 (In Progress)</option>
                     <option value="resolved">已解决 (Resolved)</option>
                   </select>
                </td>
                <td>
                   <select 
                     value={issue.assignee_id || ''} 
                     onChange={e => updateAssignee(issue.id, e.target.value)}
                     style={{ background: 'var(--bg-color)', color: 'var(--text-color)', border: '1px solid var(--border-color)', padding: '0.4rem 0.5rem', borderRadius: '4px', outline: 'none', minWidth: '100px' }}
                   >
                     <option value="" disabled>未分配</option>
                     {members.map(m => (
                       <option key={m.id} value={m.id}>{m.name} ({m.id})</option>
                     ))}
                   </select>
                </td>
                <td>
                  <button className="btn" style={{ padding: '0.3rem 0.6rem', fontSize: '0.8rem', background: 'transparent', color: 'var(--primary-color)', border: '1px solid var(--primary-color)' }}>查看详情</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

export default KeyIssues;
