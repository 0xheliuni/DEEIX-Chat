// Public entry of the desktop feature. The (app) layout mounts the bootstrap and
// update notifier; the (shell) route group mounts the shell providers and tab strip.
// Unused re-exports are dropped at build time because package.json declares
// `sideEffects`, so each route bundles only what it imports.
export { DesktopBootstrap } from "@/features/desktop/components/desktop-bootstrap";
export { DesktopUpdateNotifier } from "@/features/desktop/components/desktop-update-notifier";
export { ShellProviders } from "@/features/desktop/components/shell-providers";
export { TabStrip } from "@/features/desktop/components/tab-strip";
