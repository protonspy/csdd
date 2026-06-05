#!/usr/bin/env node
// csdd-mcp — an MCP server (stdio) that exposes the csdd CLI as one tool per
// subcommand. Each tool shells out to the csdd binary headlessly and returns
// its stdout/stderr; non-zero exits are surfaced as MCP errors (exit 2 =
// validation failure).
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";

import { makeHandler } from "./tooldef.js";
import { allTools } from "./registry.js";

const version = "0.1.0";

async function main() {
  const server = new McpServer({ name: "csdd-mcp", version });

  for (const def of allTools) {
    server.registerTool(
      def.name,
      {
        title: def.title,
        description: def.description,
        inputSchema: def.inputSchema,
      },
      makeHandler(def),
    );
  }

  const transport = new StdioServerTransport();
  await server.connect(transport);
  // Never resolves; the process stays alive serving stdio until the client
  // closes the pipe.
}

main().catch((err) => {
  process.stderr.write(`csdd-mcp fatal: ${err?.stack || err}\n`);
  process.exit(1);
});
