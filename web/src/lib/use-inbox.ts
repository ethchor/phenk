import { useCallback, useEffect, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { api, type Identity, type MessageSummary } from "./api";

/** The query key an inbox's message list lives under. */
export function messagesKey(identity: Identity | null) {
  return ["messages", identity?.id ?? "none"] as const;
}

/**
 * Loads an inbox's messages and keeps them current from the event stream.
 *
 * Reconnection re-reads from the cursor rather than trusting the stream to
 * have been continuous. A dropped connection is normal — phones sleep, proxies
 * time out — and a message that arrived during one must not be lost.
 */
export function useInbox(identity: Identity | null) {
  const queryClient = useQueryClient();
  const cursor = useRef(0);

  const query = useQuery({
    queryKey: messagesKey(identity),
    enabled: identity !== null,
    queryFn: async () => {
      if (!identity) return [] as MessageSummary[];
      const page = await api.listMessages(identity, 0);
      cursor.current = page.cursor;
      return page.messages;
    },
  });

  /** Pulls everything after the cursor into the cache. */
  const catchUp = useCallback(async () => {
    if (!identity) return;
    const page = await api.listMessages(identity, cursor.current);
    if (page.messages.length === 0) return;
    cursor.current = page.cursor;

    queryClient.setQueryData<MessageSummary[]>(messagesKey(identity), (existing = []) => {
      const seen = new Set(existing.map((m) => m.id));
      const added = page.messages.filter((m) => !seen.has(m.id));
      return [...existing, ...added];
    });
  }, [identity, queryClient]);

  /** Replaces a message already in the list, once it has been parsed. */
  const refreshParsed = useCallback(async () => {
    if (!identity) return;
    const page = await api.listMessages(identity, 0);
    cursor.current = page.cursor;
    queryClient.setQueryData<MessageSummary[]>(messagesKey(identity), page.messages);
  }, [identity, queryClient]);

  useEffect(() => {
    if (!identity) return;

    let source: EventSource | null = null;
    let retry = 0;
    let reconnectTimer: number | undefined;
    let closed = false;

    const connect = () => {
      if (closed) return;
      source = new EventSource(api.streamUrl(identity, cursor.current));

      source.onopen = () => {
        retry = 0;
        // The stream replays from the cursor on connect, but a catch-up here
        // covers the window before the subscription was established.
        void catchUp();
      };
      source.addEventListener("message.received", () => void catchUp());
      // A parsed message gains a subject and a preview, so the row it is
      // already showing has to be replaced rather than appended to.
      source.addEventListener("message.parsed", () => void refreshParsed());
      source.addEventListener("identity.expired", () => {
        void queryClient.invalidateQueries({ queryKey: ["identity", identity.id] });
      });
      source.addEventListener("identity.expiring", () => {
        void queryClient.invalidateQueries({ queryKey: ["identity", identity.id] });
      });

      source.onerror = () => {
        source?.close();
        source = null;
        if (closed) return;
        // Exponential backoff, capped: a server that is down should not be
        // hammered by every open tab at once.
        const delay = Math.min(1000 * 2 ** retry, 30_000);
        retry += 1;
        reconnectTimer = window.setTimeout(connect, delay);
      };
    };

    connect();
    return () => {
      closed = true;
      source?.close();
      if (reconnectTimer) window.clearTimeout(reconnectTimer);
    };
  }, [identity, catchUp, refreshParsed, queryClient]);

  return query;
}
