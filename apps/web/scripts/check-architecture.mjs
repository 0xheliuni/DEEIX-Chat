// Architecture rules that lint cannot express. Run by `pnpm check`.
//
// Each rule is a boundary decided in docs/ARCHITECTURE.md; a violation means a
// platform concern leaked into shared code, not a style problem.

import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";

// fileURLToPath, not URL.pathname: the latter yields "/E:/..." on Windows.
const root = fileURLToPath(new URL("..", import.meta.url));

const rules = [
  {
    // Only the platform layer may talk to the Tauri shell. Everything else goes
    // through its typed bindings, so the webview's native surface stays auditable.
    name: "Tauri APIs only under shared/platform/",
    match: (file) => /^import\s.*["']@tauri-apps\//m.test(file.source),
    allow: (file) => file.path.startsWith("shared/platform/"),
  },
  {
    // The refresh token is write-once from JS. Any read path is a regression of
    // the desktop credential model (docs/ARCHITECTURE.md §5).
    name: "no refresh-token read command",
    match: (file) => /invoke\(\s*["']read_refresh_token["']/.test(file.source),
    allow: () => false,
  },
  {
    // Server address is pinned in the shell; the web app must not keep its own copy.
    name: "no localStorage-backed server address",
    match: (file) => /localStorage\.(get|set)Item\(\s*["'][^"']*api-base-url/.test(file.source),
    allow: () => false,
  },
  {
    // What the UI offers is decided by the server's capability flags, never by
    // the platform the client runs on (docs/ARCHITECTURE.md §4). The
    // admin and settings surfaces are pure feature surfaces, so a platform check
    // there can only be doing visibility work.
    name: "feature visibility comes from capabilities, not the platform",
    match: (file) =>
      /\bisDesktopApp\(\)/.test(file.source) &&
      (file.path.startsWith("features/admin/") ||
        file.path.startsWith("features/settings/") ||
        /from ["']@\/shared\/capabilities["']/.test(file.source)),
    allow: () => false,
  },
  {
    // Every admin section page must be registered in ADMIN_SECTIONS: that table
    // drives both the sidebar and the route guard, so an unregistered section
    // would bypass capability gating.
    name: "admin section registered in ADMIN_SECTIONS",
    match: (file) => /^app\/\(app\)\/\(project\)\/admin\/[^/]+\/page\.tsx$/.test(file.path),
    allow: (file) => {
      const section = file.path.split("/").at(-2);
      const table = files.find((f) => f.path === "features/admin/model/admin-sections.ts");
      return table !== undefined && new RegExp(`href:\\s*["']/${section}["']`).test(table.source);
    },
  },
  {
    // A feature reaches another feature only through its public entry point
    // (`@/features/<name>`, i.e. its index.ts), so internals can move freely.
    // Route files under app/ are exempt: they mount feature entry components.
    name: "no deep cross-feature imports",
    match: (file) => {
      const own = /^features\/([^/]+)\//.exec(file.path)?.[1];
      if (own === undefined) return false;
      for (const [, target] of file.source.matchAll(/(?:from\s+|import\s*\(\s*)["']@\/features\/([^/"']+)\/[^"']*["']/g)) {
        if (target !== own) return true;
      }
      return false;
    },
    allow: () => false,
  },
  {
    // components/ui is a design-system layer; business code depends on it, never
    // the other way round.
    name: "components/ui has no business dependencies",
    match: (file) =>
      file.path.startsWith("components/ui/") &&
      /(?:from\s+|import\s*\(\s*)["']@\/(features|entities)(\/|["'])/.test(file.source),
    allow: () => false,
  },
  {
    // entities sit below features: they are shared by several features and must
    // not reach back up into any of them.
    name: "entities do not depend on features",
    match: (file) =>
      file.path.startsWith("entities/") && /(?:from\s+|import\s*\(\s*)["']@\/features(\/|["'])/.test(file.source),
    allow: () => false,
  },
  {
    // Route pages and layouts stay server components so the static export keeps
    // client boundaries inside features; interactive logic belongs to a feature
    // entry component. The image-loading preview is a standalone visual playground.
    name: "route pages are server components",
    match: (file) =>
      /^app\/(.+\/)?(page|layout)\.tsx$/.test(file.path) && /^\s*["']use client["']/.test(file.source),
    allow: (file) => file.path === "app/(app)/(project)/preview/image-loading/page.tsx",
  },
  {
    // File names are kebab-case with no extra dot segments (`admin-types.ts`, not
    // `admin.types.ts`). Framework-mandated names are exempt.
    name: "kebab-case file name without dot segments",
    match: (file) => !/^[a-z0-9]+(-[a-z0-9]+)*\.tsx?$/.test(file.path.split("/").at(-1)),
    allow: (file) => file.path === "next.config.ts",
  },
];

function walk(dir, out = []) {
  for (const entry of readdirSync(dir)) {
    if (entry === "node_modules" || entry === ".next" || entry === "out" || entry.startsWith(".")) continue;
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) walk(full, out);
    else if (/\.(ts|tsx)$/.test(entry) && !entry.endsWith(".d.ts")) out.push(full);
  }
  return out;
}

const files = walk(root).map((full) => ({
  path: relative(root, full).split(sep).join("/"),
  source: readFileSync(full, "utf8"),
}));

const violations = [];
for (const rule of rules) {
  for (const file of files) {
    if (rule.match(file) && !rule.allow(file)) violations.push(`${rule.name}: ${file.path}`);
  }
}

if (violations.length > 0) {
  console.error("Architecture violations:");
  for (const v of violations) console.error(`  - ${v}`);
  process.exit(1);
}
console.log(`Architecture rules OK (${rules.length} rules, ${files.length} files)`);
for (const rule of rules) console.log(`  - ${rule.name}`);
