"use client";

// Server address handling for native clients. The web build never calls this:
// it inherits its API address from the page origin (see @deeix/core resolveApiBaseUrl).

import { normalizeApiBaseUrl } from "@deeix/core";

import { isDesktopApp } from "./runtime";

/** localStorage key holding the user-chosen API base URL on native clients. */
export const API_BASE_URL_STORAGE_KEY = "deeix-chat:api-base-url";

/**
 * Normalise user input into a storable API base URL.
 * Returns "" when the value is not a usable absolute http(s) URL, so callers can
 * show a validation error instead of silently falling back.
 */
export function validateApiBaseUrl(raw: string): string {
  return normalizeApiBaseUrl(raw);
}

/** Persist the server address; an invalid value clears it. */
export function setStoredApiBaseUrl(value: string): void {
  if (typeof localStorage === "undefined") {
    return;
  }
  const normalized = normalizeApiBaseUrl(value);
  try {
    if (normalized) {
      localStorage.setItem(API_BASE_URL_STORAGE_KEY, normalized);
    } else {
      localStorage.removeItem(API_BASE_URL_STORAGE_KEY);
    }
  } catch {
    // Storage unavailable (quota, restricted profile): the address stays session-only.
  }
}

/** Read the persisted server address. Returns "" when unset or invalid. */
export function readStoredApiBaseUrl(): string {
  if (typeof localStorage === "undefined") {
    return "";
  }
  try {
    return normalizeApiBaseUrl(localStorage.getItem(API_BASE_URL_STORAGE_KEY));
  } catch {
    return "";
  }
}

/**
 * A desktop install without a configured server cannot talk to anything, so the
 * app must show the setup screen instead of a login form. Browsers always have an
 * origin to fall back on, so this is false there.
 */
export function needsServerSetup(): boolean {
  return isDesktopApp() && !readStoredApiBaseUrl();
}
