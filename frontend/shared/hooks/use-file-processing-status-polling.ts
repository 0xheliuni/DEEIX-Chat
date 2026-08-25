"use client";

import * as React from "react";

import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { getFileProcessingStatuses } from "@/shared/api/file";
import type { FileProcessingStatusDTO } from "@/shared/api/file.types";

export function useFileProcessingStatusPolling({
  fileIDs,
  intervalMs,
  onStatuses,
}: {
  fileIDs: string[];
  intervalMs: number;
  onStatuses: (statuses: FileProcessingStatusDTO[]) => void;
}) {
  const fileIDsKey = Array.from(new Set(fileIDs.filter(Boolean))).sort().join("\u0000");
  const onStatusesRef = React.useRef(onStatuses);

  React.useEffect(() => {
    onStatusesRef.current = onStatuses;
  }, [onStatuses]);

  React.useEffect(() => {
    if (!fileIDsKey) {
      return;
    }

    let cancelled = false;
    let failureCount = 0;
    let polling = false;
    let timer: number | undefined;
    const requestedFileIDs = fileIDsKey.split("\u0000");
    const schedule = () => {
      if (!cancelled && !document.hidden) {
        timer = window.setTimeout(
          poll,
          intervalMs * Math.min(2 ** failureCount, 4),
        );
      }
    };
    const poll = async () => {
      if (cancelled || polling || document.hidden) {
        return;
      }
      polling = true;
      let statuses: FileProcessingStatusDTO[] | null = null;
      try {
        const accessToken = await resolveAccessToken();
        if (!accessToken || cancelled || document.hidden) {
          return;
        }
        statuses = await getFileProcessingStatuses(accessToken, requestedFileIDs);
        failureCount = 0;
      } catch {
        failureCount += 1;
        // Polling is best-effort; the next cycle retries without interrupting the UI.
      } finally {
        polling = false;
        schedule();
      }
      if (!cancelled && statuses) {
        onStatusesRef.current(statuses);
      }
    };
    const handleVisibilityChange = () => {
      if (timer !== undefined) {
        window.clearTimeout(timer);
        timer = undefined;
      }
      if (!document.hidden) {
        void poll();
      }
    };

    document.addEventListener("visibilitychange", handleVisibilityChange);
    void poll();
    return () => {
      cancelled = true;
      document.removeEventListener("visibilitychange", handleVisibilityChange);
      if (timer !== undefined) {
        window.clearTimeout(timer);
      }
    };
  }, [fileIDsKey, intervalMs]);
}
