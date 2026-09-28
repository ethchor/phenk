import * as React from "react";

import { cn } from "../lib/cn";

/*
 * A text field (Text fields page): a filled field with no border, the system's
 * tertiary fill as its background, and a hint — the placeholder — that says
 * what it is for. Focus shows the accent ring from the theme.
 */
const Input = React.forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement>>(
  ({ className, type = "text", ...props }, ref) => (
    <input
      ref={ref}
      type={type}
      className={cn(
        "control-h w-full min-w-0 rounded-[0.625rem] bg-fill-tertiary px-3 type-body text-label",
        "placeholder:text-label-tertiary",
        "transition-[background-color,box-shadow] duration-150",
        "focus-visible:bg-content focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-[color-mix(in_srgb,var(--system-blue)_45%,transparent)]",
        "disabled:opacity-50",
        className,
      )}
      {...props}
    />
  ),
);
Input.displayName = "Input";

export { Input };
