import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "../lib/cn";

/*
 * Buttons, as the HIG's Buttons page describes them.
 *
 * - `prominent` is the filled style for the single most likely action on a
 *   screen. There should be one.
 * - `bordered` is the tinted style for secondary actions.
 * - `plain` has no background: text or a symbol in the accent colour.
 * - `glass` is for controls that float in the functional layer — toolbars —
 *   over content, per the Materials page.
 * - `destructive` is filled red, for confirming a destructive choice inside an
 *   alert. It is never the primary role on its own.
 *
 * "Use style — not size — to visually distinguish the preferred choice", so
 * sizes are about the control's context, not its importance. Every variant has
 * a pressed state, which the page requires of custom buttons.
 */
const buttonVariants = cva(
  [
    "relative inline-flex select-none items-center justify-center gap-1.5 whitespace-nowrap rounded-full font-medium",
    "transition-[background-color,transform,opacity] duration-150 ease-out active:scale-[0.97]",
    "disabled:pointer-events-none disabled:opacity-40",
    "[&_svg]:size-[1.1em] [&_svg]:shrink-0",
  ],
  {
    variants: {
      variant: {
        prominent: "bg-tint text-white hover:brightness-110 active:brightness-95",
        bordered: "bg-fill-tertiary text-tint-text hover:bg-fill-secondary active:bg-fill",
        plain: "text-tint-text hover:bg-fill-quaternary active:bg-fill-tertiary",
        glass: "glass text-label hover:brightness-[1.04] active:brightness-95",
        destructive: "bg-danger text-white hover:brightness-110 active:brightness-95",
        neutral: "bg-fill-tertiary text-label hover:bg-fill-secondary active:bg-fill",

        // Names the marketing site and older code use.
        default: "bg-tint text-white hover:brightness-110 active:brightness-95",
        outline: "bg-fill-tertiary text-tint-text hover:bg-fill-secondary active:bg-fill",
        secondary: "bg-fill-tertiary text-label hover:bg-fill-secondary active:bg-fill",
        ghost: "text-label hover:bg-fill-quaternary active:bg-fill-tertiary",
        link: "text-tint-text underline-offset-4 hover:underline active:scale-100",
      },
      size: {
        default: "control-h px-4 type-body",
        small: "control-h-sm px-3 type-subhead",
        large: "min-h-[3.25rem] px-6 type-headline",
        icon: "hit-target p-0",
        "icon-small": "control-h-sm aspect-square p-0",
        sm: "control-h-sm px-3 type-subhead",
        lg: "min-h-[3.25rem] px-6 type-headline",
      },
    },
    defaultVariants: { variant: "prominent", size: "default" },
  },
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, type, ...props }, ref) => {
    const Comp = asChild ? Slot : "button";
    return (
      <Comp
        ref={ref}
        // A button inside a form submits it unless told otherwise, which is
        // almost never what an icon button in a toolbar means.
        type={asChild ? undefined : (type ?? "button")}
        className={cn(buttonVariants({ variant, size, className }))}
        {...props}
      />
    );
  },
);
Button.displayName = "Button";

export { Button, buttonVariants };
