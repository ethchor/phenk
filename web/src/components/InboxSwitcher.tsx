import { useState, type FormEvent } from "react";
import { Inbox, Loader2 } from "lucide-react";
import { Button, Card, Input } from "@phenk/ui";

import { PublicInboxWarning } from "./PublicInboxWarning";

interface InboxSwitcherProps {
  onOpen: (localPart: string) => Promise<void>;
  busy: boolean;
}

/**
 * Opens any public inbox by name.
 *
 * The warning is rendered before anything is typed and stays there. Someone
 * about to put a name into a signup form needs to know what they are choosing
 * at the moment they choose it, not after they have already used it.
 */
export function InboxSwitcher({ onOpen, busy }: InboxSwitcherProps) {
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const localPart = name.trim().toLowerCase();
    if (!localPart) return;

    setError(null);
    try {
      await onOpen(localPart);
      setName("");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Could not open that inbox");
    }
  };

  return (
    <Card className="p-3 sm:p-4">
      <div className="flex items-center gap-2">
        <Inbox className="size-4 text-muted-foreground" aria-hidden />
        <h2 className="text-sm font-medium">Open a public inbox</h2>
      </div>

      <form onSubmit={submit} className="mt-3 flex gap-2">
        <label htmlFor="inbox-name" className="sr-only">
          Inbox name
        </label>
        <Input
          id="inbox-name"
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="invoices"
          autoComplete="off"
          autoCapitalize="none"
          spellCheck={false}
          className="address"
          disabled={busy}
        />
        <Button type="submit" disabled={busy || name.trim().length === 0}>
          {busy ? <Loader2 className="animate-spin" aria-hidden /> : null}
          Open
        </Button>
      </form>

      {error && (
        <p role="alert" className="mt-2 text-xs text-destructive">
          {error}
        </p>
      )}

      <PublicInboxWarning className="mt-3" compact />
    </Card>
  );
}
