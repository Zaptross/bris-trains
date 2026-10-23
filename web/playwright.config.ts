import { defineConfig, devices } from "@playwright/test";

// Smoke tests drive a running `trains serve`; they don't start one. CI starts
// it against the committed fixtures (pinned clock) or the live Translink feeds.
//
//   SMOKE_BASE_URL  server to test (default http://localhost:8080)
//   SMOKE_NOW       RFC3339 instant to pin the browser clock to; set it to the
//                   server's TRAINS_NOW so countdowns agree with the data
export default defineConfig({
  testDir: "smoke",
  timeout: 30_000,
  expect: { timeout: 15_000 },
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: process.env.SMOKE_BASE_URL ?? "http://localhost:8080",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    { name: "mobile", use: { ...devices["Pixel 7"] } },
    { name: "desktop", use: { ...devices["Desktop Chrome"] } },
  ],
});
