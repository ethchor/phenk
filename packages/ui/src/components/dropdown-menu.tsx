import * as React from "react";
import * as DropdownMenuPrimitive from "@radix-ui/react-dropdown-menu";

import { cn } from "../lib/cn";

/*
 * Menus and pull-down buttons (Menus, Pull-down buttons pages). Menus belong
 * to the functional layer, so they are glass. Items use title-style
 * capitalization, icons sparingly and with purpose, and an ellipsis when the
 * action needs more input before it can finish. A destructive item is red.
 */
const DropdownMenu = DropdownMenuPrimitive.Root;
const DropdownMenuTrigger = DropdownMenuPrimitive.Trigger;
const DropdownMenuGroup = DropdownMenuPrimitive.Group;

const DropdownMenuContent = React.forwardRef<
  React.ComponentRef<typeof DropdownMenuPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Content>
>(({ className, sideOffset = 6, ...props }, ref) => (
  <DropdownMenuPrimitive.Portal>
    <DropdownMenuPrimitive.Content
      ref={ref}
      sideOffset={sideOffset}
      className={cn(
        "glass z-50 min-w-[13rem] overflow-hidden rounded-[var(--radius-menu)] p-1.5 text-label",
        "data-[state=open]:animate-[phenk-pop-in_140ms_ease-out]",
        className,
      )}
      {...props}
    />
  </DropdownMenuPrimitive.Portal>
));
DropdownMenuContent.displayName = DropdownMenuPrimitive.Content.displayName;

const DropdownMenuItem = React.forwardRef<
  React.ComponentRef<typeof DropdownMenuPrimitive.Item>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Item> & { destructive?: boolean }
>(({ className, destructive, ...props }, ref) => (
  <DropdownMenuPrimitive.Item
    ref={ref}
    className={cn(
      "relative flex min-h-9 cursor-default select-none items-center gap-2.5 rounded-[var(--radius-menu-item)] px-2.5 type-body outline-none",
      "[&_svg]:size-4 [&_svg]:shrink-0 [&_svg]:text-label-secondary",
      "data-[highlighted]:bg-tint data-[highlighted]:text-white data-[highlighted]:[&_svg]:text-white",
      "data-[disabled]:pointer-events-none data-[disabled]:opacity-40",
      destructive &&
        "text-danger-text [&_svg]:text-danger-text data-[highlighted]:bg-danger data-[highlighted]:text-white",
      className,
    )}
    {...props}
  />
));
DropdownMenuItem.displayName = DropdownMenuPrimitive.Item.displayName;

const DropdownMenuLabel = React.forwardRef<
  React.ComponentRef<typeof DropdownMenuPrimitive.Label>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Label>
>(({ className, ...props }, ref) => (
  <DropdownMenuPrimitive.Label
    ref={ref}
    className={cn("px-2.5 pb-1 pt-1.5 type-footnote text-label-secondary", className)}
    {...props}
  />
));
DropdownMenuLabel.displayName = DropdownMenuPrimitive.Label.displayName;

const DropdownMenuSeparator = React.forwardRef<
  React.ComponentRef<typeof DropdownMenuPrimitive.Separator>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Separator>
>(({ className, ...props }, ref) => (
  <DropdownMenuPrimitive.Separator
    ref={ref}
    className={cn("mx-2 my-1 h-px bg-separator", className)}
    {...props}
  />
));
DropdownMenuSeparator.displayName = DropdownMenuPrimitive.Separator.displayName;

export {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
};
