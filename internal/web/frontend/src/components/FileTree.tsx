import { useState } from 'react'
import type { TreeNode } from '../types'

interface Props {
  nodes: TreeNode[]
  selectedPath: string | null
  onOpenFile: (path: string) => void
}

export function FileTree({ nodes, selectedPath, onOpenFile }: Props) {
  const list = nodes ?? []
  if (list.length === 0) {
    return <div className="muted small">empty</div>
  }
  return (
    <div className="tree">
      {list.map((n) => (
        <TreeItem key={n.path || n.name} node={n} depth={0} selectedPath={selectedPath} onOpenFile={onOpenFile} />
      ))}
    </div>
  )
}

function TreeItem({
  node,
  depth,
  selectedPath,
  onOpenFile,
}: {
  node: TreeNode
  depth: number
  selectedPath: string | null
  onOpenFile: (path: string) => void
}) {
  const [open, setOpen] = useState(depth < 1) // top-level expanded by default
  const pad = { paddingLeft: 6 + depth * 12 }

  if (node.dir) {
    return (
      <div>
        <button className="tree-row dir" style={pad} onClick={() => setOpen(!open)}>
          <span className={`caret ${open ? 'open' : ''}`}>▸</span>
          <span className="tree-name">{node.name}</span>
        </button>
        {open &&
          node.children?.map((c) => (
            <TreeItem
              key={c.path || c.name}
              node={c}
              depth={depth + 1}
              selectedPath={selectedPath}
              onOpenFile={onOpenFile}
            />
          ))}
      </div>
    )
  }

  return (
    <button
      className={`tree-row file ${selectedPath === node.path ? 'active' : ''}`}
      style={pad}
      onClick={() => onOpenFile(node.path)}
      title={node.path}
    >
      <span className="tree-dot" />
      <span className="tree-name">{node.name}</span>
    </button>
  )
}
