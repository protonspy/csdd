// Wire Monaco to use locally bundled web workers (via Vite's ?worker imports)
// and the locally bundled monaco-editor, so the dashboard works fully offline —
// no CDN fetch. Imported for its side effects before the app mounts.
import * as monaco from 'monaco-editor'
import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'
import jsonWorker from 'monaco-editor/esm/vs/language/json/json.worker?worker'
import tsWorker from 'monaco-editor/esm/vs/language/typescript/ts.worker?worker'
import { loader } from '@monaco-editor/react'

declare global {
  interface Window {
    MonacoEnvironment?: monaco.Environment
  }
}

self.MonacoEnvironment = {
  getWorker(_workerId: string, label: string) {
    if (label === 'json') return new jsonWorker()
    if (label === 'typescript' || label === 'javascript') return new tsWorker()
    return new editorWorker()
  },
}

loader.config({ monaco })
