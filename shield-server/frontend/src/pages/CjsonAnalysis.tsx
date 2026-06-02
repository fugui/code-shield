import React from 'react';
import CampaignAnalysis from './CampaignAnalysis';

export default function CjsonAnalysis() {
  return (
    <CampaignAnalysis
      campaign="cjson"
      title="cJSON 缺陷"
      description="检测代码中 cJSON 库相关的内存申请与释放（内存泄漏）问题，防范潜在的指针安全隐患。"
      taskTypeName="cjson_scan"
    />
  );
}
