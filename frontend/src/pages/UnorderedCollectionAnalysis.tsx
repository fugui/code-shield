import React from 'react';
import CampaignAnalysis from './CampaignAnalysis';

export default function UnorderedCollectionAnalysis() {
  return (
    <CampaignAnalysis
      campaign="unordered-collection"
      title="无序集合导出缺陷"
      description="针对 map 和 set 等无序集合，检测因直接迭代、导出为数组并依赖其内部排列顺序而导致的数据不稳定或计算一致性风险。"
      taskTypeName="unordered_collection"
    />
  );
}
