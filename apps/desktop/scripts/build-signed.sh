#!/usr/bin/env bash
# Local signed build: loads deploy/secrets/desktop-signing.env, builds, and on
# macOS verifies the result with Gatekeeper. CI does the same via secrets.
set -euo pipefail

root="$(cd "$(dirname "$0")/../../.." && pwd)"
env_file="$root/deploy/secrets/desktop-signing.env"
if [[ ! -f "$env_file" ]]; then
  echo "missing $env_file — copy desktop-signing.env.example and fill it in" >&2
  exit 1
fi
set -a; source "$env_file"; set +a

# Tauri wants the key content, not a path.
if [[ -n "${TAURI_SIGNING_PRIVATE_KEY_PATH:-}" && -z "${TAURI_SIGNING_PRIVATE_KEY:-}" ]]; then
  TAURI_SIGNING_PRIVATE_KEY="$(cat "$root/$TAURI_SIGNING_PRIVATE_KEY_PATH")"
  export TAURI_SIGNING_PRIVATE_KEY
fi

cd "$root"
pnpm --filter @deeix/desktop build

if [[ "$(uname)" == "Darwin" ]]; then
  app="$root/apps/desktop/src-tauri/target/release/bundle/macos/DEEIX Chat.app"
  echo "--- codesign"
  codesign --verify --deep --strict --verbose=2 "$app"
  echo "--- Gatekeeper (expect: source=Notarized Developer ID)"
  spctl --assess --type execute --verbose=2 "$app"
fi
