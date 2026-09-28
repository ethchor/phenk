import { useState } from "react";
import { ChevronRight, Inbox, Lock } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@phenk/ui";

import { api, PhenkError, type Meta } from "../lib/api";
import { useDocumentTitle } from "../lib/hooks";
import { navigate } from "../lib/router";
import { recentInboxes, rememberInbox } from "../lib/storage";
import { NameForm } from "./NameForm";
import { PublicNotice } from "./PublicNotice";

/**
 * The front door, and the product in one field: type any name and read its
 * mail.
 *
 * No onboarding flow and no splash screen (Onboarding: "teach through
 * interactivity"; Launching: "downplay the launch experience"). The public
 * notice sits beside the field because that is the moment someone chooses a
 * name, and the recent list lets them go back without retyping.
 */
export function Home({ meta }: { meta: Meta }) {
  useDocumentTitle("Phenk — any name is an inbox");
  const [creating, setCreating] = useState(false);
  const recents = recentInboxes();
  const publicDomains = meta.domains.filter((d) => d.pool === "public").map((d) => d.name);
  // Offered only when it can succeed: the feature on, and a domain able to
  // hand out a private address. A button that can only fail is worse than none.
  const privateAvailable = meta.features.disposable && meta.domains.some((d) => d.pool === "random");

  const open = (name: string) => navigate({ kind: "public", name, messageId: null });

  const createPrivate = async () => {
    setCreating(true);
    try {
      const identity = await api.createIdentity();
      rememberInbox({ kind: "private", address: identity.address, key: identity.id });
      navigate({ kind: "private", id: identity.id, messageId: null });
    } catch (cause) {
      toast.error(cause instanceof PhenkError ? cause.message : "Couldn’t create a private address.");
    } finally {
      setCreating(false);
    }
  };

  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-[40rem] flex-col px-5 pb-[max(2rem,env(safe-area-inset-bottom))] pt-[max(3rem,env(safe-area-inset-top))] sm:pt-12">
      {/* Centred in the space above the footer on a tall screen, top-aligned
          on a phone where the keyboard will take the bottom half. */}
      <div className="sm:my-auto">
        <p className="type-headline text-label-secondary" aria-label="Phenk">
          phenk<span className="text-tint">.</span>
        </p>

        <h1 className="mt-6 type-large-title text-label">Any name is an inbox.</h1>
        <p className="mt-2 type-body text-label-secondary">
          Type a name and read the mail sent to it. No sign-up, no password — for you, your tests and your
          agents.
        </p>

        <div className="mt-8">
          <NameForm domains={publicDomains} onOpen={open} autoFocus />
        </div>

        <PublicNotice retentionHours={meta.public_retention_hours} className="mt-2 px-1" />

        {recents.length > 0 && (
          <section className="mt-10" aria-labelledby="recent-heading">
            <h2
              id="recent-heading"
              className="px-4 pb-1.5 type-footnote uppercase tracking-wide text-label-secondary"
            >
              Recent
            </h2>
            <ul className="overflow-hidden rounded-[var(--radius-pane)] bg-content shadow-[0_0_0_0.5px_var(--separator)]">
              {recents.slice(0, 6).map((inbox, index) => (
                <li key={inbox.address}>
                  <button
                    type="button"
                    onClick={() =>
                      navigate(
                        inbox.kind === "public"
                          ? { kind: "public", name: inbox.key, messageId: null }
                          : { kind: "private", id: inbox.key, messageId: null },
                      )
                    }
                    className="flex min-h-[var(--control-height)] w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-fill-quaternary active:bg-fill-tertiary"
                  >
                    {inbox.kind === "private" ? (
                      <Lock
                        className="size-[1.1em] shrink-0 text-label-secondary"
                        aria-hidden
                        strokeWidth={1.75}
                      />
                    ) : (
                      <Inbox
                        className="size-[1.1em] shrink-0 text-label-secondary"
                        aria-hidden
                        strokeWidth={1.75}
                      />
                    )}
                    <span className="address min-w-0 flex-1 truncate type-body text-label">
                      {inbox.address}
                    </span>
                    <ChevronRight className="size-4 shrink-0 text-label-tertiary" aria-hidden />
                  </button>
                  {index < Math.min(recents.length, 6) - 1 && <div className="ml-11 h-px bg-separator" />}
                </li>
              ))}
            </ul>
          </section>
        )}

        {privateAvailable && (
          <section className="mt-10 rounded-[var(--radius-pane)] bg-content p-5 shadow-[0_0_0_0.5px_var(--separator)]">
            <h2 className="flex items-center gap-2 type-headline text-label">
              <Lock className="size-[1.05em]" aria-hidden strokeWidth={1.75} />
              Need a private address?
            </h2>
            <p className="mt-1 type-subhead text-label-secondary">
              Only this browser can read it, and it is destroyed on a deadline — along with everything it
              received.
            </p>
            <Button variant="bordered" className="mt-4" onClick={createPrivate} disabled={creating}>
              Create Private Address
            </Button>
          </section>
        )}
      </div>

      <p className="mt-auto pt-12 type-footnote text-label-secondary sm:mt-0">
        Using Phenk from an agent or a test suite?{" "}
        <a href="/llms.txt" className="text-tint-text underline-offset-2 hover:underline">
          Read the agent guide
        </a>
        .
      </p>
    </main>
  );
}
