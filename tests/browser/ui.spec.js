const fs = require("fs");
const { test, expect } = require("@playwright/test");

test("creates and reveals a burn-after-read text drop", async ({ page, browser }) => {
  const apiRequests = [];
  page.on("request", (request) => {
    if (request.url().includes("/api/v1/secrets/")) apiRequests.push(request.url());
  });

  await page.goto("/");
  await page.locator("#secret").fill("browser secret");
  await page.getByRole("button", { name: "Create encrypted link" }).click();

  const link = page.locator("input[aria-label='Share link']");
  await expect(link).toHaveValue(/#.+/);
  await expect(page.locator(".keep-meta")).toContainText(/Unused after/);
  const shareURL = await link.inputValue();

  const recipient = await browser.newPage();
  await recipient.goto(shareURL);
  await recipient.getByRole("button", { name: "Open encrypted drop" }).click();
  await expect(recipient.locator("#revealed-secret")).toHaveText("browser secret");
  await expect(recipient.locator("#revealed-secret")).toHaveClass(/privacy-mode/);
  expect(apiRequests.every((url) => !url.includes("#"))).toBeTruthy();

  const secondRecipient = await browser.newPage();
  await secondRecipient.goto(shareURL);
  await secondRecipient.getByRole("button", { name: "Open encrypted drop" }).click();
  await expect(secondRecipient.locator("#reveal-result")).toHaveText("Drop not found or already burned.");
});

test("creates a file drop and downloads the decrypted file", async ({ page, browser }) => {
  await page.goto("/");
  await page.locator("#file").setInputFiles({
    name: "notes.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("file secret")
  });
  await page.getByRole("button", { name: "Create encrypted link" }).click();
  const shareURL = await page.locator("input[aria-label='Share link']").inputValue();

  const recipient = await browser.newPage();
  await recipient.goto(shareURL);
  await recipient.getByRole("button", { name: "Open encrypted drop" }).click();
  const download = recipient.waitForEvent("download");
  await recipient.getByRole("link", { name: "Download notes.txt" }).click();
  const file = await download;
  expect(await file.suggestedFilename()).toBe("notes.txt");
  expect(fs.readFileSync(await file.path(), "utf8")).toBe("file secret");
});
