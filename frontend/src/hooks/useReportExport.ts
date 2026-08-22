import { useState, useCallback } from 'react';
import { apiUrl } from '../config';
import { useToast } from '../components/Toast';

export interface UseReportExportReturn {
  exporting: boolean;
  exportReport: (taskId: number, format: string, scope?: string) => Promise<void>;
  printReport: () => void;
}

export function useReportExport(): UseReportExportReturn {
  const [exporting, setExporting] = useState(false);
  const { showToast } = useToast();

  const exportReport = useCallback(
    async (taskId: number, format: string, scope = 'findings') => {
      if (!taskId || exporting) return;
      setExporting(true);

      try {
        const url = apiUrl(`/api/tasks/${taskId}/report/export?format=${encodeURIComponent(format)}&scope=${encodeURIComponent(scope)}`);
        const res = await fetch(url);

        if (!res.ok) {
          const errData = await res.json().catch(() => ({}));
          showToast(`导出失败: ${errData.error || '服务器处理异常'}`, 'error');
          return;
        }

        // 获取文件名
        const disposition = res.headers.get('Content-Disposition') || '';
        let filename = `report-${taskId}.${format === 'excel' ? 'xlsx' : format}`;
        const matchUtf8 = disposition.match(/filename\*=UTF-8''([^;]+)/i);
        if (matchUtf8 && matchUtf8[1]) {
          filename = decodeURIComponent(matchUtf8[1]);
        } else {
          const matchNorm = disposition.match(/filename="?([^";]+)"?/i);
          if (matchNorm && matchNorm[1]) {
            filename = matchNorm[1];
          }
        }

        const blob = await res.blob();
        const blobUrl = window.URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = blobUrl;
        a.download = filename;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        window.URL.revokeObjectURL(blobUrl);

        showToast(`已成功导出 ${filename}`, 'success');
      } catch (err: any) {
        console.error('Export failed:', err);
        showToast('导出请求异常，请稍后重试', 'error');
      } finally {
        setExporting(false);
      }
    },
    [exporting, showToast]
  );

  const printReport = useCallback(() => {
    window.print();
  }, []);

  return {
    exporting,
    exportReport,
    printReport,
  };
}
