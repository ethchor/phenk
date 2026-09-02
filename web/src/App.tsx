import { useCallback, useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, Moon, Sun } from "lucide-react";
import { toast } from "sonner";
import { Button, Card } from "@phenk/ui";

import { AddressBar } from "./components/AddressBar";
import { InboxSwitcher } from "./components/InboxSwitcher";
import { MessageList } from "./components/MessageList";
import { MessageView } from "./components/MessageView";
import { api, PhenkError, type Identity, type MessageSummary } from "./lib/api";
import { currentInbox, forgetInbox, rememberInbox, storeTheme, storedTheme } from "./lib/storage";
import { messagesKey, useInbox } from "./lib/use-inbox";

export function App() {
  const queryClient = useQueryClient();
  const [identity, setIdentity] = useState<Identity | null>(null);
  const [selected, setSelected] = useState<MessageSummary | null>(null);
  const [readIds, setReadIds] = useState<Set<string>>(() => new Set());
  const [busy, setBusy] = useState(false);
  const [startupError, setStartupError] = useState<string | null>(null);

  const adopt = useCallback((next: Identity) => {
    setIdentity(next);
    setSelected(null);
    rememberInbox(next);
  }, []);

  // On load, resume the remembered inbox if it is still alive, and otherwise
  // allocate a new address. Landing on a live inbox with no interaction is the
  // whole promise of the product.
  useEffect(() => {
    let cancelled = false;

    (async () => {
      const remembered = currentInbox();
      if (remembered) {
        try {
          const resumed = remembered.public
            ? await api.openNamed(remembered.localPart)
            : await api.getIdentity(remembered.id);
          if (!cancelled && resumed.state !== "purged" && resumed.state !== "reserved") {
            adopt(resumed);
            return;
          }
          forgetInbox(remembered.address);
        } catch {
          // Expired, purged, or created by a session this browser no longer
          // holds. Either way it is gone, and a new address is what the user
          // wants next.
          forgetInbox(remembered.address);
        }
      }

      try {
        const created = await api.createIdentity();
        if (!cancelled) adopt(created);
      } catch (cause) {
        if (!cancelled) {
          setStartupError(
            cause instanceof PhenkError ? cause.message : "Could not reach the server",
          );
        }
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [adopt]);

  const { data: messages = [], isPending } = useInbox(identity);

  const newAddress = async () => {
    setBusy(true);
    try {
      adopt(await api.createIdentity());
      toast.success("New address ready");
    } catch (cause) {
      toast.error(cause instanceof PhenkError ? cause.message : "Could not create an address");
    } finally {
      setBusy(false);
    }
  };

  const destroy = async () => {
    if (!identity) return;
    setBusy(true);
    try {
      await api.destroyIdentity(identity.id);
      forgetInbox(identity.address);
      toast.success("Address destroyed");
      adopt(await api.createIdentity());
    } catch (cause) {
      toast.error(cause instanceof PhenkError ? cause.message : "Could not destroy the address");
    } finally {
      setBusy(false);
    }
  };

  const openNamed = async (localPart: string) => {
    setBusy(true);
    try {
      adopt(await api.openNamed(localPart));
    } finally {
      setBusy(false);
    }
  };

  const refresh = () => {
    if (!identity) return;
    void queryClient.invalidateQueries({ queryKey: messagesKey(identity) });
  };

  const select = (message: MessageSummary) => {
    setSelected(message);
    setReadIds((previous) => new Set(previous).add(message.id));
  };

  if (startupError) {
    return (
      <main className="mx-auto flex min-h-full max-w-md items-center px-4">
        <Card className="w-full p-6 text-center">
          <h1 className="text-lg font-semibold">Phenk is not answering</h1>
          <p className="mt-2 text-sm text-muted-foreground">{startupError}</p>
          <Button className="mt-4" onClick={() => window.location.reload()}>
            Try again
          </Button>
        </Card>
      </main>
    );
  }

  if (!identity) {
    return (
      <main className="flex min-h-full items-center justify-center">
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" aria-hidden />
          Preparing an address…
        </div>
      </main>
    );
  }

  return (
    <div className="mx-auto flex min-h-full max-w-6xl flex-col gap-4 px-3 py-4 sm:px-6 sm:py-6">
      <header className="flex items-center justify-between">
        <h1 className="text-lg font-semibold tracking-tight">
          phenk<span className="text-primary">.</span>
        </h1>
        <ThemeToggle />
      </header>

      <AddressBar
        identity={identity}
        onNewAddress={newAddress}
        onDestroy={destroy}
        onRefresh={refresh}
      />

      {/* Two panes on a wide screen, one at a time on a phone: a temp-mail
          address is pasted from a phone constantly. */}
      <div className="grid min-h-0 flex-1 gap-4 lg:grid-cols-[minmax(0,22rem)_minmax(0,1fr)]">
        <section className={selected ? "hidden lg:block" : "block"} aria-label="Messages">
          <div className="space-y-4">
            <MessageList
              identity={identity}
              messages={messages}
              selectedId={selected?.id ?? null}
              loading={isPending}
              onSelect={select}
              readIds={readIds}
            />
            <InboxSwitcher onOpen={openNamed} busy={busy} />
          </div>
        </section>

        <section className={selected ? "block" : "hidden lg:block"} aria-label="Message">
          {selected ? (
            <MessageView identity={identity} summary={selected} onBack={() => setSelected(null)} />
          ) : (
            <Card className="hidden h-full items-center justify-center p-12 text-sm text-muted-foreground lg:flex">
              Select a message to read it.
            </Card>
          )}
        </section>
      </div>
    </div>
  );
}

function ThemeToggle() {
  const [dark, setDark] = useState(() => document.documentElement.classList.contains("dark"));

  const toggle = () => {
    const next = !dark;
    setDark(next);
    document.documentElement.classList.toggle("dark", next);
    storeTheme(next ? "dark" : "light");
  };

  useEffect(() => {
    // Follow the system only while the user has expressed no preference.
    if (storedTheme() !== null) return;
    const query = window.matchMedia("(prefers-color-scheme: dark)");
    const listener = (event: MediaQueryListEvent) => {
      setDark(event.matches);
      document.documentElement.classList.toggle("dark", event.matches);
    };
    query.addEventListener("change", listener);
    return () => query.removeEventListener("change", listener);
  }, []);

  return (
    <Button variant="ghost" size="icon" onClick={toggle} aria-label="Toggle theme">
      {dark ? <Sun aria-hidden /> : <Moon aria-hidden />}
    </Button>
  );
}
