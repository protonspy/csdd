// Minimal typings for react-graph-vis@1.0.7, which ships no .d.ts and has no
// @types package on the registry.
//
// The vis Network handle is declared structurally rather than re-exported from
// vis-network: npm nests vis-network under react-graph-vis/node_modules, so its
// bundled types are not resolvable from this package. Only the surface the
// dashboard actually calls is declared here.
declare module 'react-graph-vis' {
  import type { Component, CSSProperties } from 'react'

  export interface VisAnimation {
    duration?: number
    easingFunction?: string
  }

  export interface VisNetwork {
    on(event: string, cb: (params: VisEventParams) => void): void
    off(event: string, cb?: (params: VisEventParams) => void): void
    once(event: string, cb: (params: VisEventParams) => void): void
    fit(opts?: { nodes?: string[]; animation?: boolean | VisAnimation }): void
    focus(nodeId: string, opts?: { scale?: number; animation?: boolean | VisAnimation }): void
    selectNodes(nodeIds: string[], highlightEdges?: boolean): void
    unselectAll(): void
    setOptions(options: Record<string, unknown>): void
    setSize(width: string, height: string): void
    redraw(): void
    destroy(): void
  }

  export interface VisEventParams {
    nodes: string[]
    edges: string[]
    pointer?: { DOM: { x: number; y: number }; canvas: { x: number; y: number } }
    event?: unknown
  }

  export interface VisNodeItem {
    id: string
    label?: string
    title?: string | HTMLElement
    value?: number
    borderWidth?: number
    color?: unknown
    font?: unknown
    shapeProperties?: unknown
  }

  export interface VisEdgeItem {
    id: string
    from: string
    to: string
    title?: string | HTMLElement
    color?: unknown
    width?: number
  }

  export interface GraphProps {
    graph: { nodes: VisNodeItem[]; edges: VisEdgeItem[] }
    options?: Record<string, unknown>
    events?: Record<string, (params: VisEventParams) => void>
    style?: CSSProperties
    identifier?: string
    getNetwork?: (network: VisNetwork) => void
    getNodes?: (nodes: unknown) => void
    getEdges?: (edges: unknown) => void
  }

  export default class Graph extends Component<GraphProps> {}
}
