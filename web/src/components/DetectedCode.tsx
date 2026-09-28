import { Copy } from "lucide-react";
import { Button } from "@phenk/ui";

import type { DetectedCode as Code } from "../lib/api";
import { copyText } from "../lib/clipboard";

/**
 * The verification code, lifted to the top of the message.
 *
 * The single most common reason anyone opens a temporary inbox is to read one
 * short code and paste it somewhere else, so it gets the one prominent button
 * on the screen. It is labelled as detected and shown beside the line it came
 * from, so a reader can see it is right before using it — the Generative AI
 * and Machine learning pages ask exactly that of anything that guesses on a
 * person's behalf, and this guesses, by rules rather than a model.
 */
export function DetectedCode({ codes }: { codes: Code[] }) {
  const [first, ...others] = codes;
  if (!first) return null;

  return (
    <section
      aria-label="Detected verification code"
      className="rounded-[var(--radius-pane)] bg-tint-soft p-4 sm:p-5"
    >
      <div className="flex items-center justify-between gap-3">
        <p className="type-footnote font-semibold uppercase tracking-wide text-tint-text">
          Verification Code
        </p>
        <p className="type-caption text-label-secondary">Detected automatically</p>
      </div>

      <div className="mt-2 flex flex-wrap items-center gap-3">
        <output
          className="address select-all type-title1 tracking-[0.12em] text-label"
          aria-label={`Code ${first.value.split("").join(" ")}`}
        >
          {first.value}
        </output>
        <Button onClick={() => copyText(first.value, "Code")} className="ml-auto" title="Copy code (c)">
          <Copy aria-hidden />
          Copy Code
        </Button>
      </div>

      <p className="mt-2 type-footnote text-label-secondary">
        Found in: <q className="text-label">{first.context}</q>
      </p>

      {others.length > 0 && (
        <p className="mt-2 flex flex-wrap items-center gap-1.5 type-footnote text-label-secondary">
          Also found:
          {others.map((code) => (
            <button
              key={code.value}
              type="button"
              onClick={() => copyText(code.value, "Code")}
              title={code.context}
              className="address rounded-full bg-fill-tertiary px-2 py-0.5 text-label transition-colors hover:bg-fill-secondary"
            >
              {code.value}
            </button>
          ))}
        </p>
      )}
    </section>
  );
}
