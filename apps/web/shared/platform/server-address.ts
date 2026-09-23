"use client";

// Server address for the desktop shell. The pinned origin lives on the Rust
// side; this module keeps a synchronous copy for the API client, refreshed at
// bootstrap and whenever the setup screen commits a new address.

import { normalizeApiBaseUrl } from "@deeix/core";

import { getServerOrigin, setServerOrigin } from "./desktop-shell";

let cachedOrigin = "";

/** Synchronous read for the API client; empty until `loadServerOrigin` ran. */
export function readServerOrigin(): string {
  return cachedOrigin;
}

export async function loadServerOrigin(): Promise<string> {
  cachedOrigin = normalizeApiBaseUrl(await getServerOrigin());
  return cachedOrigin;
}

/** Normalise user input; "" when it is not an absolute http(s) URL. */
export function validateApiBaseUrl(raw: string): string {
  return normalizeApiBaseUrl(raw);
}

export async function commitServerOrigin(origin: string): Promise<string> {
  cachedOrigin = normalizeApiBaseUrl(await setServerOrigin(origin));
  return cachedOrigin;
}
