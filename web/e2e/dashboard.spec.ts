import { expect, test as base } from "@playwright/test";

const run = "/runs/regression-candidate";
const endpoint = `${run}/endpoints/${encodeURIComponent("GET /api/products")}`;

// Every page must load cleanly and stay on this machine.
const test = base.extend<{ problems: string[] }>({
  problems: [
    async ({ page, baseURL }, use) => {
      const problems: string[] = [];
      page.on("console", (m) => m.type() === "error" && problems.push(m.text()));
      page.on("pageerror", (e) => problems.push(e.message));
      page.on("request", (r) => {
        if (!r.url().startsWith(baseURL!)) problems.push(`external request: ${r.url()}`);
      });
      await use(problems);
      expect(problems).toEqual([]);
    },
    { auto: true },
  ],
});

test("overview separates runs by verdict", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Runs with a verdict" })).toBeVisible();
  const card = (name: string) => page.locator("article", { has: page.getByRole("link", { name, exact: true }) });
  await expect(card("regression-candidate")).toContainText("Confirmed regression");
  await expect(card("regression-twin")).toContainText("No regression");
  await expect(card("regression-fixed")).toContainText("No regression");
});

test("a run names the endpoint behind its verdict", async ({ page }) => {
  await page.goto("/");
  await page.locator("article h3 a", { hasText: "regression-candidate" }).click();
  await expect(page.getByRole("heading", { level: 1 })).toContainText("Confirmed regression");
  await expect(page.getByRole("heading", { level: 1 })).toContainText("GET /api/products");
  await expect(page.getByRole("img", { name: /p95 response time/ })).toBeVisible();
});

test("a clean run says so", async ({ page }) => {
  await page.goto("/runs/regression-twin");
  await expect(page.getByRole("heading", { level: 1 })).toContainText("No regression");
});

test("uncertainty shows one dot per trial for each target", async ({ page }) => {
  await page.goto(endpoint);
  await expect(page.getByRole("img", { name: /^Baseline trial \d/ })).toHaveCount(5);
  await expect(page.getByRole("img", { name: /^Candidate trial \d/ })).toHaveCount(5);
  await expect(page.getByText("5 of 5")).toBeVisible();
});

test("endpoint table sorts and links through to a request", async ({ page }) => {
  await page.goto(`${run}/endpoints`);
  await page.getByRole("button", { name: /^Candidate p95/ }).click();
  await expect(page.getByRole("columnheader", { name: /Candidate p95/ })).toHaveAttribute("aria-sort", "descending");
  await page.getByRole("link", { name: "GET /api/products", exact: true }).click();
  await page.getByRole("link", { name: "req_000001" }).first().click();
  await expect(page.getByRole("heading", { name: "Baseline" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Candidate" })).toBeVisible();
});

test("timeline pages through a pass", async ({ page }) => {
  await page.goto(`${run}/timeline`);
  await expect(page.getByText("Showing 1 to 40 of 901")).toBeVisible();
  await page.getByRole("button", { name: "Next" }).click();
  await expect(page.getByText("Showing 41 to 80 of 901")).toBeVisible();
  await page.getByLabel("Target").selectOption("candidate");
  await expect(page.getByText("Showing 1 to 40 of 901")).toBeVisible();
});

test("keyboard reaches a run with a visible focus ring", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("article").first()).toBeVisible();
  const focused = page.locator("article h3 a:focus");
  for (let i = 0; i < 6 && (await focused.count()) === 0; i++) await page.keyboard.press("Tab");
  await expect(focused).toHaveCount(1);
  await expect(focused).toHaveCSS("outline-style", "solid");
});

test("an unknown page explains itself", async ({ page }) => {
  await page.goto("/nowhere");
  await expect(page.getByText("That page doesn't exist.")).toBeVisible();
});

test("a missing run explains itself", async ({ page, problems }) => {
  await page.goto("/runs/no-such-run");
  await expect(page.getByRole("alert")).toContainText("didn't load");
  // The browser logs the 404 itself; only that one is expected here.
  expect(problems.every((p) => p.includes("404"))).toBe(true);
  problems.length = 0;
});

test("the theme switch overrides the system and survives a reload", async ({ page }) => {
  await page.goto("/");
  const before = await page.evaluate(() => getComputedStyle(document.body).backgroundColor);
  await page.locator(".theme-switch").click();
  const after = await page.evaluate(() => getComputedStyle(document.body).backgroundColor);
  expect(after).not.toBe(before);
  await page.reload();
  await expect(page.locator("body")).toHaveCSS("background-color", after);
});

for (const path of ["/", run, endpoint, `${run}/endpoints`, `${run}/timeline`]) {
  test(`no sideways scroll on a phone: ${path}`, async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto(path);
    await page.waitForLoadState("networkidle");
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow).toBeLessThanOrEqual(0);
  });
}

test("screenshots for the README", async ({ page }, info) => {
  test.skip(!process.env.SCREENSHOTS, "only when refreshing docs/media");
  await page.setViewportSize({ width: 1280, height: 860 });
  const scheme = info.project.name;
  for (const [name, path] of [["overview", "/"], ["run", run], ["endpoint", endpoint]] as const) {
    await page.goto(path);
    await page.waitForLoadState("networkidle");
    await page.screenshot({ path: `../docs/media/${name}-${scheme}.png` });
  }
});
