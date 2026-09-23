"use client";

import { type AuthHost, type SessionStore, classifyAuthError, createAuthClient } from "@deeix/core";
import type { LoginData } from "@/shared/api/auth.types";
import { ApiError, apiRequest } from "@/shared/api/http-client";
import { clearSessionSnapshot, readAccessToken, readSessionRevision, writeSessionSnapshot } from "@/shared/auth/session";
import { isDesktopApp } from "@/shared/platform";
import { readRefreshToken, writeRefreshToken } from "@/shared/platform/desktop-secrets";

// Runtime host for the shared auth state machine (@deeix/core).
//
// The browser and the desktop shell share one request path; only the refresh
// credential differs:
//   - browser: HttpOnly cookie, written and cleared by the server.
//   - desktop: refresh token returned in the response body (X-Client-Platform),
//     stored in the OS keychain and sent back in the request body.
//
// The platform check only picks the transport; the backend decides what it
// will actually hand out.

const AUTH_REFRESH_LOCK_NAME = "deeix-chat:auth-refresh";

type NavigatorWithLocks = Navigator & {
  locks?: {
    request<T>(name: string, callback: () => Promise<T> | T): Promise<T>;
  };
};

const sessionStore: SessionStore = {
  readAccessToken,
  readRevision: readSessionRevision,
  write: (credentials) => writeSessionSnapshot(credentials),
  // Refresh-driven clears never fan out to peers: each tab's own refresh
  // will fail on the same server state, and a peer may already hold a newer session.
  clear: () => clearSessionSnapshot({ syncPeers: false }),
};

const host: AuthHost = {
  store: sessionStore,
  async refreshSession() {
    if (!isDesktopApp()) {
      // Refresh credential travels as the HttpOnly cookie.
      const data = await apiRequest<LoginData>("/api/v1/auth/refresh", { method: "POST" });
      return data.accessToken ? { accessToken: data.accessToken, sessionID: data.sessionID } : null;
    }

    const refreshToken = await readRefreshToken();
    if (!refreshToken) {
      return null;
    }
    const data = await apiRequest<LoginData>("/api/v1/auth/refresh", {
      method: "POST",
      body: { refreshToken },
    });
    return data.accessToken ? { accessToken: data.accessToken, sessionID: data.sessionID, refreshToken: data.refreshToken } : null;
  },
  async onSessionRefreshed(credentials) {
    // Rotation issues a new refresh token; keep the keychain copy in sync.
    if (credentials.refreshToken) {
      await writeRefreshToken(credentials.refreshToken);
    }
  },
  classifyError(error) {
    return error instanceof ApiError ? classifyAuthError(error) : "other";
  },
  lock: {
    run(fn) {
      const locks = typeof navigator === "undefined" ? undefined : (navigator as NavigatorWithLocks).locks;
      return locks ? locks.request(AUTH_REFRESH_LOCK_NAME, fn) : fn();
    },
  },
};

export const authClient = createAuthClient(host);
