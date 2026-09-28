import { useEffect, useRef } from "react";

import { Home } from "./components/Home";
import { InboxScreen } from "./components/InboxScreen";
import { LaunchScreen, ServerUnavailable } from "./components/StatusScreens";
import { useMeta } from "./lib/hooks";
import { navigate, parseRoute, useRoute } from "./lib/router";
import { lastRoute } from "./lib/storage";

/**
 * The app: a home screen where any name opens an inbox, and the inbox itself.
 *
 * Launched as an installed app, it returns to the inbox that was open last
 * time (Launching: "restore the previous state"). In a browser tab it does
 * not: a visit to the root is how someone starts something new, the name field
 * is the product, and the home screen lists recent inboxes so continuing is
 * still one tap. A link to a specific inbox is always honoured either way.
 */
export function App() {
  const route = useRoute();
  const meta = useMeta();
  const restored = useRef(false);

  useEffect(() => {
    if (restored.current) return;
    restored.current = true;

    const here = new URL(window.location.href);
    if (here.searchParams.has("login")) {
      // YOPmail's link shape. Rewrite it to the canonical path.
      navigate(parseRoute(here), { replace: true });
      return;
    }
    const installed = window.matchMedia("(display-mode: standalone)").matches;
    if (installed && here.pathname === "/" && here.search === "") {
      const last = lastRoute();
      if (last && last !== "/")
        navigate(parseRoute(new URL(last, window.location.origin)), { replace: true });
    }
  }, []);

  if (meta.isPending) return <LaunchScreen />;
  if (meta.error || !meta.data) return <ServerUnavailable />;

  switch (route.kind) {
    case "home":
      return <Home meta={meta.data} />;
    case "public":
    case "private":
      // Keyed by inbox, so switching inboxes starts from a clean slate rather
      // than carrying one inbox's filter and selection into the next.
      return (
        <InboxScreen
          key={route.kind === "public" ? `public:${route.name}` : `private:${route.id}`}
          meta={meta.data}
          route={route}
        />
      );
  }
}
