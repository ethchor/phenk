import type { components } from "./schema";

/*
 * The API client.
 *
 * Every request and response type comes from api/openapi.yaml through
 * openapi-typescript, so a field the server stops sending stops type-checking
 * here. Nothing in this file is hand-written data: only the transport is.
 */

export type Identity = components["schemas"]["Identity"];
export type MessageSummary = components["schemas"]["MessageSummary"];
export type Message = components["schemas"]["Message"];
export type Attachment = components["schemas"]["Attachment"];
export type MessageList = components["schemas"]["MessageList"];
export type WaitResult = components["schemas"]["WaitResult"];
export type AuthResult = components["schemas"]["AuthResult"];
export type AuthResults = components["schemas"]["AuthResults"];
export type ApiError = components["schemas"]["Error"];

/** Thrown for any non-2xx response, carrying the server's structured error. */
export class PhenkError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "PhenkError";
    this.status = status;
    this.code = code;
  }
}

export interface ClientOptions {
  /** Base URL. Empty means same-origin, which is how the embedded app runs. */
  baseUrl?: string;
}

export function createClient({ baseUrl = "" }: ClientOptions = {}) {
  async function request<T>(path: string, init?: RequestInit): Promise<T> {
    const response = await fetch(baseUrl + path, {
      ...init,
      // Ownership of a random address is the session cookie, so every request
      // has to carry it.
      credentials: "include",
      headers: {
        Accept: "application/json",
        ...(init?.body ? { "Content-Type": "application/json" } : {}),
        ...init?.headers,
      },
    });

    if (response.status === 204) {
      return undefined as T;
    }
    if (!response.ok) {
      throw await toError(response);
    }
    return (await response.json()) as T;
  }

  async function toError(response: Response): Promise<PhenkError> {
    try {
      const body = (await response.json()) as ApiError;
      return new PhenkError(response.status, body.error.code, body.error.message);
    } catch {
      return new PhenkError(response.status, "unknown", response.statusText || "Request failed");
    }
  }

  return {
    baseUrl,

    createIdentity(ttlSeconds?: number): Promise<Identity> {
      return request<Identity>("/v1/identities", {
        method: "POST",
        body: JSON.stringify(ttlSeconds ? { ttl_seconds: ttlSeconds } : {}),
      });
    },

    getIdentity(id: string): Promise<Identity> {
      return request<Identity>(`/v1/identities/${id}`);
    },

    destroyIdentity(id: string): Promise<void> {
      return request<void>(`/v1/identities/${id}`, { method: "DELETE" });
    },

    openNamed(localPart: string): Promise<Identity> {
      return request<Identity>("/v1/named", {
        method: "POST",
        body: JSON.stringify({ local_part: localPart }),
      });
    },

    listMessages(identity: Identity, since = 0): Promise<MessageList> {
      return request<MessageList>(`${inboxPath(identity)}/messages?since=${since}`);
    },

    getMessage(id: string): Promise<Message> {
      return request<Message>(`/v1/messages/${id}`);
    },

    rawMessageUrl(id: string): string {
      return `${baseUrl}/v1/messages/${id}/raw`;
    },

    attachmentUrl(messageId: string, attachmentId: string): string {
      return `${baseUrl}/v1/messages/${messageId}/attachments/${attachmentId}`;
    },

    streamUrl(identity: Identity, since = 0): string {
      return `${baseUrl}${inboxPath(identity)}/stream?since=${since}`;
    },
  };
}

export type Client = ReturnType<typeof createClient>;

/**
 * A named inbox is addressed by name and a random one by id, because a named
 * inbox has no owner to check and a random one has nothing else to identify
 * it by. Routing on `public` rather than on which field happens to be set
 * keeps the two apart at the one place it matters.
 */
export function inboxPath(identity: Identity): string {
  return identity.public
    ? `/v1/named/${encodeURIComponent(identity.local_part)}`
    : `/v1/identities/${identity.id}`;
}
