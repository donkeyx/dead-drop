const { test, expect } = require("@playwright/test");

test("live create, reveal, and burn", async ({ page, browser }) => {
  const secret = `deploy-smoke-${Date.now()}`;

  await page.goto("/");
  await page.locator("#secret").fill(secret);
  await page.getByRole("button", { name: "Create encrypted link" }).click();

  const link = page.locator("input[aria-label='Share link']");
  await expect(link).toHaveValue(/#./);
  const shareURL = await link.inputValue();
  expect(new URL(shareURL).hash.length).toBeGreaterThan(1);

  const recipient = await browser.newPage();
  await recipient.goto(shareURL);
  await recipient.getByRole("button", { name: "Open encrypted drop" }).click();
  await expect(recipient.locator("#reveal-result")).toHaveText(secret);

  const second = await browser.newPage();
  await second.goto(shareURL);
  await second.getByRole("button", { name: "Open encrypted drop" }).click();
  await expect(second.locator("#reveal-result")).toHaveText("Drop not found or already burned.");
});
