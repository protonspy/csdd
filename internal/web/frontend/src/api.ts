import type { Overview, SpecDetail, WorkspaceTree, FileContent, TestReport } from './types'
import { authHeader, requireAuth } from './auth'

async function get<T>(url: string): Promise<T> {
  const res = await fetch(url, { headers: { ...authHeader() } })
  if (res.status === 401) {
    requireAuth()
    throw new Error('unauthorized')
  }
  if (!res.ok) {
    throw new Error(`${url} → ${res.status}`)
  }
  return (await res.json()) as T
}

export const api = {
  overview: () => get<Overview>('/api/overview'),
  spec: (feature: string) => get<SpecDetail>(`/api/spec/${encodeURIComponent(feature)}`),
  tree: () => get<WorkspaceTree>('/api/tree'),
  file: (path: string) => get<FileContent>(`/api/file?path=${encodeURIComponent(path)}`),
  tests: () => get<TestReport>('/api/tests'),
}
