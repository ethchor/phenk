import type { Metadata } from "next";

import { loadApiReference, type Operation } from "@/lib/openapi";
import { site } from "@/lib/site";

export const metadata: Metadata = {
  title: "API reference",
  description:
    "Open an inbox by name, wait for mail on it, and read the code back as structured JSON. The whole Phenk HTTP API.",
  alternates: { canonical: "/docs" },
};

// The reference is read from api/openapi.yaml at build time, so it cannot
// describe an endpoint the server does not implement.
export const dynamic = "force-static";

export default async function DocsPage() {
  const reference = await loadApiReference();
  const groups = groupByTag(reference.operations);

  return (
    <div className="mx-auto max-w-3xl px-4 py-16">
      <h1 className="text-3xl font-semibold tracking-tight">API reference</h1>
      <p className="mt-3 text-muted-foreground">
        Version {reference.version}. Generated from the same specification the server implements, so
        it cannot drift from what actually runs.
      </p>

      <section className="mt-10">
        <h2 className="text-xl font-semibold tracking-tight">Getting started</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          Pick a name, use it, then wait on it.{" "}
          <code className="rounded bg-muted px-1">wait</code> returns immediately if anything has
          already arrived and otherwise holds the request open, so a test or an agent never has to
          poll or sleep. There is no key and no account: the name is the inbox. An agent can read{" "}
          <a href={`${site.appUrl}/llms.txt`} className="text-tint-text underline-offset-2 hover:underline">
            llms.txt
          </a>{" "}
          for the whole flow in one page.
        </p>
        <pre className="mt-4 overflow-x-auto rounded-lg border bg-muted/40 p-4 text-xs leading-relaxed">
          <code>{EXAMPLE}</code>
        </pre>
      </section>

      <section className="mt-12">
        <h2 className="text-xl font-semibold tracking-tight">Two kinds of address</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          A <strong>named</strong> address is the public inbox: anyone can open it by typing its
          name, everybody who knows the name shares it, and its messages are deleted on a rolling
          window. It is the default, and what this page is mostly about. A <strong>random</strong>{" "}
          address is unguessable, owned by the browser session that created it, and destroyed on a
          deadline — an optional feature, off unless the operator enables it, which{" "}
          <code className="rounded bg-muted px-1">GET /v1/meta</code> reports. Named addresses can
          never hold a grant, a webhook, or an API key, because there is nobody to grant anything
          to.
        </p>
      </section>

      {Object.entries(groups).map(([tag, operations]) => (
        <section key={tag} className="mt-12">
          <h2 className="text-xl font-semibold capitalize tracking-tight">{tag}</h2>
          <div className="mt-4 space-y-6">
            {operations.map((operation) => (
              <OperationCard key={`${operation.method} ${operation.path}`} operation={operation} />
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}

function OperationCard({ operation }: { operation: Operation }) {
  return (
    <article className="rounded-lg border bg-card p-4">
      <div className="flex flex-wrap items-baseline gap-2">
        <span className="rounded bg-primary/10 px-2 py-0.5 font-mono text-xs font-semibold text-primary">
          {operation.method}
        </span>
        <code className="font-mono text-sm">{operation.path}</code>
      </div>
      <p className="mt-2 font-medium">{operation.summary}</p>
      {operation.description && (
        <p className="mt-1 whitespace-pre-line text-sm text-muted-foreground">
          {operation.description.trim()}
        </p>
      )}
      {operation.parameters.length > 0 && (
        <dl className="mt-3 space-y-1 text-sm">
          {operation.parameters.map((parameter) => (
            <div key={`${parameter.in}-${parameter.name}`} className="flex flex-wrap gap-2">
              <dt className="font-mono text-xs text-foreground">
                {parameter.name}
                <span className="ml-1 text-muted-foreground">({parameter.in})</span>
                {parameter.required && <span className="ml-1 text-primary">required</span>}
              </dt>
              {parameter.description && (
                <dd className="text-xs text-muted-foreground">{parameter.description}</dd>
              )}
            </div>
          ))}
        </dl>
      )}
    </article>
  );
}

function groupByTag(operations: Operation[]): Record<string, Operation[]> {
  const groups: Record<string, Operation[]> = {};
  for (const operation of operations) {
    (groups[operation.tag] ??= []).push(operation);
  }
  return groups;
}

const EXAMPLE = `# Pick a name nobody else will guess, and use it in the form that sends mail.
NAME=agent-7f3c9a21        # → agent-7f3c9a21@<public domain>

# Block until something arrives, or 60 seconds pass. Waiting on a name nobody
# has used opens its inbox, and the answer comes back once the message is parsed.
curl -s "${site.appUrl}/v1/named/$NAME/wait?timeout=60"

# The code is already detected:  .messages[0].extracted.codes[0].value
# Wait for the next one by passing the cursor back:
curl -s "${site.appUrl}/v1/named/$NAME/wait?since=$CURSOR&timeout=60"

# Read one message in full, with SPF, DKIM and DMARC results attached.
curl -s "${site.appUrl}/v1/messages/$MESSAGE_ID"`;
