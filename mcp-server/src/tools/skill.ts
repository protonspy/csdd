import { z } from "zod";
import { bool, flag, forceField, rootArg, rootField, type ToolDef } from "../tooldef.js";

const skillName = z.string().describe("Skill name (.claude/skills/<name>/).");

function addArtifact(action: string, kind: string): ToolDef {
  return {
    name: `csdd_skill_add_${action.replace("add-", "")}`,
    title: `Skill add ${kind}`,
    description: `Add a ${kind} file under .claude/skills/<skill>/${kind}s/ with a placeholder. Path traversal is rejected.`,
    inputSchema: {
      skill: skillName,
      file: z.string().describe(`File path relative to the ${kind}s/ subdir.`),
      root: rootField,
    },
    toArgs: (p) => ["skill", action, p.skill, p.file, ...rootArg(p)],
  };
}

export const skillTools: ToolDef[] = [
  {
    name: "csdd_skill_create",
    title: "Skill create",
    description:
      "Create .claude/skills/<name>/ with SKILL.md (+ references/, assets/, scripts/). Description is the one-line activation trigger.",
    inputSchema: {
      name: skillName,
      description: z.string().describe("One-sentence activation trigger for the skill."),
      title: z.string().optional().describe("Document title (defaults to Title Case of name)."),
      model: z.string().optional().describe("Model override (e.g. sonnet, opus, haiku). Omit to inherit the session config."),
      effort: z.string().optional().describe("Effort level: low|medium|high|xhigh|max. Omit to inherit the session config."),
      root: rootField,
    },
    toArgs: (p) => [
      "skill",
      "create",
      p.name,
      ...flag("--description", p.description),
      ...flag("--model", p.model),
      ...flag("--effort", p.effort),
      ...flag("--title", p.title),
      ...rootArg(p),
    ],
  },
  {
    name: "csdd_skill_list",
    title: "Skill list",
    description: "List skills with their descriptions.",
    inputSchema: { root: rootField },
    toArgs: (p) => ["skill", "list", ...rootArg(p)],
  },
  {
    name: "csdd_skill_show",
    title: "Skill show",
    description: "List a skill's files and print SKILL.md.",
    inputSchema: { name: skillName, root: rootField },
    toArgs: (p) => ["skill", "show", p.name, ...rootArg(p)],
  },
  addArtifact("add-reference", "reference"),
  addArtifact("add-script", "script"),
  addArtifact("add-asset", "asset"),
  {
    name: "csdd_skill_validate",
    title: "Skill validate",
    description: "Validate skill structure + frontmatter; report line/token counts. Exit 2 on issues.",
    inputSchema: { name: skillName, root: rootField },
    toArgs: (p) => ["skill", "validate", p.name, ...rootArg(p)],
  },
  {
    name: "csdd_skill_delete",
    title: "Skill delete",
    description: "Delete .claude/skills/<name>/ recursively. Requires force.",
    inputSchema: { name: skillName, force: forceField, root: rootField },
    toArgs: (p) => ["skill", "delete", p.name, ...bool("--force", p.force), ...rootArg(p)],
  },
];
