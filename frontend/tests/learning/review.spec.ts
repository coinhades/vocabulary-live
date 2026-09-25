import { test, expect, type Page } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import { parseState } from "../../src/types/protocol";
const correct = ["b", "a", "c", "d", "a", "c", "b", "d"];
async function state(page: Page) {
  return parseState(
    await (await page.request.get("/api/quizzes/VOCAB-DEMO/state")).json(),
  );
}
async function complete(page: Page) {
  await page.goto("/");
  await page.getByLabel("Your display name").fill("Review learner");
  await page.getByRole("button", { name: "Join the quiz" }).click();
  for (let i = 0; i < 8; i++) {
    await expect(page.getByRole("radio").first()).toBeVisible();
    const q = (await state(page)).currentQuestion!;
    const option =
      i < 3
        ? q.options.find(
            (o) => o.id !== correct[Number(q.id.split("-q")[1]) - 1],
          )!.id
        : correct[Number(q.id.split("-q")[1]) - 1];
    await page
      .getByRole("radio")
      .and(page.locator(`input[value="${option}"]`))
      .check();
    await page
      .getByRole("button", { name: "Check answer", exact: true })
      .click();
    await page
      .getByRole("button", { name: /Next question|See your results/ })
      .click();
  }
  await expect(page.locator("#completion-heading")).toBeVisible();
}
for (const theme of ["dark", "light"] as const) {
  test(`${theme}: completion reflection and unscored review survive reload`, async ({
    browser,
  }) => {
    const context = await browser.newContext({
      colorScheme: theme,
      viewport: { width: 1440, height: 900 },
    });
    const page = await context.newPage();
    await complete(page);
    const before = await state(page);
    expect(before.correctCount).toBe(5);
    expect(before.accuracy).toBe(62.5);
    await page.getByRole("button", { name: "Get study suggestions" }).click();
    await expect(
      page.locator(".reflection-card .generated-text"),
    ).toBeVisible();
    await page.getByLabel("Create new contexts with AI").check();
    await page.getByRole("button", { name: "Start practice review" }).click();
    await expect(
      page.getByText("Practice review - no leaderboard points."),
    ).toBeVisible();
    await expect(
      page.getByText("AI-generated review context · canonical choices"),
    ).toBeVisible();
    await page.getByRole("button", { name: "Give me a hint" }).click();
    await expect(
      page.getByRole("heading", { name: "AI hint", exact: true }),
    ).toBeVisible();
    await page.getByRole("radio").first().check();
    await page
      .getByRole("button", { name: "Check review answer", exact: true })
      .click();
    await expect(page.locator(".review-feedback")).toBeVisible();
    const current = await (
      await page.request.get("/api/quizzes/VOCAB-DEMO/review")
    ).json();
    await page.reload();
    await expect(
      page.getByRole("button", { name: "Resume practice review" }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Resume practice review" }).click();
    await expect(page.locator(".review-feedback")).toBeVisible();
    expect(
      await (await page.request.get("/api/quizzes/VOCAB-DEMO/review")).json(),
    ).toEqual(current);
    await expect(page.locator(".review-option input:checked")).toHaveCount(1);
    await mkdir("../docs/evidence/screenshots/ai-learning/p3", {
      recursive: true,
    });
    for (const [width, height] of [
      [1440, 900],
      [375, 812],
    ]) {
      await page.setViewportSize({ width, height });
      await expect(page.locator(".review-feedback")).toBeVisible();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
      await page.screenshot({
        path: `../docs/evidence/screenshots/ai-learning/p3/${theme}-review-${width}.png`,
        fullPage: true,
        animations: "disabled",
      });
    }
    await page.getByRole("button", { name: "Next review card" }).click();
    await expect(page.locator("#review-question-heading")).toBeFocused();
    await page.getByRole("button", { name: "Skip", exact: true }).click();
    await expect(page.locator(".review-feedback")).toContainText("Skipped");
    // Repeated skip intents on different cards remain distinct across reload.
    for (let i = 0; i < 5; i++) {
      if (await page.locator(".review-finished").isVisible()) break;
      await page
        .getByRole("button", { name: /Next review card|Finish review/ })
        .click();
      await expect(page.locator(".review-feedback")).toHaveCount(0);
      if (await page.locator(".review-finished").isVisible()) break;
      await page.getByRole("button", { name: "Skip", exact: true }).click();
      await expect(page.locator(".review-feedback")).toContainText("Skipped");
    }
    await expect(page.locator(".review-finished")).toBeVisible();
    expect(await state(page)).toEqual(before);
    await context.close();
  });
}
