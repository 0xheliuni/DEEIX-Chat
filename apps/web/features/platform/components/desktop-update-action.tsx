"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { isDesktopApp } from "@/shared/platform";
import { checkForUpdate, relaunchApp, type UpdateCheckResult } from "@/shared/platform/desktop-updater";

// "Check for updates" control shown next to the version badge on desktop only.
// Browsers get new code on reload; the desktop shell must download and install.

type Phase = "idle" | "checking" | "available" | "installing" | "installed";

export function DesktopUpdateAction() {
  const t = useTranslations("desktopUpdate");
  const [phase, setPhase] = React.useState<Phase>("idle");
  const [pending, setPending] = React.useState<Extract<UpdateCheckResult, { kind: "available" }> | null>(null);

  if (!isDesktopApp()) {
    return null;
  }

  const check = async () => {
    setPhase("checking");
    const result = await checkForUpdate();
    switch (result.kind) {
      case "available":
        setPending(result);
        setPhase("available");
        return;
      case "up-to-date":
        toast.success(t("upToDate"));
        break;
      case "failed":
        toast.error(t("checkFailed", { message: result.message }));
        break;
      case "unsupported":
        break;
    }
    setPhase("idle");
  };

  const install = async () => {
    if (!pending) {
      return;
    }
    setPhase("installing");
    try {
      await pending.install();
      setPhase("installed");
    } catch (error) {
      toast.error(t("installFailed", { message: error instanceof Error ? error.message : String(error) }));
      setPhase("available");
    }
  };

  switch (phase) {
    case "available":
      return (
        <Button type="button" size="sm" onClick={() => void install()}>
          {t("install", { version: pending?.version ?? "" })}
        </Button>
      );
    case "installing":
      return (
        <Button type="button" size="sm" disabled>
          {t("installing")}
        </Button>
      );
    case "installed":
      return (
        <Button type="button" size="sm" onClick={() => void relaunchApp()}>
          {t("relaunch")}
        </Button>
      );
    default:
      return (
        <Button type="button" size="sm" variant="outline" disabled={phase === "checking"} onClick={() => void check()}>
          {phase === "checking" ? t("checking") : t("check")}
        </Button>
      );
  }
}
