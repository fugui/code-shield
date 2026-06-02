import React from 'react';
import CampaignAnalysis from './CampaignAnalysis';

export default function ThreadAnalysis() {
  return (
    <CampaignAnalysis
      campaign="thread"
      title="线程安全"
      description="分析 C/C++ 代码中显式创建线程的合理性与生命周期管理方式，防范并发和生命周期管理风险。"
      taskTypeName="thread_create"
    />
  );
}
