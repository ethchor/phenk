import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Toaster } from "sonner";
import { TooltipProvider } from "@phenk/ui";

import { App } from "./App";
import "./index.css";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Mail arrives over the event stream, so polling would only duplicate
      // work the server is already pushing.
      refetchOnWindowFocus: false,
      staleTime: 30_000,
      retry: 1,
    },
  },
});

const root = document.getElementById("root");
if (!root) throw new Error("no root element");

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={300}>
        <App />
        {/* Brief confirmations, at the bottom where they do not cover the
            toolbar (Feedback: confirm that a significant action completed). */}
        <Toaster
          position="bottom-center"
          offset={96}
          toastOptions={{
            classNames: {
              toast: "!glass !rounded-full !border-0 !text-[var(--label)] !font-sans !py-3 !px-5",
              title: "!type-subhead !font-medium",
            },
          }}
        />
      </TooltipProvider>
    </QueryClientProvider>
  </StrictMode>,
);
