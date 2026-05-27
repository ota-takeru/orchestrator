import { defineConfig, devices } from "@playwright/test";

const port = 8767;

export default defineConfig({
  testDir: "./e2e",
  outputDir: "./test-results",
  reporter: [["html", { open: "never" }], ["list"]],
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    trace: "on-first-retry",
    screenshot: "only-on-failure"
  },
  projects: [
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1365, height: 900 }
      }
    }
  ],
  webServer: {
    command: `go run ../cmd/devos serve --project-root .. --ui --ui-dir dist --addr 127.0.0.1:${port}`,
    url: `http://127.0.0.1:${port}`,
    reuseExistingServer: !process.env.CI,
    timeout: 120_000
  }
});
