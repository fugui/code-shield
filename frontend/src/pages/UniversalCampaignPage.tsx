import React, { useEffect, useState } from 'react';
import { useParams, Navigate } from 'react-router-dom';
import CampaignAnalysis from './CampaignAnalysis';
import { appNavigatePath } from '../config';

interface TaskTypeMeta {
  id: number;
  name: string;
  display_name: string;
  description: string;
  is_campaign: boolean;
  campaign_path: string;
  governance_mode: 'defect_tracking' | 'entity_assessment';
  campaign_icon?: string;
  is_active: boolean;
}

export default function UniversalCampaignPage() {
  const { campaignKey } = useParams<{ campaignKey: string }>();
  const [taskTypes, setTaskTypes] = useState<TaskTypeMeta[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetch('/api/task-types?active_only=true')
      .then(res => res.ok ? res.json() : [])
      .then(data => {
        if (Array.isArray(data)) {
          setTaskTypes(data);
        }
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  if (!campaignKey) {
    return <Navigate to={appNavigatePath('/reports')} replace />;
  }

  if (loading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '60vh', color: '#64748b' }}>
        <div style={{ textAlign: 'center' }}>
          <div style={{ animation: 'spin 1s linear infinite', border: '3px solid rgba(59, 130, 246, 0.1)', borderTop: '3px solid #3b82f6', borderRadius: '50%', width: '32px', height: '32px', margin: '0 auto 1rem' }} />
          正在加载专项分析看板...
        </div>
      </div>
    );
  }

  // 匹配 TaskType
  const matched = taskTypes.find(
    tt => (tt.campaign_path && tt.campaign_path === campaignKey) || tt.name === campaignKey
  );

  if (!matched) {
    // 兼容可能传入的旧静态路径（如 ut 对应 ut_effectiveness）
    const legacyFallback = taskTypes.find(tt => {
      if (campaignKey === 'ut' && tt.name === 'ut_effectiveness') return true;
      if (campaignKey === 'float' && tt.name === 'float_comparison') return true;
      if (campaignKey === 'coredump' && tt.name === 'coredump_risk') return true;
      if (campaignKey === 'thread' && tt.name === 'thread_create') return true;
      if (campaignKey === 'cjson' && tt.name === 'cjson_scan') return true;
      if (campaignKey === 'unordered-collection' && tt.name === 'unordered_collection') return true;
      if (campaignKey === 'deep-review' && tt.name === 'deep_review') return true;
      return false;
    });

    if (legacyFallback) {
      return (
        <CampaignAnalysis
          campaign={campaignKey}
          title={legacyFallback.display_name}
          description={legacyFallback.description}
          taskTypeName={legacyFallback.name}
          governanceMode={legacyFallback.governance_mode || 'defect_tracking'}
        />
      );
    }

    return (
      <div style={{ textAlign: 'center', padding: '5rem 2rem', color: '#64748b' }}>
        <h2 style={{ fontSize: '1.25rem', color: 'var(--text-color)', marginBottom: '0.5rem' }}>未找到对应的专项分析治理任务</h2>
        <p style={{ fontSize: '0.875rem', marginBottom: '1.5rem' }}>可能该专项已被停用或 URL 别名已更改（{campaignKey}）</p>
        <a href={appNavigatePath('/reports')} className="btn" style={{ padding: '0.5rem 1.25rem', textDecoration: 'none' }}>
          返回报告中心
        </a>
      </div>
    );
  }

  return (
    <CampaignAnalysis
      campaign={campaignKey}
      title={matched.display_name}
      description={matched.description}
      taskTypeName={matched.name}
      governanceMode={matched.governance_mode || 'defect_tracking'}
    />
  );
}
