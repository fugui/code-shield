import React from 'react'
import ReactDOM from 'react-dom/client'
import { setupChunkErrorReloader } from '@code/common'
import App from './App.tsx'
import './index.css'

// 启动全局静态资源 Chunk 错误监听与自动重载兜底
setupChunkErrorReloader()

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
