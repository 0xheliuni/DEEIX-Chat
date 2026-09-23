// Platform layer: what differs between the browser build and the desktop shell.
//
// Not a security boundary: the backend decides how to deliver the refresh token
// from the X-Client-Platform header plus the request Origin, and never trusts
// client-side flags.

export { isDesktopApp, resolveClientPlatform } from "./runtime";
export {
  API_BASE_URL_STORAGE_KEY,
  needsServerSetup,
  readStoredApiBaseUrl,
  setStoredApiBaseUrl,
  validateApiBaseUrl,
} from "./server-address";

/** Header telling the backend how to deliver the refresh token. */
export const CLIENT_PLATFORM_HEADER = "X-Client-Platform";
