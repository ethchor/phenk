import { Globe } from "lucide-react";
import { cn } from "@phenk/ui";

import { retentionLabel } from "../lib/format";

/**
 * The one thing about a public inbox that must never be missed: anyone who
 * knows the name can read it.
 *
 * Said once, plainly, where the name is chosen and at the top of the inbox —
 * not buried, and not shouted (Design principles: "be fully transparent about
 * what your product does and why"; Writing: "match your tone to the
 * context"). An icon and words carry it, never colour alone.
 */
export function PublicNotice({ retentionHours, className }: { retentionHours: number; className?: string }) {
  return (
    <p className={cn("flex items-start gap-2 type-footnote text-label-secondary", className)}>
      <Globe className="mt-[0.15em] size-[1.1em] shrink-0" aria-hidden strokeWidth={1.75} />
      <span>
        Public inbox. Anyone who knows the name can read it. Messages are deleted after{" "}
        {retentionLabel(retentionHours)}.
      </span>
    </p>
  );
}
