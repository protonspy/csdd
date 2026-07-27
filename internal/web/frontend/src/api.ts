import type {
  Overview,
  SpecDetail,
  WorkspaceTree,
  FileContent,
  TestReport,
  PlanSummary,
  PlanDetail,
  WikiOverview,
  ADROverview,
  StackOverview,
  GlossaryOverview,
  RefResolution,
} from './types'
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
  // The graph is served gzip-compressed with Content-Encoding: gzip; the browser
  // decompresses it before we ever see the body, so this is a normal JSON fetch.
  graph: () => get<{ nodes?: unknown; links?: unknown }>('/api/graph'),
  tests: () => get<TestReport>('/api/tests'),
  plans: () => get<PlanSummary[]>('/api/plans'),
  plan: (slug: string) => get<PlanDetail>(`/api/plan/${encodeURIComponent(slug)}`),
  wiki: () => get<WikiOverview>('/api/wiki'),
  adr: () => get<ADROverview>('/api/adr'),
  stack: () => get<StackOverview>('/api/stack'),
  glossary: () => get<GlossaryOverview>('/api/glossary'),
  // Batched on purpose: a Refs column is resolved in one request, and the
  // server answers in the order asked.
  refs: (tokens: string[]) => {
    const q = new URLSearchParams()
    for (const t of tokens) q.append('token', t)
    return get<RefResolution[]>(`/api/ref?${q.toString()}`)
  },
}
