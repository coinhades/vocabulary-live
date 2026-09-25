import { test, expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import { parseState, parseClock } from "../../src/types/protocol";

const folder = "../docs/evidence/screenshots/ui-refinements";
const answers = ["b", "a", "c", "d", "a", "c", "b", "d"];
async function current(page: Page) {
  return parseState(
    await (await page.request.get("/api/quizzes/TRAVEL-DEMO/state")).json(),
  );
}
async function capture(page: Page, name: string) {
  await page.evaluate(() => document.fonts.ready);
  if (await page.locator(".quiz-layout").count()) {
    await expect(page.locator(".connection")).toHaveAttribute(
      "data-state",
      "live",
    );
  }
  // Resizing crosses the mobile tab breakpoint. Capture the settled screen,
  // after Vue has retired the previous tab/notice rather than its visual echo.
  await expect(
    page.locator(
      '.page-stage [class*="-enter-active"], .page-stage [class*="-leave-active"]',
    ),
  ).toHaveCount(0);
  if (
    page.viewportSize()!.width <= 760 &&
    (await page.locator(".quiz-layout").count())
  ) {
    await expect(page.locator("#standings-view")).not.toBeVisible();
  }
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
test("responsive cards, private vocabulary and persistent difficulty countdown", async ({
  page,
}) => {
  test.setTimeout(90000);
  const externalRequests: string[] = [];
  const answerRequests: string[] = [];
  page.on("request", (request) => {
    if (!new URL(request.url()).hostname.match(/^(127\.0\.0\.1|localhost)$/))
      externalRequests.push(request.url());
    if (request.url().endsWith("/answers") && request.method() === "POST")
      answerRequests.push(request.postData()!);
  });
  let time = new Date("2026-09-24T12:00:00Z").getTime();
  await page.clock.setFixedTime(time);
  await mkdir(folder, { recursive: true });
  await page.goto("/");
  await page.getByLabel("Your display name").fill("Theme preview");
  await expect(
    page.getByRole("button", { name: "Join the quiz" }),
  ).toBeEnabled();
  await expect(page.locator(".preview-loading")).toHaveCount(0);
  await expect(page.locator(".masthead, .site-footer")).toHaveCount(0);
  expect(
    await page
      .locator(".app-shell")
      .evaluate((el) => getComputedStyle(el).borderTopWidth),
  ).toBe("0px");
  for (const width of [320, 375, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    await expect(
      page.getByRole("link", { name: "Vocabulary Live home" }),
    ).toBeVisible();
    expect(
      await page
        .locator(".quiz-preview")
        .evaluate(
          (el) =>
            el.scrollWidth <= el.clientWidth &&
            el.scrollHeight <= el.clientHeight,
        ),
    ).toBe(true);
    expect(
      await page
        .locator(".word-art img")
        .evaluateAll((images) =>
          images.every(
            (image) =>
              (image as HTMLImageElement).complete &&
              (image as HTMLImageElement).naturalWidth > 0,
          ),
        ),
    ).toBe(true);
    await capture(page, `join-${width}`);
  }
  // Keep this participant separate from the three-player VOCAB journey.
  await page.getByRole("button", { name: "TRAVEL-DEMO", exact: true }).click();
  await expect(page.getByText("Quiz ready to join.")).toBeVisible();
  await page.getByRole("button", { name: "Join the quiz" }).click();
  await expect(page.getByText("Live & synchronized")).toBeVisible();
  const initial = await current(page);
  const q = initial.currentQuestion!;
  expect(q).not.toHaveProperty("vocabulary");
  const clockBefore = parseClock(
    await (
      await page.request.post("/api/quizzes/TRAVEL-DEMO/start", {
        headers: { Origin: new URL(page.url()).origin },
        data: { epoch: initial.epoch, questionId: q.id },
      })
    ).json(),
  );
  const duration = q.timing.durationSeconds;
  expect(duration).toBe(
    { easy: 20, medium: 30, hard: 40 }[q.timing.difficulty],
  );
  await expect(page.getByRole("timer")).toHaveText(/\d+ seconds left/);
  // Wall-clock edits cannot shorten or renew the server-owned deadline.
  await page.clock.setFixedTime((time += 7000));
  await page.reload();
  await expect(page.getByRole("timer")).toHaveText(/\d+ seconds left/);
  const clockAfter = parseClock(
    await (
      await page.request.post("/api/quizzes/TRAVEL-DEMO/start", {
        headers: { Origin: new URL(page.url()).origin },
        data: { epoch: initial.epoch, questionId: q.id },
      })
    ).json(),
  );
  expect(clockAfter.deadlineMs).toBe(clockBefore.deadlineMs);
  for (const width of [375, 768, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    await capture(page, `countdown-${width}`);
  }
  await page.clock.setFixedTime((time += duration * 1000));
  await expect(page.getByRole("timer")).toHaveText(/\d+ seconds left/);
  expect((await current(page)).receipts).toEqual([]);
  expect(answerRequests).toEqual([]);
  await capture(page, "timer-wall-clock-independent");
  const correct = answers[Number(q.id.split("-q")[1]) - 1];
  const wrong = q.options.find((o) => o.id !== correct)!;
  await page
    .getByRole("radio")
    .and(page.locator(`input[value="${wrong.id}"]`))
    .check();
  await page.getByRole("button", { name: "Check answer", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Next question" }),
  ).toBeEnabled();
  await expect(page.getByRole("timer")).toHaveCount(0);
  const accepted = await current(page);
  const receipt = accepted.receipts[0];
  await expect(page.locator(".word-heading h3")).toHaveText(
    receipt.vocabulary.word,
  );
  await expect(page.locator(".word-pronunciation")).toHaveText(
    receipt.vocabulary.pronunciation,
  );
  await expect(page.locator(".word-part")).toHaveText(
    `(${receipt.vocabulary.partOfSpeech})`,
  );
  await expect(page.locator(".definition-meaning")).toHaveText(
    receipt.vocabulary.definition,
  );
  // Optional learning controls are hidden when no key is configured.
  await expect(page.locator(".speaker-button")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Explain why" })).toHaveCount(
    0,
  );
  await expect(
    page.getByRole("button", { name: "Examples", exact: true }),
  ).toHaveCount(0);
  for (const width of [320, 375, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    const cards = await page
      .locator(".feedback-answer")
      .evaluateAll((elements) =>
        elements.map((el) => ({
          width: el.getBoundingClientRect().width,
          fits: el.scrollWidth <= el.clientWidth,
          padding: getComputedStyle(el).padding,
          border: getComputedStyle(el).borderColor,
        })),
      );
    expect(cards).toHaveLength(2);
    expect(cards.every((card) => card.fits)).toBe(true);
    expect(cards[0].width).toBe(cards[1].width);
    expect(cards[0].padding).toBe(cards[1].padding);
    expect(cards[0].border).not.toBe(cards[1].border);
    await capture(page, `wrong-feedback-${width}`);
  }
  // Time spent reading feedback must not consume the next word's countdown.
  await page.clock.setFixedTime(time + 60000);
  await page.reload();
  await expect(page.locator(".wrong-answer")).toBeVisible();
  expect((await current(page)).receipts).toEqual(accepted.receipts);
  await page.getByRole("button", { name: "Next question" }).click();
  const next = (await current(page)).currentQuestion!;
  await expect(page.getByRole("timer")).toHaveText(
    `${next.timing.durationSeconds} seconds left`,
  );
  const nextCorrect = answers[Number(next.id.split("-q")[1]) - 1];
  await page
    .getByRole("radio")
    .and(page.locator(`input[value="${nextCorrect}"]`))
    .check();
  await page.getByRole("button", { name: "Check answer", exact: true }).click();
  await expect(page.getByText("Correct!", { exact: true })).toBeVisible();
  for (const width of [375, 1440]) {
    await page.setViewportSize({ width, height: 1000 });
    await capture(page, `correct-feedback-${width}`);
  }
  expect((await current(page)).score).toBe(100);
  expect(answerRequests).toHaveLength(2);
  expect(externalRequests).toEqual([]);
});
