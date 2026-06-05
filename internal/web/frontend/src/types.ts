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
