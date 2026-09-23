"use client";

// Desktop session bootstrap: connects the shared session snapshot and the API
// client to the Tauri shell. No-op in browsers.

import type { LoginData } from "@/shared/api/auth.types";
import { registerRuntimeApiBaseURLResolver } from "@/shared/api/http-client";
import { SESSION_CLEARED_EVENT, writeSessionSnapshot } from "@/shared/auth/session";
import { isDesktopApp } from "@/shared/platform";
import { clearSession, storeSession } from "@/shared/platform/desktop-shell";
import { loadServerOrigin, readServerOrigin } from "@/shared/platform/server-address";

let initialized: Promise<string> | null = null;

/**
 * Install desktop hooks and resolve with the pinned server origin ("" on first
 * run). Idempotent; browsers resolve immediately with "".
 */
export function initializeDesktopSession(): Promise<string> {
  if (!isDesktopApp()) {
    return Promise.resolve("");
  }
  initialized ??= (async () => {
    registerRuntimeApiBaseURLResolver(readServerOrigin);
    window.addEventListener(SESSION_CLEARED_EVENT, () => {
      void clearSession();
    });
    return loadServerOrigin();
  })();
  return initialized;
}

/**
 * Record an authenticated session. Every sign-in path goes through here; on
 * desktop the refresh token is handed to the shell and never read back.
 */
export async function completeNativeSignIn(result: LoginData): Promise<void> {
  writeSessionSnapshot({ accessToken: result.accessToken, sessionID: result.sessionID });
  if (isDesktopApp() && result.refreshToken) {
    await storeSession(result.refreshToken);
  }
}
