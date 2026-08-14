const { defineConfig } = require("@playwright/test");

const baseURL = process.env.SMOKE_URL;
if (!baseURL) {
  throw new Error("SMOKE_URL is required for the live smoke (e.g. https://drop.donkeyx.dev)");
}

module.exports = defineConfig({
  testDir: "./tests/browser",
  testMatch: "smoke.spec.js",
  timeout: 60_000,
  expect: { timeout: 15_000 },
  retries: 1,
  use: {
    baseURL,
    browserName: "chromium",
    headless: true,
    trace: "retain-on-failure",
  },
});
