import type { Identity } from "@phenk/ui/api";

/*
 * What the browser remembers.
 *
 * Only identifiers are kept, never message contents: the whole point of the
 * service is that mail lives on the server for a bounded time and then stops
 * existing, and a copy in localStorage would quietly outlive that.
 */

const CURRENT = "phenk-current-inbox";
const RECENT = "phenk-recent-inboxes";
const THEME = "phenk-theme";
const MAX_RECENT = 8;

export interface RememberedInbox {
  id: string;
  address: string;
  localPart: string;
  public: boolean;
}

function read<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : fallback;
  } catch {
    // Private browsing, disabled storage, or corrupted data. None of them is
    // worth failing a page load over.
    return fallback;
  }
}

function write(key: string, value: unknown): void {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // Nothing here is load bearing.
  }
}

export function rememberInbox(identity: Identity): void {
  const entry: RememberedInbox = {
    id: identity.id,
    address: identity.address,
    localPart: identity.local_part,
    public: identity.public,
  };
  write(CURRENT, entry);

  const recent = read<RememberedInbox[]>(RECENT, []).filter((i) => i.address !== entry.address);
  write(RECENT, [entry, ...recent].slice(0, MAX_RECENT));
}

export function currentInbox(): RememberedInbox | null {
  return read<RememberedInbox | null>(CURRENT, null);
}

export function recentInboxes(): RememberedInbox[] {
  return read<RememberedInbox[]>(RECENT, []);
}

export function forgetInbox(address: string): void {
  const current = currentInbox();
  if (current?.address === address) {
    try {
      localStorage.removeItem(CURRENT);
    } catch {
      // ignored
    }
  }
  write(RECENT, read<RememberedInbox[]>(RECENT, []).filter((i) => i.address !== address));
}

export function storedTheme(): "light" | "dark" | null {
  try {
    const value = localStorage.getItem(THEME);
    return value === "light" || value === "dark" ? value : null;
  } catch {
    return null;
  }
}

export function storeTheme(theme: "light" | "dark"): void {
  try {
    localStorage.setItem(THEME, theme);
  } catch {
    // ignored
  }
}
