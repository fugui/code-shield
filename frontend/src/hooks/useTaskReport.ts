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

  updateFindingStatus: (
    finding: TaskFindingItem,
    newStatus: string,
    assigneeId?: number | null,
    comment?: string
  ) => Promise<boolean>;

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

  // 1. 轻量概览加载
  const loadSummary = useCallback(async (taskId: number) => {
    if (!taskId) return;
    currentTaskIdRef.current = taskId;
    setLoadingSummary(true);
    setSummaryError(null);
    try {
      const res = await fetch(apiUrl(`/api/tasks/${taskId}/report/summary`));
      if (!res.ok) {
        // 兼容回退旧接口
        const fallbackRes = await fetch(apiUrl(`/api/tasks/${taskId}/report`));
        if (fallbackRes.ok) {
          const md = await fallbackRes.text();
          const taskRes = await fetch(apiUrl(`/api/tasks/${taskId}`));
          const taskData = taskRes.ok ? await taskRes.json() : {};
          setSummary({
            meta: {
              id: taskId,
              repo_id: taskData.repo_id || 0,
              repo_name: taskData.repo?.name || '',
              repo_url: taskData.repo?.url || '',
              branch: taskData.repo?.branch || 'master',
              task_type_id: taskData.task_type_id || 0,
              task_type_name: taskData.task_type?.name || '',
              task_type_display: taskData.task_type?.display_name || '代码检视',
              engine_mode: taskData.task_type?.engine_mode || 'single',
              governance_mode: taskData.task_type?.governance_mode || 'defect_tracking',
              status: taskData.status || 'success',
              score: taskData.score || 0,
              rating: taskData.score >= 90 ? '优' : taskData.score >= 75 ? '良' : '中',
              total_chunks: taskData.total_chunks || 0,
              processed_chunks: taskData.processed_chunks || 0,
              success_chunks: taskData.success_chunks || 0,
              created_at: taskData.created_at || new Date().toISOString(),
            },
            markdown_content: md,
            metrics: {
              total_findings: 0,
              fatal_count: 0,
              critical_count: 0,
              major_count: 0,
              minor_count: 0,
              suggestion_count: 0,
              pass_count: 0,
            },
          });
          return;
        }
        throw new Error('无法加载任务报告概览');
      }
      const data = await res.json();
      setSummary(data);
    } catch (err: any) {
      setSummaryError(err.message || '加载报告概览失败');
    } finally {
      setLoadingSummary(false);
    }
  }, []);

  // 2. 按需详细清单加载
  const loadFindings = useCallback(async (taskId: number, page = 1, filters: Record<string, any> = {}) => {
    if (!taskId) return;
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
      if (!res.ok) {
        // 回退旧接口 /api/tasks/:id/findings
        const fallbackRes = await fetch(apiUrl(`/api/tasks/${taskId}/findings`));
        if (fallbackRes.ok) {
          const list = await fallbackRes.json();
          setFindingsPage({
            items: Array.isArray(list) ? list : [],
            total: Array.isArray(list) ? list.length : 0,
            page: 1,
            pageSize: 50,
            totalPages: 1,
            metrics: {
              total_findings: Array.isArray(list) ? list.length : 0,
              fatal_count: 0,
              critical_count: 0,
              major_count: 0,
              minor_count: 0,
              suggestion_count: 0,
              pass_count: 0,
            },
          });
          return;
        }
        throw new Error('无法加载详细问题清单');
      }
      const data = await res.json();
      setFindingsPage(data);
    } catch (err: any) {
      setFindingsError(err.message || '加载详细问题清单失败');
    } finally {
      setLoadingFindings(false);
    }
  }, []);

  // 3. 运行轨迹与诊断加载
  const loadDiagnostics = useCallback(async (taskId: number) => {
    if (!taskId) return;
    setLoadingDiagnostics(true);
    setDiagnosticsError(null);
    try {
      const res = await fetch(apiUrl(`/api/tasks/${taskId}/report/diagnostics`));
      if (!res.ok) {
        // 回退旧接口 /api/tasks/:id/summary
        const fallbackRes = await fetch(apiUrl(`/api/tasks/${taskId}/summary`));
        if (fallbackRes.ok) {
          const sumData = await fallbackRes.json();
          setDiagnostics({
            meta: summary?.meta || ({} as any),
            pipeline_steps: [
              { name: '代码静态分析', status: sumData.analysis?.status || 'success', duration_seconds: sumData.analysis?.duration_seconds || 0 },
              { name: '综合报告生成', status: sumData.synthesis?.status || 'success', duration_seconds: sumData.synthesis?.duration_seconds || 0 },
            ],
            total_duration: sumData.duration_seconds || 0,
            analysis_duration: sumData.analysis?.duration_seconds || 0,
            chunks: (sumData.analysis?.chunks || []).map((c: any) => ({
              chunk_name: c.chunk_name,
              status: c.status,
              duration_seconds: c.duration_seconds,
              attempts: c.attempts || 1,
              files_count: c.files?.length || 0,
              findings_count: 0,
              error_message: c.error_message,
              files: c.files,
            })),
            raw_output_log: '',
            log_truncated: false,
            total_log_lines: 0,
          });
          return;
        }
        throw new Error('未发现详细诊断数据');
      }
      const data = await res.json();
      setDiagnostics(data);
    } catch (err: any) {
      setDiagnosticsError(err.message || '加载诊断数据失败');
    } finally {
      setLoadingDiagnostics(false);
    }
  }, [summary]);

  // 4. 缺陷流转与责任人指派 (乐观更新)
  const updateFindingStatus = useCallback(
    async (finding: TaskFindingItem, newStatus: string, assigneeId?: number | null, comment?: string): Promise<boolean> => {
      if (!finding || !finding.id) return false;

      // 乐观更新
      setFindingsPage(prev => {
        if (!prev) return prev;
        const updatedItems = prev.items.map(it => {
          if (it.id === finding.id) {
            return {
              ...it,
              status: newStatus,
              status_display: newStatus === 'resolved' ? '已解决' : newStatus === 'invalid' ? '忽略/误报' : '待处理',
              assignee_id: assigneeId !== undefined ? assigneeId : it.assignee_id,
              latest_comment: comment !== undefined ? comment : it.latest_comment,
            };
          }
          return it;
        });
        return {
          ...prev,
          items: updatedItems,
        };
      });

      try {
        const payload: Record<string, any> = {
          status: newStatus,
        };
        if (assigneeId !== undefined) payload.assignee_id = assigneeId;
        if (comment) payload.comment = comment;

        // 尝试向通用的 campaign finding 流转接口提交
        const res = await fetch(apiUrl(`/api/campaign/findings/${finding.id}/status`), {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });

        return res.ok;
      } catch {
        return false;
      }
    },
    []
  );

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

    updateFindingStatus,
    resetState,
  };
}
