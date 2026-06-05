import './monaco'
import React from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { consumeTokenLink } from './auth'
import './styles.css'

// Handle a /token/<token> magic link before the app mounts.
consumeTokenLink()

createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
