import { useEffect, useRef } from "react";
import { Copy, Inbox, Paperclip, SearchX } from "lucide-react";
import { Button, Skeleton, cn } from "@phenk/ui";

import type { Identity, MessageSummary } from "../lib/api";
import { copyText } from "../lib/clipboard";
import { listTime } from "../lib/format";
import { EmptyState } from "./EmptyState";

interface MessageListProps {
  identity: Identity;
  messages: MessageSummary[];
  selectedId: string | null;
  readIds: Set<string>;
  loading: boolean;
  filtered: boolean;
  onSelect: (message: MessageSummary) => void;
}

/**
 * The message list (Lists and tables).
 *
 * Rows are succinct — sender, subject, a two-line preview, the time — and the
 * current selection stays highlighted even when focus is elsewhere (Split
 * views: "persistently highlight the current selection in each pane"). A
 * highlight, not a ring, marks the selected row; the ring is for keyboard
 * focus (Focus and selection).
 */
export function MessageList({
  identity,
  messages,
  selectedId,
  readIds,
  loading,
  filtered,
  onSelect,
}: MessageListProps) {
  const selectedRef = useRef<HTMLButtonElement | null>(null);
  const listRef = useRef<HTMLUListElement | null>(null);

  // When the selection moves — by j/k, the arrow keys, or a click — keep it in
  // view, and let focus follow it if the person was already working in the
  // list. Otherwise focus would stay on the last row clicked while the
  // selection moved on, and two rows would each claim to be current. Focus is
  // never pulled out of anywhere else, such as the filter field (Focus and
  // selection: "avoid changing focus without people's interaction").
  useEffect(() => {
    const row = selectedRef.current;
    if (!row) return;
    row.scrollIntoView({ block: "nearest" });
    const active = document.activeElement;
    if (active === document.body || (active && listRef.current?.contains(active))) {
      row.focus({ preventScroll: true });
    }
  }, [selectedId]);

  if (loading) {
    return (
      <div className="flex flex-col gap-1 p-2" aria-label="Loading messages">
        {Array.from({ length: 5 }, (_, i) => (
          <div key={i} className="flex flex-col gap-2 rounded-[var(--radius-row)] px-3 py-3">
            <Skeleton className="h-4 w-1/2" />
            <Skeleton className="h-3.5 w-3/4" />
            <Skeleton className="h-3.5 w-full" />
          </div>
        ))}
      </div>
    );
  }

  if (messages.length === 0) {
    return filtered ? (
      <EmptyState icon={<SearchX />} title="No Matches">
        No message in this inbox matches the filter.
      </EmptyState>
    ) : (
      <EmptyState
        icon={<Inbox />}
        title="No Mail Yet"
        actions={
          <Button variant="bordered" onClick={() => copyText(identity.address, "Address")}>
            <Copy aria-hidden /> Copy Address
          </Button>
        }
      >
        <p>
          Send anything to <span className="address select-text text-label">{identity.address}</span> and it
          appears here the moment it arrives.
        </p>
        <p className="mt-3 inline-flex items-center gap-2 type-footnote">
          <span className="relative flex size-2" aria-hidden>
            <span className="absolute inline-flex size-full animate-ping rounded-full bg-system-green opacity-60" />
            <span className="relative inline-flex size-2 rounded-full bg-system-green" />
          </span>
          Waiting for mail
        </p>
      </EmptyState>
    );
  }

  return (
    <ul ref={listRef} className="flex flex-col gap-px p-2" aria-label={`Messages in ${identity.address}`}>
      {messages.map((message) => {
        const selected = message.id === selectedId;
        const unread = !readIds.has(message.id);
        const code = message.extracted?.codes[0]?.value;
        const sender = message.from.name || message.from.address || "Unknown sender";

        return (
          <li key={message.id}>
            <button
              type="button"
              ref={selected ? selectedRef : undefined}
              onClick={() => onSelect(message)}
              aria-current={selected ? "true" : undefined}
              className={cn(
                "relative grid w-full grid-cols-[0.75rem_1fr] gap-x-1.5 rounded-[var(--radius-row)] py-2.5 pl-1.5 pr-3 text-left",
                // A list shows focus as a highlight, not a ring (Focus and
                // selection); focus follows the selection, so the selected
                // row's tint is the indicator, and an unselected row reached
                // by Tab gets a highlight of its own.
                "transition-colors duration-100 focus-visible:outline-none",
                selected
                  ? "bg-tint-soft focus-visible:bg-[color-mix(in_srgb,var(--system-blue)_24%,transparent)]"
                  : "hover:bg-fill-quaternary active:bg-fill-tertiary focus-visible:bg-fill-tertiary",
              )}
            >
              <span className="flex justify-center pt-[0.45em]" aria-hidden>
                {unread && <span className="size-2 rounded-full bg-tint" />}
              </span>
              <span className="min-w-0">
                <span className="flex items-baseline justify-between gap-3">
                  <span
                    className={cn("truncate type-body text-label", unread ? "font-semibold" : "font-medium")}
                  >
                    {unread && <span className="sr-only">Unread. </span>}
                    {sender}
                  </span>
                  <time
                    dateTime={message.received_at}
                    className="shrink-0 type-footnote text-label-secondary"
                  >
                    {listTime(message.received_at)}
                  </time>
                </span>
                <span className="block truncate type-subhead text-label">
                  {message.state === "received" ? "Receiving…" : message.subject || "(No Subject)"}
                </span>
                {message.preview && (
                  <span className="mt-0.5 line-clamp-2 type-subhead text-label-secondary">
                    {message.preview}
                  </span>
                )}
                {(code || message.attachment_count > 0) && (
                  <span className="mt-1.5 flex flex-wrap items-center gap-1.5">
                    {code && (
                      <span className="address inline-flex items-center rounded-full bg-tint-soft px-2 py-0.5 type-caption font-semibold tracking-wider text-tint-text">
                        <span className="sr-only">Code </span>
                        {code}
                      </span>
                    )}
                    {message.attachment_count > 0 && (
                      <span className="inline-flex items-center gap-1 type-caption text-label-secondary">
                        <Paperclip className="size-3" aria-hidden />
                        {message.attachment_count}
                        <span className="sr-only"> attachments</span>
                      </span>
                    )}
                  </span>
                )}
              </span>
            </button>
          </li>
        );
      })}
    </ul>
  );
}
