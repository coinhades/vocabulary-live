import { expect, test } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import { parseState } from "../../src/types/protocol";

// Independent content audit in the learning suite's separate Redis namespace;
// the core suite deliberately fills TRAVEL-DEMO to test capacity. No provider
// action is needed here, and grading keys remain exclusively on the server.
const words: Record<string, string> = {
  "vocab-q1": "concise",
  "vocab-q2": "refine",
  "vocab-q3": "reliable",
  "vocab-q4": "simultaneously",
  "vocab-q5": "abundant",
  "vocab-q6": "clarify",
  "vocab-q7": "temporary",
  "vocab-q8": "feasible",
  "travel-q1": "itinerary",
  "travel-q2": "destination",
  "travel-q3": "departure",
  "travel-q4": "direct",
  "travel-q5": "reserve",
  "travel-q6": "pedestrian",
  "travel-q7": "delayed",
  "travel-q8": "landmark",
};
const hiddenTargets = new Set(["vocab-q2", "vocab-q8"]);
const regressions = new Set([
  "abundant",
  "clarify",
  "destination",
  "departure",
  "reserve",
  "delayed",
]);
const captures = "../docs/evidence/screenshots/question-highlight";

for (const theme of ["dark", "light"] as const) {
  test.describe(theme, () => {
    test.use({ colorScheme: theme, reducedMotion: "reduce" });
    for (const quiz of ["VOCAB-DEMO", "TRAVEL-DEMO"]) {
      test(`${quiz}: every prompt emphasizes its visible vocabulary target`, async ({
        page,
      }) => {
        test.setTimeout(120000);
        const errors: string[] = [];
        page.on("pageerror", (error) => errors.push(error.message));
        await mkdir(captures, { recursive: true });
        await page.goto("/");
        await page.getByLabel("Quiz code", { exact: true }).fill(quiz);
        await page.getByLabel("Your display name").fill(`Word audit ${theme}`);
        await expect(page.getByText("Quiz ready to join.")).toBeVisible();
        await page.getByRole("button", { name: "Join the quiz" }).click();
        await expect(page.locator(".connection")).toHaveAttribute(
          "data-state",
          "live",
        );

        const seen = new Set<string>();
        for (let i = 0; i < 8; i++) {
          const response = await page.request.get(`/api/quizzes/${quiz}/state`);
          expect(response.ok()).toBe(true);
          const state = parseState(await response.json());
          const question = state.currentQuestion!;
          const word = words[question.id];
          expect(word).toBeTruthy();
          expect(seen.has(question.id)).toBe(false);
          seen.add(question.id);
          const emphasis = hiddenTargets.has(question.id) ? [] : [word];
          expect(question.pronunciationText).toBe(emphasis[0]);

          const heading = page.getByRole("heading", {
            level: 2,
            name: question.prompt,
            exact: true,
          });
          await expect(heading).toBeVisible();
          await expect(heading.locator("em")).toHaveText(emphasis);
          // A restored question uses the same metadata and saved presentation.
          if (i === 0) {
            await page.reload();
            await expect(heading).toBeVisible();
            await expect(heading.locator("em")).toHaveText(emphasis);
          }
          if (regressions.has(word)) {
            for (const [width, height] of [
              [1440, 900],
              [375, 812],
            ]) {
              await page.setViewportSize({ width, height });
              await expect(heading).toBeVisible();
              await expect(heading.locator("em")).toHaveText(emphasis);
              await expect(
                page.locator(
                  '.page-stage [class*="-enter-active"], .page-stage [class*="-leave-active"]',
                ),
              ).toHaveCount(0);
              await page.evaluate(() => document.fonts.ready);
              expect(
                await page.evaluate(
                  () => document.documentElement.scrollWidth <= innerWidth,
                ),
              ).toBe(true);
              await page.screenshot({
                path: `${captures}/${theme}-${word}-${width}.png`,
                fullPage: true,
              });
            }
          }

          await page.getByRole("radio").first().check();
          await page
            .getByRole("button", { name: "Check answer", exact: true })
            .click();
          const next = page.getByRole("button", {
            name: /Next question|See your results/,
          });
          await expect(next).toBeEnabled();
          const accepted = parseState(
            await (await page.request.get(`/api/quizzes/${quiz}/state`)).json(),
          );
          const receipt = accepted.receipts.find(
            (r) => r.questionId === question.id,
          )!;
          expect(receipt.vocabulary.word).toBe(word);
          expect(receipt.question.prompt).toBe(question.prompt);
          await expect(page.locator(".word-heading h3")).toHaveText(word);
          await next.click();
        }
        expect([...seen].sort()).toEqual(
          Object.keys(words)
            .filter((id) =>
              id.startsWith(quiz === "VOCAB-DEMO" ? "vocab-" : "travel-"),
            )
            .sort(),
        );
        expect(errors).toEqual([]);
      });
    }
  });
}
