import { defineConfig } from "@playwright/test";
const baseURL = `http://127.0.0.1:${process.env.E2E_PORT ?? "8080"}`;
export default defineConfig({
  testDir: "./tests/e2e",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 60000,
  expect: { timeout: 10000 },
  use: {
    baseURL,
    browserName: "chromium",
    // Specs assert dark-theme copy; light-theme.spec.ts overrides this.
    colorScheme: "dark",
    screenshot: "only-on-failure",
    trace: "off",
  },
  reporter: [["list"]],
  outputDir: "../test-results",
  webServer: {
    command: "node ../scripts/task.mjs serve-e2e",
    url: `${baseURL}/readyz`,
    reuseExistingServer: false,
    timeout: 30000,
  },
});
