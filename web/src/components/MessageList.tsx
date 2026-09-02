import { Mail, Paperclip } from "lucide-react";
import { Badge, Card, Skeleton, cn } from "@phenk/ui";

import type { Identity, MessageSummary } from "../lib/api";
import { relativeTime } from "../lib/format";

interface MessageListProps {
  identity: Identity;
  messages: MessageSummary[];
  selectedId: string | null;
  loading: boolean;
  onSelect: (message: MessageSummary) => void;
  readIds: Set<string>;
}

export function MessageList({
  identity,
  messages,
  selectedId,
  loading,
  onSelect,
  readIds,
}: MessageListProps) {
  if (loading) {
    return (
      <div className="space-y-2" aria-busy="true" aria-label="Loading messages">
        {[0, 1, 2].map((i) => (
          <Card key={i} className="p-3">
            <Skeleton className="h-4 w-1/3" />
            <Skeleton className="mt-2 h-4 w-2/3" />
            <Skeleton className="mt-2 h-3 w-full" />
          </Card>
        ))}
      </div>
    );
  }

  if (messages.length === 0) {
    return <EmptyInbox identity={identity} />;
  }

  return (
    <ul className="space-y-2">
      {messages.map((message) => {
        const unread = !readIds.has(message.id);
        return (
          <li key={message.id}>
            <button
              type="button"
              onClick={() => onSelect(message)}
              aria-current={selectedId === message.id}
              className={cn(
                "w-full rounded-lg border bg-card p-3 text-left transition-colors hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                selectedId === message.id && "border-primary bg-accent/60",
              )}
            >
              <div className="flex items-center gap-2">
                {unread && (
                  <span
                    className="size-2 shrink-0 rounded-full bg-primary"
                    aria-label="Unread"
                  />
                )}
                <span className="min-w-0 flex-1 truncate text-sm font-medium">
                  {message.from.name || message.from.address || "Unknown sender"}
                </span>
                {message.attachment_count > 0 && (
                  <Paperclip className="size-3.5 shrink-0 text-muted-foreground" aria-label="Has attachments" />
                )}
                <span className="shrink-0 text-xs text-muted-foreground">
                  {relativeTime(message.received_at)}
                </span>
              </div>

              <p className="mt-1 truncate text-sm">
                {message.subject || <span className="text-muted-foreground">(no subject)</span>}
              </p>

              {message.state === "received" ? (
                <p className="mt-1 text-xs text-muted-foreground">Still opening this one…</p>
              ) : message.state === "failed" ? (
                <Badge variant="outline" className="mt-1">
                  Could not be read — the original is still downloadable
                </Badge>
              ) : (
                <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">{message.preview}</p>
              )}
            </button>
          </li>
        );
      })}
    </ul>
  );
}

/**
 * The empty state repeats the address on purpose. It is the thing the user came
 * for, and this is the screen they are looking at while they paste it somewhere
 * else.
 */
function EmptyInbox({ identity }: { identity: Identity }) {
  return (
    <Card className="flex flex-col items-center gap-3 px-6 py-12 text-center">
      <span className="relative flex size-10 items-center justify-center">
        <span className="absolute inline-flex size-full animate-ping rounded-full bg-primary/20" />
        <Mail className="relative size-6 text-primary" aria-hidden />
      </span>
      <div>
        <p className="text-sm font-medium">This inbox is live and waiting</p>
        <p className="mt-1 text-xs text-muted-foreground">
          Mail sent to <span className="address font-medium">{identity.address}</span> appears here
          the moment it arrives.
        </p>
      </div>
    </Card>
  );
}
