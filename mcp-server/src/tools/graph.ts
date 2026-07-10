import { z } from "zod";
import { bool, flag, rootArg, rootField, type ToolDef } from "../tooldef.js";

// The knowledge-graph tools — the agent's read-first "brain" over the workspace.
// query/path/explain/analyze each rebuild the graph in memory from the corpus, so
// their answers are always current; build/export persist docs/graph/graph.{json,html}
// for humans and the web dashboard. Exposed so an MCP-only agent can consult the
// graph instead of shelling out (README: "Consult it before you grep").

const jsonField = z
  .boolean()
  .optional()
  .describe("Emit machine-readable JSON instead of the human-readable text summary.");

export const graphTools: ToolDef[] = [
  {
    name: "csdd_graph_build",
    title: "Graph build",
    description:
      "Rebuild docs/graph/graph.json.gz from the workspace corpus (specs, plans, ADRs, wiki, glossary, stack, CLAUDE.md, Go source). Incremental by default; pass full to force a complete rebuild.",
    inputSchema: {
      full: z.boolean().optional().describe("Force a full rebuild instead of the incremental default."),
      json: jsonField,
      root: rootField,
    },
    toArgs: (p) => ["graph", "build", ...bool("--full", p.full), ...bool("--json", p.json), ...rootArg(p)],
  },
  {
    name: "csdd_graph_query",
    title: "Graph query",
    description:
      "Find nodes whose label matches the terms and show their neighborhood — the fast, token-cheap way to locate an artifact and see what connects to it. Quote multi-word queries.",
    inputSchema: {
      terms: z.string().describe('Search terms, e.g. "AlbumService" or "photo upload".'),
      hops: z.number().int().optional().describe("Neighborhood radius (default 1)."),
      budget: z.number().int().optional().describe("Token budget for rendering (default 2000)."),
      json: jsonField,
      root: rootField,
    },
    toArgs: (p) => [
      "graph",
      "query",
      p.terms,
      ...flag("--hops", p.hops),
      ...flag("--budget", p.budget),
      ...bool("--json", p.json),
      ...rootArg(p),
    ],
  },
  {
    name: "csdd_graph_path",
    title: "Graph path",
    description:
      "Show the shortest path between two nodes — how a requirement connects to the code, or what links two components. Each endpoint is resolved by label; the response reports the tier (exact | prefix | substring) so a fuzzy match is never mistaken for an exact one.",
    inputSchema: {
      from: z.string().describe("Start node (label or id)."),
      to: z.string().describe("End node (label or id)."),
      maxHops: z.number().int().optional().describe("Maximum path length (0 = unbounded)."),
      json: jsonField,
      root: rootField,
    },
    toArgs: (p) => [
      "graph",
      "path",
      p.from,
      p.to,
      ...flag("--max-hops", p.maxHops),
      ...bool("--json", p.json),
      ...rootArg(p),
    ],
  },
  {
    name: "csdd_graph_explain",
    title: "Graph explain",
    description:
      "Show a node and all its connections, ordered by neighbor degree — the deep view of one artifact and everything it touches.",
    inputSchema: {
      label: z.string().describe("Node label or id to explain."),
      json: jsonField,
      root: rootField,
    },
    toArgs: (p) => ["graph", "explain", p.label, ...bool("--json", p.json), ...rootArg(p)],
  },
  {
    name: "csdd_graph_analyze",
    title: "Graph analyze",
    description:
      "Surface traceability gaps and wiki/tech/plan/glossary lints (untested criteria, orphan tasks, broken wikilinks, undeclared/phantom tech, id collisions, …). Pass strict to exit non-zero when any finding exists (the CI gate).",
    inputSchema: {
      strict: z.boolean().optional().describe("Exit non-zero (2) when any finding is reported."),
      json: jsonField,
      root: rootField,
    },
    toArgs: (p) => ["graph", "analyze", ...bool("--strict", p.strict), ...bool("--json", p.json), ...rootArg(p)],
  },
  {
    name: "csdd_graph_export",
    title: "Graph export",
    description:
      "Write docs/graph/graph.html — a self-contained interactive visualization of the current graph.",
    inputSchema: {
      out: z.string().optional().describe("Output path (default docs/graph/graph.html)."),
      json: jsonField,
      root: rootField,
    },
    toArgs: (p) => ["graph", "export", ...flag("--out", p.out), ...bool("--json", p.json), ...rootArg(p)],
  },
];
