import { Dices, Inbox, Lock, Plus, X } from "lucide-react";
import { Button, cn } from "@phenk/ui";

import { randomName } from "../lib/names";
import { navigate } from "../lib/router";
import type { RecentInbox } from "../lib/storage";
import { NameForm } from "./NameForm";

interface SidebarProps {
  currentAddress: string;
  recents: RecentInbox[];
  publicDomains: string[];
  canCreatePrivate: boolean;
  onCreatePrivate: () => void;
  /** Present when the sidebar is shown as an overlay, to dismiss it. */
  onClose?: () => void;
  className?: string;
}

/**
 * The sidebar (Sidebars page): glass, in the functional layer, floating over
 * the window. Familiar symbols, one level of hierarchy, and hideable. The
 * inbox being read stays highlighted, like every other selection in the app.
 */
export function Sidebar({
  currentAddress,
  recents,
  publicDomains,
  canCreatePrivate,
  onCreatePrivate,
  onClose,
  className,
}: SidebarProps) {
  const go = (inbox: RecentInbox) => {
    navigate(
      inbox.kind === "public"
        ? { kind: "public", name: inbox.key, messageId: null }
        : { kind: "private", id: inbox.key, messageId: null },
    );
    onClose?.();
  };

  const openName = (name: string) => {
    navigate({ kind: "public", name, messageId: null });
    onClose?.();
  };

  return (
    <nav
      aria-label="Inboxes"
      className={cn("glass flex min-h-0 flex-col rounded-[var(--radius-pane)] text-label", className)}
    >
      <div className="flex items-center justify-between gap-2 px-4 pb-2 pt-4">
        <button
          type="button"
          onClick={() => {
            navigate({ kind: "home" });
            onClose?.();
          }}
          className="rounded-md type-headline text-label-secondary transition-colors hover:text-label"
          aria-label="Phenk home"
        >
          phenk<span className="text-tint">.</span>
        </button>
        {onClose && (
          <Button variant="neutral" size="icon-small" onClick={onClose} aria-label="Close sidebar">
            <X aria-hidden />
          </Button>
        )}
      </div>

      <div className="px-3">
        <NameForm domains={publicDomains} onOpen={openName} size="compact" />
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-2">
        <h2 className="px-3 pb-1 pt-2 type-footnote font-semibold text-label-secondary">Inboxes</h2>
        <ul className="flex flex-col gap-px">
          {recents.map((inbox) => {
            const current = inbox.address === currentAddress;
            const [local, domain] = splitAddress(inbox.address);
            return (
              <li key={inbox.address}>
                <button
                  type="button"
                  onClick={() => go(inbox)}
                  aria-current={current ? "page" : undefined}
                  className={cn(
                    "flex min-h-[var(--control-height)] w-full items-center gap-2.5 rounded-[var(--radius-row)] px-3 py-1.5 text-left transition-colors",
                    current ? "bg-tint-soft" : "hover:bg-fill-quaternary active:bg-fill-tertiary",
                  )}
                >
                  {inbox.kind === "private" ? (
                    <Lock
                      className={cn(
                        "size-[1.1em] shrink-0",
                        current ? "text-tint-text" : "text-label-secondary",
                      )}
                      aria-hidden
                      strokeWidth={1.75}
                    />
                  ) : (
                    <Inbox
                      className={cn(
                        "size-[1.1em] shrink-0",
                        current ? "text-tint-text" : "text-label-secondary",
                      )}
                      aria-hidden
                      strokeWidth={1.75}
                    />
                  )}
                  <span className="min-w-0 flex-1">
                    <span className="address block truncate type-body text-label">{local}</span>
                    {domain && (
                      <span className="address block truncate type-caption text-label-secondary">
                        @{domain}
                      </span>
                    )}
                  </span>
                  {inbox.kind === "private" && <span className="sr-only">(private)</span>}
                </button>
              </li>
            );
          })}
        </ul>
      </div>

      <div className="flex flex-col gap-1 border-t border-separator p-2">
        <Button variant="ghost" size="small" className="justify-start" onClick={() => openName(randomName())}>
          <Dices aria-hidden /> New Random Inbox
        </Button>
        {canCreatePrivate && (
          <Button
            variant="ghost"
            size="small"
            className="justify-start"
            onClick={() => {
              onCreatePrivate();
              onClose?.();
            }}
          >
            <Plus aria-hidden /> New Private Address
          </Button>
        )}
      </div>
    </nav>
  );
}

function splitAddress(address: string): [string, string] {
  const at = address.lastIndexOf("@");
  return at < 0 ? [address, ""] : [address.slice(0, at), address.slice(at + 1)];
}
