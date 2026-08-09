import { authedFetch, authedRequest } from "@/shared/api/authed-client";

export type ContentModerationPolicy = {
  inputTextCategories: string[];
  outputTextCategories: string[];
  inputImageCategories: string[];
  outputImageCategories: string[];
  version: number;
};

export type ContentModerationConfig = {
  baseUrl: string;
  apiKeyMasked?: string;
  hasAPIKey: boolean;
  model: string;
  timeoutSeconds: number;
  maxConcurrency: number;
  queueCapacity: number;
  policy: ContentModerationPolicy;
  policyVersion: number;
};

export type ContentModerationConfigResponse = {
  config: ContentModerationConfig;
  categories: {
    text: string[];
    image: string[];
  };
};

export type ContentModerationUpdateInput = {
  baseUrl?: string;
  apiKey?: string;
  clearAPIKey?: boolean;
  model?: string;
  timeoutSeconds?: number;
  maxConcurrency?: number;
  queueCapacity?: number;
  policy?: Partial<ContentModerationPolicy>;
};

export type ProbeResult = {
  valid: boolean;
  model?: string;
  latencyMS: number;
  error?: string;
};

export type ProbeResponse = {
  text: ProbeResult;
  image: ProbeResult;
};

export type DailyStat = {
  statDate: string;
  direction: string;
  modality: string;
  result: string;
  category: string;
  checkCount: number;
  contentItems: number;
  hitCount: number;
  failureCount: number;
  latencySumMS: number;
  latencyCount: number;
};

export type ModerationEvent = {
  publicID: string;
  userID: number;
  userLabel?: string;
  username?: string;
  conversationID: number;
  runID: string;
  messagePublicID: string;
  direction: string;
  modality: string;
  model: string;
  policyVersion: number;
  result: string;
  categoriesJSON: string;
  latencyMS: number;
  errorCode: string;
  errorMessage: string;
  contentSummary: string;
  createdAt: string;
};

export async function getContentModerationConfig(accessToken: string) {
  return authedRequest<ContentModerationConfigResponse>(
    "/api/v1/admin/content-moderation/config",
    { method: "GET", accessToken },
    true,
  );
}

export async function updateContentModerationConfig(
  accessToken: string,
  payload: ContentModerationUpdateInput,
) {
  return authedRequest<{ config: ContentModerationConfig }>(
    "/api/v1/admin/content-moderation/config",
    { method: "PUT", accessToken, body: payload },
    true,
  );
}

export async function probeContentModeration(accessToken: string) {
  return authedRequest<ProbeResponse>(
    "/api/v1/admin/content-moderation/probe",
    { method: "POST", accessToken },
    true,
  );
}

export async function getContentModerationStats(accessToken: string) {
  return authedRequest<{ items: DailyStat[] }>(
    "/api/v1/admin/content-moderation/stats",
    { method: "GET", accessToken },
    true,
  );
}

export async function listContentModerationEvents(
  accessToken: string,
  params: { page?: number; pageSize?: number; result?: string; direction?: string } = {},
) {
  const query = new URLSearchParams();
  if (params.page) query.set("page", String(params.page));
  if (params.pageSize) query.set("pageSize", String(params.pageSize));
  if (params.result) query.set("result", params.result);
  if (params.direction) query.set("direction", params.direction);
  const suffix = query.toString() ? `?${query.toString()}` : "";
  return authedRequest<{ items: ModerationEvent[]; total: number; page: number; pageSize: number }>(
    `/api/v1/admin/content-moderation/events${suffix}`,
    { method: "GET", accessToken },
    true,
  );
}

export type ContentModerationEventDetail = {
  event?: ModerationEvent & {
    imageCount?: number;
    imageMetaJSON?: string;
  };
  userLabel?: string;
  username?: string;
  categories?: string[];
  categoryScores?: Record<string, number>;
  decryptedText?: string;
  textAvailable?: boolean;
  imagesAvailable?: boolean;
  images?: Array<{
    index: number;
    sha256?: string;
    mimeType?: string;
    sizeBytes?: number;
    sourceFileID?: string;
  }>;
};

export async function getContentModerationEvent(accessToken: string, eventID: string) {
  return authedRequest<ContentModerationEventDetail>(
    `/api/v1/admin/content-moderation/events/${encodeURIComponent(eventID)}`,
    { method: "GET", accessToken },
    true,
  );
}

export async function fetchContentModerationEventImage(
  accessToken: string,
  eventID: string,
  index: number,
): Promise<{ blob: Blob; mimeType: string }> {
  const response = await authedFetch(
    `/api/v1/admin/content-moderation/events/${encodeURIComponent(eventID)}/images/${index}`,
    {
      method: "GET",
      accessToken,
      cache: "no-store",
    },
  );
  const mimeType = response.headers.get("Content-Type") || "image/png";
  const blob = await response.blob();
  return { blob, mimeType };
}
