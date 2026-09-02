import { useMemo } from "react";

/**
 * Renders a message's HTML.
 *
 * The body goes into a sandboxed iframe with `allow-popups` and
 * `allow-popups-to-escape-sandbox` and nothing else. No `allow-scripts`, no
 * `allow-same-origin`. With both withheld the frame has an opaque origin and
 * no scripting at all, so even markup that got past the server-side sanitizer
 * can neither run nor read anything of ours.
 *
 * `dangerouslySetInnerHTML` is never used for message content anywhere in this
 * codebase, sanitized or not. Sanitizing is a filter and filters have bugs;
 * the sandbox is what makes a bug survivable.
 */
export function MessageBody({ html, title }: { html: string; title: string }) {
  const document = useMemo(() => wrap(html), [html]);

  return (
    <iframe
      // srcDoc rather than a blob URL, so nothing is ever fetched from an
      // origin the browser might treat as ours.
      srcDoc={document}
      sandbox="allow-popups allow-popups-to-escape-sandbox"
      referrerPolicy="no-referrer"
      title={title}
      className="h-[60vh] w-full rounded-md border bg-white lg:h-[calc(100vh-22rem)]"
    />
  );
}

/**
 * Wraps the body in a minimal document.
 *
 * The Content-Security-Policy is belt and braces on top of the sandbox: the
 * sandbox already forbids scripts, and this makes the frame's own rules say so
 * too. Images are allowed only from this origin, which is where the server's
 * proxy serves them; a message that somehow kept a direct remote URL still
 * cannot load it and still cannot report the reader.
 */
function wrap(html: string): string {
  return `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; font-src data:; base-uri 'none'; form-action 'none'; frame-src 'none'; script-src 'none'">
<style>
  html { color-scheme: light; }
  body { margin: 0; padding: 16px; font: 14px/1.5 system-ui, -apple-system, "Segoe UI", sans-serif; color: #18181b; background: #fff; overflow-wrap: anywhere; }
  img { max-width: 100%; height: auto; }
  table { max-width: 100%; }
  a { color: #0f766e; }
</style>
</head>
<body>${html}</body>
</html>`;
}
