import React from 'react';

function OpenSourceManagement() {
  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
        <h2 style={{ margin: 0 }}>开源管理</h2>
      </div>

      <div className="card" style={{ padding: '0', overflow: 'hidden' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border-color)', color: '#64748b', fontSize: '0.875rem', textAlign: 'left', background: 'var(--bg-color)' }}>
              <th style={{ padding: '1rem', fontWeight: 600 }}>组件名称</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>版本</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>许可证</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>归属代码仓</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>状态 / 漏洞</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td colSpan={6} style={{ padding: '5rem 1rem', textAlign: 'center', color: '#64748b' }}>
                <div style={{ marginBottom: '1rem' }}>
                  <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1" strokeLinecap="round" strokeLinejoin="round" style={{ color: '#cbd5e1' }}>
                    <polygon points="12 2 2 7 12 12 22 7 12 2"></polygon>
                    <polyline points="2 17 12 22 22 17"></polyline>
                    <polyline points="2 12 12 17 22 12"></polyline>
                  </svg>
                </div>
                正在建设中... 敬请期待
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  );
}

export default OpenSourceManagement;
