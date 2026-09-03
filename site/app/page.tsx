import type { Metadata } from "next";
import Link from "next/link";

import { site } from "@/lib/site";

export const metadata: Metadata = {
  title: `${site.name} — ${site.tagline}`,
  description: site.description,
  alternates: { canonical: "/" },
};

export default function HomePage() {
  return (
    <>
      <section className="mx-auto max-w-5xl px-4 py-20 text-center sm:py-28">
        <h1 className="text-balance text-4xl font-semibold tracking-tight sm:text-6xl">
          Email addresses that <span className="text-primary">expire</span>
        </h1>
        <p className="mx-auto mt-5 max-w-2xl text-balance text-lg text-muted-foreground">
          Take an address, receive real mail on it in real time, and let it destroy itself on a
          deadline you set. No account, no password, no forwarding address that outlives its
          purpose.
        </p>

        <div className="mt-9 flex flex-col items-center gap-3">
          <a
            href={site.appUrl}
            className="inline-flex h-12 items-center justify-center rounded-md bg-primary px-8 text-base font-medium text-primary-foreground transition-colors hover:bg-primary/90"
          >
            Open an inbox
          </a>
          <p className="text-xs text-muted-foreground">
            One click. Nothing to fill in, nothing to confirm.
          </p>
        </div>
      </section>

      <section className="mx-auto max-w-5xl px-4 pb-20">
        <div className="grid gap-6 sm:grid-cols-3">
          <Audience
            title="For people"
            lede="One less signup"
            body="Give a shop, a forum, or a download gate an address that stops existing tomorrow. Read the confirmation, take the code, walk away. Nothing follows you home."
          />
          <Audience
            title="For developers"
            lede="A test inbox with an API"
            body="Create an address, wait on it, and read the message back as structured JSON with SPF, DKIM and DMARC results attached. Signup flows become something you can assert on."
            action={{ href: "/docs", label: "Read the API" }}
          />
          <Audience
            title="For agents"
            lede="Receive mail without a mailbox"
            body="An autonomous process that needs to confirm an address does not need a permanent identity to do it. Scoped, expiring, and revocable by construction."
            action={{ href: "/docs", label: "Read the API" }}
          />
        </div>
      </section>

      <section className="mx-auto max-w-5xl px-4 pb-24">
        <h2 className="text-2xl font-semibold tracking-tight">What it actually does</h2>
        <dl className="mt-6 grid gap-6 sm:grid-cols-2">
          <Fact
            term="Mail arrives in real time"
            detail="Messages appear as they land, over an event stream. There is nothing to refresh and no polling to configure."
          />
          <Fact
            term="Expiry is real"
            detail="When an address expires, its encryption key is destroyed. The messages are not hidden or marked deleted; they stop being readable."
          />
          <Fact
            term="Addresses are never reused"
            detail="A destroyed address stays claimed forever, so mail for it can never reach somebody else."
          />
          <Fact
            term="Nothing renders unsandboxed"
            detail="Message HTML is stripped of anything executable and displayed in a sandbox with no scripting. Remote images are fetched by the server, so a sender never learns your address or when you read."
          />
          <Fact
            term="Public inboxes, honestly labelled"
            detail="You can open any inbox by typing its name, exactly as you would expect. Every screen that shows one says plainly that anybody who guesses the name can read it."
          />
          <Fact
            term="Self-hostable"
            detail="One binary and a Postgres database. The whole thing is open source."
          />
        </dl>
      </section>
    </>
  );
}

function Audience({
  title,
  lede,
  body,
  action,
}: {
  title: string;
  lede: string;
  body: string;
  action?: { href: string; label: string };
}) {
  return (
    <div className="rounded-lg border bg-card p-6">
      <p className="text-xs font-medium uppercase tracking-wide text-primary">{title}</p>
      <h2 className="mt-2 text-lg font-semibold">{lede}</h2>
      <p className="mt-2 text-sm text-muted-foreground">{body}</p>
      {action && (
        <Link href={action.href} className="mt-4 inline-block text-sm font-medium text-primary hover:underline">
          {action.label} →
        </Link>
      )}
    </div>
  );
}

function Fact({ term, detail }: { term: string; detail: string }) {
  return (
    <div>
      <dt className="font-medium">{term}</dt>
      <dd className="mt-1 text-sm text-muted-foreground">{detail}</dd>
    </div>
  );
}
