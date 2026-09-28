import { cn } from "../lib/cn";

/**
 * A loading placeholder shaped like the thing it stands in for (Loading page:
 * "show something as soon as possible"). The pulse stops under Reduce Motion
 * through the theme's global rule.
 */
function Skeleton({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div aria-hidden className={cn("animate-pulse rounded-md bg-fill-tertiary", className)} {...props} />
  );
}

export { Skeleton };
