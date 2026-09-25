import { test, expect } from "@playwright/test";
import { mkdir, writeFile } from "node:fs/promises";
import { parseState, parseClock, parseAnswer } from "../../src/types/protocol";

const folder = "../docs/evidence/screenshots/timed-scoring";
const answers = ["b", "a", "c", "d", "a", "c", "b", "d"];

test("smooth server countdown, sharp avatars and 50-point late credit survive reload and replay", async ({
  browser,
  baseURL,
}) => {
  test.setTimeout(90000);
  const context = await browser.newContext({
    baseURL,
    viewport: { width: 1440, height: 1000 },
    deviceScaleFactor: 2,
  });
  const page = await context.newPage();
  const submissions: string[] = [];
  page.on("request", (request) => {
    if (request.url().endsWith("/answers") && request.method() === "POST")
      submissions.push(request.postData()!);
  });
  const current = async () =>
    parseState(
      await (await page.request.get("/api/quizzes/TRAVEL-DEMO/state")).json(),
    );
  await mkdir(folder, { recursive: true });
  try {
    await page.goto("/");
    await page
      .getByRole("button", { name: "TRAVEL-DEMO", exact: true })
      .click();
    await expect(page.getByText("Quiz ready to join.")).toBeVisible();
    await page.getByLabel("Your display name").fill("Timer learner");
    await page.getByRole("button", { name: "Join the quiz" }).click();
    await expect(page.getByText("Live & synchronized")).toBeVisible();
    await expect(page.getByRole("timer")).toHaveText(/\d+ seconds left/);
    const initial = await current();
    const q = initial.currentQuestion!;
    const clock = parseClock(
      await (
        await page.request.post("/api/quizzes/TRAVEL-DEMO/start", {
          headers: { Origin: new URL(page.url()).origin },
          data: { epoch: initial.epoch, questionId: q.id },
        })
      ).json(),
    );
    const samples = await page
      .locator(".timer-track > span")
      .evaluate(async (el) => {
        const values: number[] = [];
        for (let i = 0; i < 24; i++) {
          await new Promise<void>((resolve) =>
            requestAnimationFrame(() => resolve()),
          );
          values.push(new DOMMatrixReadOnly(getComputedStyle(el).transform).a);
        }
        return values;
      });
    expect(new Set(samples).size).toBeGreaterThan(12);
    expect(
      samples.every((value, i) => i === 0 || value <= samples[i - 1]),
    ).toBe(true);
    const portrait = await page
      .locator(".player-avatar")
      .first()
      .evaluate(async (el) => {
        const image = new Image();
        const src = getComputedStyle(el).backgroundImage.slice(5, -2);
        image.src = src;
        await image.decode();
        return {
          src: new URL(src).pathname,
          width: image.naturalWidth,
          height: image.naturalHeight,
          backgroundSize: getComputedStyle(el).backgroundSize,
        };
      });
    expect(portrait.src).toMatch(/^\/art\/avatar-[0-5]\.webp$/);
    expect(portrait.width).toBeGreaterThanOrEqual(400);
    expect(portrait.height).toBeGreaterThanOrEqual(400);
    expect(portrait.backgroundSize).toBe("cover");
    await page.screenshot({
      path: `${folder}/countdown-desktop-2x.png`,
      fullPage: true,
    });

    // Editing Date or deleting client-only timer data cannot renew the deadline.
    await page.clock.setFixedTime(new Date("2100-01-01T00:00:00Z"));
    await page.evaluate(() => {
      for (const key of Object.keys(sessionStorage))
        if (key.startsWith("vocabulary.timer.")) sessionStorage.removeItem(key);
    });
    await page.reload();
    await expect(page.getByRole("timer")).toHaveText(/\d+ seconds left/);
    const restored = parseClock(
      await (
        await page.request.post("/api/quizzes/TRAVEL-DEMO/start", {
          headers: { Origin: new URL(page.url()).origin },
          data: { epoch: initial.epoch, questionId: q.id },
        })
      ).json(),
    );
    expect(restored.deadlineMs).toBe(clock.deadlineMs);
    await expect(page.getByRole("timer")).toHaveText("Time’s up", {
      timeout: q.timing.durationSeconds * 1000 + 3000,
    });
    await expect(
      page.getByText("You can still earn 50 points for a correct answer."),
    ).toBeVisible();
    expect(submissions).toHaveLength(0);
    expect((await current()).receipts).toEqual([]);
    await page.screenshot({
      path: `${folder}/expired-desktop-2x.png`,
      fullPage: true,
    });
    const correct = answers[Number(q.id.split("-q")[1]) - 1];
    await page
      .getByRole("radio")
      .and(page.locator(`input[value="${correct}"]`))
      .check();
    await page
      .getByRole("button", { name: "Check answer", exact: true })
      .click();
    await expect(page.getByText("+50 points", { exact: true })).toBeVisible();
    await expect(
      page.getByText("Correct answer after time ran out."),
    ).toBeVisible();
    await expect(page.getByTestId("personal-score")).toHaveText("50");
    const late = await current();
    expect(late.receipts[0].correctness).toBe(true);
    expect(late.receipts[0].timedOut).toBe(true);
    await page.reload();
    await expect(page.getByText("+50 points", { exact: true })).toBeVisible();
    const replay = parseAnswer(
      await (
        await page.request.post("/api/quizzes/TRAVEL-DEMO/answers", {
          headers: { Origin: new URL(page.url()).origin },
          data: JSON.parse(submissions[0]),
        })
      ).json(),
    );
    expect(replay.outcome).toBe("replayed");
    expect(replay.receipt).toEqual(late.receipts[0]);
    await expect(page.locator(".connection")).toHaveAttribute(
      "data-state",
      "live",
    );
    await expect(page.locator(".network-notice")).toHaveCount(0);
    for (const width of [1440, 375, 320]) {
      await page.setViewportSize({ width, height: 1000 });
      await expect(
        page.locator(
          '.page-stage [class*="-enter-active"], .page-stage [class*="-leave-active"]',
        ),
      ).toHaveCount(0);
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
      await page.screenshot({
        path: `${folder}/late-correct-${width}-2x.png`,
        fullPage: true,
        animations: "disabled",
      });
    }
    await page.getByRole("button", { name: "Next question" }).click();
    const next = (await current()).currentQuestion!;
    await expect(page.getByRole("timer")).toHaveText(
      `${next.timing.durationSeconds} seconds left`,
    );
    const nextCorrect = answers[Number(next.id.split("-q")[1]) - 1];
    await page
      .getByRole("radio")
      .and(page.locator(`input[value="${nextCorrect}"]`))
      .check();
    await page
      .getByRole("button", { name: "Check answer", exact: true })
      .click();
    await expect(page.getByText("+100 points", { exact: true })).toBeVisible();
    await expect(page.getByTestId("personal-score")).toHaveText("150");
    await writeFile(
      `${folder}/checks.json`,
      JSON.stringify(
        {
          samples,
          portrait,
          deadlineRetained: restored.deadlineMs === clock.deadlineMs,
          lateScore: late.score,
          finalScore: (await current()).score,
        },
        null,
        2,
      ),
    );
  } finally {
    await context.close();
  }
});
