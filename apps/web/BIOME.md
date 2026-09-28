# Frontend Biome policy

DEEIX Chat uses Biome as the frontend linter. TypeScript 7 (`tsc --noEmit`) is responsible for type checking, and `scripts/check-architecture.mjs` enforces architecture boundaries that lint rules cannot express. ESLint, `typescript-eslint`, and `eslint-config-next` are not part of the frontend toolchain.

Architecture and layering rules are summarized in [README.md](./README.md) and enforced by `scripts/check-architecture.mjs`; this file only covers Biome.

## Commands

```bash
pnpm check        # pnpm lint && pnpm typecheck && pnpm check:arch
pnpm lint         # biome lint .
pnpm lint:fix     # biome lint --write . (safe fixes only)
pnpm typecheck    # tsc --noEmit
pnpm check:arch   # node scripts/check-architecture.mjs
```

`pnpm check` is the required non-mutating gate, both locally and in CI (`.github/workflows/ci.yml` runs `turbo run check` for `@deeix/web`).

The Biome formatter is disabled (`formatter.enabled: false`). Enabling a repository-wide formatter requires a separate mechanical baseline so that tooling changes stay reviewable and do not rewrite unrelated files.

Files excluded from linting (`files.includes` in `biome.jsonc`): `node_modules`, `.next`, `out`, `build`, `next-env.d.ts`, `public/sw.js`, `public/vendor`, and the generated `shared/generated`.

## Severity

`biome.jsonc` enables the `recommended` preset plus the recommended `next` and `react` domains. Each rule keeps Biome's own default severity (`error` or `warn`) unless `biome.jsonc` overrides it:

- `correctness/useExhaustiveDependencies` is downgraded to `warn`, with `reportUnnecessaryDependencies: false`.
- `nursery/noComponentHookFactories` and `nursery/noReactStringRefs` are opted in at `error`.

`biome lint` exits non-zero only on `error` diagnostics; `warn` diagnostics are printed but do not fail `pnpm lint`. The current baseline is zero diagnostics of either level, and new warnings are treated as blocking in review. Note that `suspicious/noExplicitAny` has a default severity of `warn`.

## Rules turned off globally

Reasons marked *Reason pending* were not recorded when the rule was disabled and should be filled in (or the rule re-enabled) by whoever next touches it.

| Rule | Reason |
| --- | --- |
| `a11y/noLabelWithoutControl` | *Reason pending.* |
| `a11y/noStaticElementInteractions` | *Reason pending.* |
| `a11y/noSvgWithoutTitle` | Inline SVGs are decorative icons (for example `components/animate-ui/icons`); accessible names come from the surrounding control. |
| `a11y/useAriaPropsSupportedByRole` | Radix and polymorphic controls expose roles and ARIA attributes through runtime composition that Biome cannot resolve statically. |
| `a11y/useSemanticElements` | *Reason pending.* |
| `correctness/noUnusedFunctionParameters` | *Reason pending.* |
| `correctness/useImageSize` | Image dimensions are unknown ahead of time for the same sources listed under `noImgElement`. |
| `performance/noBarrelFile` | Features and entities intentionally expose a public `index.ts` entry; cross-feature imports must go through it. |
| `performance/noImgElement` | The app renders administrator-configured provider icons, arbitrary Markdown/tool images, and local previews whose URLs cannot be declared in a fixed Next image domain list; static export also sets `images.unoptimized`. |
| `performance/noNamespaceImport` | The codebase uses `import * as React from "react"` consistently. |
| `performance/useTopLevelRegex` | *Reason pending.* |
| `suspicious/noAlert` | *Reason pending* (no current `alert`/`confirm`/`prompt` usage; candidate for re-enabling). |
| `suspicious/noArrayIndexKey` | *Reason pending.* |
| `suspicious/noDocumentCookie` | The locale preference is written to a cookie via `document.cookie` (`i18n/app-i18n-provider.tsx`). |
| `suspicious/noEmptyBlockStatements` | *Reason pending.* |
| `suspicious/noShadowRestrictedNames` | *Reason pending.* |
| `suspicious/useAwait` | *Reason pending.* |
| `complexity/noExcessiveCognitiveComplexity` | *Reason pending.* Size thresholds in the architecture rules are the current complexity control. |
| `complexity/noUselessFragments` | *Reason pending.* |
| `complexity/noUselessSwitchCase` | *Reason pending.* |
| `complexity/useOptionalChain` | *Reason pending.* |
| `style/noNestedTernary` | *Reason pending.* |
| `style/noNonNullAssertion` | *Reason pending.* |
| `style/useBlockStatements` | *Reason pending.* |
| `style/useConsistentTypeDefinitions` | *Reason pending.* |
| `style/useConst` | *Reason pending.* |
| `style/useImportType` | *Reason pending.* |

## File-scoped overrides

Each override group in `biome.jsonc` applies to the listed files only.

| Files | Rules off | Reason |
| --- | --- | --- |
| `components/animate-ui/icons/icon.tsx` | `correctness/useExhaustiveDependencies`, `correctness/useHookAtTopLevel` | Registry-owned Animate UI file. Its hook-backed helper keeps the upstream name `getVariants` so re-downloaded registry components stay compatible. |
| `components/animate-ui/utils/get-strict-context.tsx` | `nursery/noComponentHookFactories` | Registry-owned Animate UI utility whose purpose is to return a `Provider` component and a context hook from one factory. |
| `shared/components/file-preview/preview-docx.tsx` | `suspicious/noUnknownAttribute` | Uses styled-jsx `<style jsx global>`, whose `jsx`/`global` attributes are not standard DOM attributes. |
| `components/ui/live-waveform.tsx`, `components/ui/virtual-table.tsx` | `a11y/noAriaHiddenOnFocusable` | Decorative `aria-hidden` elements (waveform canvas, virtual-table padding rows) that are not actually focusable. |
| `features/admin/components/sections/billing/billing-redemption.tsx`, `features/chat/components/message/message-meta.tsx` | `a11y/noNoninteractiveTabindex` | `tabIndex={0}` on a `<span>` tooltip trigger so keyboard users can reach the tooltip. |
| `features/chat/components/sections/chat-input.tsx`, `features/layouts/components/navigation/sidebar-conversation-item.tsx`, `features/settings/components/sections/chat/settings-chat.tsx` | `a11y/noAutofocus` | `autoFocus` on inputs that appear in response to an explicit user action (inline edit/rename fields). |
| `shared/components/file-preview/preview-media.tsx` | `a11y/useMediaCaption` | Previews user-uploaded audio/video, for which no caption track exists. |
| `components/ui/input-group.tsx` | `a11y/useKeyWithClickEvents` | shadcn component: clicking the addon forwards focus to the input, which keyboard users reach directly. |

## Exception policy

- A new file-level or directory-level exception goes into `biome.jsonc` `overrides` and must be added to the table above with a reason.
- An inline suppression is allowed only for a single statement, in the form `// biome-ignore lint/<category>/<rule>: <concrete reason>`.
- Blanket disables (whole file via inline comment, or a whole rule category) are not accepted.
- Changing a global rule setting requires updating the tables above in the same change.

## Coverage gaps

Biome covers the correctness, hooks, accessibility, security, and performance rules that could be mapped from the former Next.js ESLint configuration. Biome 2.5 does not yet implement every React Compiler rule or every Next.js-specific rule; notable gaps include React Compiler diagnostics (immutability, refs, purity, set-state-in-render, static components) and Next.js rules for page-specific HTML, `Head`, `Script`, and relative `location` assignments. `tsc` and `pnpm build` remain required checks, and these gaps should be revisited when Biome adds native equivalents.
