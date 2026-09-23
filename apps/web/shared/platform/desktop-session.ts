"use client";

// Desktop session bootstrap: connects the shared session snapshot and the API
// client to the Tauri shell — server address resolution, keychain persistence
// of the rotating refresh token, and desktop sign-out. No-op in browsers.

import type { LoginData } from "@/shared/api/auth.types";
import { registerRuntimeApiBaseURLResolver } from "@/shared/api/http-client";
import { SESSION_CLEARED_EVENT, writeSessionSnapshot } from "@/shared/auth/session";
import { isDesktopApp, readStoredApiBaseUrl } from "@/shared/platform";
import { clearRefreshToken, writeRefreshToken } from "@/shared/platform/desktop-secrets";

let initialized = false;

export function initializeDesktopSession(): void {
  if (initialized || !isDesktopApp()) {
    return;
  }
  initialized = true;

  registerRuntimeApiBaseURLResolver(readStoredApiBaseUrl);

  // Sign-out must also drop the keychain copy of the refresh token.
  window.addEventListener(SESSION_CLEARED_EVENT, () => {
    void clearRefreshToken();
  });
}

/**
 * Record an authenticated session. Every sign-in path (password, email
 * registration, OAuth callback, provider bridge) goes through here so the
 * keychain copy of the rotating refresh token stays correct.
 */
export async function completeNativeSignIn(result: LoginData): Promise<void> {
  writeSessionSnapshot({ accessToken: result.accessToken, sessionID: result.sessionID });
  if (isDesktopApp() && result.refreshToken) {
    await writeRefreshToken(result.refreshToken);
  }
}
