import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  // The build output is embedded into the Go binary, so it lands where
  // internal/web/embed.go expects it rather than in a directory of its own.
  build: {
    outDir: "../internal/web/dist",
    emptyOutDir: true,
    sourcemap: false,
  },
  server: {
    // In development the SPA runs on Vite and the API on the Go binary, so
    // everything the API owns is proxied through. Cookies then come from one
    // origin, exactly as they do in production.
    proxy: {
      "/v1": { target: "http://localhost:8080", changeOrigin: false },
      "/i": { target: "http://localhost:8080", changeOrigin: false },
      "/healthz": { target: "http://localhost:8080", changeOrigin: false },
    },
  },
});
