import { createClient } from "@phenk/ui/api";

/** The app is served by the Go binary, so the API is same-origin. */
export const api = createClient();

export type {
  Attachment,
  AuthResult,
  AuthResults,
  DetectedCode,
  DetectedLink,
  Identity,
  Message,
  MessageSummary,
  Meta,
} from "@phenk/ui/api";
export { PhenkError, inboxPath } from "@phenk/ui/api";
