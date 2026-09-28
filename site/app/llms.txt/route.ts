import { site } from "@/lib/site";

// Agents often start at a site's root domain. The full guide — with this
// deployment's real domains and limits — is served by the app itself; this
// points there and gives enough to begin.
export const dynamic = "force-static";

export function GET() {
  const body = `# ${site.name}

> ${site.description}

Any name is a public inbox. Mail sent to NAME@<public domain> can be read by
anyone who knows NAME, with no account and no API key.

## The whole flow

1. Pick an unguessable name, such as agent-7f3c9a21.
2. Use NAME@<public domain> in the form that sends the email. The domain is
   listed at ${site.appUrl}/v1/meta.
3. Wait for it: GET ${site.appUrl}/v1/named/NAME/wait?timeout=60
4. Read messages[0].extracted.codes[0].value, or the first link whose kind is
   "verify".

## Full guide

- ${site.appUrl}/llms.txt — the complete agent guide, for this deployment.
- ${site.url}/docs — the HTTP API reference.
`;
  return new Response(body, {
    headers: {
      "Content-Type": "text/plain; charset=utf-8",
      "Cache-Control": "public, max-age=3600",
    },
  });
}
