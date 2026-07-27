import './monaco'
import React from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { consumeTokenLink } from './auth'
import { initTheme } from './theme'
import './styles.css'

// Handle a /token/<token> magic link before the app mounts.
consumeTokenLink()

// Stamp <html data-theme> before React mounts, so the first paint is already in
// the right theme rather than flashing dark and correcting.
initTheme()

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
