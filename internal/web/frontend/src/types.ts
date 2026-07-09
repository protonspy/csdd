// Mirrors the JSON shapes emitted by internal/web handlers (which serialize
// internal/session types). Keep in sync with those Go structs.

export interface Approval {
  generated: boolean
  approved: boolean
}

export interface TaskStats {
  total: number
  done: number
  red: number
  green: number
  pct: number
}

export interface Artifact {
  name: string
  path: string
  description?: string // agents, skills, steering(auto)
  tools?: string // agents
  model?: string // agents, skills (optional override)
  effort?: string // agents, skills (optional override)
  inclusion?: string // steering: always | fileMatch | manual | auto
}

export interface SpecCard {
  feature: string
  phase: string
  language: string
  createdAt: string
  ready: boolean
  readable: boolean
  approvals: Record<string, Approval>
  artifacts: string[]
  tasks: TaskStats
  issues: number
}

export interface Task {
  id: string
  title: string
  done: boolean
  parallel: boolean
  boundary?: string
  requirements?: string[]
  depends?: string[]
  tdd?: string
  indent: number
}

export interface TaskPhase {
  name: string
  tasks: Task[]
}

export interface ValidationIssue {
  file: string
  line: number
  msg: string
}

export interface SpecTestCounts {
  total: number
  passed: number
  failed: number
  skipped: number
}

export interface SpecCovSummary {
  pct: number
  covered: number
  lines: number
}

export interface SpecReport {
  feature: string
  updatedAt: string
  command?: string
  tests?: SpecTestCounts
  coverage?: SpecCovSummary
  testPaths?: string[]
  attentions?: string[]
}

export interface DiffLine {
  type: 'context' | 'add' | 'del'
  old?: number
  new?: number
  text: string
}

export interface DiffHunk {
  oldStart: number
  oldLines: number
  newStart: number
  newLines: number
  header?: string
  lines: DiffLine[]
}

export interface DiffFile {
  path: string
  oldPath?: string
  status: 'added' | 'modified' | 'deleted' | 'renamed'
  additions: number
  deletions: number
  binary?: boolean
  truncated?: boolean
  hunks?: DiffHunk[]
}

export interface DiffSummary {
  files: number
  additions: number
  deletions: number
}

export interface SpecDiff {
  feature: string
  updatedAt: string
  base: string
  baseSha?: string
  head: string
  summary: DiffSummary
  files: DiffFile[]
  truncated?: boolean
}

export interface SpecDetail extends SpecCard {
  phases: TaskPhase[]
  issueList: ValidationIssue[]
  report: SpecReport | null
  diff: SpecDiff | null
}

export interface Overview {
  root: string
  version: number
  specs: SpecCard[]
  steering: Artifact[]
  skills: Artifact[]
  agents: Artifact[]
  mcp: Artifact[]
  hooks: Artifact[]
  commands: Artifact[]
}

export interface TreeNode {
  name: string
  path: string
  dir: boolean
  children?: TreeNode[]
}

export interface WorkspaceTree {
  csdd: TreeNode[]
  project: TreeNode[]
}

export interface FileContent {
  path: string
  lang: string
  text: string
}

export interface FileCoverage {
  path: string
  pct: number
  lines: number
  covered: number
}

export interface Coverage {
  format: string // 'lcov' | 'jacoco' | 'cobertura' | 'gocover'
  source: string
  pct: number
  lines: number
  covered: number
  files: FileCoverage[]
}

export interface TestSuite {
  name: string
  total: number
  passed: number
  failed: number
  skipped: number
  time: number
}

export interface TestFailure {
  suite: string
  name: string
  message: string
}

export interface TestSummary {
  source: string
  total: number
  passed: number
  failed: number
  skipped: number
  durationSec: number
  suites: TestSuite[]
  failures?: TestFailure[]
}

export interface TestReport {
  coverage: Coverage | null
  tests: TestSummary | null
  sources: string[]
}

// Plans — the layer above specs. A plan (docs/plans/<slug>/) decomposes an
// initiative into feats, each becoming one spec; all state is derived read-only.
export interface PlanSummary {
  slug: string
  name: string
  approved: boolean
  drift: boolean
  deviations: number
  feats: number
  done: number
  blocked: number
}

export interface PlanFeat {
  slug: string
  num: string
  objective: string
  milestone: string
  depends: string[]
  parallel: boolean
  state: string
  tasks_total: number
  tasks_checked: number
  block_kind?: string
  block_reason?: string
  block_log?: string
}

export interface MilestoneProgress {
  name: string
  total: number
  done: number
}

export interface PlanDetail {
  slug: string
  name: string
  approved: boolean
  drift: boolean
  deviations: number
  feats: PlanFeat[]
  milestones: MilestoneProgress[]
}

// Wiki — the LLM-authored knowledge base under docs/wiki/. docs/wiki/index.md is
// the structuring catalog (categories + order); pages/ holds the content.
export interface WikiLink {
  text: string
  target: string // resolved page slug, or "" when broken
  broken: boolean
}

export interface WikiPage {
  slug: string
  title: string
  path: string
  category: string
  tags?: string[]
  sources?: string[]
  links: WikiLink[]
  in_index: boolean
}

export interface WikiOverview {
  present: boolean
  has_index: boolean
  categories: string[]
  pages: WikiPage[]
  raw_sources: string[]
}
