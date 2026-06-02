import React from 'react';
import CampaignAnalysis from './CampaignAnalysis';

export default function FloatAnalysis() {
  return (
    <CampaignAnalysis
      campaign="float"
      title="Python 浮点数"
      description="深度分析 Python 代码中的浮点数精度比较和常见逻辑缺陷，提升浮点运算部分的准确性和稳定性。"
      taskTypeName="float_comparison"
    />
  );
}
