import React from 'react';

interface ReportEmptyStateProps {
  type: 'pass' | 'failed' | 'empty' | 'no-chunks';
  title?: string;
  description?: string;
  errorMessage?: string;
  onResume?: () => void;
}

export default function ReportEmptyState({
  type,
  title,
  description,
  errorMessage,
  onResume,
}: ReportEmptyStateProps) {
  if (type === 'pass') {
    return (
      <div
        style={{
          background: 'var(--card-bg, #ffffff)',
          border: '1px solid #bbf7d0',
          borderRadius: '8px',
          padding: '3rem 2rem',
          textAlign: 'center',
          margin: '1rem 0',
        }}
      >
        <div style={{ fontSize: '2.5rem', marginBottom: '0.75rem' }}>🎉</div>
        <h3 style={{ margin: '0 0 0.5rem', color: '#15803d', fontSize: '1.25rem', fontWeight: 600 }}>
          {title || '代码检视评估通过！'}
        </h3>
        <p style={{ color: '#475569', fontSize: '0.9rem', maxWidth: '500px', margin: '0 auto' }}>
          {description || '本次扫描分析未检出任何缺陷隐患或违规项，代码状态良好，符合质量与安全规范。'}
        </p>
      </div>
    );
  }

  if (type === 'failed') {
    return (
      <div
        style={{
          background: '#fff1f2',
          border: '1px solid #fecdd3',
          borderRadius: '8px',
          padding: '2rem',
          margin: '1rem 0',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', color: '#be123c', fontWeight: 700, fontSize: '1.05rem', marginBottom: '0.5rem' }}>
          <span>⚠️</span>
          <span>{title || '任务执行异常中断'}</span>
        </div>
        <p style={{ color: '#881337', fontSize: '0.875rem', lineHeight: 1.6, margin: '0 0 1rem' }}>
          {description || '该任务在执行过程中遭遇错误未能全部完成，以下为异常诊断信息：'}
        </p>
        {errorMessage && (
          <div style={{ background: '#4c0519', color: '#ffe4e6', padding: '0.75rem 1rem', borderRadius: '6px', fontFamily: 'monospace', fontSize: '0.8rem', whiteSpace: 'pre-wrap', wordBreak: 'break-all', marginBottom: '1rem' }}>
            {errorMessage}
          </div>
        )}
        {onResume && (
          <button
            onClick={onResume}
            className="nav-btn no-print"
            style={{ background: '#be123c', color: 'white', borderColor: 'transparent', fontWeight: 600, padding: '0.4rem 1rem' }}
          >
            🔄 恢复失败分片并继续任务 (Resume)
          </button>
        )}
      </div>
    );
  }

  if (type === 'no-chunks') {
    return (
      <div
        style={{
          background: 'var(--card-bg, #ffffff)',
          border: '1px solid var(--border-color, #e2e8f0)',
          borderRadius: '8px',
          padding: '2rem',
          textAlign: 'center',
          color: 'var(--text-secondary, #64748b)',
          fontSize: '0.875rem',
        }}
      >
        该任务采用单次分析引擎 (Single Engine) 运行，无多进程分片执行矩阵。
      </div>
    );
  }

  return (
    <div
      style={{
        background: 'var(--card-bg, #ffffff)',
        border: '1px solid var(--border-color, #e2e8f0)',
        borderRadius: '8px',
        padding: '2.5rem',
        textAlign: 'center',
        color: 'var(--text-secondary, #64748b)',
        fontSize: '0.875rem',
      }}
    >
      {description || '暂无相关数据'}
    </div>
  );
}
