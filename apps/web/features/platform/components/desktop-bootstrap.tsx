"use client";

import * as React from "react";

import { ServerSetup } from "@/features/platform/components/server-setup";
import { initializeDesktopSession } from "@/shared/platform/desktop-session";
import { needsServerSetup } from "@/shared/platform";

// Desktop bootstrap. In browsers this component renders its children and does
// nothing else; the platform work is compiled in but never activated.

export function DesktopBootstrap({ children }: { children: React.ReactNode }) {
  React.useEffect(() => {
    initializeDesktopSession();
  }, []);

  if (!needsServerSetup()) {
    return <>{children}</>;
  }

  return <ServerSetup>{children}</ServerSetup>;
}
