import { ArrowUpRight, Link2 } from "lucide-react";

import type { DetectedLink } from "../lib/api";
import { hostOf } from "../lib/format";

/**
 * Links that act on an account — verify, confirm, sign in, reset — pulled out
 * of the message so they can be opened without hunting through it.
 *
 * Only `verify` links are listed; unsubscribe links are left in the body where
 * they belong, never presented next to the thing someone came for. Each shows
 * its host, because a link's text says what it claims and the host says where
 * it actually goes.
 */
export function DetectedLinks({ links }: { links: DetectedLink[] }) {
  const account = links.filter((l) => l.kind === "verify").slice(0, 3);
  if (account.length === 0) return null;

  return (
    <section
      aria-labelledby="account-links"
      className="rounded-[var(--radius-pane)] bg-fill-quaternary p-1.5"
    >
      <h3
        id="account-links"
        className="px-3 pb-1 pt-2 type-footnote font-semibold uppercase tracking-wide text-label-secondary"
      >
        Account Links
      </h3>
      <ul>
        {account.map((link) => (
          <li key={link.url}>
            <a
              href={link.url}
              target="_blank"
              rel="noopener noreferrer nofollow"
              referrerPolicy="no-referrer"
              className="flex min-h-[var(--control-height)] items-center gap-3 rounded-[var(--radius-row)] px-3 py-2 transition-colors hover:bg-fill-tertiary active:bg-fill-secondary"
            >
              <Link2 className="size-[1.1em] shrink-0 text-tint-text" aria-hidden strokeWidth={1.75} />
              <span className="min-w-0 flex-1">
                <span className="block truncate type-body text-label">{link.text || "Open link"}</span>
                <span className="block truncate type-footnote text-label-secondary">{hostOf(link.url)}</span>
              </span>
              <ArrowUpRight className="size-4 shrink-0 text-label-tertiary" aria-hidden />
              <span className="sr-only">(opens in a new tab)</span>
            </a>
          </li>
        ))}
      </ul>
      <p className="px-3 pb-2 pt-1 type-caption text-label-secondary">
        Detected automatically. Check where a link goes before you open it.
      </p>
    </section>
  );
}
