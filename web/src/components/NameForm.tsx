import { useId, useState, type FormEvent } from "react";
import { ArrowRight, Dices } from "lucide-react";
import { Button, cn } from "@phenk/ui";

import { nameProblem, parseName, randomName } from "../lib/names";

interface NameFormProps {
  /** The public domains on offer. */
  domains: string[];
  onOpen: (name: string) => void;
  size?: "large" | "compact";
  autoFocus?: boolean;
  busy?: boolean;
}

/**
 * Type a name, open its inbox.
 *
 * Entering data, applied: the field says what it wants, validates as you type
 * rather than after you submit, never autocorrects or capitalizes, and accepts
 * a pasted full address as readily as a bare name. The random button is the
 * way out for anyone who just wants an inbox and does not care what it is
 * called ("offer choices instead of requiring text entry").
 */
export function NameForm({ domains, onOpen, size = "large", autoFocus, busy }: NameFormProps) {
  const [value, setValue] = useState("");
  const inputId = useId();
  const hintId = useId();

  const parsed = parseName(value);
  const knownDomain = parsed.domain === null || domains.includes(parsed.domain);
  const problem = !knownDomain
    ? `${parsed.domain} isn’t a domain this server receives mail for.`
    : nameProblem(parsed.local);
  const valid = parsed.local.length > 0 && problem === null;

  // A name maps to a fixed domain on the server. With one public domain the
  // resulting address is known before opening; with several it is assigned.
  const suffix = domains.length === 1 ? `@${domains[0]}` : domains.length > 1 ? "@…" : "";

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (valid && !busy) onOpen(parsed.local);
  };

  const large = size === "large";

  return (
    <form onSubmit={submit} className="w-full" noValidate>
      <label htmlFor={inputId} className="sr-only">
        Inbox name
      </label>
      <div className={cn("flex w-full items-stretch gap-2", large ? "flex-col sm:flex-row" : "flex-row")}>
        <div
          className={cn(
            "group flex min-w-0 flex-1 items-center rounded-full bg-fill-tertiary transition-[background-color,box-shadow]",
            "focus-within:bg-content focus-within:ring-[3px] focus-within:ring-[color-mix(in_srgb,var(--system-blue)_45%,transparent)]",
            large ? "min-h-[3.25rem] pl-5 pr-2" : "control-h pl-3.5 pr-1",
            problem && value ? "ring-[3px] ring-[color-mix(in_srgb,var(--system-red)_40%,transparent)]" : "",
          )}
        >
          <input
            id={inputId}
            value={value}
            onChange={(event) => setValue(event.target.value)}
            placeholder={large ? "Type any name" : "Open any inbox"}
            autoComplete="off"
            autoCapitalize="none"
            autoCorrect="off"
            spellCheck={false}
            enterKeyHint="go"
            inputMode="email"
            autoFocus={autoFocus}
            aria-describedby={hintId}
            aria-invalid={problem !== null && value !== ""}
            className={cn(
              "address min-w-0 flex-1 bg-transparent text-label outline-none placeholder:font-sans placeholder:text-label-tertiary",
              large ? "type-title3 font-normal" : "type-body",
            )}
          />
          {suffix && (
            <span
              className={cn(
                "address shrink-0 truncate text-label-secondary",
                large ? "type-body" : "type-subhead",
              )}
              title={domains.length > 1 ? "The domain is assigned when the inbox is opened." : undefined}
            >
              {suffix}
            </span>
          )}
          {!large && (
            <Button
              type="submit"
              variant="plain"
              size="icon-small"
              aria-label="Open inbox"
              disabled={!valid || busy}
            >
              <ArrowRight aria-hidden />
            </Button>
          )}
        </div>

        {large && (
          <div className="flex gap-2">
            <Button type="submit" size="large" disabled={!valid || busy} className="flex-1 sm:flex-none">
              Open Inbox
            </Button>
            <Button
              variant="neutral"
              size="large"
              className="aspect-square px-0 sm:aspect-auto sm:px-5"
              onClick={() => onOpen(randomName())}
              disabled={busy}
              aria-label="Open a random inbox"
              title="Open a random inbox"
            >
              <Dices aria-hidden />
              <span className="hidden sm:inline">Random</span>
            </Button>
          </div>
        )}
      </div>

      <p
        id={hintId}
        role={problem && value ? "alert" : undefined}
        className={cn(
          "mt-2 min-h-[1.25em] type-footnote",
          large ? "px-5" : "px-3.5",
          problem && value ? "text-danger-text" : "text-label-secondary",
        )}
      >
        {problem && value
          ? problem
          : value && valid
            ? domains.length === 1
              ? `${parsed.local}@${domains[0]}`
              : `${parsed.local} — the domain is assigned when you open it`
            : large
              ? "Letters, digits, dots, dashes and underscores. No sign-up."
              : ""}
      </p>
    </form>
  );
}
