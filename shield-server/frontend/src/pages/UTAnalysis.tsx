import React from 'react';

function UTAnalysis() {
  return (
    <div>
      <div className="card" style={{ padding: '0', overflow: 'hidden' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid var(--border-color)', color: '#64748b', fontSize: '0.875rem', textAlign: 'left', background: 'var(--bg-color)' }}>
              <th style={{ padding: '1rem', fontWeight: 600 }}>代码仓</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>归属部门</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>用例总数</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>合格率</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>最近扫描</th>
              <th style={{ padding: '1rem', fontWeight: 600 }}>趋势</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td colSpan={6} style={{ padding: '5rem 1rem', textAlign: 'center', color: '#64748b' }}>
                <div style={{ marginBottom: '1rem' }}>
                  <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1" strokeLinecap="round" strokeLinejoin="round" style={{ color: '#cbd5e1' }}>
                    <path d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5L14.5 2z"></path>
                    <polyline points="14 2 14 8 20 8"></polyline>
                    <line x1="16" y1="13" x2="8" y2="13"></line>
                    <line x1="16" y1="17" x2="8" y2="17"></line>
                    <line x1="10" y1="9" x2="8" y2="9"></line>
                  </svg>
                </div>
                <div style={{ fontSize: '1rem', fontWeight: 500, color: '#475569', marginBottom: '0.5rem' }}>测试有效性看板建设中</div>
                <div style={{ fontSize: '0.85rem', color: '#94a3b8', maxWidth: '400px', margin: '0 auto', lineHeight: 1.6 }}>
                  将基于「测试用例有效性评估」和「单元测试代码质量审计」的扫描报告数据，展现各代码仓、各部门的测试合格率、问题分布与趋势分析。
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  );
}

export default UTAnalysis;
