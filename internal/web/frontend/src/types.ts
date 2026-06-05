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

export interface SpecDetail extends SpecCard {
  phases: TaskPhase[]
  issueList: ValidationIssue[]
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
