/**
 * Code-Shield 专用高保真打印引擎 (Dedicated Print Engine)
 * 使用隔离 iframe 实现无污染、无分页截断、顶格排版的跨浏览器打印方案
 */

export function printReportContainer(containerElement?: HTMLElement | null): void {
  const target = containerElement || document.querySelector('.report-viewer-container');
  if (!target) {
    window.print();
    return;
  }

  // 1. 克隆容器并清洗无需打印的控件
  const clone = target.cloneNode(true) as HTMLElement;

  // 移除无关的按钮和操作栏
  const removeSelectors = [
    '.no-print',
    '.report-header-actions',
    '.report-tab-bar',
    '.findings-filter-toolbar',
    '.export-dropdown-wrapper',
    '.nav-btn',
    '.status-select',
    'button',
    'select',
  ];
  removeSelectors.forEach(sel => {
    clone.querySelectorAll(sel).forEach(el => el.remove());
  });

  // 获取当前页面所有样式表规则
  let stylesHtml = '';
  document.querySelectorAll('style, link[rel="stylesheet"]').forEach(node => {
    stylesHtml += node.outerHTML;
  });

  // 专属高保真打印样式
  const printDocStyles = `
    <style>
      @page {
        size: A4 portrait;
        margin: 15mm 12mm 15mm 12mm;
      }
      *, *::before, *::after {
        box-sizing: border-box !important;
      }
      html, body {
        background: #ffffff !important;
        color: #0f172a !important;
        font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif !important;
        margin: 0 !important;
        padding: 0 !important;
        height: auto !important;
        min-height: 0 !important;
        overflow: visible !important;
        font-size: 13px !important;
        line-height: 1.6 !important;
        -webkit-print-color-adjust: exact !important;
        print-color-adjust: exact !important;
      }
      .report-viewer-container {
        display: block !important;
        height: auto !important;
        overflow: visible !important;
        padding: 0 !important;
        background: #ffffff !important;
      }
      .report-header-bar {
        display: flex !important;
        justify-content: space-between !important;
        align-items: center !important;
        padding: 0 0 12px 0 !important;
        border-bottom: 2px solid #0f172a !important;
        margin-bottom: 20px !important;
        page-break-after: avoid !important;
        break-after: avoid !important;
      }
      .report-title-meta {
        font-size: 16px !important;
        font-weight: 700 !important;
      }
      .report-rating-badge {
        display: inline-flex !important;
        align-items: center !important;
        padding: 3px 8px !important;
        border-radius: 9999px !important;
        font-size: 12px !important;
        font-weight: 700 !important;
        background: rgba(245, 158, 11, 0.12) !important;
        color: #b45309 !important;
        border: 1px solid rgba(245, 158, 11, 0.3) !important;
      }
      .report-kpi-grid {
        display: grid !important;
        grid-template-columns: repeat(4, 1fr) !important;
        gap: 12px !important;
        margin-bottom: 20px !important;
        page-break-inside: avoid !important;
        break-inside: avoid !important;
      }
      .report-kpi-card {
        border: 1px solid #cbd5e1 !important;
        background: #ffffff !important;
        border-radius: 6px !important;
        padding: 10px 12px !important;
        display: flex !important;
        flex-direction: column !important;
        gap: 4px !important;
      }
      .kpi-title {
        font-size: 11px !important;
        font-weight: 600 !important;
        color: #64748b !important;
        text-transform: uppercase !important;
      }
      .kpi-number {
        font-size: 18px !important;
        font-weight: 700 !important;
        color: #0f172a !important;
      }
      .report-content-body {
        display: block !important;
        height: auto !important;
        overflow: visible !important;
        padding: 0 !important;
      }
      .markdown-body {
        border: none !important;
        padding: 0 !important;
        font-size: 13px !important;
        line-height: 1.6 !important;
      }
      .markdown-body h1, .markdown-body h2, .markdown-body h3, .markdown-body h4 {
        page-break-after: avoid !important;
        break-after: avoid !important;
        color: #0f172a !important;
        margin-top: 18px !important;
        margin-bottom: 8px !important;
      }
      .markdown-body p, .markdown-body ul, .markdown-body ol, .markdown-body blockquote {
        page-break-inside: auto !important;
        break-inside: auto !important;
        margin-bottom: 10px !important;
      }
      .markdown-body table {
        width: 100% !important;
        border-collapse: collapse !important;
        margin: 14px 0 !important;
        page-break-inside: auto !important;
        break-inside: auto !important;
      }
      .markdown-body th, .markdown-body td {
        border: 1px solid #cbd5e1 !important;
        padding: 6px 10px !important;
        font-size: 12px !important;
        text-align: left !important;
      }
      .markdown-body th {
        background: #f1f5f9 !important;
        font-weight: 600 !important;
      }
      .finding-card {
        box-shadow: none !important;
        border: 1px solid #cbd5e1 !important;
        border-radius: 6px !important;
        page-break-inside: avoid !important;
        break-inside: avoid !important;
        margin-bottom: 16px !important;
        padding: 14px !important;
        background: #ffffff !important;
      }
      .finding-card-header {
        display: flex !important;
        justify-content: space-between !important;
        align-items: flex-start !important;
        margin-bottom: 8px !important;
      }
      .finding-location-bar {
        display: inline-flex !important;
        align-items: center !important;
        gap: 6px !important;
        background: #f8fafc !important;
        padding: 4px 8px !important;
        border-radius: 4px !important;
        border: 1px solid #e2e8f0 !important;
        font-family: monospace !important;
        font-size: 11px !important;
        color: #475569 !important;
        margin: 6px 0 !important;
      }
      .code-snippet-box {
        background: #f8fafc !important;
        color: #0f172a !important;
        border: 1px solid #cbd5e1 !important;
        border-radius: 4px !important;
        padding: 8px 12px !important;
        font-family: monospace !important;
        font-size: 11px !important;
        line-height: 1.5 !important;
        white-space: pre-wrap !important;
        word-break: break-all !important;
        max-height: none !important;
        height: auto !important;
        overflow: visible !important;
        margin: 8px 0 !important;
      }
      .suggestion-box {
        background: #f0fdf4 !important;
        border: 1px solid #86efac !important;
        color: #166534 !important;
        padding: 8px 12px !important;
        border-radius: 4px !important;
        font-size: 12px !important;
        margin-top: 8px !important;
        page-break-inside: avoid !important;
        break-inside: avoid !important;
      }
      .pipeline-step-flow {
        display: flex !important;
        gap: 8px !important;
        margin: 12px 0 16px !important;
        page-break-inside: avoid !important;
        break-inside: avoid !important;
      }
      .step-card {
        flex: 1 !important;
        border: 1px solid #cbd5e1 !important;
        border-radius: 6px !important;
        padding: 8px 10px !important;
      }
    </style>
  `;

  // 2. 创建隐藏的 iframe
  const iframe = document.createElement('iframe');
  iframe.style.position = 'fixed';
  iframe.style.right = '0';
  iframe.style.bottom = '0';
  iframe.style.width = '0';
  iframe.style.height = '0';
  iframe.style.border = '0';
  iframe.style.visibility = 'hidden';
  iframe.setAttribute('title', 'Report Print Frame');

  document.body.appendChild(iframe);

  const doc = iframe.contentWindow?.document;
  if (!doc) {
    document.body.removeChild(iframe);
    window.print();
    return;
  }

  doc.open();
  doc.write(`
    <!DOCTYPE html>
    <html lang="zh-CN">
      <head>
        <meta charset="utf-8" />
        <title>Code-Shield 任务审计报告</title>
        ${stylesHtml}
        ${printDocStyles}
      </head>
      <body>
        ${clone.outerHTML}
      </body>
    </html>
  `);
  doc.close();

  const handlePrint = () => {
    setTimeout(() => {
      try {
        iframe.contentWindow?.focus();
        iframe.contentWindow?.print();
      } catch (err) {
        console.error('Iframe print failed:', err);
        window.print();
      } finally {
        setTimeout(() => {
          if (document.body.contains(iframe)) {
            document.body.removeChild(iframe);
          }
        }, 2000);
      }
    }, 150);
  };

  if (iframe.contentWindow?.document.readyState === 'complete') {
    handlePrint();
  } else {
    iframe.onload = handlePrint;
  }
}
