import { useState } from "react";
import { Check, Copy, KeyRound } from "lucide-react";
import { toast } from "sonner";
import { Button, Card } from "@phenk/ui";

import type { DetectedCode } from "../lib/verification-code";

/**
 * Lifts a detected verification code to the top of the message.
 *
 * This is the shortcut for the single most common reason anyone opens a
 * temporary inbox. The code is set large and in a monospace face because it is
 * as often read off the screen and typed on a phone as it is copied.
 */
export function CodeCallout({ detected }: { detected: DetectedCode }) {
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(detected.code);
      setCopied(true);
      toast.success("Code copied");
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      toast.error("Could not copy. The code is above.");
    }
  };

  return (
    <Card className="border-primary/40 bg-accent/50 p-4">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <KeyRound className="size-3.5" aria-hidden />
        <span>Looks like a verification code</span>
      </div>
      <div className="mt-2 flex items-center justify-between gap-3">
        <p className="address select-all text-2xl font-semibold tracking-widest sm:text-3xl">
          {detected.code}
        </p>
        <Button onClick={copy} size="sm" className="shrink-0 gap-1.5">
          {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>
      <p className="mt-2 line-clamp-2 text-xs text-muted-foreground">{detected.context}</p>
    </Card>
  );
}
