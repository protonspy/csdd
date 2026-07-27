/* =============================================================================
   Mock workspace — shapes mirror what internal/web already serves (or would
   serve, for the four read models this draft proposes). Field names follow the
   Go JSON tags so the React port is a rename-free swap.

   Everything here is one fictional workspace: a photo-sharing product mid-plan.
   ========================================================================== */

const DATA = {
  root: "/home/dev/photo-sharing",
  version: 412,

  /* -- GET /api/overview (existing) --------------------------------------- */
  specs: [
    {
      feature: "album-sharing",
      phase: "implementation",
      ready: true,
      issues: 0,
      createdAt: "2026-07-14",
      approvals: { requirements: true, design: true, tasks: true },
      tasks: { total: 18, done: 11, pct: 61 },
    },
    {
      feature: "share-link-expiry",
      phase: "tasks",
      ready: false,
      issues: 2,
      createdAt: "2026-07-19",
      approvals: { requirements: true, design: true, tasks: false },
      tasks: { total: 9, done: 0, pct: 0 },
    },
    {
      feature: "upload-pipeline",
      phase: "implementation",
      ready: true,
      issues: 0,
      createdAt: "2026-06-30",
      approvals: { requirements: true, design: true, tasks: true },
      tasks: { total: 24, done: 24, pct: 100 },
    },
    {
      feature: "album-thumbnails",
      phase: "design",
      ready: false,
      issues: 0,
      createdAt: "2026-07-22",
      approvals: { requirements: true, design: false, tasks: false },
      tasks: { total: 0, done: 0, pct: 0 },
    },
    {
      feature: "viewer-analytics",
      phase: "requirements",
      ready: false,
      issues: 1,
      createdAt: "2026-07-24",
      approvals: { requirements: false, design: false, tasks: false },
      tasks: { total: 0, done: 0, pct: 0 },
    },
    {
      feature: "storage-migration",
      phase: "implementation",
      ready: true,
      issues: 0,
      createdAt: "2026-06-11",
      approvals: { requirements: true, design: true, tasks: true },
      tasks: { total: 15, done: 15, pct: 100 },
    },
  ],

  /* -- GET /api/spec/{feature} (existing) --------------------------------- */
  specDetail: {
    "album-sharing": {
      feature: "album-sharing",
      phase: "implementation",
      ready: true,
      language: "en",
      createdAt: "2026-07-14",
      approvals: { requirements: true, design: true, tasks: true },
      // NEW: the refs a spec's own documents cite. Same token grammar as a
      // plan's Refs cell, so one resolver serves both.
      refs: ["[[album-sharing]]", "adr:signed-urls-over-acl", "stack:go", "stack:postgres"],
      issueList: [],
      report: {
        updatedAt: "2026-07-26 09:12",
        command: "go test ./internal/share/...",
        tests: { total: 84, passed: 84, failed: 0, skipped: 0 },
        coverage: { pct: 78.4, covered: 1412, lines: 1801 },
      },
      phases: [
        {
          name: "Phase 1: Foundation",
          tasks: [
            { id: "1.1", title: "Album share record and its lifecycle states", done: true, boundary: "ShareStore", requirements: ["1.1", "1.2"] },
            { id: "1.2", title: "Signed-URL minting with a bounded TTL", done: true, boundary: "ShareStore", requirements: ["2.1"], refs: ["adr:signed-urls-over-acl"] },
            { id: "1.3", title: "Revocation invalidates every outstanding link", done: true, boundary: "ShareStore", requirements: ["2.3"] },
          ],
        },
        {
          name: "Phase 2: Core",
          tasks: [
            { id: "2.1", title: "Share an album with a named recipient", done: true, parallel: true, boundary: "ShareService", requirements: ["1.3"] },
            { id: "2.2", title: "Public link sharing with view-only access", done: true, parallel: true, boundary: "LinkService", requirements: ["1.4"], depends: ["1.2"] },
            { id: "2.3", title: "Recipient list pagination", done: true, boundary: "ShareService", requirements: ["1.5"] },
            { id: "2.4", title: "Audit trail for every share and revoke", done: false, boundary: "ShareService", requirements: ["3.1"], refs: ["[[audit-trail]]"] },
          ],
        },
        {
          name: "Phase 3: Integration",
          tasks: [
            { id: "3.1", title: "HTTP surface for share, list, revoke", done: false, boundary: "ShareAPI", requirements: ["1.3", "1.4"], depends: ["2.1", "2.2"] },
            { id: "3.2", title: "Rate limit the public link endpoint", done: false, boundary: "ShareAPI", requirements: ["4.2"], refs: ["stack:redis"] },
          ],
        },
      ],
    },
  },

  /* -- GET /api/plans + /api/plan/{slug} (existing; `refs` is NEW) --------- */
  plans: [
    { slug: "photo-sharing", name: "Photo sharing", approved: true, drift: false, feats: 7, done: 3 },
    { slug: "creator-payouts", name: "Creator payouts", approved: false, drift: false, feats: 5, done: 0 },
  ],

  planDetail: {
    "photo-sharing": {
      slug: "photo-sharing",
      name: "Photo sharing",
      approved: true,
      drift: false,
      milestones: [
        { name: "M1 — storage", total: 2, done: 2 },
        { name: "M2 — sharing", total: 3, done: 1 },
        { name: "M3 — insight", total: 2, done: 0 },
      ],
      feats: [
        {
          num: "1",
          slug: "storage-migration",
          objective: "Move originals to object storage behind a content-addressed key",
          milestone: "M1 — storage",
          depends: [],
          parallel: false,
          state: "done",
          tasks_total: 15,
          tasks_checked: 15,
          refs: ["[[storage-design]]", "stack:s3", "adr:content-addressed-keys"],
        },
        {
          num: "2",
          slug: "upload-pipeline",
          objective: "Resumable upload with server-side checksum verification",
          milestone: "M1 — storage",
          depends: ["1"],
          parallel: false,
          state: "done",
          tasks_total: 24,
          tasks_checked: 24,
          refs: ["[[upload-pipeline]]", "stack:go", "stack:s3"],
        },
        {
          num: "3",
          slug: "album-sharing",
          objective: "Share an album with a person or a public link, revocable at any time",
          milestone: "M2 — sharing",
          depends: ["2"],
          parallel: false,
          state: "running",
          tasks_total: 18,
          tasks_checked: 11,
          refs: ["[[album-sharing]]", "adr:signed-urls-over-acl", "stack:go", "stack:postgres"],
        },
        {
          num: "4",
          slug: "share-link-expiry",
          objective: "Every public link carries an expiry the owner chooses",
          milestone: "M2 — sharing",
          depends: ["3"],
          parallel: true,
          state: "ready",
          tasks_total: 9,
          tasks_checked: 0,
          // Deliberately broken + superseded citations: this is what the lint
          // and the UI have to make obvious.
          refs: ["[[link-expiry]]", "adr:url-signing-v1", "stack:redis"],
        },
        {
          num: "5",
          slug: "album-thumbnails",
          objective: "Derive and cache thumbnails on first view",
          milestone: "M2 — sharing",
          depends: ["2"],
          parallel: true,
          state: "todo",
          tasks_total: 0,
          tasks_checked: 0,
          refs: ["stack:imagemagick"],
        },
        {
          num: "6",
          slug: "viewer-analytics",
          objective: "Count views per album without identifying the viewer",
          milestone: "M3 — insight",
          depends: ["3"],
          parallel: false,
          state: "blocked",
          tasks_total: 0,
          tasks_checked: 0,
          refs: ["[[privacy-stance]]", "adr:no-viewer-identity"],
        },
        {
          num: "7",
          slug: "owner-dashboard",
          objective: "One page an owner opens to see reach and revoke access",
          milestone: "M3 — insight",
          depends: ["6"],
          parallel: false,
          state: "todo",
          tasks_total: 0,
          tasks_checked: 0,
          refs: ["[[album-sharing]]"],
        },
      ],
      log: [
        { when: "09:41", what: "feat 3 · album-sharing — implementer finished task 2.3, 11/18 checked" },
        { when: "09:12", what: "feat 3 · album-sharing — test gate green (84 passed, 78.4% covered)" },
        { when: "08:55", what: "feat 3 · album-sharing — spec approved through tasks, ready_for_implementation" },
        { when: "08:02", what: "feat 2 · upload-pipeline — verdict done accepted (24/24, evidence green)" },
      ],
    },
  },

  /* -- GET /api/adr (NEW) -------------------------------------------------- */
  adrs: [
    {
      number: 1,
      slug: "content-addressed-keys",
      title: "Object keys are content hashes, not album paths",
      body: "A key derived from the bytes makes an upload idempotent and lets two albums share one blob. Renaming an album never rewrites storage.",
      status: "accepted",
      file: "docs/adr/0001-content-addressed-keys.md",
      cited_by: ["feat:storage-migration"],
    },
    {
      number: 2,
      slug: "url-signing-v1",
      title: "Public links are signed URLs with a 24h TTL",
      body: "Chosen over a share table lookup for the first cut: no read path to the database on every view.",
      status: "superseded",
      superseded_by: 4,
      file: "docs/adr/0002-url-signing-v1.md",
      cited_by: ["feat:share-link-expiry"],
    },
    {
      number: 3,
      slug: "no-viewer-identity",
      title: "View counts never carry viewer identity",
      body: "Counting is aggregate-only. No cookie, no fingerprint, no IP retention beyond the request. This closes the door on a feature request we expect to get and intend to refuse.",
      status: "accepted",
      file: "docs/adr/0003-no-viewer-identity.md",
      cited_by: ["feat:viewer-analytics"],
    },
    {
      number: 4,
      slug: "signed-urls-over-acl",
      title: "Signed URLs with a per-share key, replacing the global TTL",
      body: "The v1 global TTL could not express revocation. Each share now mints its own key, so revoking a share invalidates exactly its links and nothing else.",
      status: "accepted",
      file: "docs/adr/0004-signed-urls-over-acl.md",
      cited_by: ["feat:album-sharing", "spec:album-sharing"],
    },
  ],

  /* -- GET /api/stack (NEW) ------------------------------------------------ */
  stack: {
    rows: [
      { name: "go", domain: "Language", choice: "Go", version: "1.24", why: "One static binary; the CLI and the server ship together.", refs: ["[[go-conventions]]"], lint: "ok", external: false },
      { name: "postgres", domain: "Database", choice: "PostgreSQL", version: "16", why: "Shares and audit rows are relational and want real constraints.", refs: ["[[storage-design]]"], lint: "ok", external: false },
      { name: "s3", domain: "Object storage", choice: "Amazon S3", version: "—", why: "Content-addressed originals, lifecycle rules for cold data.", refs: ["[[storage-design]]"], lint: "ok", external: true },
      { name: "redis", domain: "Cache / rate limit", choice: "Redis", version: "7.2", why: "Token bucket for the public link endpoint.", refs: [], lint: "unrefined", external: false },
      { name: "imagemagick", domain: "Image processing", choice: "ImageMagick", version: "7.1", why: "Thumbnail derivation.", refs: [], lint: "phantom", external: false },
    ],
    undeclared: [{ name: "golang-jwt/jwt", where: "go.mod", hint: "used by internal/share/token.go — decide it or drop it" }],
    open: ["Queue for thumbnail jobs — SQS vs an in-process worker pool. Blocked on feat 5."],
  },

  /* -- GET /api/glossary (NEW) --------------------------------------------- */
  glossary: [
    {
      canonical: "Album",
      cluster: "Content",
      definition: "An owner-created, ordered set of photos. The unit that is shared; a photo is never shared on its own.",
      avoid: ["gallery", "collection"],
      used_in: ["spec:album-sharing", "[[album-sharing]]", "feat:album-thumbnails"],
    },
    {
      canonical: "Share",
      cluster: "Access",
      definition: "A revocable grant of view access to one album, held by a recipient or by anyone holding a public link.",
      avoid: ["invite", "grant"],
      used_in: ["spec:album-sharing", "feat:album-sharing"],
    },
    {
      canonical: "Public link",
      cluster: "Access",
      definition: "A share whose recipient is 'anyone with the URL'. Carries its own signing key so it can be revoked alone.",
      avoid: ["magic link", "anonymous link"],
      used_in: ["feat:share-link-expiry", "adr:signed-urls-over-acl"],
    },
    {
      canonical: "Original",
      cluster: "Content",
      definition: "The uploaded bytes as received, stored under a content hash and never mutated. Derivatives reference it.",
      avoid: ["master", "source file"],
      used_in: ["[[storage-design]]", "feat:storage-migration"],
    },
  ],

  /* -- GET /api/wiki (existing) -------------------------------------------- */
  wiki: {
    present: true,
    has_index: true,
    categories: ["Architecture", "Domain", "Operations"],
    pages: [
      {
        slug: "storage-design",
        title: "Storage design",
        category: "Architecture",
        in_index: true,
        path: "docs/wiki/pages/storage-design.md",
        tags: ["storage", "s3"],
        sources: ["aws-s3-guide.md", "protonspy-csdd.md"],
        links: ["upload-pipeline", "album-sharing"],
        body:
          "# Storage design\n\nOriginals live under a content hash, so an upload of identical bytes is a no-op and an album rename never touches storage. See [[upload-pipeline]] for how bytes arrive and [[album-sharing]] for who may read them.\n\n## Layout\n\n| Prefix | Holds | Lifecycle |\n|---|---|---|\n| `originals/<sha256>` | the uploaded bytes, immutable | glacier after 180d |\n| `derived/<sha256>/<preset>` | thumbnails and web sizes | regenerated on demand |\n\nThe decision behind the key shape is recorded as adr:content-addressed-keys — it is the one thing here that is hard to reverse.\n\n## Why not album paths\n\nA path-shaped key ties storage to a mutable name. Every rename becomes a copy, and two albums holding the same photo pay twice.",
      },
      {
        slug: "upload-pipeline",
        title: "Upload pipeline",
        category: "Architecture",
        in_index: true,
        path: "docs/wiki/pages/upload-pipeline.md",
        tags: ["ingest"],
        sources: ["protonspy-csdd.md"],
        links: ["storage-design"],
        body:
          "# Upload pipeline\n\nResumable, checksum-verified ingest. The client chunks; the server verifies the whole-object hash before the blob becomes addressable — a half-written original is never reachable.\n\n```mermaid\nflowchart LR\n  C[Client] -->|init| A[API]\n  A -->|presign| S[(S3)]\n  C -->|PUT chunks| S\n  C -->|complete| A\n  A -->|verify sha256| S\n  A -->|record| D[(Postgres)]\n```\n\nStorage layout is [[storage-design]]. The technology is stack:s3 and stack:go.",
      },
      {
        slug: "album-sharing",
        title: "Album sharing",
        category: "Domain",
        in_index: true,
        path: "docs/wiki/pages/album-sharing.md",
        tags: ["access", "domain"],
        sources: [],
        links: ["storage-design", "privacy-stance", "audit-trail"],
        body:
          "# Album sharing\n\nA **Share** is a revocable grant of view access to one Album. Two shapes exist: a named recipient, and a public link (see the glossary term Public link).\n\nRevocation must be total and immediate — that requirement is what killed the global-TTL design and produced adr:signed-urls-over-acl.\n\n- A share never widens: view-only, one album, no descendants.\n- Every share and every revoke lands in [[audit-trail]].\n- Viewer counting stays aggregate — see [[privacy-stance]].",
      },
      {
        slug: "privacy-stance",
        title: "Privacy stance",
        category: "Domain",
        in_index: true,
        path: "docs/wiki/pages/privacy-stance.md",
        tags: ["privacy"],
        sources: [],
        links: [],
        body:
          "# Privacy stance\n\nWe count views. We do not identify viewers. adr:no-viewer-identity records the decision and the request it is meant to refuse.",
      },
      {
        slug: "audit-trail",
        title: "Audit trail",
        category: "Operations",
        in_index: true,
        path: "docs/wiki/pages/audit-trail.md",
        tags: ["ops"],
        sources: [],
        links: ["album-sharing"],
        body:
          "# Audit trail\n\nAppend-only record of every share and revoke: who, which album, which recipient shape, when. Retained 400 days.\n\nRead by the owner dashboard; never by the viewer surface ([[album-sharing]]).",
      },
      {
        slug: "go-conventions",
        title: "Go conventions",
        category: "Operations",
        in_index: false,
        path: "docs/wiki/pages/go-conventions.md",
        tags: [],
        sources: ["protonspy-csdd.md"],
        links: [],
        body: "# Go conventions\n\nPackage per boundary, no interface until the second implementation, table tests everywhere.",
      },
    ],
    raw_sources: ["aws-s3-guide.md", "protonspy-csdd.md"],
  },

  /* -- GET /api/codewiki (NEW) --------------------------------------------- */
  codewiki: {
    docs: [
      {
        file: "docs/raw/protonspy-csdd.md",
        repo: "docs/raw/csdd/",
        owner: "protonspy",
        name: "csdd",
        generated: "2026-07-25",
        sections: 12,
        citations: 148,
        resolved: 145,
        ingested_by: ["storage-design", "upload-pipeline", "go-conventions"],
        lint: { status: "fail", findings: 3 },
        structure: [
          { path: "internal/cli/", kind: "dir" },
          { path: "internal/codewiki/codewiki.go", kind: "file", lines: 688 },
          { path: "internal/graph/extract.go", kind: "file", lines: 402 },
          { path: "internal/plan/runner.go", kind: "file", lines: 913 },
        ],
        toc: [
          { slug: "overview", title: "Overview", depth: 1, cites: 9 },
          { slug: "cli-surface", title: "CLI surface", depth: 1, cites: 21 },
          { slug: "the-plan-runner", title: "The plan runner", depth: 2, cites: 34 },
          { slug: "the-graph-extractors", title: "The graph extractors", depth: 2, cites: 27 },
          { slug: "codewiki-lint", title: "codewiki lint", depth: 2, cites: 19 },
          { slug: "glossary", title: "Glossary", depth: 1, cites: 38 },
        ],
        findings: [
          { line: 214, cite: "internal/plan/Runner.go:1-40", msg: "path not in Structure — the file is runner.go (case)", severity: "critical" },
          { line: 402, cite: "internal/graph/extract.go:380-460", msg: "range past end of file (402 lines)", severity: "critical" },
          { line: 688, cite: "internal/codewiki/verdict.go:1-12", msg: "path not in Structure — no such file in the checkout", severity: "critical" },
        ],
      },
      {
        file: "docs/raw/aws-s3-guide.md",
        repo: "—",
        owner: "aws",
        name: "s3-guide",
        generated: "2026-07-18",
        sections: 6,
        citations: 0,
        resolved: 0,
        ingested_by: ["storage-design"],
        lint: { status: "n/a", findings: 0 },
        structure: [],
        toc: [],
        findings: [],
      },
    ],
  },

  /* -- GET /api/tests (existing) ------------------------------------------- */
  tests: {
    tests: { source: "go test ./...", total: 1284, passed: 1279, failed: 3, skipped: 2, durationSec: 96.4 },
    coverage: { format: "gocover", pct: 74.2, covered: 18422, lines: 24829 },
    suites: [
      { name: "internal/plan", total: 312, passed: 311, failed: 1, skipped: 0, time: 28.1 },
      { name: "internal/graph", total: 264, passed: 264, failed: 0, skipped: 0, time: 19.4 },
      { name: "internal/codewiki", total: 141, passed: 139, failed: 2, skipped: 0, time: 6.2 },
      { name: "internal/web", total: 98, passed: 98, failed: 0, skipped: 0, time: 11.7 },
      { name: "internal/cli", total: 469, passed: 467, failed: 0, skipped: 2, time: 31.0 },
    ],
    failures: [
      { suite: "internal/codewiki", name: "TestLintCitationRange/one_past_last_line", message: "want 0 findings, got 1\n  docs/raw/x.md:41 range 1-44 past end of file (43 lines)" },
      { suite: "internal/codewiki", name: "TestLintSlugDerivedFromHeading", message: "slug \"the-runner\" not derived from heading \"The plan runner\"" },
      { suite: "internal/plan", name: "TestSeqDAG/diamond_with_parallel_leaves", message: "feat 6 scheduled before its dependency 3" },
    ],
  },

  /* -- resources (existing overview fields) -------------------------------- */
  resources: {
    steering: [
      { name: "product.md", inclusion: "always", description: "What we are building and for whom" },
      { name: "tech.md", inclusion: "always", description: "Language, frameworks, and the shape of a service" },
      { name: "structure.md", inclusion: "always", description: "Directory layout and boundary rules" },
      { name: "security.md", inclusion: "fileMatch", description: "Auth, secrets, and input handling" },
    ],
    skills: [
      { name: "tdd-cycle", description: "Red → green → refactor, one task at a time" },
      { name: "wiki", description: "Ingest raw sources into cross-linked pages" },
      { name: "codewiki", description: "Read a source checkout into one cited document" },
      { name: "stack", description: "Refine a technology against current docs before first use" },
      { name: "prd", description: "Interview, research, decompose into feats" },
    ],
    agents: [
      { name: "implementer", description: "Drives one task through its development cycle", tools: "Read, Edit, Bash" },
      { name: "code-reviewer", description: "Reviews a slice against its spec", tools: "Read, Grep" },
      { name: "security-reviewer", description: "Auth, secrets, input surfaces", tools: "Read, Grep" },
      { name: "quality-gate", description: "Runs the command gate at feat exit", tools: "Bash" },
    ],
    mcp: [{ name: "csdd", description: "stdio · npx @protonspy/csdd mcp" }],
    hooks: [
      { name: "format-after-edit.sh", description: "PostToolUse · gofmt the touched file" },
      { name: "block-destructive.sh", description: "PreToolUse · refuse rm -rf and force pushes" },
    ],
    commands: [
      { name: "csdd-commit", description: "Conventional Commit from the diff + active spec" },
      { name: "csdd-codewiki", description: "Compile a checkout into one cited document" },
      { name: "prd", description: "Plan a multi-feature initiative" },
    ],
  },

  /* -- files (existing /api/tree) ------------------------------------------ */
  tree: [
    {
      name: "docs",
      dir: true,
      children: [
        { name: "adr", dir: true, children: [{ name: "0004-signed-urls-over-acl.md", dir: false, path: "docs/adr/0004-signed-urls-over-acl.md" }] },
        { name: "plans", dir: true, children: [{ name: "photo-sharing", dir: true, children: [{ name: "plan.md", dir: false, path: "docs/plans/photo-sharing/plan.md" }] }] },
        { name: "wiki", dir: true, children: [{ name: "index.md", dir: false, path: "docs/wiki/index.md" }] },
        { name: "glossary.md", dir: false, path: "docs/glossary.md" },
        { name: "stack.md", dir: false, path: "docs/stack.md" },
      ],
    },
    {
      name: "specs",
      dir: true,
      children: [
        {
          name: "album-sharing",
          dir: true,
          children: [
            { name: "requirements.md", dir: false, path: "specs/album-sharing/requirements.md" },
            { name: "design.md", dir: false, path: "specs/album-sharing/design.md" },
            { name: "tasks.md", dir: false, path: "specs/album-sharing/tasks.md" },
          ],
        },
      ],
    },
  ],

  fileSample: {
    "docs/plans/photo-sharing/plan.md":
      "---\nname: Photo sharing\nstatus: approved\n---\n\n## Feats\n\n| # | Feat | Objective | Depends | Milestone | (P) | Refs |\n|---|---|---|---|---|---|---|\n| 1 | storage-migration | Move originals to object storage | — | M1 | | [[storage-design]] stack:s3 adr:content-addressed-keys |\n| 3 | album-sharing | Share an album, revocable | 2 | M2 | | [[album-sharing]] adr:signed-urls-over-acl stack:go |\n",
  },

  /* -- graph (existing /api/graph, summarised) ----------------------------- */
  graph: {
    nodes: 1841,
    edges: 4260,
    kinds: [
      { kind: "spec", n: 6 },
      { kind: "task", n: 66 },
      { kind: "requirement", n: 41 },
      { kind: "wiki_page", n: 6 },
      { kind: "adr", n: 4 },
      { kind: "tech", n: 5 },
      { kind: "term", n: 4 },
      { kind: "file", n: 1709 },
    ],
  },

  /* -- gates (csdd graph analyze --strict, wiki lint, codewiki lint) -------- */
  gates: [
    { name: "csdd spec validate", state: "ok", detail: "6 specs · 0 issues" },
    { name: "csdd graph analyze --strict", state: "warn", detail: "2 tech findings: redis unrefined, imagemagick phantom" },
    { name: "csdd wiki lint", state: "warn", detail: "1 page not in index.md (go-conventions)" },
    { name: "csdd codewiki lint", state: "bad", detail: "3 unresolved citations in protonspy-csdd.md" },
    { name: "csdd plan validate", state: "bad", detail: "feat 4 cites adr:url-signing-v1 (superseded by 0004)" },
  ],

  activity: [
    { when: "09:41", what: "task 2.3 checked in album-sharing — 11/18" },
    { when: "09:12", what: "test report written: 84 passed, 78.4% covered" },
    { when: "08:55", what: "spec album-sharing approved through tasks" },
    { when: "08:31", what: "docs/adr/0004-signed-urls-over-acl.md added, supersedes 0002" },
    { when: "08:02", what: "feat upload-pipeline accepted done by the plan runner" },
    { when: "07:48", what: "docs/raw/protonspy-csdd.md compiled by the codewiki skill" },
  ],
}
