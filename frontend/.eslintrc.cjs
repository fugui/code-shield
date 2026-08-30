module.exports = {
  root: true,
  env: {
    browser: true,
    es2020: true,
    node: true,
  },
  extends: [
    'eslint:recommended',
    'plugin:@typescript-eslint/recommended',
    'plugin:react-hooks/recommended',
  ],
  ignorePatterns: ['dist', 'node_modules', '.eslintrc.cjs'],
  parser: '@typescript-eslint/parser',
  parserOptions: {
    ecmaVersion: 'latest',
    sourceType: 'module',
  },
  plugins: ['react-refresh'],
  rules: {
    // 历史代码大量使用 any（API 响应等），当前暂以警告形式保留，不阻断 lint；
    // 后续可单独推进 any 类型收敛（no-explicit-any 建议最终恢复为 error）。
    '@typescript-eslint/no-explicit-any': 'warn',
    // Fast Refresh 为开发体验类规则，当前代码结构（context/工具函数与组件同文件）暂不强制。
    'react-refresh/only-export-components': 'off',
  },
}
