"use client";

import { invoke } from "@tauri-apps/api/core";

// Typed bindings for the Tauri shell's session commands (apps/desktop/src-tauri/src/session.rs).
// This is the whole surface the webview has for the long-lived credential:
// it can hand a refresh token over once, ask for an access token, or sign out.
// It can never read the token back or point it at a different server.

export type ShellSessionError = {
  kind: "network" | "http" | "storage" | "no_session" | "invalid_origin";
  message: string;
  status?: number;
  errorCode?: string;
};

export function isShellSessionError(value: unknown): value is ShellSessionError {
  return typeof value === "object" && value !== null && "kind" in value && "message" in value;
}

export type ShellCredentials = { accessToken: string; sessionID: string };

/** Pinned server origin, or "" on first run. */
export async function getServerOrigin(): Promise<string> {
  return (await invoke<string | null>("get_server_origin")) ?? "";
}

/** Pin the server origin. Switching servers drops the previous session. */
export function setServerOrigin(origin: string): Promise<string> {
  return invoke<string>("set_server_origin", { origin });
}

/** Store the refresh token issued at login. One-way: there is no read. */
export function storeSession(refreshToken: string): Promise<void> {
  return invoke("store_session", { refreshToken });
}

export function hasSession(): Promise<boolean> {
  return invoke<boolean>("has_session");
}

export function clearSession(): Promise<void> {
  return invoke("clear_session");
}

/** Rotate the stored refresh token and return short-lived credentials. */
export function refreshSession(): Promise<ShellCredentials> {
  return invoke<ShellCredentials>("refresh_session");
}
