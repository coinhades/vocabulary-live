import { test, expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import { parseState } from "../../src/types/protocol";

test.use({ colorScheme: "light" });

// Test-owned grading fixture, never imported by the application bundle.
const correctOptions = ["b", "a", "c", "d", "a", "c", "b", "d"];
const folder = "../docs/evidence/screenshots/light-theme";
function correctOption(questionId: string) {
  return correctOptions[Number(questionId.split("-q")[1]) - 1];
}
async function current(page: Page) {
  const response = await page.request.get("/api/quizzes/VOCAB-DEMO/state");
  expect(response.ok()).toBe(true);
  return parseState(await response.json());
}
async function capture(page: Page, name: string) {
  await page.evaluate(() => document.fonts.ready);
  await expect(
    page.locator(
      '.page-stage [class*="-enter-active"], .page-stage [class*="-leave-active"]',
    ),
  ).toHaveCount(0);
  // Resizing crosses the mobile tab breakpoint; wait for the settled tab.
  if (
    page.viewportSize()!.width <= 760 &&
    (await page.locator(".quiz-layout").count())
  )
    await expect(page.locator("#standings-view")).not.toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.screenshot({
    path: `${folder}/${name}.png`,
    fullPage: true,
    animations: "disabled",
  });
}
async function captureBoth(page: Page, name: string) {
  for (const width of [1440, 375]) {
    await page.setViewportSize({ width, height: width === 375 ? 812 : 900 });
    await capture(page, `${width === 375 ? "mobile" : "desktop"}-${name}`);
  }
  await page.setViewportSize({ width: 1440, height: 900 });
}
async function answer(page: Page, correct: boolean) {
  const question = (await current(page)).currentQuestion!;
  const option = correct
    ? correctOption(question.id)
    : question.options.find((o) => o.id !== correctOption(question.id))!.id;
  await page
    .getByRole("radio")
    .and(page.locator(`input[value="${option}"]`))
    .check();
  await page.getByRole("button", { name: "Check answer", exact: true }).click();
  await expect(
    page.getByRole("button", { name: /Next question|See your results/ }),
  ).toBeEnabled();
}
async function advance(page: Page) {
  await page
    .getByRole("button", { name: /Next question|See your results/ })
    .click();
}

test("light theme follows the system, keeps an explicit choice and renders every practice state", async ({
  page,
}) => {
  test.setTimeout(120000);
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await mkdir(folder, { recursive: true });
  await page.goto("/");
  const html = page.locator("html");
  await expect(html).toHaveAttribute("data-theme", "light");
  await expect(page.locator('meta[name="theme-color"]')).toHaveAttribute(
    "content",
    "#f6f5f0",
  );
  expect(
    await page.evaluate(() => getComputedStyle(document.body).backgroundColor),
  ).toBe("rgb(246, 245, 240)");
  // Without an explicit choice the page tracks the operating-system setting.
  await page.emulateMedia({ colorScheme: "dark" });
  await expect(html).toHaveAttribute("data-theme", "dark");
  await page.emulateMedia({ colorScheme: "light" });
  await expect(html).toHaveAttribute("data-theme", "light");

  await page.setViewportSize({ width: 1440, height: 900 });
  await expect(page.getByText("Live together")).toBeVisible();
  await page.getByLabel("Your display name").fill("Light learner");
  await expect(page.getByText("Quiz ready to join.")).toBeVisible();
  await expect(page.locator(".preview-loading")).toHaveCount(0);
  for (const width of [320, 375, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: width < 700 ? 812 : 900 });
    expect(
      await page
        .locator(".join-illustration")
        .evaluate(
          (image: HTMLImageElement) => image.complete && image.naturalWidth > 0,
        ),
    ).toBe(true);
    await capture(page, `join-${width}`);
  }

  await page.setViewportSize({ width: 1440, height: 900 });
  await page.getByRole("button", { name: "Join the quiz" }).click();
  const status = page.locator(".connection");
  await expect(status).toHaveAttribute("data-state", "live");
  await expect(status).toHaveText("Live");
  await expect(
    page.getByRole("button", { name: "Leave", exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "All quizzes" })).toHaveCount(
    0,
  );
  await expect(page.getByTestId("personal-score")).toHaveText("0");
  await expect(
    page.getByRole("columnheader", { name: "Answered" }),
  ).toBeVisible();
  const mine = page.locator("tr.is-you");
  await expect(mine.locator(".answered-cell")).toHaveText("0/8");
  const first = (await current(page)).currentQuestion!;
  await expect(page.locator(".question-meta .eyebrow")).toHaveText(
    `${first.timing.difficulty[0].toUpperCase()}${first.timing.difficulty.slice(1)} word`,
  );
  await page
    .getByRole("radio")
    .and(page.locator(`input[value="${correctOption(first.id)}"]`))
    .check();
  await captureBoth(page, "selected");

  await page.getByRole("button", { name: "Check answer", exact: true }).click();
  await expect(page.getByText("+100 points", { exact: true })).toBeVisible();
  await expect(page.locator(".feedback-title")).toContainText("Correct!");
  await expect(page.locator(".feedback-icon")).toBeVisible();
  await expect(page.locator(".definition-meaning")).toBeVisible();
  const taught = (await current(page)).receipts[0].vocabulary;
  await expect(page.locator(".word-pronunciation")).toHaveText(
    taught.pronunciation,
  );
  await expect(page.locator(".word-part")).toHaveText(taught.partOfSpeech);
  const ipa = (await page.locator(".word-pronunciation").boundingBox())!;
  const part = (await page.locator(".word-part").boundingBox())!;
  expect(ipa.x + ipa.width).toBeLessThanOrEqual(part.x);
  expect(
    Math.abs(ipa.y + ipa.height / 2 - (part.y + part.height / 2)),
  ).toBeLessThan(4);
  await expect(mine.locator(".answered-cell")).toHaveText("1/8");
  await captureBoth(page, "correct");

  await advance(page);
  await answer(page, false);
  const feedback = page.locator(".answer-feedback");
  await expect(feedback).toContainText("Not quite.");
  await expect(feedback).toContainText("Your answer");
  await expect(feedback).toContainText("Correct answer");
  await expect(page.locator(".question-panel")).toHaveClass(
    /feedback-incorrect/,
  );
  await captureBoth(page, "incorrect");

  // Theme is presentation only: switching keeps the same receipt and state,
  // and an explicit choice outlives reloads despite the light system setting.
  const beforeSwitch = await current(page);
  await page.getByRole("button", { name: "Switch to dark theme" }).click();
  await expect(html).toHaveAttribute("data-theme", "dark");
  await expect(page.getByText("Live & synchronized")).toBeVisible();
  await expect(
    page.locator(".answer-feedback img.feedback-symbol"),
  ).toBeVisible();
  await expect(feedback).toContainText("Not quite.");
  expect(
    await page.evaluate(() => localStorage.getItem("vocabulary.theme")),
  ).toBe("dark");
  await page.reload();
  await expect(html).toHaveAttribute("data-theme", "dark");
  await expect(page.getByText("Live & synchronized")).toBeVisible();
  await expect(feedback).toContainText("Not quite.");
  await page.getByRole("button", { name: "Switch to light theme" }).click();
  await expect(html).toHaveAttribute("data-theme", "light");
  await expect(page.locator(".feedback-icon")).toBeVisible();
  const afterSwitch = await current(page);
  expect(afterSwitch.receipts).toEqual(beforeSwitch.receipts);
  expect(afterSwitch.score).toBe(beforeSwitch.score);
  expect(afterSwitch.currentQuestion).toEqual(beforeSwitch.currentQuestion);

  for (const correct of [true, true, false, true, true, true]) {
    await advance(page);
    await answer(page, correct);
  }
  await advance(page);
  await expect(page.locator("#completion-heading")).toHaveText(
    "Great job, Light learner!",
  );
  await expect(page.getByRole("button", { name: "New quiz" })).toBeVisible();
  await expect(page.locator(".result-grid")).toContainText("6 / 8");
  await expect(page.locator(".result-grid")).toContainText("75% accuracy");
  await expect(page.locator(".score-result strong")).toHaveText(
    String((await current(page)).score),
  );
  await expect(page.locator(".summary-list li")).toHaveCount(8);
  await expect(page.locator(".review-tip")).toContainText(
    "Two new words to review",
  );
  await expect(status).toHaveAttribute("data-state", "live");
  await expect(page.locator(".result-grid")).toContainText("Current rank");
  await captureBoth(page, "completion");

  const beforeReview = await current(page);
  await page
    .getByRole("button", { name: "Review missed words", exact: true })
    .click();
  await expect(page.locator(".missed-review article")).toHaveCount(2);
  await expect(page.locator(".missed-review")).toContainText(
    "Review does not change your score.",
  );
  await capture(page, "desktop-missed-review");
  expect(await current(page)).toEqual(beforeReview);
  await page.getByRole("button", { name: "View live standings" }).click();
  await expect(page.locator(".standings")).toBeVisible();
  await expect(page.locator(".standings-sync-card")).toContainText(
    "Standings are live",
  );
  expect(errors).toEqual([]);
  await page
    .getByRole("button", { name: "Start practice review", exact: true })
    .click();
  await expect(
    page.getByText("Canonical practice question", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Give me a hint" }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Skip", exact: true }).click();
  await expect(page.locator(".review-feedback")).toContainText("Skipped");
  expect(await current(page)).toEqual(beforeReview);
});
