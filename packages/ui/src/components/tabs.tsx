import * as React from "react";
import * as TabsPrimitive from "@radix-ui/react-tabs";

import { cn } from "../lib/cn";

/*
 * A segmented control (Segmented controls page): closely related choices that
 * change a view, shown as one control with the selection visible. It is built
 * on Radix Tabs because that is what it is semantically — it switches between
 * views of the same content — and Tabs brings the arrow-key behaviour and the
 * ARIA roles for free.
 *
 * Labels should be nouns, all text or all symbols, and few.
 */
const Tabs = TabsPrimitive.Root;

const TabsList = React.forwardRef<
  React.ComponentRef<typeof TabsPrimitive.List>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.List>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.List
    ref={ref}
    className={cn("inline-flex items-stretch rounded-full bg-fill-tertiary p-0.5 text-label", className)}
    {...props}
  />
));
TabsList.displayName = TabsPrimitive.List.displayName;

const TabsTrigger = React.forwardRef<
  React.ComponentRef<typeof TabsPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Trigger
    ref={ref}
    className={cn(
      "control-h-sm inline-flex min-w-[5.5rem] flex-1 items-center justify-center rounded-full px-3 type-subhead font-medium",
      "text-label-secondary transition-[background-color,color,box-shadow] duration-150",
      "hover:text-label",
      "data-[state=active]:bg-elevated data-[state=active]:text-label data-[state=active]:shadow-[0_1px_3px_rgb(0_0_0/0.12),0_0_0_0.5px_rgb(0_0_0/0.04)]",
      "disabled:pointer-events-none disabled:opacity-40",
      className,
    )}
    {...props}
  />
));
TabsTrigger.displayName = TabsPrimitive.Trigger.displayName;

const TabsContent = React.forwardRef<
  React.ComponentRef<typeof TabsPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Content>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Content ref={ref} className={cn("focus-visible:outline-none", className)} {...props} />
));
TabsContent.displayName = TabsPrimitive.Content.displayName;

export { Tabs, TabsContent, TabsList, TabsTrigger };
