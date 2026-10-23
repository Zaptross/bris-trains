import { expect, test, type Page } from "@playwright/test";

// One smoke test per page of the production build. Assertions are about the
// page working, not about particular trains, so they hold against both the
// fixture capture and the live feeds.

const now = process.env.SMOKE_NOW;

test.beforeEach(async ({ page }) => {
  if (now) await page.clock.setFixedTime(new Date(now));
  page.on("pageerror", (err) => {
    throw new Error(`uncaught page error: ${err.message}`);
  });
});

async function pick(page: Page, label: string, query: string, name: string) {
  const box = page.getByRole("combobox", { name: label });
  await box.fill(query);
  await page.getByRole("option", { name: new RegExp(name) }).first().click();
  await expect(box).toHaveValue(name);
}

async function searchCentralToRoma(page: Page) {
  await page.goto("/");
  await pick(page, "From", "central", "Central station");
  await pick(page, "To", "roma", "Roma Street station");
  const rows = page.getByRole("list", { name: "Departures" }).getByRole("button");
  await expect(rows.first()).toBeVisible();
  return rows;
}

test("departures board lists journeys between two stations", async ({ page }) => {
  const rows = await searchCentralToRoma(page);
  expect(await rows.count()).toBeGreaterThan(0);
  await expect(rows.first()).toContainText(/\d\d:\d\d/);
  await expect(rows.first()).toContainText(/Platform/);
  await expect(page.getByText("Data © Translink")).toBeVisible();
});

test("trip page shows the ride's stops and the line diagram", async ({ page }) => {
  const rows = await searchCentralToRoma(page);
  await rows.first().click();
  await expect(page).toHaveURL(/\/trip\//);

  const stops = page.getByRole("list", { name: "Stops" });
  await expect(stops).toBeVisible();
  await expect(stops.getByText("board", { exact: true })).toBeVisible();
  await expect(stops.getByText("alight", { exact: true })).toBeVisible();
  await expect(stops.getByText(/Central station/)).toBeVisible();
  await expect(page.getByRole("img", { name: /Route diagram/ })).toBeVisible();

  // A deep link survives a reload (server-side SPA fallback).
  await page.reload();
  await expect(page.getByRole("list", { name: "Stops" })).toBeVisible();
});

test("unknown paths: server 404s, the app shows its not-found page", async ({ page }) => {
  // Loaded directly, an unknown path is a plain 404 from the server (only /
  // and /trip/* fall back to the app).
  const res = await page.request.get("/no-such-page");
  expect(res.status()).toBe(404);

  // Reached inside the app, the router renders its not-found page.
  await page.goto("/");
  await page.evaluate(() => {
    history.pushState({}, "", "/no-such-page");
    dispatchEvent(new PopStateEvent("popstate"));
  });
  await expect(page.getByText("Nothing here.")).toBeVisible();
  await page.getByRole("link", { name: "← Departures" }).click();
  await expect(page.getByRole("combobox", { name: "From" })).toBeVisible();
});
