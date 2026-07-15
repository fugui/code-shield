import React from 'react';
import CampaignAnalysis from './CampaignAnalysis';

export default function DeepReviewAnalysis() {
  return (
    <CampaignAnalysis
      campaign="deep-review"
      title="深度代码分析"
      description="对整个代码仓进行深度的架构审计与全量存量代码安全质量评估，扫描隐藏的系统性风险。"
      taskTypeName="deep_review"
    />
  );
}
