import { useEffect, useState } from "react";
import { Check, ChevronDown, Copy, RefreshCw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import {
  Badge,
  Button,
  Card,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@phenk/ui";

import type { Identity } from "../lib/api";
import { countdown } from "../lib/format";
import { PublicInboxWarning } from "./PublicInboxWarning";

interface AddressBarProps {
  identity: Identity;
  onNewAddress: () => void;
  onDestroy: () => void;
  onRefresh: () => void;
}

export function AddressBar({ identity, onNewAddress, onDestroy, onRefresh }: AddressBarProps) {
  const [copied, setCopied] = useState(false);
  const remaining = useCountdown(identity.expires_at);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(identity.address);
      setCopied(true);
      toast.success("Address copied");
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard access can be refused outright, and the address is on screen
      // anyway, so this is worth saying rather than swallowing.
      toast.error("Could not copy. Select the address and copy it by hand.");
    }
  };

  return (
    <Card className="p-3 sm:p-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="min-w-0 flex-1">
          <p className="text-xs text-muted-foreground">Your address</p>
          <button
            type="button"
            onClick={copy}
            title="Click to copy"
            className="address mt-0.5 block w-full truncate text-left text-base font-medium sm:text-lg"
          >
            {identity.address}
          </button>
        </div>

        <div className="flex items-center gap-2">
          {identity.public ? (
            <Badge variant="warning">Public</Badge>
          ) : (
            <ExpiryBadge remaining={remaining} state={identity.state} />
          )}

          <Button onClick={copy} size="sm" className="gap-1.5">
            {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
            <span>{copied ? "Copied" : "Copy"}</span>
          </Button>

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="icon" aria-label="Address options">
                <ChevronDown aria-hidden />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={onRefresh}>
                <RefreshCw aria-hidden /> Refresh
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={onNewAddress}>
                <RefreshCw aria-hidden /> New address
              </DropdownMenuItem>
              {!identity.public && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem destructive onSelect={onDestroy}>
                    <Trash2 aria-hidden /> Destroy now
                  </DropdownMenuItem>
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      {identity.public && <PublicInboxWarning className="mt-3" />}
    </Card>
  );
}

function ExpiryBadge({ remaining, state }: { remaining: string; state: Identity["state"] }) {
  if (state !== "active" && state !== "expiring") {
    return <Badge variant="destructive">Expired</Badge>;
  }
  // Under five minutes the badge turns red, because at that point the countdown
  // is the most important thing on the screen.
  const urgent = /^(expired|[0-4]m|\d+s)/.test(remaining);
  return (
    <Badge variant={urgent ? "destructive" : "secondary"} title="Time until this address stops accepting mail">
      {remaining || "—"}
    </Badge>
  );
}

/** Ticks once a second, and only while there is something to count down to. */
function useCountdown(expiresAt: string | undefined): string {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (!expiresAt) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [expiresAt]);

  return countdown(expiresAt, now);
}
