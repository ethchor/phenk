import type { Metadata } from "next";
import Link from "next/link";

import { NameBox } from "@/components/NameBox";
import { site } from "@/lib/site";

export const metadata: Metadata = {
  title: `${site.name} — ${site.tagline}`,
  description: site.description,
  alternates: { canonical: "/" },
};

const AGENT_EXAMPLE = `# Wait for mail to a name nobody has used. Returns the moment it arrives.
curl -s "${site.appUrl}/v1/named/agent-7f3c9a21/wait?timeout=60"

# {"messages": [{"subject": "Your Acme code",
#   "extracted": {"codes": [{"value": "482913", ...}],
#                 "links": [{"kind": "verify", "url": "https://…"}]}}], ...}`;

export default function HomePage() {
  return (
    <>
      <section className="mx-auto max-w-5xl px-4 py-20 text-center sm:py-28">
        <h1 className="text-balance type-large-title sm:text-6xl sm:leading-tight">Any name is an inbox.</h1>
        <p className="mx-auto mt-5 max-w-2xl text-balance type-body text-label-secondary sm:text-lg">
          Type a name and read the mail sent to it. No sign-up, no password, nothing to confirm — for you,
          your test suite, and your agents.
        </p>

        <div className="mt-10">
          <NameBox appUrl={site.appUrl} />
        </div>
        <p className="mx-auto mt-4 max-w-xl type-footnote text-label-secondary">
          Inboxes are public: anyone who knows a name can read its mail. Messages are deleted after seven
          days.
        </p>
      </section>

      <section className="mx-auto max-w-5xl px-4 pb-20">
        <div className="grid gap-4 sm:grid-cols-3">
          <Audience
            title="For people"
            lede="One less signup"
            body="Give a shop, a forum or a download gate a throwaway name. Read the confirmation, copy the code — it is already lifted to the top of the message — and walk away."
          />
          <Audience
            title="For test suites"
            lede="A real inbox with an API"
            body="Use a fresh name per test, trigger your signup flow, and wait on the inbox. The code and the confirmation link come back as JSON, with SPF, DKIM and DMARC results attached."
            action={{ href: "/docs", label: "Read the API" }}
          />
          <Audience
            title="For agents"
            lede="One call from email to code"
            body="An agent picks a name, puts it in a form, and waits. Detected codes and links arrive in the wait response, and an llms.txt explains the whole flow to a model in one page."
            action={{ href: `${site.appUrl}/llms.txt`, label: "Read the agent guide" }}
          />
        </div>
      </section>

      <section className="mx-auto max-w-5xl px-4 pb-20">
        <h2 className="type-title2">For agents, the whole flow is one request</h2>
        <p className="mt-2 max-w-2xl type-subhead text-label-secondary">
          Waiting on a name that has never been used opens its inbox. The request is held until mail arrives
          and is parsed, so the code is in the response rather than a follow-up read.
        </p>
        <pre className="mt-5 overflow-x-auto rounded-[var(--radius-pane)] bg-content p-5 type-footnote leading-relaxed shadow-[0_0_0_0.5px_var(--separator)]">
          <code className="address">{AGENT_EXAMPLE}</code>
        </pre>
      </section>

      <section className="mx-auto max-w-5xl px-4 pb-24">
        <h2 className="type-title2">What it actually does</h2>
        <dl className="mt-6 grid gap-6 sm:grid-cols-2">
          <Fact
            term="Instant, public inboxes"
            detail="Any name works the moment you type it, with no account behind it. Every screen that shows an inbox says plainly that anyone who knows the name can read it."
          />
          <Fact
            term="Codes and links, found for you"
            detail="Verification codes and account links are detected on the server — conservatively, because a wrong code is worse than none — and shown with the line they came from so you can check them."
          />
          <Fact
            term="Mail arrives in real time"
            detail="Messages appear as they land, over an event stream. There is nothing to refresh and nothing to poll."
          />
          <Fact
            term="Nothing renders unsandboxed"
            detail="Message HTML is stripped of anything executable and shown in a sandbox with no scripting. Remote images are fetched by the server, so a sender never learns your address or when you read."
          />
          <Fact
            term="Encrypted at rest, per inbox"
            detail="Every inbox has its own key. Mail is encrypted under it before it is stored, and deleted on a rolling window."
          />
          <Fact
            term="Self-hostable"
            detail="One binary and a Postgres database, open source. Private, expiring addresses are an optional extra an operator can switch on."
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
    <div className="rounded-[var(--radius-pane)] bg-content p-6 shadow-[0_0_0_0.5px_var(--separator)]">
      <p className="type-footnote font-semibold uppercase tracking-wide text-tint-text">{title}</p>
      <h2 className="mt-2 type-title3">{lede}</h2>
      <p className="mt-2 type-subhead text-label-secondary">{body}</p>
      {action &&
        (action.href.startsWith("/") ? (
          <Link
            href={action.href}
            className="mt-4 inline-block type-subhead font-medium text-tint-text hover:underline"
          >
            {action.label} →
          </Link>
        ) : (
          <a
            href={action.href}
            className="mt-4 inline-block type-subhead font-medium text-tint-text hover:underline"
          >
            {action.label} →
          </a>
        ))}
    </div>
  );
}

function Fact({ term, detail }: { term: string; detail: string }) {
  return (
    <div>
      <dt className="type-headline">{term}</dt>
      <dd className="mt-1 type-subhead text-label-secondary">{detail}</dd>
    </div>
  );
}
