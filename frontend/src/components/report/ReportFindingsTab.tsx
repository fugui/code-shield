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

interface SeverityCardItem {
  key: string;
  name: string;
  count: number;
  color: string;
}

export default function ReportFindingsTab({
  meta,
  findingsPage,
  loading,
  onFilterChange,
  onStatusChange,
  isReadOnly = false,
}: ReportFindingsTabProps) {
  const isEntityMode = meta?.governance_mode === 'entity_assessment';
  const defaultKeys = isEntityMode
    ? ['pass', 'fatal']
    : ['fatal', 'critical', 'major', 'suggestion'];

  const [selectedSevs, setSelectedSevs] = useState<string[]>(defaultKeys);
  const [selectedStatus, setSelectedStatus] = useState<string>('');
  const [keyword, setKeyword] = useState<string>('');
  const [page, setPage] = useState<number>(1);

  const metrics = findingsPage?.metrics;
  const items = findingsPage?.items || [];
  const total = findingsPage?.total || 0;

  // 构建指标卡片列表
  const cards: SeverityCardItem[] = [];
  if (isEntityMode) {
    cards.push({
      key: 'pass',
      name: '合格项',
      count: metrics?.pass_count ?? 0,
      color: '#10b981',
    });
    cards.push({
      key: 'fatal',
      name: '不合格 / 风险',
      count: (metrics?.fatal_count ?? 0) + (metrics?.critical_count ?? 0) + (metrics?.major_count ?? 0),
      color: '#ef4444',
    });
  } else {
    if ((metrics?.fatal_count ?? 0) > 0) {
      cards.push({
        key: 'fatal',
        name: '致命',
        count: metrics?.fatal_count ?? 0,
        color: '#dc2626',
      });
    }
    cards.push({
      key: 'critical',
      name: '严重',
      count: metrics?.critical_count ?? 0,
      color: '#ea580c',
    });
    cards.push({
      key: 'major',
      name: '一般',
      count: metrics?.major_count ?? 0,
      color: '#d97706',
    });
    cards.push({
      key: 'suggestion',
      name: '建议',
      count: metrics?.suggestion_count ?? 0,
      color: '#64748b',
    });
  }

  const allAvailableKeys = cards.map(c => c.key);
  const isAllSelected = allAvailableKeys.length > 0 && allAvailableKeys.every(k => selectedSevs.includes(k));

  useEffect(() => {
    // 全选或未选时，severity 传空代表查询全部
    const sevParam = isAllSelected || selectedSevs.length === 0 ? '' : selectedSevs.join(',');
    onFilterChange({
      severity: sevParam,
      status: selectedStatus,
      keyword: keyword.trim(),
      page,
      pageSize: 50,
    });
  }, [selectedSevs, selectedStatus, keyword, page, isAllSelected]);

  // 点击卡片智能切换 (全选状态下点击某张则单独选中该项；部分选中状态下 toggle)
  const handleCardClick = (key: string) => {
    if (isAllSelected) {
      // 当前是全选状态，用户点击某卡片通常意图是“单独查看此级别”
      setSelectedSevs([key]);
    } else {
      // 当前是部分筛选状态，进行多选 Toggle
      if (selectedSevs.includes(key)) {
        const next = selectedSevs.filter(k => k !== key);
        // 如果取消到 0 个，自动恢复全选
        setSelectedSevs(next.length === 0 ? allAvailableKeys : next);
      } else {
        setSelectedSevs([...selectedSevs, key]);
      }
    }
    setPage(1);
  };

  const handleResetAllSevs = () => {
    setSelectedSevs(allAvailableKeys);
    setPage(1);
  };

  return (
    <div>
      {/* 1. 严重性指标卡片看板 (默认全选中高亮，支持智能单选/多选组合) */}
      <div className="severity-cards-grid no-print">
        {cards.map((c) => {
          const isSelected = selectedSevs.includes(c.key);
          return (
            <div
              key={c.key}
              onClick={() => handleCardClick(c.key)}
              className={`severity-metric-card ${isSelected ? 'selected' : ''}`}
              style={{
                border: isSelected ? `1.5px solid ${c.color}` : '1.5px solid var(--border-color, #e2e8f0)',
                opacity: isSelected ? 1 : 0.45,
              }}
            >
              <div className="card-top-row">
                <span
                  className="card-sev-name"
                  style={{ color: isSelected ? c.color : 'var(--text-secondary, #64748b)' }}
                >
                  {c.name}
                </span>
                <div
                  className="filter-checkbox"
                  style={{
                    borderColor: isSelected ? c.color : '#cbd5e1',
                    background: isSelected ? c.color : 'transparent',
                  }}
                >
                  {isSelected && (
                    <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="4" strokeLinecap="round" strokeLinejoin="round">
                      <polyline points="20 6 9 17 4 12" />
                    </svg>
                  )}
                </div>
              </div>

              <div className="card-count-row">
                <span
                  className="card-count-number"
                  style={{ color: isSelected ? c.color : 'var(--text-color, #0f172a)' }}
                >
                  {c.count}
                </span>
                <span className="card-count-unit">个</span>
              </div>

              <div
                className="card-color-bar"
                style={{
                  background: isSelected ? c.color : '#e2e8f0',
                }}
              />
            </div>
          );
        })}
      </div>

      {/* 2. 独立搜索与状态过滤工具栏 */}
      <div className="findings-filter-toolbar no-print">
        <div className="filter-summary-info">
          <span>共检索到 <strong>{total}</strong> 项结果</span>
          {!isAllSelected && (
            <button
              type="button"
              className="filter-reset-btn"
              onClick={handleResetAllSevs}
              title="恢复所有级别全选"
            >
              ✕ 恢复全部级别
            </button>
          )}
        </div>

        <div className="filter-search-group">
          <select
            className="filter-status-select"
            value={selectedStatus}
            onChange={(e) => {
              setSelectedStatus(e.target.value);
              setPage(1);
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

          <div className="filter-search-input-wrapper">
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" style={{ position: 'absolute', left: '9px', color: 'var(--text-secondary, #94a3b8)', pointerEvents: 'none' }}>
              <circle cx="11" cy="11" r="8" />
              <line x1="21" y1="21" x2="16.65" y2="16.65" />
            </svg>
            <input
              type="text"
              className="filter-search-input"
              placeholder="搜索标题、文件路径..."
              value={keyword}
              onChange={(e) => {
                setKeyword(e.target.value);
                setPage(1);
              }}
            />
            {keyword && (
              <button
                type="button"
                className="filter-search-clear"
                onClick={() => setKeyword('')}
                title="清空搜索"
              >
                ✕
              </button>
            )}
          </div>
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
