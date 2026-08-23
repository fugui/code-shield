import { useState, useCallback, useRef } from 'react';
import { apiUrl } from '../config';
import {
  TaskReportSummary,
  FindingsPageResponse,
  TaskDiagnostics,
  TaskFindingItem,
} from '../types/report';

export interface UseTaskReportReturn {
  summary: TaskReportSummary | null;
  loadingSummary: boolean;
  summaryError: string | null;
  loadSummary: (taskId: number) => Promise<void>;

  findingsPage: FindingsPageResponse | null;
  loadingFindings: boolean;
  findingsError: string | null;
  loadFindings: (taskId: number, page?: number, filters?: Record<string, any>) => Promise<void>;

  diagnostics: TaskDiagnostics | null;
  loadingDiagnostics: boolean;
  diagnosticsError: string | null;
  loadDiagnostics: (taskId: number) => Promise<void>;

  resetState: () => void;
}

export function useTaskReport(): UseTaskReportReturn {
  const [summary, setSummary] = useState<TaskReportSummary | null>(null);
  const [loadingSummary, setLoadingSummary] = useState(false);
  const [summaryError, setSummaryError] = useState<string | null>(null);

  const [findingsPage, setFindingsPage] = useState<FindingsPageResponse | null>(null);
  const [loadingFindings, setLoadingFindings] = useState(false);
  const [findingsError, setFindingsError] = useState<string | null>(null);

  const [diagnostics, setDiagnostics] = useState<TaskDiagnostics | null>(null);
  const [loadingDiagnostics, setLoadingDiagnostics] = useState(false);
  const [diagnosticsError, setDiagnosticsError] = useState<string | null>(null);

  const currentTaskIdRef = useRef<number | null>(null);

  const resetState = useCallback(() => {
    setSummary(null);
    setSummaryError(null);
    setFindingsPage(null);
    setFindingsError(null);
    setDiagnostics(null);
    setDiagnosticsError(null);
    currentTaskIdRef.current = null;
  }, []);

  // 1. 轻量概览加载 (带竞态保护)
  const loadSummary = useCallback(async (taskId: number) => {
    if (!taskId) return;
    currentTaskIdRef.current = taskId;
    setLoadingSummary(true);
    setSummaryError(null);
    try {
      const res = await fetch(apiUrl(`/api/tasks/${taskId}/report/summary`));
      // 竞态保护：如果当前活跃任务已改变，丢弃该响应
      if (currentTaskIdRef.current !== taskId) return;

      if (!res.ok) {
        throw new Error('无法加载任务报告概览');
      }
      const data = await res.json();
      if (currentTaskIdRef.current !== taskId) return;
      setSummary(data);
    } catch (err: any) {
      if (currentTaskIdRef.current === taskId) {
        setSummaryError(err.message || '加载报告概览失败');
      }
    } finally {
      if (currentTaskIdRef.current === taskId) {
        setLoadingSummary(false);
      }
    }
  }, []);

  // 2. 按需详细清单加载 (带竞态保护)
  const loadFindings = useCallback(async (taskId: number, page = 1, filters: Record<string, any> = {}) => {
    if (!taskId) return;
    currentTaskIdRef.current = taskId;
    setLoadingFindings(true);
    setFindingsError(null);

    const params = new URLSearchParams({
      page: page.toString(),
      pageSize: (filters.pageSize || 50).toString(),
    });
    if (filters.severity) params.append('severity', filters.severity);
    if (filters.status) params.append('status', filters.status);
    if (filters.category) params.append('category', filters.category);
    if (filters.keyword) params.append('keyword', filters.keyword);
    if (filters.sortField) params.append('sort_field', filters.sortField);
    if (filters.sortOrder) params.append('sort_order', filters.sortOrder);
    if (filters.assigneeId) params.append('assignee_id', filters.assigneeId);

    try {
      const res = await fetch(apiUrl(`/api/tasks/${taskId}/report/findings?${params.toString()}`));
      if (currentTaskIdRef.current !== taskId) return;

      if (!res.ok) {
        throw new Error('无法加载详细问题清单');
      }
      const data = await res.json();
      if (currentTaskIdRef.current !== taskId) return;
      setFindingsPage(data);
    } catch (err: any) {
      if (currentTaskIdRef.current === taskId) {
        setFindingsError(err.message || '加载详细问题清单失败');
      }
    } finally {
      if (currentTaskIdRef.current === taskId) {
        setLoadingFindings(false);
      }
    }
  }, []);

  // 3. 运行轨迹与诊断加载 (带竞态保护)
  const loadDiagnostics = useCallback(async (taskId: number) => {
    if (!taskId) return;
    currentTaskIdRef.current = taskId;
    setLoadingDiagnostics(true);
    setDiagnosticsError(null);
    try {
      const res = await fetch(apiUrl(`/api/tasks/${taskId}/report/diagnostics`));
      if (currentTaskIdRef.current !== taskId) return;

      if (!res.ok) {
        throw new Error('未发现详细诊断数据');
      }
      const data = await res.json();
      if (currentTaskIdRef.current !== taskId) return;
      setDiagnostics(data);
    } catch (err: any) {
      if (currentTaskIdRef.current === taskId) {
        setDiagnosticsError(err.message || '加载诊断数据失败');
      }
    } finally {
      if (currentTaskIdRef.current === taskId) {
        setLoadingDiagnostics(false);
      }
    }
  }, []);

  return {
    summary,
    loadingSummary,
    summaryError,
    loadSummary,

    findingsPage,
    loadingFindings,
    findingsError,
    loadFindings,

    diagnostics,
    loadingDiagnostics,
    diagnosticsError,
    loadDiagnostics,

    resetState,
  };
}
