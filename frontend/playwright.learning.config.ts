import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests/learning",
  workers: 1,
  retries: 0,
  timeout: 60000,
  expect: { timeout: 10000 },
  use: {
    baseURL: "http://127.0.0.1:18082",
    browserName: "chromium",
    colorScheme: "dark",
    screenshot: "only-on-failure",
  },
  reporter: [["list"]],
  outputDir: "../test-results-learning",
  webServer: {
    command: "node ../scripts/task.mjs serve-learning-e2e",
    url: "http://127.0.0.1:18082/readyz",
    reuseExistingServer: false,
    timeout: 60000,
  },
});
