import { z } from "zod";
import {
  bool,
  flag,
  forceField,
  multi,
  rootArg,
  rootField,
  type ToolDef,
} from "../tooldef.js";

const serverName = z.string().describe("MCP server name (key in .mcp.json).");

export const mcpTools: ToolDef[] = [
  {
    name: "csdd_mcp_add",
    title: "MCP add server",
    description:
      "Add an MCP server to .mcp.json. Provide either command (+arg) for stdio, or url (+type) for a remote server. Use force to replace an existing entry.",
    inputSchema: {
      name: serverName,
      command: z.string().optional().describe("Executable for a stdio server."),
      arg: z.array(z.string()).optional().describe("Arguments for the command. Repeatable."),
      url: z.string().optional().describe("Endpoint for a remote server (mutually exclusive with command)."),
      type: z.enum(["sse", "http"]).optional().describe("Remote transport (default http when url set)."),
      env: z
        .array(z.string())
        .optional()
        .describe("Environment variables as KEY=VALUE. Repeatable."),
      disabled: z.boolean().optional().describe("Add in disabled state."),
      autoApprove: z
        .array(z.string())
        .optional()
        .describe("Tools to auto-approve (avoid; breaks least privilege). Repeatable."),
      force: forceField,
      root: rootField,
    },
    toArgs: (p) => [
      "mcp",
      "add",
      p.name,
      ...flag("--command", p.command),
      ...multi("--arg", p.arg),
      ...flag("--url", p.url),
      ...flag("--type", p.type),
      ...multi("--env", p.env),
      ...bool("--disabled", p.disabled),
      ...multi("--auto-approve", p.autoApprove),
      ...bool("--force", p.force),
      ...rootArg(p),
    ],
  },
  {
    name: "csdd_mcp_list",
    title: "MCP list",
    description: "List MCP servers with type, state, and endpoint.",
    inputSchema: { root: rootField },
    toArgs: (p) => ["mcp", "list", ...rootArg(p)],
  },
  {
    name: "csdd_mcp_show",
    title: "MCP show",
    description: "Print a server's config as pretty JSON.",
    inputSchema: { name: serverName, root: rootField },
    toArgs: (p) => ["mcp", "show", p.name, ...rootArg(p)],
  },
  {
    name: "csdd_mcp_remove",
    title: "MCP remove",
    description: "Remove a server from .mcp.json. Requires force.",
    inputSchema: { name: serverName, force: forceField, root: rootField },
    toArgs: (p) => ["mcp", "remove", p.name, ...bool("--force", p.force), ...rootArg(p)],
  },
  {
    name: "csdd_mcp_enable",
    title: "MCP enable",
    description: "Enable a server (disabled=false).",
    inputSchema: { name: serverName, root: rootField },
    toArgs: (p) => ["mcp", "enable", p.name, ...rootArg(p)],
  },
  {
    name: "csdd_mcp_disable",
    title: "MCP disable",
    description: "Disable a server (disabled=true).",
    inputSchema: { name: serverName, root: rootField },
    toArgs: (p) => ["mcp", "disable", p.name, ...rootArg(p)],
  },
  {
    name: "csdd_mcp_validate",
    title: "MCP validate",
    description: "Validate .mcp.json for schema errors. Exit 2 on issues.",
    inputSchema: { root: rootField },
    toArgs: (p) => ["mcp", "validate", ...rootArg(p)],
  },
];
