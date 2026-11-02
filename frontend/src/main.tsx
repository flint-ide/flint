import React from 'react'
import { createRoot } from 'react-dom/client'
import './style.css'
import { loadSettings } from './store/settings' // applies the cached theme before the first paint
import App from './App'
import { installFeatures } from './features/install'
import { installErrorReporting } from './store/diagnostics'

installErrorReporting()
installFeatures() // panels, Settings pages, status items and settings sections, before anything renders
void loadSettings()

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
