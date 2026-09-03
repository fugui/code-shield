import type { SubMenuItem, MenuGroup, ModuleMenuConfig } from '@code/common';
export type { SubMenuItem, MenuGroup, ModuleMenuConfig };

export interface TaskTypeMenuMeta {
  id: number;
  name: string;
  display_name: string;
  is_campaign: boolean;
  campaign_path: string;
  campaign_icon?: string;
  is_active: boolean;
}

// 默认内置专项的图标映射（未指定 icon 时的优雅兜底）
const DEFAULT_CAMPAIGN_ICONS: Record<string, string> = {
  ut: 'M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z',
  ut_effectiveness: 'M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z',
  coredump: 'M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z',
  coredump_risk: 'M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z',
  float: 'M16 8v8m-4-5v5m-4-2v2M2 4h20a2 2 0 012 2v12a2 2 0 01-2 2H2a2 2 0 01-2-2V6a2 2 0 012-2z',
  float_comparison: 'M16 8v8m-4-5v5m-4-2v2M2 4h20a2 2 0 012 2v12a2 2 0 01-2 2H2a2 2 0 01-2-2V6a2 2 0 012-2z',
  thread: 'M13 10V3L4 14h7v7l9-11h-7z',
  thread_create: 'M13 10V3L4 14h7v7l9-11h-7z',
  cjson: 'M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z',
  cjson_scan: 'M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z',
  'unordered-collection': 'M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2',
  unordered_collection: 'M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2',
  'deep-review': 'M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z',
  deep_review: 'M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z'
};

const DEFAULT_ICON = 'M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-3 7h3m-3 4h3m-6-4h.01M9 16h.01';

const CACHE_KEY = 'shield_task_types_cache';

const loadCachedTaskTypes = (): TaskTypeMenuMeta[] | null => {
  if (typeof window === 'undefined') return null;
  try {
    const cached = sessionStorage.getItem(CACHE_KEY);
    if (cached) {
      const parsed = JSON.parse(cached);
      if (Array.isArray(parsed) && parsed.length > 0) return parsed;
    }
  } catch (_) { /* ignore cache read errors */ }
  return null;
};

let currentTaskTypes: TaskTypeMenuMeta[] | null = loadCachedTaskTypes();
const menuListeners = new Set<(config: ModuleMenuConfig) => void>();

function notifyListeners(config: ModuleMenuConfig) {
  menuListeners.forEach(listener => {
    try { listener(config); } catch (e) { console.error('Menu listener error:', e); }
  });
}

export const buildDynamicMenuGroups = (taskTypes?: TaskTypeMenuMeta[]): MenuGroup[] => {
  let campaignItems: SubMenuItem[] = [];

  const types = (Array.isArray(taskTypes) && taskTypes.length > 0)
    ? taskTypes
    : (currentTaskTypes && currentTaskTypes.length > 0 ? currentTaskTypes : []);

  if (types.length > 0) {
    campaignItems = types
      .filter(tt => tt.is_campaign && tt.is_active !== false)
      .map(tt => {
        const pathKey = tt.campaign_path || tt.name;
        const icon = tt.campaign_icon || DEFAULT_CAMPAIGN_ICONS[pathKey] || DEFAULT_CAMPAIGN_ICONS[tt.name] || DEFAULT_ICON;
        return {
          path: `/analysis/${pathKey}`,
          label: tt.display_name,
          headerTitle: `${tt.display_name}专项`,
          icon
        };
      });
  }

  return [
    {
      title: '个人中心',
      items: [
        { path: '/workbench', label: '个人工作台', icon: 'M4 6a2 2 0 012-2h2a2 2 0 012 2v4a2 2 0 01-2 2H6a2 2 0 01-2-2V6z M14 6a2 2 0 012-2h2a2 2 0 012 2v4a2 2 0 01-2 2h-2a2 2 0 01-2-2V6z M4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2z M14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z' }
      ]
    },
    {
      title: '报告中心',
      items: [
        { path: '/reports', label: '报告概览', headerTitle: '报告中心', icon: 'M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-3 7h3m-3 4h3m-6-4h.01M9 16h.01' },
      ],
    },
    {
      title: '专项分析',
      items: campaignItems,
    },
    {
      title: '管理中心',
      adminOnly: true,
      items: [
        { path: '/admin/scan', label: '扫描任务', headerTitle: '扫描任务管理', icon: 'M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z M21 12a9 9 0 11-18 0 9 9 0 0118 0z', adminOnly: true },
        { path: '/admin/task-types', label: '任务类型', headerTitle: '任务类型管理', icon: 'M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10', adminOnly: true },
        { path: '/admin/config', label: '系统配置', headerTitle: '系统动态配置中心', icon: 'M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z M15 12a3 3 0 11-6 0 3 3 0 016 0z', adminOnly: true },
        { path: '/admin/debug', label: '系统诊断', headerTitle: '系统性能与堆栈诊断', icon: 'M9 3v2m6-2v2M9 19v2m6-2v2M5 9H3m2 6H3m18-6h-2m2 6h-2M7 19h10a2 2 0 002-2V7a2 2 0 00-2-2H7a2 2 0 00-2 2v10a2 2 0 002 2zM9 9h6v6H9V9z', adminOnly: true },
      ],
    },
  ];
};

let inFlightPromise: Promise<ModuleMenuConfig> | null = null;

export async function fetchShieldMenuConfig(): Promise<ModuleMenuConfig> {
  if (inFlightPromise) {
    return inFlightPromise;
  }

  inFlightPromise = (async () => {
    try {
      // 智能探测 API 路径：微前端容器环境下优先使用 /shield/api 前缀
      const isEmbedded = typeof window !== 'undefined' && !!window.__POWERED_BY_PORTAL__;
      const primaryUrl = isEmbedded ? '/shield/api/task-types?active_only=true' : '/api/task-types?active_only=true';

      let res = await fetch(primaryUrl);
      // 若在微前端模式下首次尝试失败且非 401（例如独立调试场景），优雅尝试回退路径
      if (!res.ok && res.status !== 401 && isEmbedded) {
        res = await fetch('/api/task-types?active_only=true');
      }

      if (res.ok) {
        const data = await res.json();
        if (Array.isArray(data)) {
          currentTaskTypes = data;
          try {
            sessionStorage.setItem(CACHE_KEY, JSON.stringify(data));
          } catch (_) { /* ignore quota/security errors */ }

          const dynamicConfig: ModuleMenuConfig = {
            moduleKey: 'shield',
            moduleName: '代码质量 (Code Shield)',
            groups: buildDynamicMenuGroups(data)
          };
          shieldMenuConfig.groups = dynamicConfig.groups;
          notifyListeners(dynamicConfig);
          return dynamicConfig;
        }
      }
    } catch (err) {
      console.warn('Failed to fetch dynamic task types for shield menu:', err);
    } finally {
      inFlightPromise = null;
    }
    return shieldMenuConfig;
  })();

  return inFlightPromise;
}

export function subscribeMenuChanges(listener: (config: ModuleMenuConfig) => void): () => void {
  menuListeners.add(listener);
  // 订阅时立即使用当前已知数据（含缓存）推送一次，确保微框架即时获取
  listener({
    moduleKey: 'shield',
    moduleName: '代码质量 (Code Shield)',
    groups: buildDynamicMenuGroups(currentTaskTypes || undefined)
  });
  // 后台异步刷新最新数据
  fetchShieldMenuConfig();
  return () => {
    menuListeners.delete(listener);
  };
}

// 监听全局任务类型变更与鉴权状态变更事件
if (typeof window !== 'undefined') {
  window.addEventListener('shield-task-types-changed', () => {
    fetchShieldMenuConfig();
  });
  window.addEventListener('auth-change', () => {
    fetchShieldMenuConfig();
  });
  // 模块加载时异步刷新一次
  fetchShieldMenuConfig();
}

export const shieldMenuConfig: ModuleMenuConfig = {
  moduleKey: 'shield',
  moduleName: '代码质量 (Code Shield)',
  groups: buildDynamicMenuGroups(currentTaskTypes || undefined)
};

export const menuGroups: MenuGroup[] = shieldMenuConfig.groups;
export const menuItems: SubMenuItem[] = shieldMenuConfig.groups.flatMap(group => group.items);

export default shieldMenuConfig;


