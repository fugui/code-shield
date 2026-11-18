import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Pagination } from '@code/common';
import { apiUrl } from '../../config';
import {
  CompareBucket,
  ReportCompareItem,
  ReportCompareResult,
  TaskHistoryCandidate,
  TaskReportMeta,
} from '../../types/report';
import { compareBucketList, compareBucketMap, verdictLabels } from './reconciliationLabels';

interface ReportCompareTabProps {
  targetReportId?: number | null;
  meta?: TaskReportMeta;
}

const VALID_BUCKETS = new Set<CompareBucket>([
  'COMMON_PERSISTED',
  'ONLY_A_CODE_CHANGED',
  'ONLY_A_CODE_UNCHANGED',
  'ONLY_A_NOT_COVERED',
  'ONLY_B_NEW',
]);

const ALLOWED_PAGE_SIZES = new Set([15, 25, 50, 100]);

function formatShortHash(value?: string): string {
  if (!value) return '—';
  return value.length > 10 ? value.slice(0, 10) : value;
}

function formatLineSpan(start?: number, end?: number): string {
  if (!start || start <= 0) return '—';
  if (end && end > start) return `${start}-${end}`;
  return String(start);
}

function itemsForBucket(result: ReportCompareResult, bucket: CompareBucket | 'ALL'): ReportCompareItem[] {
  if (bucket === 'ALL') {
    return [
      ...(result.only_b_new || []),
      ...(result.only_a_code_unchanged || []),
      ...(result.only_a_code_changed || []),
      ...(result.only_a_not_covered || []),
      ...(result.common_persisted || []),
    ];
  }
  switch (bucket) {
    case 'COMMON_PERSISTED':
      return result.common_persisted || [];
    case 'ONLY_A_CODE_CHANGED':
      return result.only_a_code_changed || [];
    case 'ONLY_A_CODE_UNCHANGED':
      return result.only_a_code_unchanged || [];
    case 'ONLY_A_NOT_COVERED':
      return result.only_a_not_covered || [];
    case 'ONLY_B_NEW':
      return result.only_b_new || [];
  }
}

export default function ReportCompareTab({ targetReportId, meta }: ReportCompareTabProps) {
  const [searchParams, setSearchParams] = useSearchParams();

  const rawBaseParam = Number.parseInt(searchParams.get('cmpBase') || searchParams.get('baseReportId') || '', 10);
  const urlBaseId = Number.isFinite(rawBaseParam) && rawBaseParam > 0 ? rawBaseParam : null;

  const rawBucketParam = searchParams.get('cmpBucket') || 'ALL';
  const activeBucket: CompareBucket | 'ALL' = VALID_BUCKETS.has(rawBucketParam as CompareBucket)
    ? (rawBucketParam as CompareBucket)
    : 'ALL';

  const keyword = searchParams.get('cmpQ') || '';

  const rawPage = Number.parseInt(searchParams.get('cmpPage') || '1', 10);
  const currentPage = Number.isFinite(rawPage) && rawPage > 0 ? rawPage : 1;

  const rawPageSize = Number.parseInt(searchParams.get('cmpPageSize') || '25', 10);
  const pageSize = ALLOWED_PAGE_SIZES.has(rawPageSize) ? rawPageSize : 25;

  const [candidates, setCandidates] = useState<TaskHistoryCandidate[]>([]);
  const [loadingCandidates, setLoadingCandidates] = useState(false);
  const [selectedBaseId, setSelectedBaseId] = useState<number | null>(urlBaseId);

  const [compareResult, setCompareResult] = useState<ReportCompareResult | null>(null);
  const [loadingCompare, setLoadingCompare] = useState(false);
  const [compareError, setCompareError] = useState<string | null>(null);

  const updateCompareParams = useCallback((patch: Record<string, string | null>) => {
    setSearchParams(prev => {
      const next = new URLSearchParams(prev);
      for (const [k, v] of Object.entries(patch)) {
        if (!v) {
          next.delete(k);
        } else {
          next.set(k, v);
        }
      }
      return next;
    }, { replace: true });
  }, [setSearchParams]);

  // 加载同仓库、同任务类型的历史报告作为默认/可选基线（A）
  useEffect(() => {
    if (!meta?.repo_id || !meta?.task_type_id || !targetReportId) return;
    const controller = new AbortController();
    setLoadingCandidates(true);

    const query = new URLSearchParams({
      repo_id: String(meta.repo_id),
      task_type_id: String(meta.task_type_id),
      status: 'success,degraded',
      pageSize: '30',
      page: '1',
    });

    fetch(apiUrl(`/api/tasks?${query.toString()}`), { signal: controller.signal })
      .then(async res => {
        if (!res.ok) throw new Error('无法加载历史对比基线列表');
        const payload = await res.json() as { items?: TaskHistoryCandidate[] };
        const items = (payload.items || []).filter(item => item.id !== targetReportId);
        setCandidates(items);
        if (!urlBaseId && items.length > 0) {
          const older = items.find(item => item.id < targetReportId);
          const defaultBase = older ? older.id : items[0].id;
          setSelectedBaseId(defaultBase);
        } else if (urlBaseId) {
          setSelectedBaseId(urlBaseId);
        }
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') return;
        setCandidates([]);
      })
      .finally(() => setLoadingCandidates(false));

    return () => controller.abort();
  }, [meta?.repo_id, meta?.task_type_id, targetReportId, urlBaseId]);

  const executeCompare = useCallback((baseId: number, targetId: number, signal?: AbortSignal) => {
    setLoadingCompare(true);
    setCompareError(null);
    const params = new URLSearchParams({
      base_report_id: String(baseId),
      target_report_id: String(targetId),
    });
    return fetch(apiUrl(`/api/reports/compare?${params.toString()}`), { signal })
      .then(async res => {
        if (!res.ok) {
          let message = '双报告对比失败';
          try {
            const errBody = await res.json() as { error?: string };
            if (errBody.error) message = errBody.error;
          } catch {
            // ignore non-json error body
          }
          throw new Error(message);
        }
        const data = await res.json() as ReportCompareResult;
        setCompareResult(data);
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') return;
        setCompareResult(null);
        setCompareError(err instanceof Error ? err.message : '双报告对比请求异常');
      })
      .finally(() => setLoadingCompare(false));
  }, []);

  useEffect(() => {
    if (!selectedBaseId || !targetReportId) {
      setCompareResult(null);
      return;
    }
    const controller = new AbortController();
    void executeCompare(selectedBaseId, targetReportId, controller.signal);
    return () => controller.abort();
  }, [selectedBaseId, targetReportId, executeCompare]);

  const filteredItems = useMemo(() => {
    if (!compareResult) return [];
    const list = itemsForBucket(compareResult, activeBucket);
    const trimmed = keyword.trim().toLowerCase();
    if (!trimmed) return list;
    return list.filter(item =>
      item.norm_path.toLowerCase().includes(trimmed) ||
      item.title.toLowerCase().includes(trimmed) ||
      item.category.toLowerCase().includes(trimmed) ||
      (item.scope_symbol && item.scope_symbol.toLowerCase().includes(trimmed)) ||
      (item.defect_id && String(item.defect_id).includes(trimmed)),
    );
  }, [compareResult, activeBucket, keyword]);

  const totalPages = Math.max(1, Math.ceil(filteredItems.length / pageSize));
  const safePage = Math.min(currentPage, totalPages);
  const pagedItems = useMemo(() => {
    const start = (safePage - 1) * pageSize;
    return filteredItems.slice(start, start + pageSize);
  }, [filteredItems, safePage, pageSize]);

  const summaryCounts: Record<CompareBucket, number> = {
    COMMON_PERSISTED: compareResult?.summary.common_persisted || 0,
    ONLY_A_CODE_CHANGED: compareResult?.summary.only_a_code_changed || 0,
    ONLY_A_CODE_UNCHANGED: compareResult?.summary.only_a_code_unchanged || 0,
    ONLY_A_NOT_COVERED: compareResult?.summary.only_a_not_covered || 0,
    ONLY_B_NEW: compareResult?.summary.only_b_new || 0,
  };

  return (
    <div className="cs-report-compare">
      <div className="cs-report-compare-header">
        <div className="cs-report-compare-controls">
          <div className="cs-report-compare-field">
            <label htmlFor="compare-base-select">基线报告 A</label>
            <select
              id="compare-base-select"
              className="cs-report-compare-select"
              value={selectedBaseId ?? ''}
              disabled={loadingCandidates}
              onChange={e => {
                const nextId = Number.parseInt(e.target.value, 10);
                if (Number.isFinite(nextId) && nextId > 0) {
                  setSelectedBaseId(nextId);
                  updateCompareParams({ cmpBase: String(nextId), cmpPage: null });
                } else {
                  setSelectedBaseId(null);
                  updateCompareParams({ cmpBase: null, cmpPage: null });
                }
              }}
            >
              <option value="">
                {loadingCandidates ? '正在加载同仓历史报告…' : '请选择对比基线报告 (A)'}
              </option>
              {candidates.map(cand => (
                <option key={cand.id} value={cand.id}>
                  #{cand.id} · {cand.branch || '默认分支'} · {formatShortHash(cand.head_commit)} · {new Date(cand.created_at).toLocaleString()}
                </option>
              ))}
            </select>
          </div>

          <div className="cs-report-compare-arrow" aria-hidden="true">→</div>

          <div className="cs-report-compare-target">
            <span className="cs-report-compare-label">当前目标报告 B</span>
            <strong>
              #{targetReportId || '—'} · {compareResult?.target_report.branch || meta?.branch || '默认分支'} · {formatShortHash(compareResult?.target_report.head_commit || meta?.head_commit)}
            </strong>
          </div>
        </div>

        <div className="cs-report-compare-actions">
          {compareResult && (
            <span className="cs-report-compare-source-pill">
              数据源：{compareResult.data_source === 'entity_assessment_slots' ? '专项槽位对比' : 'SSOT 缺陷台账'}
            </span>
          )}
          <button
            type="button"
            className="btn btn-secondary"
            disabled={!selectedBaseId || !targetReportId || loadingCompare}
            onClick={() => {
              if (selectedBaseId && targetReportId) {
                void executeCompare(selectedBaseId, targetReportId);
              }
            }}
          >
            {loadingCompare ? '比对中…' : '重新比对'}
          </button>
        </div>
      </div>

      {compareError && (
        <div className="reconciliation-alert" role="alert">
          {compareError}
        </div>
      )}

      {!selectedBaseId && !loadingCandidates && (
        <div className="cs-report-compare-empty">
          {candidates.length === 0
            ? '当前仓库与任务类型下尚无其他已完成的对比基线报告。'
            : '请在上方选择一份基线报告 (A)，开始对比分析。'}
        </div>
      )}

      {compareResult && (
        <>
          <div className="cs-report-compare-buckets">
            {compareBucketList.map(bucket => {
              const count = summaryCounts[bucket.key];
              const isSelected = activeBucket === bucket.key;
              return (
                <button
                  key={bucket.key}
                  type="button"
                  title={bucket.description}
                  className={`cs-report-compare-bucket cs-report-compare-bucket--${bucket.tone} ${isSelected ? 'is-selected' : ''}`}
                  onClick={() => {
                    const nextBucket = isSelected ? 'ALL' : bucket.key;
                    updateCompareParams({
                      cmpBucket: nextBucket === 'ALL' ? null : nextBucket,
                      cmpPage: null,
                    });
                  }}
                >
                  <span className="cs-report-compare-bucket-title">{bucket.label}</span>
                  <strong className="cs-report-compare-bucket-count">{count}</strong>
                </button>
              );
            })}
          </div>

          <div className="cs-report-compare-toolbar">
            <div className="cs-report-compare-meta-strip">
              <span>基线 A 总数：<strong>{compareResult.summary.total_a}</strong></span>
              <span>目标 B 总数：<strong>{compareResult.summary.total_b}</strong></span>
              <span>当前筛选：<strong>{activeBucket === 'ALL' ? '全部桶' : compareBucketMap[activeBucket].label}</strong> ({filteredItems.length} 项)</span>
            </div>
            <div className="cs-report-compare-search">
              <input
                type="search"
                className="cs-report-compare-input"
                placeholder="按文件路径 / 缺陷标题 / 分类 / DefectID 过滤…"
                value={keyword}
                onChange={e => updateCompareParams({ cmpQ: e.target.value || null, cmpPage: null })}
              />
            </div>
          </div>

          <div className="cs-report-compare-table-wrap">
            {pagedItems.length === 0 ? (
              <div className="cs-report-compare-empty">当前筛选条件下无对比差异条目。</div>
            ) : (
              <table className="code-trace-table cs-report-compare-table">
                <thead>
                  <tr>
                    <th>差异归因桶</th>
                    <th>编号 / 严重度</th>
                    <th>文件位置</th>
                    <th>类别与问题描述</th>
                    <th>覆盖与哈希对比</th>
                  </tr>
                </thead>
                <tbody>
                  {pagedItems.map((item, idx) => {
                    const bucketInfo = compareBucketMap[item.bucket];
                    const verdictTag = item.verdict_b
                      ? verdictLabels[item.verdict_b]
                      : item.verdict_a
                        ? verdictLabels[item.verdict_a]
                        : undefined;
                    const hasLocationDrift = item.bucket === 'COMMON_PERSISTED' && Boolean(
                      (item.base_norm_path && item.base_norm_path !== item.norm_path) ||
                      (item.base_line_start && item.base_line_start !== item.line_start) ||
                      (item.base_line_end && item.base_line_end !== item.line_end)
                    );
                    return (
                      <tr key={`${item.bucket}-${item.defect_id ?? item.slot_key ?? item.norm_path}-${idx}`}>
                        <td>
                          <span className={`code-trace-tag cs-report-compare-tag--${bucketInfo.tone}`}>
                            {bucketInfo.label}
                          </span>
                          {item.defect_status && (
                            <div className="cs-report-compare-subtext">台账：{item.defect_status}</div>
                          )}
                        </td>
                        <td>
                          <div className="cs-report-compare-id">
                            {item.defect_id ? `#${item.defect_id}` : formatShortHash(item.slot_key)}
                          </div>
                          <div className="cs-report-compare-subtext">
                            {item.severity || '—'}
                            {verdictTag ? ` · ${verdictTag.label}` : ''}
                          </div>
                        </td>
                        <td className="col-mono">
                          <div>{item.norm_path}:{formatLineSpan(item.line_start, item.line_end)}</div>
                          {hasLocationDrift && (
                            <div className="cs-report-compare-subtext">
                              基线 A：{item.base_norm_path || item.norm_path}:{formatLineSpan(item.base_line_start, item.base_line_end)}
                            </div>
                          )}
                          {item.scope_symbol && (
                            <div className="cs-report-compare-subtext">作用域：{item.scope_symbol}</div>
                          )}
                        </td>
                        <td>
                          <div className="cs-report-compare-item-title">{item.title || '—'}</div>
                          <div className="cs-report-compare-subtext">{item.category || '未分类'}</div>
                        </td>
                        <td className="col-mono">
                          <div>A:{formatShortHash(item.base_blob_hash)} / B:{formatShortHash(item.target_blob_hash)}</div>
                          {item.scope_outcome_b && (
                            <div className="cs-report-compare-subtext">B 范围：{item.scope_outcome_b}</div>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            )}
          </div>

          {filteredItems.length > 0 && (
            <div className="file-group-pagination">
              <Pagination
                totalItems={filteredItems.length}
                page={safePage}
                pageSize={pageSize}
                onPageChange={p => updateCompareParams({ cmpPage: p > 1 ? String(p) : null })}
                onPageSizeChange={nextSize => updateCompareParams({ cmpPageSize: nextSize === 25 ? null : String(nextSize), cmpPage: null })}
                className="file-group-standard-pagination"
              />
            </div>
          )}
        </>
      )}
    </div>
  );
}
