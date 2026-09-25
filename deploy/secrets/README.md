# deploy/secrets

Local-only credentials. Everything here except this file is git-ignored.

| File | What | Where else it lives |
| --- | --- | --- |
| `deeix-chat-updater.key` | Tauri updater minisign **private** key. Signs `latest.json` and every desktop package; an installed app only accepts updates signed with it. | GitHub Actions secret `TAURI_SIGNING_PRIVATE_KEY` (password secret left empty) |
| `deeix-chat-updater.key.pub` | Matching public key. | Embedded in `apps/desktop/src-tauri/tauri.conf.json` (`plugins.updater.pubkey`) |

Losing the private key means no installed desktop client can ever receive an
update again (they would have to reinstall), so keep a copy somewhere durable
outside this machine as well.

| `desktop-signing.env` | All signing variables for a local release build (copy from `desktop-signing.env.example`). | GitHub Actions secrets of the same names |

Signed local build (loads `desktop-signing.env`, builds, verifies with Gatekeeper on macOS):

```bash
pnpm --filter @deeix/desktop build:signed
```
