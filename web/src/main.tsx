import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import { SessionProvider } from './session'
import { ThemeProvider } from './theme'
import { ToastProvider } from './toast'
import './styles.css'

const root = document.getElementById('root')
if (!root) throw new Error('未找到 #root 挂载点')

createRoot(root).render(
  <StrictMode>
    <ThemeProvider>
      <ToastProvider>
        <SessionProvider>
          <BrowserRouter>
            <App />
          </BrowserRouter>
        </SessionProvider>
      </ToastProvider>
    </ThemeProvider>
  </StrictMode>,
)