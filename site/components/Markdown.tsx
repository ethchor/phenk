import { Fragment } from "react";

/**
 * Renders the Markdown subset the posts use, as React elements.
 *
 * Nothing here produces raw HTML, so a post cannot inject markup even by
 * accident. That matters less for content written in this repository than it
 * would for content from outside it, but it costs nothing and removes the
 * question entirely.
 */
export function Markdown({ source }: { source: string }) {
  const blocks = source.split(/\n{2,}/);

  return (
    <div>
      {blocks.map((block, index) => (
        <Block key={index} text={block.trim()} />
      ))}
    </div>
  );
}

function Block({ text }: { text: string }) {
  if (!text) return null;

  if (text.startsWith("### ")) {
    return <h3 className="mt-8 text-lg font-semibold tracking-tight">{inline(text.slice(4))}</h3>;
  }
  if (text.startsWith("## ")) {
    return <h2 className="mt-10 text-xl font-semibold tracking-tight">{inline(text.slice(3))}</h2>;
  }
  if (text.startsWith("> ")) {
    return (
      <blockquote className="mt-4 border-l-2 border-primary pl-4 text-muted-foreground">
        {inline(text.replace(/^> ?/gm, ""))}
      </blockquote>
    );
  }
  if (text.startsWith("```")) {
    return (
      <pre className="mt-4 overflow-x-auto rounded-lg border bg-muted/40 p-4 text-xs">
        <code>{text.replace(/^```[a-z]*\n?/, "").replace(/```$/, "")}</code>
      </pre>
    );
  }
  if (/^[-*] /.test(text)) {
    const items = text.split("\n").map((line) => line.replace(/^[-*] /, ""));
    return (
      <ul className="mt-4 list-disc space-y-1 pl-5">
        {items.map((item, index) => (
          <li key={index}>{inline(item)}</li>
        ))}
      </ul>
    );
  }

  return <p className="mt-4 leading-relaxed">{inline(text)}</p>;
}

/** Bold, code spans, and links. */
function inline(text: string) {
  const pattern = /(\*\*[^*]+\*\*|`[^`]+`|\[[^\]]+\]\([^)]+\))/g;
  const parts = text.split(pattern);

  return (
    <>
      {parts.map((part, index) => {
        if (!part) return null;
        if (part.startsWith("**")) {
          return <strong key={index}>{part.slice(2, -2)}</strong>;
        }
        if (part.startsWith("`")) {
          return (
            <code key={index} className="rounded bg-muted px-1 py-0.5 text-[0.9em]">
              {part.slice(1, -1)}
            </code>
          );
        }
        const link = /^\[([^\]]+)\]\(([^)]+)\)$/.exec(part);
        if (link) {
          return (
            <a key={index} href={link[2]} className="text-primary hover:underline">
              {link[1]}
            </a>
          );
        }
        return <Fragment key={index}>{part}</Fragment>;
      })}
    </>
  );
}
