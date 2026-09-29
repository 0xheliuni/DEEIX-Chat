// Architecture rules that lint cannot express. Run by `pnpm check`.
//
// Each rule is a boundary decided in docs/ARCHITECTURE.md; a violation means a
// platform concern leaked into shared code, not a style problem.

import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, posix, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";

// fileURLToPath, not URL.pathname: the latter yields "/E:/..." on Windows.
const root = fileURLToPath(new URL("..", import.meta.url));

const rules = [
  {
    // Only the platform layer may talk to the Tauri shell. Everything else goes
    // through its typed bindings, so the webview's native surface stays auditable.
    name: "Tauri APIs only under shared/platform/",
    match: (file) => file.imports.some((specifier) => specifier.startsWith("@tauri-apps/")),
    allow: (file) => file.path.startsWith("shared/platform/"),
  },
  {
    // The refresh token is write-once from JS. Any read path is a regression of
    // the desktop credential model (docs/ARCHITECTURE.md §5).
    name: "no refresh-token read command",
    match: (file) => /invoke\(\s*["']read_refresh_token["']/.test(file.code),
    allow: () => false,
  },
  {
    // Server address is pinned in the shell; the web app must not keep its own copy.
    name: "no localStorage-backed server address",
    match: (file) => /localStorage\.(get|set)Item\(\s*["'][^"']*api-base-url/.test(file.code),
    allow: () => false,
  },
  {
    // What the UI offers is decided by the server's capability flags, never by
    // the platform the client runs on (docs/ARCHITECTURE.md §4). The
    // admin and settings surfaces are pure feature surfaces, so a platform check
    // there can only be doing visibility work.
    name: "feature visibility comes from capabilities, not the platform",
    match: (file) =>
      /\bisDesktopApp\(\)/.test(file.code) &&
      (file.path.startsWith("features/admin/") ||
        file.path.startsWith("features/settings/") ||
        file.resolvedImports.includes("shared/capabilities")),
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
    // Relative paths are resolved too, so `../../<other-feature>/...` cannot slip through.
    match: (file) => {
      const own = /^features\/([^/]+)\//.exec(file.path)?.[1];
      if (own === undefined) return false;
      return file.resolvedImports.some((target) => {
        const match = /^features\/([^/]+)\/(.+)$/.exec(target);
        return match !== null && match[1] !== own && match[2] !== "index";
      });
    },
    allow: () => false,
  },
  {
    // components/ui is a design-system layer; business code depends on it, never
    // the other way round.
    name: "components/ui has no business dependencies",
    match: (file) =>
      file.path.startsWith("components/ui/") &&
      file.resolvedImports.some((target) => /^(features|entities)(\/|$)/.test(target)),
    allow: () => false,
  },
  {
    // entities sit below features: they are shared by several features and must
    // not reach back up into any of them.
    name: "entities do not depend on features",
    match: (file) =>
      file.path.startsWith("entities/") && file.resolvedImports.some((target) => /^features(\/|$)/.test(target)),
    allow: () => false,
  },
  {
    // Route pages and layouts stay server components so the static export keeps
    // client boundaries inside features; interactive logic belongs to a feature
    // entry component. The image-loading preview is a standalone visual playground.
    // Comments are stripped first: a license or doc comment may precede the directive.
    name: "route pages are server components",
    match: (file) => /^app\/(.+\/)?(page|layout)\.tsx$/.test(file.path) && /^\s*["']use client["']/.test(file.code),
    allow: (file) => file.path === "app/(app)/(project)/preview/image-loading/page.tsx",
  },
  {
    // File names are kebab-case with no extra dot segments (`admin-types.ts`, not
    // `admin.types.ts`). Tool-recognized suffixes (`*.test.ts`, `*.spec.tsx`,
    // `*.config.ts`) are the only allowed dot segments.
    name: "kebab-case file name without dot segments",
    match: (file) =>
      !/^[a-z0-9]+(-[a-z0-9]+)*(\.(test|spec))?\.tsx?$|^[a-z0-9]+(-[a-z0-9]+)*\.config\.ts$/.test(
        file.path.split("/").at(-1),
      ),
    allow: () => false,
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

// Removes // and /* */ comments while keeping string and template literals intact, so
// import-like text inside comments does not trigger rules. Outside strings a backslash only
// occurs in regex literals, where the escaped character is skipped so `\/*` is not a comment.
function stripComments(source) {
  let out = "";
  let quote = null;
  for (let i = 0; i < source.length; i++) {
    const char = source[i];
    const next = source[i + 1];
    if (char === "\\") {
      out += char + (next ?? "");
      i++;
      continue;
    }
    if (quote) {
      if (char === quote || (char === "\n" && quote !== "`")) quote = null;
      out += char;
      continue;
    }
    if (char === '"' || char === "'" || char === "`") {
      quote = char;
      out += char;
      continue;
    }
    if (char === "/" && next === "/") {
      while (i < source.length && source[i] !== "\n") i++;
      out += "\n";
      continue;
    }
    if (char === "/" && next === "*") {
      const end = source.indexOf("*/", i + 2);
      const comment = end === -1 ? source.slice(i) : source.slice(i, end + 2);
      // Keep line breaks so `^` anchors and line-based checks still line up.
      out += comment.replace(/[^\n]/g, "");
      i = end === -1 ? source.length : end + 1;
      continue;
    }
    out += char;
  }
  return out;
}

// Static, side-effect, dynamic and re-export specifiers: `from "x"`, `import "x"`,
// `import("x")`, `require("x")`.
function collectImports(code) {
  const specifiers = [];
  for (const match of code.matchAll(/(?:\bfrom\s*|\bimport\s*\(\s*|\bimport\s+|\brequire\s*\(\s*)(["'])([^"'\n]+)\1/g)) {
    specifiers.push(match[2]);
  }
  return specifiers;
}

// Maps `@/x` and relative specifiers to root-relative paths without extension; bare package
// specifiers are dropped.
function resolveImport(filePath, specifier) {
  let target;
  if (specifier.startsWith("@/")) target = specifier.slice(2);
  else if (specifier.startsWith("./") || specifier.startsWith("../")) {
    target = posix.normalize(posix.join(posix.dirname(filePath), specifier));
  } else return null;
  return target.replace(/\/$/, "").replace(/\.(tsx?|mjs|js)$/, "");
}

const files = walk(root).map((full) => {
  const path = relative(root, full).split(sep).join("/");
  const source = readFileSync(full, "utf8");
  const code = stripComments(source);
  const imports = collectImports(code);
  const resolvedImports = imports.map((specifier) => resolveImport(path, specifier)).filter((target) => target !== null);
  return { path, source, code, imports, resolvedImports };
});

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
