# apps/desktop

Tauri 2 shell around the `@deeix/web` build. It contains **no business logic**:
windows, tray, deep links, auto-update and OS keychain access only. If a feature
needs code here, the right fix is a missing abstraction in `packages/core` or
`apps/web` — see [docs/ARCHITECTURE.md](../../docs/ARCHITECTURE.md).

## How the pieces fit

```
apps/web (Next.js static export — the same bundle the Go server serves)
        │  frontendDist: ../../web/out
        ▼
apps/desktop/src-tauri (Rust)
        ├── keychain commands   src/commands.rs   read/write/clear_refresh_token
        ├── tray               src/tray.rs       show / quit, left click restores the window
        ├── OAuth loopback     src/oauth_loopback.rs  RFC 8252 receiver on 127.0.0.1:<ephemeral>
        ├── updater            tauri-plugin-updater + apps/web/shared/platform/desktop-updater.ts
        └── CSP                no inline/remote scripts; network open to http(s) (user picks the server)
```

The desktop app loads the exact web bundle; there is no desktop-specific build.
At runtime the page detects the shell through `window.isTauri` and shows the
first-run server setup screen. Token delivery is decided server-side: requests
carrying `X-Client-Platform: desktop` from a non-web Origin get the refresh
token in the response body instead of a `SameSite` cookie, which a cross-origin
webview would never receive.

## Prerequisites

- Rust toolchain (`rustup`) with a recent stable
- Platform dependencies for Tauri 2 (WebKitGTK on Linux, WebView2 on Windows,
  Xcode command line tools on macOS)
- `pnpm install` from the repository root

## Development

```bash
# from the repository root
pnpm --filter @deeix/desktop dev
```

`beforeDevCommand` starts `next dev` on port 3000 and the shell loads it. The Go
server must already be reachable at the address you enter on the setup screen
(default `http://127.0.0.1:8080`).

## Build

```bash
pnpm --filter @deeix/desktop build
```

Artifacts land in `apps/desktop/src-tauri/target/release/bundle/`.

Building is not enough to ship: macOS packages must be **notarized** and Windows
packages **code-signed**, otherwise users cannot install them. Signing keys and
the updater key live only in CI secrets — never in this repository.

## Third-party sign-in (OAuth / OIDC)

The webview cannot receive a provider redirect, so the desktop app follows the
RFC 8252 native-app flow using the server's provider-auth bridge:

1. The shell binds an ephemeral port on `127.0.0.1` and hands the web app
   `http://127.0.0.1:<port>/oauth/callback` as the redirect URI.
2. The web app starts the bridge with client id `com.deeix.chat.desktop`; the
   server validates that the redirect is a loopback address with a port and
   nothing else, then returns the provider authorization URL.
3. The authorization URL opens in the **system browser** (the user's existing
   provider session is reused; the webview never sees provider credentials).
4. The provider redirects to the server callback, which issues a one-time DEEIX
   grant and redirects the browser to the loopback URI.
5. The shell answers that request with a "you can close this tab" page and
   emits the callback URL to the webview, which navigates to `/auth/callback`
   and exchanges the grant with its PKCE verifier — the same code path the
   browser build uses.

Requirements on the server side: `PUBLIC_API_BASE_URL` set (this enables the
bridge) and the instance callback registered with each provider — see the
"OAuth callbacks for Web, App, and Desktop" section of the root README. No
custom URL scheme is registered and nothing is added to the provider allowlist
for the desktop app.

## Auto-update

The updater is configured in `tauri.conf.json` (`plugins.updater`) and surfaced in
Settings → About → "Check for updates" (`features/platform/components/desktop-update-action.tsx`).

Release flow: pushing a `v*.*.*` tag builds all targets and creates a **draft**
GitHub Release with the installers and a signed `latest.json`. The app polls
`releases/latest/download/latest.json`, which GitHub resolves to the newest
published, non-prerelease release. Publishing the draft is therefore the step
that ships the update; until then existing installs see nothing.

Every artifact is signed with the updater private key and verified against the
public key in `tauri.conf.json` before installation, so a compromised release
endpoint cannot push arbitrary code. The private key is a CI secret; the public
key is committed.

Adding a **beta channel** means a second endpoint, a second keypair and a second
build matrix leg — not a second code path.

## Signing (required before shipping)

| Platform | What is needed | CI secrets |
| --- | --- | --- |
| macOS | Developer ID Application certificate (.p12) + Apple ID app-specific password for notarization | `APPLE_CERTIFICATE`, `APPLE_CERTIFICATE_PASSWORD`, `APPLE_SIGNING_IDENTITY`, `APPLE_ID`, `APPLE_PASSWORD`, `APPLE_TEAM_ID` |
| Windows | Code-signing certificate (.pfx) | `WINDOWS_CERTIFICATE`, `WINDOWS_CERTIFICATE_PASSWORD` |
| All | Updater keypair (`tauri signer generate`) | `TAURI_SIGNING_PRIVATE_KEY`, `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` |

`tauri.conf.json` already enables the hardened runtime on macOS and SHA-256 +
RFC 3161 timestamping on Windows; CI imports the Windows certificate into the
runner's store and passes its thumbprint to the bundler.

**Tag releases refuse to run without the macOS and Windows secrets.** Unsigned
installers are blocked by Gatekeeper / SmartScreen, so shipping one would only
generate support tickets. `workflow_dispatch` runs skip that guard and upload
artifacts for testing the pipeline before certificates exist. Signing keys never
enter the repository.

To obtain the .p12 on macOS: Keychain Access → My Certificates → right-click the
"Developer ID Application" cert → Export → base64 it (`base64 -i cert.p12 | pbcopy`).

## Keychain entries

| Field | Value |
| --- | --- |
| Service | `com.deeix.chat.desktop` |
| Account | `refresh-token` |

A single entry is intentional: the app talks to one user-chosen server at a time
and switching servers discards the previous session. The access token never
touches the keychain — it lives in JS memory and dies with the process.
