import React, { useState, useRef, useEffect } from 'react';

interface ReportExportMenuProps {
  taskId: number;
  exporting: boolean;
  onExport: (format: string, scope?: string) => void;
  onPrint: () => void;
}

export default function ReportExportMenu({
  exporting,
  onExport,
  onPrint,
}: ReportExportMenuProps) {
  const [open, setOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handleOutsideClick = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    if (open) {
      document.addEventListener('mousedown', handleOutsideClick);
    }
    return () => {
      document.removeEventListener('mousedown', handleOutsideClick);
    };
  }, [open]);

  const handleAction = (format: string, scope?: string) => {
    setOpen(false);
    onExport(format, scope);
  };

  return (
    <div className="export-dropdown-wrapper no-print" ref={menuRef}>
      <button
        className="nav-btn"
        onClick={() => setOpen(!open)}
        disabled={exporting}
        style={{
          background: 'var(--primary-color, #2563eb)',
          color: '#ffffff',
          borderColor: 'transparent',
          fontWeight: 600,
        }}
        title="导出或打印多格式报告"
      >
        {exporting ? (
          <>⏳ 正在生成...</>
        ) : (
          <>
            <span>📥 导出报告</span>
            <span style={{ fontSize: '0.65rem' }}>▼</span>
          </>
        )}
      </button>

      {open && (
        <div className="export-menu-popup">
          <div className="export-menu-group-title">📋 详细问题清单 (Findings)</div>
          <div className="export-menu-item" onClick={() => handleAction('excel', 'findings')}>
            <span>📊</span>
            <span>Excel 工作簿 (.xlsx)</span>
            <span style={{ fontSize: '0.7rem', color: '#16a34a', marginLeft: 'auto', fontWeight: 600 }}>推荐</span>
          </div>
          <div className="export-menu-item" onClick={() => handleAction('csv', 'findings')}>
            <span>📄</span>
            <span>CSV 表格文件 (.csv)</span>
          </div>
          <div className="export-menu-item" onClick={() => handleAction('json', 'findings')}>
            <span>🔌</span>
            <span>结构化数据 (.json)</span>
          </div>

          <div className="export-menu-divider" />

          <div className="export-menu-group-title">📑 审计总结报告 (Summary)</div>
          <div className="export-menu-item" onClick={() => handleAction('md', 'summary')}>
            <span>📝</span>
            <span>Markdown 文档 (.md)</span>
          </div>

          <div className="export-menu-divider" />

          <div className="export-menu-group-title">📦 交付归档与打印</div>
          <div className="export-menu-item" onClick={() => handleAction('zip', 'all')}>
            <span>🗜️</span>
            <span>ZIP 全量任务交付包 (.zip)</span>
          </div>
          <div
            className="export-menu-item"
            onClick={() => {
              setOpen(false);
              onPrint();
            }}
          >
            <span>🖨️</span>
            <span>打印 / 另存为 PDF (当前视图)</span>
          </div>
        </div>
      )}
    </div>
  );
}
