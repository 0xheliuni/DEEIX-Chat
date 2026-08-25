"use client";

import * as React from "react";

import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import { getFileProcessingStatuses } from "@/shared/api/file";
import type { FileProcessingStatusDTO } from "@/shared/api/file.types";

type FileStatus = {
  fileID: string;
};

export type FileStatusPollingResult<Status extends FileStatus> = {
  statuses: Status[];
  missingFileIDs: string[];
};

type FileStatusPollingOptions<Status extends FileStatus> = {
  fileIDs: string[];
  intervalMs: number;
  loadStatuses: (accessToken: string, fileIDs: string[], signal: AbortSignal) => Promise<Status[]>;
  onResult: (result: FileStatusPollingResult<Status>) => void;
};

export function useFileStatusPolling<Status extends FileStatus>({
  fileIDs,
  intervalMs,
  loadStatuses,
  onResult,
}: FileStatusPollingOptions<Status>) {
  const fileIDsKey = Array.from(new Set(fileIDs.filter(Boolean))).sort().join("\u0000");
  const onResultRef = React.useRef(onResult);

  React.useEffect(() => {
    onResultRef.current = onResult;
  }, [onResult]);

  React.useEffect(() => {
    if (!fileIDsKey) {
      return;
    }

    let cancelled = false;
    let failureCount = 0;
    let polling = false;
    let timer: number | undefined;
    let requestController: AbortController | null = null;
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
      let statuses: Status[] | null = null;
      requestController = new AbortController();
      const controller = requestController;
      try {
        const accessToken = await resolveAccessToken();
        if (!accessToken || cancelled || controller.signal.aborted || document.hidden) {
          return;
        }
        statuses = await loadStatuses(accessToken, requestedFileIDs, controller.signal);
        failureCount = 0;
      } catch {
        if (!controller.signal.aborted) {
          failureCount += 1;
          // Polling is best-effort; the next cycle retries without interrupting the UI.
        }
      } finally {
        if (requestController === controller) {
          requestController = null;
        }
        polling = false;
        schedule();
      }
      if (!cancelled && statuses) {
        const returnedFileIDs = new Set(statuses.map((status) => status.fileID));
        const missingFileIDs = requestedFileIDs.filter((fileID) => !returnedFileIDs.has(fileID));
        onResultRef.current({ statuses, missingFileIDs });
      }
    };
    const handleVisibilityChange = () => {
      if (timer !== undefined) {
        window.clearTimeout(timer);
        timer = undefined;
      }
      if (document.hidden) {
        requestController?.abort();
      } else {
        void poll();
      }
    };

    document.addEventListener("visibilitychange", handleVisibilityChange);
    void poll();
    return () => {
      cancelled = true;
      requestController?.abort();
      document.removeEventListener("visibilitychange", handleVisibilityChange);
      if (timer !== undefined) {
        window.clearTimeout(timer);
      }
    };
  }, [fileIDsKey, intervalMs, loadStatuses]);
}

export function useFileProcessingStatusPolling(
  options: Omit<FileStatusPollingOptions<FileProcessingStatusDTO>, "loadStatuses">,
) {
  useFileStatusPolling({
    ...options,
    loadStatuses: getFileProcessingStatuses,
  });
}
