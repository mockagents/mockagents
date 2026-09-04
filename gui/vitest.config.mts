import { fileURLToPath } from "node:url";

import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// UX-08: component-interaction harness for the GUI.
//
// Vitest is already this repo's JavaScript runner (sdk/typescript uses it), so
// the console adopts it rather than introducing a second framework. Tests run
// against the real components — no page-level mocking of the design — with the
// network boundary (lib/api.ts) stubbed per test, because that module reaches
// for next/headers and is server-only by construction.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./", import.meta.url)),
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./test/setup.ts"],
    include: ["test/**/*.test.{ts,tsx}"],
    exclude: ["node_modules/**", ".next/**", "e2e/**"],
    // Components under test are presentational islands; a real browser is the
    // job of the Playwright smoke suite, not of this layer.
    css: false,
    restoreMocks: true,
  },
});
