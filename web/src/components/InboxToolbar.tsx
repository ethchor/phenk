import type { RefObject } from "react";
import {
  BookOpen,
  ChevronLeft,
  Copy,
  Ellipsis,
  Keyboard,
  Link as LinkIcon,
  PanelLeft,
  Search,
  Share,
  Trash2,
  X,
} from "lucide-react";
import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  cn,
} from "@phenk/ui";

import type { Identity } from "../lib/api";
import { canShare, copyText, shareAddress } from "../lib/clipboard";
import type { SizeClass } from "../lib/hooks";

interface InboxToolbarProps {
  identity: Identity;
  sizeClass: SizeClass;
  subtitle: string;
  /** Compact width with a message open: the leading control goes back. */
  onBack?: () => void;
  onToggleSidebar?: () => void;
  sidebarVisible: boolean;
  filter: string;
  onFilterChange: (value: string) => void;
  filterRef: RefObject<HTMLInputElement | null>;
  onShowShortcuts: () => void;
  onDestroy?: () => void;
}

/**
 * The toolbar (Toolbars page), floating in the functional layer over content.
 *
 * - The title is the inbox, never the app name ("don't title windows with
 *   your app name").
 * - Controls are grouped into glass capsules and chosen so nothing overflows
 *   at any width; the rest live in the More menu, which is kept to actions
 *   that really are secondary.
 * - Copy Address is the one action a public inbox exists for, so it is the
 *   pinned trailing item that stays visible at every size — the behaviour the
 *   Xcode 27 toolbar guidance describes for a pinned item.
 * - Standard Back button, symbols without borders.
 */
export function InboxToolbar({
  identity,
  sizeClass,
  subtitle,
  onBack,
  onToggleSidebar,
  sidebarVisible,
  filter,
  onFilterChange,
  filterRef,
  onShowShortcuts,
  onDestroy,
}: InboxToolbarProps) {
  const compact = sizeClass === "compact";
  const inboxUrl = window.location.origin + window.location.pathname;

  return (
    <header
      className={cn(
        "absolute inset-x-0 top-0 z-20 flex items-center gap-2 px-3 pb-3 pt-[max(0.75rem,env(safe-area-inset-top))]",
        // The scroll edge effect: content fades and softens as it passes
        // under the floating controls (Scroll views: use one only behind
        // floating interface elements).
        "bg-[linear-gradient(to_bottom,var(--bg-content)_45%,color-mix(in_srgb,var(--bg-content)_0%,transparent))]",
      )}
    >
      {onBack ? (
        <Button
          variant="glass"
          size="icon"
          onClick={onBack}
          aria-label="Back to messages"
          className="shrink-0"
        >
          <ChevronLeft aria-hidden />
        </Button>
      ) : (
        onToggleSidebar && (
          <Button
            variant="glass"
            size="icon"
            onClick={onToggleSidebar}
            aria-label={sidebarVisible ? "Hide sidebar" : "Show sidebar"}
            aria-pressed={sidebarVisible}
            className="shrink-0"
          >
            <PanelLeft aria-hidden />
          </Button>
        )
      )}

      <div className="min-w-0 flex-1 px-1">
        <h1 className="address truncate select-text type-headline text-label" title={identity.address}>
          {identity.address}
        </h1>
        <p className="truncate type-footnote text-label-secondary">{subtitle}</p>
      </div>

      {!compact && (
        <div className="glass flex min-w-0 max-w-[16rem] flex-1 items-center gap-1.5 rounded-full px-3 control-h">
          <Search className="size-4 shrink-0 text-label-secondary" aria-hidden />
          <input
            ref={filterRef}
            value={filter}
            onChange={(event) => onFilterChange(event.target.value)}
            placeholder="Filter"
            aria-label="Filter messages by sender, subject or text (/)"
            className="min-w-0 flex-1 bg-transparent type-body text-label outline-none placeholder:text-label-secondary"
            type="search"
            enterKeyHint="search"
            autoComplete="off"
            spellCheck={false}
          />
          {filter && (
            <button
              type="button"
              onClick={() => onFilterChange("")}
              className="grid size-5 shrink-0 place-items-center rounded-full bg-fill text-label-secondary"
              aria-label="Clear filter"
            >
              <X className="size-3" aria-hidden />
            </button>
          )}
        </div>
      )}

      <div className="glass flex shrink-0 items-center gap-0.5 rounded-full p-0.5">
        <Button
          variant="ghost"
          size={compact ? "icon" : "small"}
          onClick={() => copyText(identity.address, "Address")}
          aria-label="Copy address"
          title="Copy address"
          className={compact ? "" : "px-3"}
        >
          <Copy aria-hidden />
          {!compact && <span>Copy</span>}
        </Button>

        {canShare() && (
          <Button
            variant="ghost"
            size="icon"
            onClick={() => shareAddress(identity.address, inboxUrl)}
            aria-label="Share address"
            title="Share"
          >
            <Share aria-hidden />
          </Button>
        )}

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" aria-label="More actions" title="More">
              <Ellipsis aria-hidden />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={() => copyText(identity.address, "Address")}>
              <Copy aria-hidden /> Copy Address
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => copyText(inboxUrl, "Link")}>
              <LinkIcon aria-hidden /> Copy Link to Inbox
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => window.open("/llms.txt", "_blank", "noopener")}>
              <BookOpen aria-hidden /> Agent Guide
            </DropdownMenuItem>
            {!compact && (
              <DropdownMenuItem onSelect={onShowShortcuts}>
                <Keyboard aria-hidden /> Keyboard Shortcuts
              </DropdownMenuItem>
            )}
            {onDestroy && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem destructive onSelect={onDestroy}>
                  <Trash2 aria-hidden /> Delete Address Now…
                </DropdownMenuItem>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>
  );
}
