"use client";

import { invoke } from "@tauri-apps/api/core";

// Thin wrapper over the Tauri shell's keychain commands (apps/desktop/src-tauri).
// Only this file knows the command names. Every call degrades to a no-op outside
// the desktop shell so a browser build can import it safely.

const isAvailable = () => typeof window !== "undefined" && "isTauri" in window;

export async function readRefreshToken(): Promise<string> {
  if (!isAvailable()) {
    return "";
  }
  try {
    return (await invoke<string | null>("read_refresh_token")) ?? "";
  } catch (error) {
    console.error("failed to read refresh token from keychain", error);
    return "";
  }
}

export async function writeRefreshToken(token: string): Promise<void> {
  if (!isAvailable()) {
    return;
  }
  try {
    await invoke("write_refresh_token", { token });
  } catch (error) {
    console.error("failed to store refresh token in keychain", error);
  }
}

export async function clearRefreshToken(): Promise<void> {
  if (!isAvailable()) {
    return;
  }
  try {
    await invoke("clear_refresh_token");
  } catch (error) {
    console.error("failed to clear refresh token from keychain", error);
  }
}
