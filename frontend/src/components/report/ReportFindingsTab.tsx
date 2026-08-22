import React, { useState, useEffect } from 'react';
import { FindingsPageResponse, TaskReportMeta, TaskFindingItem } from '../../types/report';
import FindingCard from './FindingCard';
import ReportEmptyState from './ReportEmptyState';

interface ReportFindingsTabProps {
  meta?: TaskReportMeta;
  findingsPage: FindingsPageResponse | null;
  loading: boolean;
  onFilterChange: (filters: Record<string, any>) => void;
  onStatusChange?: (finding: TaskFindingItem, newStatus: string) => void;
  isReadOnly?: boolean;
}

export default function ReportFindingsTab({
  meta,
  findingsPage,
  loading,
  onFilterChange,
  onStatusChange,
  isReadOnly = false,
}: ReportFindingsTabProps) {
  const [selectedSev, setSelectedSev] = useState<string>('');
  const [selectedStatus, setSelectedStatus] = useState<string>('');
  const [keyword, setKeyword] = useState<string>('');
  const [page, setPage] = useState<number>(1);

  const isEntityMode = meta?.governance_mode === 'entity_assessment';
  const metrics = findingsPage?.metrics;
  const items = findingsPage?.items || [];
  const total = findingsPage?.total || 0;

  useEffect(() => {
    onFilterChange({
      severity: selectedSev,
      status: selectedStatus,
      keyword: keyword.trim(),
      page,
      pageSize: 50,
    });
  }, [selectedSev, selectedStatus, keyword, page]);

  const handleSevChip = (sev: string) => {
    setSelectedSev(prev => (prev === sev ? '' : sev));
    setPage(1);
  };

  return (
    <div>
      {/* 过滤工具栏 */}
      <div className="findings-filter-toolbar no-print">
        {/* 严重度/结论过滤 Chips */}
        <div className="filter-chips">
          <button
            className="chip-btn"
            style={{
              background: selectedSev === '' ? 'var(--primary-color, #2563eb)' : 'var(--bg-color, #f1f5f9)',
              color: selectedSev === '' ? '#ffffff' : 'var(--text-secondary, #475569)',
            }}
            onClick={() => handleSevChip('')}
          >
            全部 ({metrics?.total_findings ?? total})
          </button>

          {isEntityMode ? (
            <>
              <button
                className="chip-btn"
                style={{
                  background: selectedSev === 'pass' ? '#10b981' : 'rgba(16, 185, 129, 0.12)',
                  color: selectedSev === 'pass' ? '#ffffff' : '#059669',
                }}
                onClick={() => handleSevChip('pass')}
              >
                合格 ({metrics?.pass_count ?? 0})
              </button>
              <button
                className="chip-btn"
                style={{
                  background: selectedSev === 'fatal' ? '#ef4444' : 'rgba(239, 68, 68, 0.12)',
                  color: selectedSev === 'fatal' ? '#ffffff' : '#dc2626',
                }}
                onClick={() => handleSevChip('fatal')}
              >
                不合格/风险 ({(metrics?.fatal_count ?? 0) + (metrics?.critical_count ?? 0) + (metrics?.major_count ?? 0)})
              </button>
            </>
          ) : (
            <>
              {(metrics?.fatal_count ?? 0) > 0 && (
                <button
                  className="chip-btn"
                  style={{
                    background: selectedSev === 'fatal' ? '#ef4444' : 'rgba(239, 68, 68, 0.12)',
                    color: selectedSev === 'fatal' ? '#ffffff' : '#dc2626',
                  }}
                  onClick={() => handleSevChip('fatal')}
                >
                  致命 ({metrics?.fatal_count})
                </button>
              )}
              {(metrics?.critical_count ?? 0) > 0 && (
                <button
                  className="chip-btn"
                  style={{
                    background: selectedSev === 'critical' ? '#f97316' : 'rgba(249, 115, 22, 0.12)',
                    color: selectedSev === 'critical' ? '#ffffff' : '#ea580c',
                  }}
                  onClick={() => handleSevChip('critical')}
                >
                  严重 ({metrics?.critical_count})
                </button>
              )}
              {(metrics?.major_count ?? 0) > 0 && (
                <button
                  className="chip-btn"
                  style={{
                    background: selectedSev === 'major' ? '#eab308' : 'rgba(234, 179, 8, 0.12)',
                    color: selectedSev === 'major' ? '#ffffff' : '#ca8a04',
                  }}
                  onClick={() => handleSevChip('major')}
                >
                  一般 ({metrics?.major_count})
                </button>
              )}
              {(metrics?.suggestion_count ?? 0) > 0 && (
                <button
                  className="chip-btn"
                  style={{
                    background: selectedSev === 'suggestion' ? '#6b7280' : 'rgba(107, 114, 128, 0.12)',
                    color: selectedSev === 'suggestion' ? '#ffffff' : '#475569',
                  }}
                  onClick={() => handleSevChip('suggestion')}
                >
                  建议 ({metrics?.suggestion_count})
                </button>
              )}
            </>
          )}
        </div>

        {/* 关键字搜索与状态下拉 */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', flexWrap: 'wrap' }}>
          <select
            value={selectedStatus}
            onChange={(e) => {
              setSelectedStatus(e.target.value);
              setPage(1);
            }}
            style={{
              padding: '0.35rem 0.6rem',
              borderRadius: '6px',
              border: '1px solid var(--border-color, #cbd5e1)',
              background: 'var(--card-bg, #ffffff)',
              fontSize: '0.8rem',
              color: 'var(--text-color, #0f172a)',
            }}
          >
            <option value="">全部状态</option>
            {isEntityMode ? (
              <>
                <option value="pass">合格</option>
                <option value="fail">待整改/不合格</option>
                <option value="analyzing">复核中</option>
              </>
            ) : (
              <>
                <option value="open">待处理</option>
                <option value="analyzing">问题分析</option>
                <option value="resolved">已解决</option>
                <option value="closed">已关闭</option>
                <option value="invalid">忽略/误报</option>
              </>
            )}
          </select>

          <input
            type="text"
            placeholder="搜索标题、文件或详细描述..."
            value={keyword}
            onChange={(e) => {
              setKeyword(e.target.value);
              setPage(1);
            }}
            style={{
              padding: '0.35rem 0.65rem',
              borderRadius: '6px',
              border: '1px solid var(--border-color, #cbd5e1)',
              fontSize: '0.8rem',
              width: '200px',
              background: 'var(--card-bg, #ffffff)',
              color: 'var(--text-color, #0f172a)',
            }}
          />
        </div>
      </div>

      {/* 列表内容区 */}
      {loading && items.length === 0 ? (
        <div style={{ textAlign: 'center', padding: '3rem', color: 'var(--text-secondary, #64748b)' }}>
          ⏳ 正在加载问题清单...
        </div>
      ) : items.length === 0 ? (
        <ReportEmptyState
          type="pass"
          title={isEntityMode ? "实体评估合格！" : "代码检视通过！"}
          description={isEntityMode ? "所有评估用例及测试项均达到合格指标要求。" : "本次扫描未发现任何安全隐患或缺陷，状态良好。"}
        />
      ) : (
        <div>
          {items.map((item) => (
            <FindingCard
              key={`${item.id}-${item.file_path}-${item.line_number}`}
              finding={item}
              governanceMode={meta?.governance_mode}
              repoUrl={meta?.repo_url}
              branch={meta?.branch}
              isReadOnly={isReadOnly}
              onStatusChange={onStatusChange}
            />
          ))}

          {/* 分页控制 */}
          {findingsPage && findingsPage.totalPages > 1 && (
            <div className="no-print" style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', gap: '0.5rem', marginTop: '1.5rem' }}>
              <button
                className="nav-btn"
                disabled={page <= 1}
                onClick={() => setPage(p => Math.max(1, p - 1))}
              >
                ‹ 上一页
              </button>
              <span style={{ fontSize: '0.85rem', color: 'var(--text-secondary, #64748b)' }}>
                第 {page} / {findingsPage.totalPages} 页 (共 {total} 项)
              </span>
              <button
                className="nav-btn"
                disabled={page >= findingsPage.totalPages}
                onClick={() => setPage(p => Math.min(findingsPage.totalPages, p + 1))}
              >
                下一页 ›
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
