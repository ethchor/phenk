import type { ReactNode } from "react";
import { cn } from "@phenk/ui";

/**
 * A blank screen that says what to do next (Writing: "provide clear next steps
 * on any blank screens").
 */
export function EmptyState({
  icon,
  title,
  children,
  actions,
  className,
}: {
  icon: ReactNode;
  title: string;
  children?: ReactNode;
  actions?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex flex-col items-center justify-center gap-2 px-6 py-12 text-center", className)}>
      <div className="mb-1 text-label-tertiary [&_svg]:size-11" aria-hidden>
        {icon}
      </div>
      <h2 className="type-title3 text-label">{title}</h2>
      {children && <div className="max-w-[26rem] type-subhead text-label-secondary">{children}</div>}
      {actions && <div className="mt-3 flex flex-wrap items-center justify-center gap-2">{actions}</div>}
    </div>
  );
}
