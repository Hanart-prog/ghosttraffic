import { defineConfig } from "@playwright/test";

// Uses the installed Chrome and the real server over real runs from make demo.
export default defineConfig({
  testDir: "e2e",
  fullyParallel: true,
  reporter: "line",
  use: { baseURL: "http://127.0.0.1:7079", channel: "chrome" },
  projects: [
    { name: "dark", use: { colorScheme: "dark" } },
    { name: "light", use: { colorScheme: "light" } },
  ],
  webServer: {
    command: "../bin/ghosttraffic serve --listen 127.0.0.1:7079 --runs-dir ../runs --web-dir dist",
    url: "http://127.0.0.1:7079/health",
    reuseExistingServer: false,
  },
});
