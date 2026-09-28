import { useSyncExternalStore } from "react";

/*
 * Routing, in the few lines it needs.
 *
 *   /                      the home screen: type a name
 *   /inbox/NAME            a public inbox
 *   /inbox/NAME?m=ID       …with a message open
 *   /private/ID            a private address, when the server offers them
 *   /?login=NAME           YOPmail's link shape, redirected to /inbox/NAME so
 *                          links people already have keep working
 *
 * Every inbox has a URL, so it can be bookmarked and shared, and opening a
 * message pushes a history entry on a narrow screen so the system Back gesture
 * returns to the list (Toolbars: "use the standard Back and Close buttons").
 */

export type Route =
  | { kind: "home" }
  | { kind: "public"; name: string; messageId: string | null }
  | { kind: "private"; id: string; messageId: string | null };

const NAVIGATE = "phenk:navigate";

export function parseRoute(url: URL): Route {
  const login = url.searchParams.get("login");
  if (url.pathname === "/" && login) {
    return { kind: "public", name: login, messageId: null };
  }

  const messageId = url.searchParams.get("m");
  const parts = url.pathname.split("/").filter(Boolean);
  if (parts[0] === "inbox" && parts[1]) {
    return { kind: "public", name: safeDecode(parts[1]), messageId };
  }
  if (parts[0] === "private" && parts[1]) {
    return { kind: "private", id: safeDecode(parts[1]), messageId };
  }
  return { kind: "home" };
}

export function routePath(route: Route): string {
  switch (route.kind) {
    case "home":
      return "/";
    case "public":
      return `/inbox/${encodeURIComponent(route.name)}${route.messageId ? `?m=${route.messageId}` : ""}`;
    case "private":
      return `/private/${encodeURIComponent(route.id)}${route.messageId ? `?m=${route.messageId}` : ""}`;
  }
}

export function navigate(route: Route, options: { replace?: boolean } = {}): void {
  const path = routePath(route);
  if (path === window.location.pathname + window.location.search) return;
  if (options.replace) {
    window.history.replaceState(null, "", path);
  } else {
    window.history.pushState(null, "", path);
  }
  window.dispatchEvent(new Event(NAVIGATE));
}

function subscribe(onChange: () => void): () => void {
  window.addEventListener("popstate", onChange);
  window.addEventListener(NAVIGATE, onChange);
  return () => {
    window.removeEventListener("popstate", onChange);
    window.removeEventListener(NAVIGATE, onChange);
  };
}

// The snapshot is the URL string; a new Route object on every render would
// make useSyncExternalStore loop.
function snapshot(): string {
  return window.location.href;
}

export function useRoute(): Route {
  const href = useSyncExternalStore(subscribe, snapshot);
  return parseRoute(new URL(href));
}

function safeDecode(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}
