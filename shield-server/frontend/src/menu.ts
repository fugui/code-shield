export interface SubMenuItem {
  path: string;
  label: string;
}

export const menuItems: SubMenuItem[] = [
  { path: '/tasks', label: '任务中心' },
  { path: '/issues', label: '问题清单' },
  { path: '/opensource', label: '开源管理' },
  { path: '/teams', label: '团队管理' },
  { path: '/config', label: '系统管理' }
];
