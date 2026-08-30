/// <reference types="vite/client" />

interface Window {
  /** 微前端（qiankun/portal）注入的全局标记 */
  __POWERED_BY_PORTAL__?: boolean;
}
