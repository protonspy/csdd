// Single source of truth for the browsable "Resources" — the agent/skill/steering
// artifact kinds the dashboard lists and views. App, Sidebar, and ResourceView all
// consume this registry instead of each branching on the kind, so adding a kind is
// a one-line change here.
import type { Artifact, Overview } from './types'

export type ResourceKind = 'agent' | 'skill' | 'steering'

export interface ResourceDef {
  kind: ResourceKind
  /** Singular label, e.g. shown as a badge on the resource view. */
  label: string
  /** Plural section title in the sidebar. */
  plural: string
  /** Where this kind's artifacts live in the workspace overview. */
  items: (ov: Overview | null) => Artifact[]
}

export const RESOURCES: ResourceDef[] = [
  { kind: 'agent', label: 'Agent', plural: 'Agents', items: (ov) => ov?.agents ?? [] },
  { kind: 'skill', label: 'Skill', plural: 'Skills', items: (ov) => ov?.skills ?? [] },
  { kind: 'steering', label: 'Steering', plural: 'Steering', items: (ov) => ov?.steering ?? [] },
]

const BY_KIND: Record<ResourceKind, ResourceDef> = Object.fromEntries(
  RESOURCES.map((r) => [r.kind, r]),
) as Record<ResourceKind, ResourceDef>

/** Singular display label for a resource kind. */
export const resourceLabel = (kind: ResourceKind): string => BY_KIND[kind].label

/** Sidebar/empty-state hint listing the browsable kinds, e.g. "agent, skill, or steering". */
export const resourceKindsHint = (): string => {
  const labels = RESOURCES.map((r) => r.label.toLowerCase())
  if (labels.length <= 1) return labels.join('')
  return `${labels.slice(0, -1).join(', ')}, or ${labels[labels.length - 1]}`
}
