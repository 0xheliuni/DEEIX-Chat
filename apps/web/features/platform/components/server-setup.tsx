"use client";

import * as React from "react";
import { useTranslations } from "next-intl";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { readStoredApiBaseUrl, setStoredApiBaseUrl, validateApiBaseUrl } from "@/shared/platform/server-address";

// Desktop-only first-run screen: a Tauri install has no web origin to inherit an
// API address from, so the user must point it at their own server before login.
// Rendered only when the desktop build has no stored address (see DesktopBootstrap).

/** Endpoint used to confirm the address before we commit to it. */
const HEALTH_PATH = "/healthz";

type ProbeState =
  | { kind: "idle" }
  | { kind: "probing" }
  | { kind: "failed"; message: string };

export function ServerSetup({ children }: { children: React.ReactNode }) {
  const t = useTranslations("desktopSetup");
  const [configured, setConfigured] = React.useState(() => Boolean(readStoredApiBaseUrl()));

  if (configured) {
    return <>{children}</>;
  }

  return <ServerSetupForm labels={t} onConfigured={() => setConfigured(true)} />;
}

function ServerSetupForm({
  labels,
  onConfigured,
}: {
  labels: ReturnType<typeof useTranslations<"desktopSetup">>;
  onConfigured: () => void;
}) {
  const [value, setValue] = React.useState("");
  const [state, setState] = React.useState<ProbeState>({ kind: "idle" });

  const submit = React.useCallback(async () => {
    const candidate = validateApiBaseUrl(value);
    if (!candidate) {
      setState({ kind: "failed", message: labels("invalidUrl") });
      return;
    }

    setState({ kind: "probing" });

    // Probe before persisting: a wrong address must not be stored, otherwise the
    // user is left with a configured-but-unreachable app after a restart.
    let status = 0;
    try {
      const response = await fetch(`${candidate}${HEALTH_PATH}`, { method: "GET", cache: "no-store" });
      status = response.status;
      if (!response.ok) {
        setState({ kind: "failed", message: labels("unexpectedStatus", { status }) });
        return;
      }
    } catch {
      setState({ kind: "failed", message: labels("networkError") });
      return;
    }

    setStoredApiBaseUrl(candidate);
    onConfigured();
  }, [labels, onConfigured, value]);

  return (
    <main className="flex h-svh w-full items-center justify-center px-6">
      <form
        className="flex w-full max-w-md flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        <div className="flex flex-col gap-1">
          <h1 className="text-lg font-semibold">{labels("title")}</h1>
          <p className="text-sm text-muted-foreground">{labels("description")}</p>
        </div>

        <Input
          autoFocus
          inputMode="url"
          placeholder="https://chat.example.com"
          value={value}
          onChange={(event) => setValue(event.target.value)}
          aria-invalid={state.kind === "failed"}
        />

        {state.kind === "failed" ? (
          <p className="text-sm text-destructive">{state.message}</p>
        ) : (
          <p className="text-xs text-muted-foreground">{labels("hint")}</p>
        )}

        <Button type="submit" disabled={state.kind === "probing"}>
          {state.kind === "probing" ? labels("connecting") : labels("connect")}
        </Button>
      </form>
    </main>
  );
}
