import { expect, test } from "@playwright/test";
import { mkdir } from "node:fs/promises";

const captures = "../docs/evidence/screenshots/production";

test("restricted storage leaves a usable join screen with actionable recovery", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.setViewportSize({ width: 375, height: 812 });
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  await page.addInitScript(() => {
    Object.defineProperty(window, "sessionStorage", {
      get() {
        throw new DOMException("Storage disabled", "SecurityError");
      },
    });
  });
  await page.goto("/");
  await page.getByLabel("Your display name").fill("Storage check");
  const join = page.getByRole("button", { name: "Join the quiz", exact: true });
  await expect(join).toBeEnabled();
  await join.click();
  await expect(page.getByText(/Browser storage is unavailable/)).toBeVisible();
  await expect(join).toBeEnabled();
  expect(errors).toEqual([]);
  await mkdir(captures, { recursive: true });
  await page.screenshot({
    path: `${captures}/storage-unavailable-375.png`,
    fullPage: true,
  });
});

test("quiz links take precedence over saved navigation", async ({ page }) => {
  await page.addInitScript(() =>
    sessionStorage.setItem(
      "vocabulary.last",
      JSON.stringify({ quizId: "VOCAB-DEMO", displayName: "Previous player" }),
    ),
  );
  await page.goto("/?quiz=TRAVEL-DEMO");
  await expect(page.getByLabel("Quiz code", { exact: true })).toHaveValue(
    "TRAVEL-DEMO",
  );
  await expect(page.locator(".quiz-preview h3")).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Join a practice" }),
  ).toBeVisible();
});

test("learner loads only its app and recovers from a failed app chunk", async ({
  page,
  request,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const scripts: string[] = [];
  page.on("request", (r) => {
    if (r.resourceType() === "script") scripts.push(r.url());
  });
  await page.route("**/assets/App-*.js", (route) => route.abort("failed"));
  await page.goto("/");
  await expect(page.getByText(/Vocabulary Live could not load/)).toBeVisible();
  await mkdir(captures, { recursive: true });
  await page.screenshot({
    path: `${captures}/load-error-1440.png`,
    fullPage: true,
  });
  await page.unroute("**/assets/App-*.js");
  await page.getByRole("button", { name: "Try again", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Join a practice" }),
  ).toBeVisible();
  expect(scripts.some((url) => /\/InstructorApp-/.test(url))).toBe(false);
  const asset = scripts.find((url) => /\/assets\/App-/.test(url));
  expect(asset).toBeTruthy();
  const response = await request.get(asset!);
  expect(response.headers()["cache-control"]).toContain("immutable");
  expect((await request.get("/assets/missing-build.js")).status()).toBe(404);
  expect((await request.get("/")).headers()["cache-control"]).toBe("no-cache");
});
