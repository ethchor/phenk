import { Eye } from "lucide-react";

import { cn } from "@phenk/ui";

/**
 * The public-inbox warning.
 *
 * This is required wherever a named inbox is shown, and it is deliberately not
 * a toast: a toast is dismissed and forgotten, and the fact does not stop being
 * true when it disappears. Anyone who guesses the name can read this mailbox,
 * and that is the difference between a feature and a vulnerability.
 */
export function PublicInboxWarning({ className, compact }: { className?: string; compact?: boolean }) {
  return (
    <div
      role="note"
      className={cn(
        "flex items-start gap-2 rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-warning-foreground dark:text-foreground",
        className,
      )}
    >
      <Eye className="mt-0.5 size-4 shrink-0" aria-hidden />
      <p className={cn("text-xs leading-relaxed", compact && "text-[11px]")}>
        <strong className="font-semibold">This inbox is public.</strong>{" "}
        Anyone who guesses the name can read every message in it. Never use it for
        password resets, banking, or anything you would mind a stranger seeing.
      </p>
    </div>
  );
}
