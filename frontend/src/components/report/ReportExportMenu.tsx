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
        className="nav-btn btn-primary-export"
        onClick={() => setOpen(!open)}
        disabled={exporting}
        title="导出或打印多格式报告"
      >
        {exporting ? (
          <>
            <span className="export-spin">⏳</span>
            <span>正在生成...</span>
          </>
        ) : (
          <>
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
              <polyline points="7 10 12 15 17 10" />
              <line x1="12" y1="15" x2="12" y2="3" />
            </svg>
            <span>导出报告</span>
            <span style={{ fontSize: '0.65rem', opacity: 0.8, marginLeft: '1px' }}>▼</span>
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

          <div className="export-menu-group-title">🖨️ 视图打印与 PDF</div>
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
