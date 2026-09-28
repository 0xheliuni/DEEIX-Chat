import type { LoginOptionsData, LoginPageSettings, SecurityVerificationMethod } from "@/shared/api/auth-types";
import { ApiError } from "@/shared/api/http-client";
import { DEFAULT_AUTH_NEXT_PATH } from "@/shared/auth/local-path";

export type LoginMode = "login" | "register" | "reset-password";
export type ProviderAuthIntent = "login" | "register";

export const DEFAULT_LOGIN_SETTINGS: LoginPageSettings = {
  defaultNextPath: DEFAULT_AUTH_NEXT_PATH,
};

export const DEFAULT_LOGIN_OPTIONS: LoginOptionsData = {
  usernameEnabled: true,
  emailEnabled: true,
  emailRegistrationEnabled: true,
  emailVerificationEnabled: false,
  passwordResetEnabled: false,
  turnstileRegistrationEnabled: false,
  turnstileSiteKey: "",
  providerAuthBridge: {
    callbackBaseURL: "",
    enabled: false,
    protocolVersion: 1,
  },
  providers: [],
};

const TWO_FACTOR_CHALLENGE_STORAGE_KEY = "deeix-chat:2fa:challenge";
// Matches the backend two-factor challenge TTL (twoFactorChallengeTTL); an older
// token would only be rejected on submit, so it is dropped on read instead.
const TWO_FACTOR_CHALLENGE_TTL_MS = 5 * 60 * 1000;

export type TwoFactorChallenge = {
  token: string;
  methods: SecurityVerificationMethod[];
};

type StoredTwoFactorChallenge = TwoFactorChallenge & {
  expiresAt: number;
};

function normalizeVerificationMethods(value: readonly unknown[] | undefined): SecurityVerificationMethod[] {
  const methods = (value ?? []).filter(
    (item): item is SecurityVerificationMethod => item === "two_factor" || item === "email",
  );
  return methods.length > 0 ? methods : ["two_factor"];
}

function isStoredTwoFactorChallenge(value: unknown): value is StoredTwoFactorChallenge {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  const record = value as Record<string, unknown>;
  return typeof record.token === "string" && Array.isArray(record.methods) && typeof record.expiresAt === "number";
}

// Hands a provider-login 2FA challenge from the callback page to the login page.
export function writeTwoFactorChallenge(token: string, methods: readonly SecurityVerificationMethod[] | undefined): void {
  const stored: StoredTwoFactorChallenge = {
    token,
    methods: normalizeVerificationMethods(methods),
    expiresAt: Date.now() + TWO_FACTOR_CHALLENGE_TTL_MS,
  };
  try {
    window.sessionStorage.setItem(TWO_FACTOR_CHALLENGE_STORAGE_KEY, JSON.stringify(stored));
  } catch {
    // sessionStorage may be unavailable; the user simply signs in again.
  }
}

// Reads and removes the pending challenge. Malformed or expired entries count as absent.
export function takeTwoFactorChallenge(): TwoFactorChallenge | null {
  try {
    const raw = window.sessionStorage.getItem(TWO_FACTOR_CHALLENGE_STORAGE_KEY);
    window.sessionStorage.removeItem(TWO_FACTOR_CHALLENGE_STORAGE_KEY);
    if (!raw) {
      return null;
    }
    const parsed: unknown = JSON.parse(raw);
    if (!isStoredTwoFactorChallenge(parsed) || !parsed.token || parsed.expiresAt <= Date.now()) {
      return null;
    }
    return { token: parsed.token, methods: normalizeVerificationMethods(parsed.methods) };
  } catch {
    return null;
  }
}

export function normalizeTwoFactorInput(value: string): string {
  return value.replace(/[^a-zA-Z0-9-]/g, "").slice(0, 32);
}

export function normalizeRegisterCode(value: string): string {
  return value.replace(/\D/g, "").slice(0, 6);
}

export function isTwoFactorChallengeExpired(error: unknown): boolean {
  return error instanceof ApiError && error.status === 401 && error.message === "two factor challenge expired";
}


