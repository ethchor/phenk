/*
 * What the browser remembers.
 *
 * Only which inboxes were opened, and which messages were read — never message
 * contents. Mail lives on the server for a bounded time and then stops
 * existing; a copy in localStorage would quietly outlive that.
 *
 * There is no stored appearance: the app follows the system's (Dark Mode:
 * "avoid offering an app-specific appearance setting").
 */

const RECENT = "phenk-recent";
const LAST = "phenk-last-route";
const READ_PREFIX = "phenk-read:";
const MAX_RECENT = 12;

export interface RecentInbox {
  kind: "public" | "private";
  address: string;
  /** The name for a public inbox, the identity id for a private one. */
  key: string;
}

function read<T>(storage: Storage | undefined, key: string, fallback: T): T {
  try {
    const raw = storage?.getItem(key);
    return raw ? (JSON.parse(raw) as T) : fallback;
  } catch {
    // Private browsing, disabled storage, or corrupted data. None of them is
    // worth failing a page load over.
    return fallback;
  }
}

function write(storage: Storage | undefined, key: string, value: unknown): void {
  try {
    storage?.setItem(key, JSON.stringify(value));
  } catch {
    // Nothing here is load bearing.
  }
}

const local = () => (typeof localStorage === "undefined" ? undefined : localStorage);
const session = () => (typeof sessionStorage === "undefined" ? undefined : sessionStorage);

export function recentInboxes(): RecentInbox[] {
  return read<RecentInbox[]>(local(), RECENT, []);
}

export function rememberInbox(entry: RecentInbox): void {
  const rest = recentInboxes().filter((i) => i.address !== entry.address);
  write(local(), RECENT, [entry, ...rest].slice(0, MAX_RECENT));
}

export function forgetInbox(address: string): void {
  write(
    local(),
    RECENT,
    recentInboxes().filter((i) => i.address !== address),
  );
}

/** The last inbox path, so a relaunch lands where the person left off (Launching). */
export function lastRoute(): string | null {
  return read<string | null>(local(), LAST, null);
}

export function rememberRoute(path: string): void {
  write(local(), LAST, path);
}

export function clearLastRoute(): void {
  try {
    local()?.removeItem(LAST);
  } catch {
    // ignored
  }
}

/** Read state lasts for the browser session: long enough to be useful, short enough not to linger. */
export function readMessages(inbox: string): Set<string> {
  return new Set(read<string[]>(session(), READ_PREFIX + inbox, []));
}

export function markMessageRead(inbox: string, messageId: string): void {
  const ids = readMessages(inbox);
  ids.add(messageId);
  write(session(), READ_PREFIX + inbox, [...ids].slice(-500));
}
