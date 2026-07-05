import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    // Plain vitest (no Workers pool) — the pure functions in lib.ts
    // use only standard Web APIs (atob, btoa, crypto.subtle, TextEncoder)
    // which are available in the Node.js 20+ test environment.
    environment: "node",
    globals: false,
    include: ["src/**/*.test.ts"],
  },
});
