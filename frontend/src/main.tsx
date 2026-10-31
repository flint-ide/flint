import React from 'react'
import { createRoot } from 'react-dom/client'
import './style.css'
import { loadSettings } from './store/settings' // applies the cached theme before the first paint
import App from './App'
import { installErrorReporting } from './store/diagnostics'

installErrorReporting()
void loadSettings()

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
