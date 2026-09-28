import { useEffect, useState, useSyncExternalStore } from "react";
import { useQuery } from "@tanstack/react-query";

import { api } from "./api";

/** What this server offers. Read once; it changes only when an operator reconfigures. */
export function useMeta() {
  return useQuery({
    queryKey: ["meta"],
    queryFn: () => api.getMeta(),
    staleTime: Infinity,
    retry: 2,
  });
}

/**
 * Layout by available width, not by device (Layout: "determine layout based
 * on size classes, not device type or orientation").
 *
 *   compact   one column at a time
 *   regular   message list and message side by side
 *   wide      sidebar, list and message
 */
export type SizeClass = "compact" | "regular" | "wide";

const REGULAR = "(min-width: 760px)";
const WIDE = "(min-width: 1120px)";

function subscribeToWidth(onChange: () => void): () => void {
  const queries = [window.matchMedia(REGULAR), window.matchMedia(WIDE)];
  queries.forEach((q) => q.addEventListener("change", onChange));
  return () => queries.forEach((q) => q.removeEventListener("change", onChange));
}

function currentSizeClass(): SizeClass {
  if (window.matchMedia(WIDE).matches) return "wide";
  if (window.matchMedia(REGULAR).matches) return "regular";
  return "compact";
}

export function useSizeClass(): SizeClass {
  return useSyncExternalStore(subscribeToWidth, currentSizeClass, () => "wide" as SizeClass);
}

/** A clock that ticks only while there is something to count down to. */
export function useNow(active: boolean, intervalMs = 1000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    const timer = window.setInterval(() => setNow(Date.now()), intervalMs);
    return () => window.clearInterval(timer);
  }, [active, intervalMs]);
  return now;
}

/** Sets the document title. The window is titled by what it shows, never the app name alone (Toolbars). */
export function useDocumentTitle(title: string): void {
  useEffect(() => {
    document.title = title;
  }, [title]);
}
