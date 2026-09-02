import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Download, Paperclip } from "lucide-react";
import {
  Button,
  Card,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Separator,
  Skeleton,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@phenk/ui";

import { api, type Identity, type MessageSummary } from "../lib/api";
import { fileSize, relativeTime } from "../lib/format";
import { detectVerificationCode } from "../lib/verification-code";
import { AuthBadges } from "./AuthBadges";
import { CodeCallout } from "./CodeCallout";
import { MessageBody } from "./MessageBody";

interface MessageViewProps {
  identity: Identity;
  summary: MessageSummary;
  onBack: () => void;
}

export function MessageView({ identity, summary, onBack }: MessageViewProps) {
  const [pendingDownload, setPendingDownload] = useState<{ id: string; filename: string } | null>(null);

  const { data: message, isPending, error } = useQuery({
    queryKey: ["message", summary.id],
    queryFn: () => api.getMessage(summary.id),
  });

  const detected = useMemo(() => {
    if (!message) return null;
    return detectVerificationCode(message.subject, message.text ?? "");
  }, [message]);

  return (
    <div className="flex h-full flex-col gap-3">
      <div className="flex items-start gap-2">
        <Button variant="ghost" size="icon" onClick={onBack} className="lg:hidden" aria-label="Back to inbox">
          <ArrowLeft aria-hidden />
        </Button>
        <div className="min-w-0 flex-1">
          <h2 className="truncate text-base font-semibold sm:text-lg">
            {summary.subject || "(no subject)"}
          </h2>
          <p className="mt-0.5 truncate text-sm text-muted-foreground">
            {summary.from.name ? `${summary.from.name} · ` : ""}
            <span className="address">{summary.from.address}</span>
          </p>
          <p className="mt-0.5 text-xs text-muted-foreground">{relativeTime(summary.received_at)}</p>
        </div>
      </div>

      <AuthBadges auth={summary.auth} />
      {detected && <CodeCallout detected={detected} />}
      <Separator />

      {isPending && <Skeleton className="h-64 w-full" />}
      {error && (
        <Card className="p-4 text-sm text-destructive">
          This message could not be loaded. It may have expired.
        </Card>
      )}

      {message && (
        <>
          <Tabs defaultValue={message.html ? "html" : "text"} className="flex min-h-0 flex-1 flex-col">
            <TabsList>
              {message.html && <TabsTrigger value="html">Formatted</TabsTrigger>}
              <TabsTrigger value="text">Plain text</TabsTrigger>
              <TabsTrigger value="raw">Original</TabsTrigger>
            </TabsList>

            {message.html && (
              <TabsContent value="html" className="min-h-0 flex-1">
                <MessageBody html={message.html} title={message.subject || "Message"} />
              </TabsContent>
            )}

            <TabsContent value="text" className="min-h-0 flex-1">
              <pre className="max-h-[60vh] overflow-auto whitespace-pre-wrap break-words rounded-md border bg-muted/40 p-4 text-sm lg:max-h-[calc(100vh-22rem)]">
                {message.text || "This message has no plain text part."}
              </pre>
            </TabsContent>

            <TabsContent value="raw" className="min-h-0 flex-1">
              <Card className="p-4">
                <p className="text-sm text-muted-foreground">
                  The message exactly as it arrived, headers and all. Everything else on this
                  screen is derived from it.
                </p>
                <Button asChild variant="outline" size="sm" className="mt-3">
                  <a href={api.rawMessageUrl(message.id)} download>
                    <Download aria-hidden /> Download original
                  </a>
                </Button>
              </Card>
            </TabsContent>
          </Tabs>

          {message.attachments.length > 0 && (
            <Card className="p-3">
              <p className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                <Paperclip className="size-3.5" aria-hidden /> {message.attachments.length} attachment
                {message.attachments.length === 1 ? "" : "s"}
              </p>
              <ul className="mt-2 space-y-1">
                {message.attachments.map((attachment) => (
                  <li key={attachment.id} className="flex items-center justify-between gap-2">
                    <span className="min-w-0 flex-1 truncate text-sm">
                      {attachment.filename || "unnamed"}
                      <span className="ml-2 text-xs text-muted-foreground">
                        {fileSize(attachment.size_bytes)}
                      </span>
                    </span>
                    {attachment.available ? (
                      identity.public ? (
                        // A public inbox is a stranger's inbox. Downloading a
                        // file somebody else sent to a name anyone can guess
                        // deserves a moment's thought.
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() =>
                            setPendingDownload({
                              id: attachment.id,
                              filename: attachment.filename || "attachment",
                            })
                          }
                        >
                          <Download aria-hidden /> Download
                        </Button>
                      ) : (
                        <Button asChild variant="outline" size="sm">
                          <a href={api.attachmentUrl(message.id, attachment.id)} download={attachment.filename}>
                            <Download aria-hidden /> Download
                          </a>
                        </Button>
                      )
                    ) : (
                      <span className="text-xs text-muted-foreground">too large to store</span>
                    )}
                  </li>
                ))}
              </ul>
            </Card>
          )}

          <Dialog open={pendingDownload !== null} onOpenChange={(open) => !open && setPendingDownload(null)}>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Download from a public inbox?</DialogTitle>
                <DialogDescription>
                  Anyone can send to <span className="address">{identity.address}</span>, and anyone
                  who knows the name can put a file there. Open{" "}
                  <span className="font-medium">{pendingDownload?.filename}</span> only if you are
                  sure you know what it is.
                </DialogDescription>
              </DialogHeader>
              <DialogFooter>
                <Button variant="ghost" onClick={() => setPendingDownload(null)}>
                  Cancel
                </Button>
                <Button asChild variant="destructive">
                  <a
                    href={pendingDownload ? api.attachmentUrl(message.id, pendingDownload.id) : "#"}
                    download={pendingDownload?.filename}
                    onClick={() => setPendingDownload(null)}
                  >
                    Download anyway
                  </a>
                </Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
        </>
      )}
    </div>
  );
}
