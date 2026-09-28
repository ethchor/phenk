import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Download, FileText, Paperclip, ShieldCheck } from "lucide-react";
import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Skeleton,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@phenk/ui";

import { api, type Identity, type MessageSummary } from "../lib/api";
import { fileSize, fullDate } from "../lib/format";
import { AuthBadges } from "./AuthBadges";
import { DetectedCode } from "./DetectedCode";
import { DetectedLinks } from "./DetectedLinks";
import { EmptyState } from "./EmptyState";
import { MessageBody } from "./MessageBody";

interface MessageDetailProps {
  identity: Identity;
  summary: MessageSummary;
}

/**
 * One message, in the content layer.
 *
 * Order follows importance (Layout: "order content by relative importance"):
 * who sent it and what it is, then the code or link most people came for,
 * then the message itself. The subject and addresses are selectable text,
 * because people copy them (Labels: "make useful label text selectable").
 */
export function MessageDetail({ identity, summary }: MessageDetailProps) {
  const [pendingDownload, setPendingDownload] = useState<{ id: string; filename: string } | null>(null);

  const {
    data: message,
    isPending,
    error,
  } = useQuery({
    queryKey: ["message", summary.id, summary.state],
    queryFn: () => api.getMessage(summary.id),
  });

  const extracted = message?.extracted ?? summary.extracted;
  const from = summary.from.name ? `${summary.from.name}` : summary.from.address;

  return (
    <article className="mx-auto flex w-full max-w-[52rem] flex-col gap-5 px-4 pb-10 pt-4 sm:px-6">
      <header className="flex flex-col gap-2">
        <h1 className="select-text type-title2 text-label">{summary.subject || "(No Subject)"}</h1>
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <span className="type-headline text-label">{from}</span>
          {summary.from.name && (
            <span className="address select-text type-subhead text-label-secondary">
              {summary.from.address}
            </span>
          )}
        </div>
        <p className="type-footnote text-label-secondary">
          <time dateTime={summary.received_at}>{fullDate(summary.received_at)}</time>
          {message && message.to.length > 0 && (
            <>
              {" · To "}
              <span className="address select-text">{message.to.join(", ")}</span>
            </>
          )}
        </p>
        <AuthBadges auth={summary.auth} />
      </header>

      {extracted && extracted.codes.length > 0 && <DetectedCode codes={extracted.codes} />}
      {extracted && <DetectedLinks links={extracted.links} />}

      {isPending && (
        <div className="flex flex-col gap-3" aria-label="Loading message">
          <Skeleton className="h-8 w-48 rounded-full" />
          <Skeleton className="h-80 w-full rounded-[0.875rem]" />
        </div>
      )}

      {error && (
        <EmptyState icon={<FileText />} title="Couldn’t Load This Message">
          It may have been deleted when the inbox’s retention window passed. The inbox list is still current.
        </EmptyState>
      )}

      {message && (
        <>
          <Tabs defaultValue={message.html ? "formatted" : "plain"} className="flex flex-col gap-3">
            {message.html && (
              <TabsList aria-label="Message view" className="self-start">
                <TabsTrigger value="formatted">Formatted</TabsTrigger>
                <TabsTrigger value="plain">Plain Text</TabsTrigger>
              </TabsList>
            )}

            {message.html && (
              <TabsContent value="formatted">
                <MessageBody html={message.html} title={message.subject || "Message"} />
              </TabsContent>
            )}

            <TabsContent value="plain">
              {message.text ? (
                <pre className="select-text whitespace-pre-wrap break-words rounded-[0.875rem] bg-fill-quaternary p-4 font-sans type-body text-label">
                  {message.text}
                </pre>
              ) : (
                <p className="rounded-[0.875rem] bg-fill-quaternary p-4 type-subhead text-label-secondary">
                  This message has no plain text version.
                </p>
              )}
            </TabsContent>
          </Tabs>

          {message.attachments.length > 0 && (
            <section
              aria-labelledby="attachments"
              className="rounded-[var(--radius-pane)] bg-fill-quaternary p-1.5"
            >
              <h3
                id="attachments"
                className="flex items-center gap-1.5 px-3 pb-1 pt-2 type-footnote font-semibold uppercase tracking-wide text-label-secondary"
              >
                <Paperclip className="size-3.5" aria-hidden /> {message.attachments.length} Attachment
                {message.attachments.length === 1 ? "" : "s"}
              </h3>
              <ul>
                {message.attachments.map((attachment) => (
                  <li
                    key={attachment.id}
                    className="flex min-h-[var(--control-height)] items-center gap-3 px-3 py-1.5"
                  >
                    <span className="min-w-0 flex-1">
                      <span className="block truncate type-body text-label">
                        {attachment.filename || "Unnamed"}
                      </span>
                      <span className="type-footnote text-label-secondary">
                        {fileSize(attachment.size_bytes)}
                      </span>
                    </span>
                    {attachment.available ? (
                      identity.public ? (
                        // A public inbox is a stranger's inbox. A file somebody
                        // else sent to a name anyone can guess deserves a
                        // moment's thought before it is opened.
                        <Button
                          variant="bordered"
                          size="small"
                          onClick={() =>
                            setPendingDownload({
                              id: attachment.id,
                              filename: attachment.filename || "attachment",
                            })
                          }
                        >
                          <Download aria-hidden /> Download…
                        </Button>
                      ) : (
                        <Button asChild variant="bordered" size="small">
                          <a
                            href={api.attachmentUrl(message.id, attachment.id)}
                            download={attachment.filename}
                          >
                            <Download aria-hidden /> Download
                          </a>
                        </Button>
                      )
                    ) : (
                      <span className="type-footnote text-label-secondary">Too large to store</span>
                    )}
                  </li>
                ))}
              </ul>
            </section>
          )}

          <footer className="flex flex-col gap-3 border-t border-separator pt-4 sm:flex-row sm:items-center sm:justify-between">
            <p className="flex items-start gap-2 type-footnote text-label-secondary">
              <ShieldCheck className="mt-[0.15em] size-[1.1em] shrink-0" aria-hidden strokeWidth={1.75} />
              Images are loaded through Phenk, so the sender never sees your address or when you read this.
            </p>
            <Button asChild variant="plain" size="small" className="self-start sm:self-auto">
              <a href={api.rawMessageUrl(message.id)} download>
                <Download aria-hidden /> Download Original
              </a>
            </Button>
          </footer>

          <Dialog open={pendingDownload !== null} onOpenChange={(open) => !open && setPendingDownload(null)}>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Download a File from a Public Inbox?</DialogTitle>
                <DialogDescription>
                  Anyone can send mail to <span className="address">{identity.address}</span>. Open{" "}
                  <span className="font-medium text-label">{pendingDownload?.filename}</span> only if you know
                  what it is.
                </DialogDescription>
              </DialogHeader>
              <DialogFooter>
                <Button variant="neutral" onClick={() => setPendingDownload(null)}>
                  Cancel
                </Button>
                <Button asChild variant="prominent">
                  <a
                    href={pendingDownload ? api.attachmentUrl(message.id, pendingDownload.id) : "#"}
                    download={pendingDownload?.filename}
                    onClick={() => setPendingDownload(null)}
                  >
                    Download
                  </a>
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </>
      )}
    </article>
  );
}
