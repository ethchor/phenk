import { createClient } from "@phenk/ui/api";

/** The app is served by the Go binary, so the API is same-origin. */
export const api = createClient();

export type { Identity, Message, MessageSummary, Attachment } from "@phenk/ui/api";
export { PhenkError, inboxPath } from "@phenk/ui/api";
