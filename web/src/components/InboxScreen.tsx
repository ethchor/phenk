import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Copy, Mail, Search, UserX, X } from "lucide-react";
import { toast } from "sonner";
import {
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  cn,
} from "@phenk/ui";

import { api, PhenkError, type Identity, type MessageSummary, type Meta } from "../lib/api";
import { copyText } from "../lib/clipboard";
import { countdown } from "../lib/format";
import { useDocumentTitle, useNow, useSizeClass } from "../lib/hooks";
import { navigate, routePath, type Route } from "../lib/router";
import {
  clearLastRoute,
  forgetInbox,
  markMessageRead,
  readMessages,
  recentInboxes,
  rememberInbox,
  rememberRoute,
} from "../lib/storage";
import { useHotkeys } from "../lib/use-hotkeys";
import { useInbox } from "../lib/use-inbox";
import { EmptyState } from "./EmptyState";
import { InboxToolbar } from "./InboxToolbar";
import { MessageDetail } from "./MessageDetail";
import { MessageList } from "./MessageList";
import { PublicNotice } from "./PublicNotice";
import { ShortcutsDialog } from "./ShortcutsDialog";
import { Sidebar } from "./Sidebar";
import { FullScreen, LaunchScreen } from "./StatusScreens";

type InboxRoute = Extract<Route, { kind: "public" | "private" }>;

/** Resolves the inbox a route names, then shows it. */
export function InboxScreen({ meta, route }: { meta: Meta; route: InboxRoute }) {
  const key = route.kind === "public" ? route.name : route.id;
  const identity = useQuery({
    queryKey: route.kind === "public" ? ["named", route.name] : ["identity", route.id],
    queryFn: () => (route.kind === "public" ? api.openNamed(route.name) : api.getIdentity(route.id)),
    retry: false,
    staleTime: route.kind === "public" ? Infinity : 15_000,
  });

  useEffect(() => {
    if (!identity.data) return;
    rememberInbox({ kind: route.kind, address: identity.data.address, key });
    rememberRoute(routePath({ ...route, messageId: null }));
  }, [identity.data, route, key]);

  if (identity.isPending) return <LaunchScreen />;
  if (identity.error || !identity.data) {
    return <InboxUnavailable route={route} error={identity.error} />;
  }
  if (identity.data.state === "expired" || identity.data.state === "purged") {
    return <InboxUnavailable route={route} error={new PhenkError(410, "expired", "expired")} />;
  }
  return <InboxView meta={meta} route={route} identity={identity.data} />;
}

function InboxView({ meta, route, identity }: { meta: Meta; route: InboxRoute; identity: Identity }) {
  const sizeClass = useSizeClass();
  const wide = sizeClass === "wide";
  const compact = sizeClass === "compact";

  const [sidebarPinned, setSidebarPinned] = useState(true);
  const [sidebarOverlay, setSidebarOverlay] = useState(false);
  const [filter, setFilter] = useState("");
  const [readIds, setReadIds] = useState(() => readMessages(identity.address));
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
  const [confirmDestroy, setConfirmDestroy] = useState(false);
  const filterRef = useRef<HTMLInputElement | null>(null);
  const compactFilterRef = useRef<HTMLInputElement | null>(null);
  const pushedDetail = useRef(false);

  useEffect(() => setReadIds(readMessages(identity.address)), [identity.address]);

  const { data: oldestFirst = [], isPending } = useInbox(identity);
  const messages = useMemo(() => [...oldestFirst].sort((a, b) => b.seq - a.seq), [oldestFirst]);

  const visible = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    if (!needle) return messages;
    return messages.filter((m) =>
      [m.from.name ?? "", m.from.address, m.subject, m.preview, m.extracted?.codes[0]?.value ?? ""].some(
        (field) => field.toLowerCase().includes(needle),
      ),
    );
  }, [messages, filter]);

  const selected = route.messageId ? (messages.find((m) => m.id === route.messageId) ?? null) : null;
  const unread = messages.filter((m) => !readIds.has(m.id)).length;
  const detailOpen = route.messageId !== null;

  useDocumentTitle(unread > 0 ? `(${unread}) ${identity.address}` : identity.address);

  // A message that has gone — deleted by retention while open — releases
  // the selection rather than leaving an empty detail pane behind.
  useEffect(() => {
    if (route.messageId && !isPending && messages.length > 0 && !selected) {
      navigate({ ...route, messageId: null }, { replace: true });
    }
  }, [route, isPending, messages.length, selected]);

  const select = useCallback(
    (message: MessageSummary) => {
      markMessageRead(identity.address, message.id);
      setReadIds((previous) => new Set(previous).add(message.id));
      // On a phone, opening a message is a navigation the system Back gesture
      // should undo. With panes side by side it is a selection, and filling
      // the history with every click would make Back useless.
      const push = compact && !detailOpen;
      pushedDetail.current = push || pushedDetail.current;
      navigate({ ...route, messageId: message.id }, { replace: !push });
    },
    [identity.address, route, compact, detailOpen],
  );

  const back = useCallback(() => {
    if (pushedDetail.current) {
      pushedDetail.current = false;
      window.history.back();
    } else {
      navigate({ ...route, messageId: null }, { replace: true });
    }
  }, [route]);

  const step = (delta: number) => {
    if (visible.length === 0) return;
    const index = selected ? visible.findIndex((m) => m.id === selected.id) : -1;
    const next = visible[Math.min(visible.length - 1, Math.max(0, index + delta))];
    if (next && next.id !== selected?.id) select(next);
  };

  const focusFilter = () => (compact ? compactFilterRef : filterRef).current?.focus();

  useHotkeys({
    "/": focusFilter,
    j: () => step(1),
    ArrowDown: () => step(1),
    k: () => step(-1),
    ArrowUp: () => step(-1),
    c: () => {
      const code = selected?.extracted?.codes[0]?.value;
      if (code) void copyText(code, "Code");
      else toast("No code detected in this message.", { duration: 1600 });
    },
    a: () => void copyText(identity.address, "Address"),
    Escape: (event) => {
      const target = event.target as HTMLElement | null;
      if (target?.tagName === "INPUT") {
        if (filter) setFilter("");
        else target.blur();
        return;
      }
      if (compact && detailOpen) back();
      else if (sidebarOverlay) setSidebarOverlay(false);
    },
  });

  const now = useNow(identity.kind === "random");
  const subtitle =
    identity.kind === "random"
      ? `Private · ${identity.expires_at ? `expires in ${countdown(identity.expires_at, now)}` : "active"}`
      : `Public · ${messages.length === 0 ? "no messages" : `${messages.length} message${messages.length === 1 ? "" : "s"}`}${unread > 0 ? `, ${unread} unread` : ""}`;

  const createPrivate = async () => {
    try {
      const created = await api.createIdentity();
      rememberInbox({ kind: "private", address: created.address, key: created.id });
      navigate({ kind: "private", id: created.id, messageId: null });
    } catch (cause) {
      toast.error(cause instanceof PhenkError ? cause.message : "Couldn’t create a private address.");
    }
  };

  const destroy = async () => {
    setConfirmDestroy(false);
    try {
      await api.destroyIdentity(identity.id);
      forgetInbox(identity.address);
      clearLastRoute();
      toast.success("Address Deleted");
      navigate({ kind: "home" });
    } catch (cause) {
      toast.error(cause instanceof PhenkError ? cause.message : "Couldn’t delete the address.");
    }
  };

  const publicDomains = meta.domains.filter((d) => d.pool === "public").map((d) => d.name);
  const sidebar = (overlay: boolean) => (
    <Sidebar
      currentAddress={identity.address}
      recents={recentInboxes()}
      publicDomains={publicDomains}
      canCreatePrivate={meta.features.disposable && meta.domains.some((d) => d.pool === "random")}
      onCreatePrivate={createPrivate}
      onClose={overlay ? () => setSidebarOverlay(false) : undefined}
      className={overlay ? "h-full" : "w-[17.5rem] shrink-0"}
    />
  );

  const showSidebarInline = wide && sidebarPinned;
  const showList = !compact || !detailOpen;
  const showDetail = !compact || detailOpen;

  return (
    <div className="flex h-dvh gap-2 bg-canvas sm:p-2">
      {showSidebarInline && sidebar(false)}

      {!showSidebarInline && sidebarOverlay && (
        <div className="fixed inset-0 z-40">
          <button
            type="button"
            aria-label="Close sidebar"
            className="absolute inset-0 bg-black/25 animate-[phenk-fade-in_150ms_ease-out]"
            onClick={() => setSidebarOverlay(false)}
          />
          <div className="absolute bottom-2 left-2 top-[max(0.5rem,env(safe-area-inset-top))] w-[min(20rem,calc(100%-1rem))] animate-[phenk-pop-in_160ms_ease-out]">
            {sidebar(true)}
          </div>
        </div>
      )}

      <main
        className={cn(
          "relative flex min-w-0 flex-1 overflow-hidden bg-content",
          "sm:rounded-[var(--radius-pane)] sm:shadow-[0_0_0_0.5px_var(--separator),0_2px_12px_rgb(0_0_0/0.04)]",
        )}
      >
        <InboxToolbar
          identity={identity}
          sizeClass={sizeClass}
          subtitle={subtitle}
          onBack={compact && detailOpen ? back : undefined}
          onToggleSidebar={() => (wide ? setSidebarPinned((v) => !v) : setSidebarOverlay(true))}
          sidebarVisible={wide ? sidebarPinned : sidebarOverlay}
          filter={filter}
          onFilterChange={setFilter}
          filterRef={filterRef}
          onShowShortcuts={() => setShortcutsOpen(true)}
          onDestroy={identity.kind === "random" ? () => setConfirmDestroy(true) : undefined}
        />

        {showList && (
          <section
            aria-label="Message list"
            className={cn(
              "flex min-h-0 flex-col overflow-y-auto overscroll-contain pt-[calc(4.5rem+env(safe-area-inset-top))]",
              compact ? "w-full pb-28" : "w-[min(24rem,42%)] shrink-0 border-r border-separator",
            )}
          >
            {identity.public && (
              <PublicNotice retentionHours={meta.public_retention_hours} className="mx-4 mb-1 mt-1" />
            )}
            {compact && (
              <div className="mx-3 mt-2 flex items-center gap-1.5 rounded-full bg-fill-tertiary px-3 control-h">
                <Search className="size-4 shrink-0 text-label-secondary" aria-hidden />
                <input
                  ref={compactFilterRef}
                  value={filter}
                  onChange={(event) => setFilter(event.target.value)}
                  placeholder="Filter"
                  aria-label="Filter messages"
                  type="search"
                  enterKeyHint="search"
                  className="min-w-0 flex-1 bg-transparent type-body text-label outline-none placeholder:text-label-secondary"
                />
                {filter && (
                  <button
                    type="button"
                    onClick={() => setFilter("")}
                    className="grid size-5 place-items-center rounded-full bg-fill text-label-secondary"
                    aria-label="Clear filter"
                  >
                    <X className="size-3" aria-hidden />
                  </button>
                )}
              </div>
            )}
            <MessageList
              identity={identity}
              messages={visible}
              selectedId={route.messageId}
              readIds={readIds}
              loading={isPending}
              filtered={filter.trim() !== ""}
              onSelect={select}
            />
          </section>
        )}

        {showDetail && (
          <section
            aria-label="Message"
            className="min-h-0 min-w-0 flex-1 overflow-y-auto overscroll-contain pt-[calc(4.5rem+env(safe-area-inset-top))]"
          >
            {selected ? (
              <MessageDetail key={selected.id} identity={identity} summary={selected} />
            ) : route.messageId || messages.length === 0 ? null : (
              // With an empty inbox the list already says what to do next;
              // a second empty state beside it would only repeat it.
              <EmptyState icon={<Mail />} title="No Message Selected" className="h-full">
                Choose a message to read it.
              </EmptyState>
            )}
          </section>
        )}

        {compact && showList && (
          <div className="pointer-events-none absolute inset-x-0 bottom-0 z-20 flex justify-center px-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
            <Button
              size="large"
              className="pointer-events-auto shadow-[0_8px_24px_rgb(0_0_0/0.18)]"
              onClick={() => copyText(identity.address, "Address")}
            >
              <Copy aria-hidden /> Copy Address
            </Button>
          </div>
        )}
      </main>

      <ShortcutsDialog open={shortcutsOpen} onOpenChange={setShortcutsOpen} />

      <Dialog open={confirmDestroy} onOpenChange={setConfirmDestroy}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete This Address Now?</DialogTitle>
            <DialogDescription>
              Everything it received is destroyed and can’t be recovered. The address is never given to anyone
              else.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="neutral" onClick={() => setConfirmDestroy(false)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={destroy}>
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

/** What went wrong opening an inbox, and what to do instead. */
function InboxUnavailable({ route, error }: { route: InboxRoute; error: unknown }) {
  const status = error instanceof PhenkError ? error.status : 0;
  const message = error instanceof PhenkError ? error.message : "";

  useEffect(() => {
    // A private address this browser can no longer read is not worth listing.
    if (route.kind === "private" && (status === 404 || status === 410)) {
      const gone = recentInboxes().find((i) => i.key === route.id);
      if (gone) forgetInbox(gone.address);
      clearLastRoute();
    }
  }, [route, status]);

  let title = "Couldn’t Open This Inbox";
  let body = "Something went wrong. Try again in a moment.";
  if (route.kind === "public" && status === 400) {
    title = "That Name Isn’t Available";
    body = message || "Choose a different name.";
  } else if (status === 429) {
    title = "Too Many New Inboxes";
    body =
      "Your network has opened a lot of new inboxes recently. Try again later, or open one you’ve used before.";
  } else if (status === 503) {
    title = "No Domain Available";
    body = "This server has no public domain handing out addresses right now.";
  } else if (route.kind === "private" && (status === 404 || status === 410)) {
    title = "This Private Address Is Gone";
    body = "It expired, was deleted, or was created in another browser. Its mail no longer exists.";
  }

  return (
    <FullScreen>
      <EmptyState
        icon={<UserX />}
        title={title}
        actions={
          <Button onClick={() => navigate({ kind: "home" })}>
            {route.kind === "public" && status === 400 ? "Choose Another Name" : "Open a Public Inbox"}
          </Button>
        }
      >
        {body}
      </EmptyState>
    </FullScreen>
  );
}
