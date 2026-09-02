import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

/** Merges class names, letting a caller override a component's own utilities. */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
