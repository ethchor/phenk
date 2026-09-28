import { Badge, Tooltip, TooltipContent, TooltipTrigger } from "@phenk/ui";

import type { AuthResult, AuthResults } from "../lib/api";

/**
 * The three mail authentication results.
 *
 * `none` is shown neutral rather than green. It means the check was not
 * evaluated, or the sender published nothing to check against — not that the
 * message passed. Colouring it like a pass would be a lie the reader would act
 * on. Every badge says its result in words; colour only reinforces it.
 */
export function AuthBadges({ auth }: { auth: AuthResults }) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <AuthBadge label="SPF" result={auth.spf} explanation={SPF_EXPLANATION} />
      <AuthBadge label="DKIM" result={auth.dkim} explanation={DKIM_EXPLANATION} />
      <AuthBadge label="DMARC" result={auth.dmarc} explanation={DMARC_EXPLANATION} />
    </div>
  );
}

const SPF_EXPLANATION =
  "Whether the server that delivered this message is one the sender’s domain permits to send for it.";
const DKIM_EXPLANATION =
  "Whether the message carries a signature that matches the sending domain’s published key, proving it was not altered in transit.";
const DMARC_EXPLANATION =
  "Whether SPF or DKIM passed for the same domain the message claims to be from. This is the check that actually resists forgery.";

function AuthBadge({
  label,
  result,
  explanation,
}: {
  label: string;
  result: AuthResult;
  explanation: string;
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button type="button" className="rounded-full focus-visible:outline-offset-1">
          <Badge variant={variantFor(result)} className="cursor-help">
            {label} {result}
          </Badge>
        </button>
      </TooltipTrigger>
      <TooltipContent>
        <p className="font-semibold">{label}</p>
        <p className="mt-1">{explanation}</p>
        <p className="mt-1 text-label-secondary">{meaningOf(result)}</p>
      </TooltipContent>
    </Tooltip>
  );
}

function variantFor(result: AuthResult) {
  switch (result) {
    case "pass":
      return "success" as const;
    case "fail":
    case "permerror":
      return "danger" as const;
    case "softfail":
    case "temperror":
      return "warning" as const;
    default:
      return "neutral" as const;
  }
}

function meaningOf(result: AuthResult): string {
  switch (result) {
    case "pass":
      return "This check passed.";
    case "fail":
      return "This check failed. Treat the sender’s identity as unproven.";
    case "softfail":
      return "The sender’s domain suspects this is not authorized but did not insist.";
    case "neutral":
      return "The sender’s domain takes no position.";
    case "temperror":
      return "The check could not be completed. It says nothing either way.";
    case "permerror":
      return "The sender’s published policy is broken.";
    default:
      return "Not evaluated, or nothing was published to check against. This is not a pass.";
  }
}
