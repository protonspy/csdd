// Shared types + helpers for declaring csdd-backed MCP tools.
import { z, type ZodRawShape } from "zod";
import { runCsdd, type CsddResult } from "./csdd.js";

export interface ToolDef {
  /** MCP tool name, e.g. "csdd_spec_generate". */
  name: string;
  /** Human title shown in clients. */
  title: string;
  /** When/why an agent should call this tool. */
  description: string;
  /** Zod raw shape (object of field schemas) describing the inputs. */
  inputSchema: ZodRawShape;
  /** Map validated params to the csdd argv (excluding the binary itself). */
  toArgs: (params: Record<string, any>) => string[];
}

// --- argv builders ---------------------------------------------------------

/** `--flag value` when value is a non-empty string/number, else nothing. */
export function flag(name: string, value: unknown): string[] {
  if (value === undefined || value === null || value === "") return [];
  return [name, String(value)];
}

/** `--flag` when truthy, else nothing. */
export function bool(name: string, value: unknown): string[] {
  return value ? [name] : [];
}

/** Repeat `--flag value` for each item. */
export function multi(name: string, values: unknown): string[] {
  if (!Array.isArray(values)) return [];
  return values.flatMap((v) => [name, String(v)]);
}

/** Standard `--root` passthrough (most commands accept it). */
export function rootArg(params: Record<string, any>): string[] {
  return flag("--root", params.root);
}

// Reusable field schemas -----------------------------------------------------
export const rootField = z
  .string()
  .optional()
  .describe(
    "Workspace root (the directory containing .claude/). Defaults to walking up from the current directory.",
  );

export const forceField = z
  .boolean()
  .optional()
  .describe("Bypass safety checks / overwrite or delete without confirmation.");

// --- result formatting -----------------------------------------------------

/** Turn a csdd run into an MCP tool result, flagging non-zero exits. */
export function toMcpResult(r: CsddResult) {
  const parts: string[] = [];
  if (r.stdout.trim()) parts.push(r.stdout.trimEnd());
  if (r.stderr.trim()) parts.push((r.ok ? "" : "[stderr] ") + r.stderr.trimEnd());
  if (parts.length === 0) {
    parts.push(r.ok ? "(ok, no output)" : `csdd exited with code ${r.exitCode}`);
  }
  // Exit 2 = validation failure; surface it distinctly in the text.
  const header =
    r.exitCode === 2
      ? "csdd validation failed (exit 2):\n"
      : r.ok
        ? ""
        : `csdd failed (exit ${r.exitCode}):\n`;
  return {
    content: [{ type: "text" as const, text: header + parts.join("\n\n") }],
    isError: !r.ok,
  };
}

/** Build the handler that runs a ToolDef's argv and formats the result. */
export function makeHandler(def: ToolDef) {
  return async (params: Record<string, any>) => {
    const argv = def.toArgs(params ?? {});
    const result = await runCsdd(argv, params?.root);
    return toMcpResult(result);
  };
}
