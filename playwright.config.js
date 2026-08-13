const { defineConfig } = require("@playwright/test");

module.exports = defineConfig({
  testDir: "./tests/browser",
  timeout: 30_000,
  expect: { timeout: 5_000 },
  use: {
    baseURL: "http://127.0.0.1:18080",
    browserName: "chromium",
    headless: true,
    trace: "retain-on-failure"
  },
  webServer: {
    command: "go run ./cmd/dead-drop serve -addr 127.0.0.1:18080 -data /tmp/dead-drop-playwright-data -static ./web/static",
    url: "http://127.0.0.1:18080/readyz",
    reuseExistingServer: false,
    timeout: 120_000
  }
});
