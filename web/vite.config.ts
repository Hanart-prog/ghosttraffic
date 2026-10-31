import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  // Fonts stay files: the API's content security policy refuses data: URLs.
  build: { assetsInlineLimit: 0 },
  server: {
    host: "127.0.0.1",
    proxy: { "/api": "http://127.0.0.1:7070" },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: "src/setup-tests.ts",
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
