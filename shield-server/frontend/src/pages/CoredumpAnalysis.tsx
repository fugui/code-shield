import React from 'react';
import CampaignAnalysis from './CampaignAnalysis';

export default function CoredumpAnalysis() {
  return (
    <CampaignAnalysis
      campaign="coredump"
      title="Coredump 风险"
      description="跟踪治理 C/C++ 核心代码中可能导致进程异常退出（Coredump）的高危隐患，落实每一个缺陷的认领与修复工作。"
      taskTypeName="coredump_risk"
    />
  );
}
