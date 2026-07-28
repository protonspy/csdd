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
  /** unit | tdd | tdd-e2e; absent on legacy specs (treated as tdd). Gates the TDD-only displays. */
  developmentFlow?: string
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

/** One task's own recorded run, keyed by task ID in SpecReport.tasks. */
export interface SpecTaskReport {
  updatedAt: string
  command?: string
  tests?: SpecTestCounts
  coverage?: SpecCovSummary
  attentions?: string[]
}

export interface SpecReport {
  feature: string
  updatedAt: string
  command?: string
  tests?: SpecTestCounts
  coverage?: SpecCovSummary
  testPaths?: string[]
  attentions?: string[]
  /**
   * Per-task evidence, keyed by task ID, written by
   * `csdd spec test-report <feature> --run --task <id>`. The fields above stay
   * the latest run's rollup; this is what says WHICH task produced a result,
   * which is the only thing that survives several implementers running at once.
   */
  tasks?: Record<string, SpecTaskReport>
}

export interface SpecDetail extends SpecCard {
  phases: TaskPhase[]
  issueList: ValidationIssue[]
  report: SpecReport | null
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
  feats: number
  done: number
  /** Every feat delivered. Derived server-side so each surface agrees. */
  complete: boolean
}

export interface PlanFeat {
  slug: string
  num: string
  objective: string
  milestone: string
  depends: string[]
  parallel: boolean
  state: string
  /** Citation tokens from the feat's Refs cell, verbatim and in table order. */
  refs: string[]
  tasks_total: number
  tasks_checked: number
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
  complete: boolean
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

// ---------------------------------------------------------------------------
// Citations. One grammar — [[wiki-page]] · adr:<slug> · stack:<name> · and the
// in-app kinds spec:/feat:/term: — resolved server-side by /api/ref so the UI
// and `csdd plan validate` can never disagree about what a token points at.

export type RefState = 'ok' | 'broken' | 'superseded' | 'ambiguous'
export type RefKind = 'wiki' | 'adr' | 'stack' | 'spec' | 'feat' | 'term' | 'unknown'

export interface RefResolution {
  token: string
  kind: RefKind
  label: string
  state: RefState
  title?: string
  body?: string
  /** Provenance: the file the target lives in. */
  meta?: string
  /** Hash route for the target; absent when nothing resolves. */
  route?: string
  /** For a superseded record: the token to cite instead. */
  successor?: string
}

export interface ADRRecord {
  number: number
  slug: string
  title: string
  body: string
  status: string
  superseded_by?: number
  superseded_by_slug?: string
  file: string
  cited_by: string[]
}

export interface ADROverview {
  present: boolean
  records: ADRRecord[]
}

export interface StackRow {
  name: string
  domain: string
  choice: string
  version: string
  why: string
  refs: string[]
  cited_by: string[]
}

export interface StackOverview {
  present: boolean
  rows: StackRow[]
}

export interface GlossaryTerm {
  canonical: string
  cluster?: string
  definition: string
  avoid?: string[]
  line?: number
  cited_by: string[]
}

export interface GlossaryOverview {
  present: boolean
  terms: GlossaryTerm[]
  issues?: string[]
}
