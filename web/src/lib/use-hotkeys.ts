import { useEffect, useRef } from "react";

/*
 * Single-key shortcuts for the most frequent commands (Keyboards page).
 *
 * The page's rules, applied: standard shortcuts are never repurposed — any
 * key pressed with Command, Control or Option passes straight through, so ⌘R
 * stays the browser's reload — and custom shortcuts exist only for the
 * commands people repeat. Shortcuts never fire while someone is typing, except
 * Escape, which leaves the field.
 */

export type HotkeyMap = Partial<Record<string, (event: KeyboardEvent) => void>>;

export function useHotkeys(bindings: HotkeyMap, enabled = true): void {
  const latest = useRef(bindings);
  latest.current = bindings;

  useEffect(() => {
    if (!enabled) return;

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.metaKey || event.ctrlKey || event.altKey || event.isComposing) return;

      const typing = isTypingTarget(event.target);
      if (typing && event.key !== "Escape") return;

      const handler = latest.current[event.key];
      if (!handler) return;
      event.preventDefault();
      handler(event);
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [enabled]);
}

function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  const tag = target.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT";
}
