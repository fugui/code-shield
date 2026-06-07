/**
 * Base path for the application, derived from Vite's `base` config.
 * - Dev mode (default): "/"
 * - Sub-path deployment: "/code-shield/" (set via VITE_BASE_PATH env var)
 *
 * `import.meta.env.BASE_URL` always reflects the Vite `base` option and
 * includes the trailing slash.
 */
const raw = import.meta.env.BASE_URL ?? '/';
export const BASE_PATH = raw.endsWith('/') ? raw.slice(0, -1) : raw;

/**
 * Prepend the base path to an absolute URL path.
 * e.g. apiUrl('/api/tasks') => '/code-shield/api/tasks' (or '/api/tasks' in dev)
 */
export function apiUrl(path: string): string {
  return BASE_PATH + path;
}

/**
 * Key used for storing the authentication token in localStorage.
 */
export const AUTH_TOKEN_KEY = 'code_shield_token';

/**
 * Helper to compute absolute navigation paths that dynamically prefix BASE_PATH
 * only when running in embedded portal mode.
 */
export function appNavigatePath(path: string): string {
  const isEmbeddedMode = !!(window as any).__POWERED_BY_PORTAL__;
  if (isEmbeddedMode) {
    return BASE_PATH + path;
  }
  return path;
}
