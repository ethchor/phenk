import type { Metadata } from "next";

import { site } from "@/lib/site";

export const metadata: Metadata = {
  title: "Domains",
  description:
    "Which domains Phenk hands out addresses on, why there are two pools, and what that means for the mail you receive.",
  alternates: { canonical: "/domains" },
};

// The list is genuinely useful — people want to know what an address will look
// like before they take one — and it happens to be a strong long-tail search
// surface. Revalidated rather than static so a rotation shows up without a
// deploy.
export const revalidate = 3600;

interface Domain {
  name: string;
  pool: "random" | "public";
}

async function loadDomains(): Promise<Domain[] | null> {
  try {
    const response = await fetch(`${site.appUrl}/v1/domains`, {
      next: { revalidate: 3600 },
    });
    if (!response.ok) return null;
    return (await response.json()) as Domain[];
  } catch {
    // The marketing site must render whether or not the API is reachable. It
    // is deployed separately, and a deploy that fails because the mail server
    // is briefly down would be its own outage.
    return null;
  }
}

export default async function DomainsPage() {
  const domains = await loadDomains();
  const random = domains?.filter((d) => d.pool === "random") ?? [];
  const shared = domains?.filter((d) => d.pool === "public") ?? [];

  return (
    <div className="mx-auto max-w-3xl px-4 py-16">
      <h1 className="text-3xl font-semibold tracking-tight">Domains</h1>
      <p className="mt-3 text-muted-foreground">
        Addresses are handed out across a rotating set of domains, in two pools that never mix.
      </p>

      <section className="mt-10">
        <h2 className="text-xl font-semibold tracking-tight">Why two pools</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          Public inboxes — the ones anybody can open by typing a name — attract far more spam than
          generated addresses do, and get blocklisted faster as a result. Keeping them on separate
          domains contains that damage to the pool that earned it, so a generated address keeps
          working when a shared one has been abused.
        </p>
        <p className="mt-2 text-sm text-muted-foreground">
          It also means a chosen name can never collide with a generated address, whatever it is
          called.
        </p>
      </section>

      <DomainList
        title="Generated addresses"
        note="Unguessable, owned by one session, destroyed on a deadline."
        domains={random}
        unavailable={domains === null}
      />
      <DomainList
        title="Public inboxes"
        note="Anyone who guesses the name can read these. Never use one for anything that matters."
        domains={shared}
        unavailable={domains === null}
        warn
      />
    </div>
  );
}

function DomainList({
  title,
  note,
  domains,
  unavailable,
  warn,
}: {
  title: string;
  note: string;
  domains: Domain[];
  unavailable: boolean;
  warn?: boolean;
}) {
  return (
    <section className="mt-10">
      <h2 className="text-xl font-semibold tracking-tight">{title}</h2>
      <p
        className={
          warn
            ? "mt-2 rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-sm"
            : "mt-2 text-sm text-muted-foreground"
        }
      >
        {note}
      </p>

      {unavailable ? (
        <p className="mt-4 text-sm text-muted-foreground">
          The current list is unavailable. Open an inbox at{" "}
          <a href={site.appUrl} className="text-primary hover:underline">
            {new URL(site.appUrl).host}
          </a>{" "}
          to see the domain you are given.
        </p>
      ) : domains.length === 0 ? (
        <p className="mt-4 text-sm text-muted-foreground">No domains in this pool right now.</p>
      ) : (
        <ul className="mt-4 grid gap-2 sm:grid-cols-2">
          {domains.map((domain) => (
            <li key={domain.name} className="rounded-md border bg-card px-3 py-2 font-mono text-sm">
              {domain.name}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
