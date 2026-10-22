import { writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";

// In development the UI is served by Vite and `/api` is proxied to
// `trains serve`, so the frontend uses the same relative paths as production.
const api = process.env.TRAINS_API ?? "http://localhost:8080";

// `go build` embeds web/dist, and must work before the first UI build, so the
// repo commits dist/.gitkeep. The build empties dist; restore the marker after.
const keepDist: Plugin = {
  name: "keep-dist-gitkeep",
  closeBundle() {
    writeFileSync(resolve(import.meta.dirname, "dist/.gitkeep"), "");
  },
};

export default defineConfig({
  plugins: [react(), keepDist],
  server: {
    proxy: { "/api": { target: api, changeOrigin: true } },
  },
  build: { outDir: "dist", emptyOutDir: true },
});
