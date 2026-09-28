import { cn } from "../lib/cn";

/** A hairline separator in the system separator colour. */
function Separator({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div role="separator" className={cn("h-px w-full bg-separator", className)} {...props} />;
}

export { Separator };
