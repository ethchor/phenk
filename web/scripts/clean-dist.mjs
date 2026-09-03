// Removes the previous build's output from the embed directory.
//
// Vite's own emptyOutDir would do this, but it empties the directory
// completely, including the placeholder that keeps `internal/web/dist` present
// in a fresh checkout. Without that placeholder the Go package's go:embed
// pattern matches nothing and the whole module fails to compile — which is
// invisible locally, because a developer's working tree always has a build in
// it, and fatal in CI, which does not.
//
// So the cleanup is done here instead, and it removes only what the build
// produces.
import { rm } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const dist = path.resolve(here, "..", "..", "internal", "web", "dist");

for (const entry of ["assets", "index.html"]) {
  await rm(path.join(dist, entry), { recursive: true, force: true });
}
