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
      <TooltipProvider delayDuration={200}>
        <App />
        <Toaster position="bottom-center" richColors closeButton />
      </TooltipProvider>
    </QueryClientProvider>
  </StrictMode>,
);
