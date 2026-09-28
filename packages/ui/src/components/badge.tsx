import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "../lib/cn";

/*
 * A small capsule label. Colour is never the only signal (Color page): every
 * badge carries words, and the tint only reinforces them.
 */
const badgeVariants = cva(
  "inline-flex items-center gap-1 rounded-full px-2 py-0.5 type-caption font-medium whitespace-nowrap [&_svg]:size-3",
  {
    variants: {
      variant: {
        neutral: "bg-fill-tertiary text-label-secondary",
        accent: "bg-tint-soft text-tint-text",
        success: "bg-[color-mix(in_srgb,var(--system-green)_16%,transparent)] text-ok-text",
        warning: "bg-caution-soft text-caution-text",
        danger: "bg-[color-mix(in_srgb,var(--system-red)_14%,transparent)] text-danger-text",

        // Names older code uses.
        default: "bg-tint-soft text-tint-text",
        secondary: "bg-fill-tertiary text-label-secondary",
        destructive: "bg-[color-mix(in_srgb,var(--system-red)_14%,transparent)] text-danger-text",
        outline: "border border-separator text-label",
      },
    },
    defaultVariants: { variant: "neutral" },
  },
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLSpanElement>, VariantProps<typeof badgeVariants> {}

function Badge({ className, variant, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ variant }), className)} {...props} />;
}

export { Badge, badgeVariants };
