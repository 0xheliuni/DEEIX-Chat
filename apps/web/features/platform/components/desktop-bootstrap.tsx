"use client";

import * as React from "react";

import { ServerSetup } from "@/features/platform/components/server-setup";
import { isDesktopApp } from "@/shared/platform";
import { initializeDesktopSession } from "@/shared/platform/desktop-session";

// Desktop bootstrap: load the pinned server origin from the shell before any
// request is made, and gate the app behind the setup screen until one exists.
// In browsers this renders children immediately.

type State = "loading" | "setup" | "ready";

export function DesktopBootstrap({ children }: { children: React.ReactNode }) {
  const [state, setState] = React.useState<State>(() => (isDesktopApp() ? "loading" : "ready"));

  React.useEffect(() => {
    if (!isDesktopApp()) {
      return;
    }
    let cancelled = false;
    void initializeDesktopSession().then((origin) => {
      if (!cancelled) {
        setState(origin ? "ready" : "setup");
      }
    });
    return () => {
      cancelled = true;
    };
  }, []);

  if (state === "loading") {
    return null;
  }
  if (state === "setup") {
    return <ServerSetup onConfigured={() => setState("ready")} />;
  }
  return <>{children}</>;
}
