import { test, expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import { mkdir, writeFile } from "node:fs/promises";
import { parseState } from "../../src/types/protocol";

const evidence = "../docs/evidence/screenshots/dark-theme/motion";
// Test-owned fixture; never part of the browser application.
const answers = ["b", "a", "c", "d", "a", "c", "b", "d"];
const correctID = (id: string) => answers[Number(id.split("-q")[1]) - 1];
async function state(page: Page) {
  const response = await page.request.get("/api/quizzes/VOCAB-DEMO/state");
  expect(response.ok()).toBe(true);
  return parseState(await response.json());
}
async function capture(page: Page, label: string) {
  await mkdir(evidence, { recursive: true });
  await page.screenshot({ path: `${evidence}/${label}.png`, fullPage: true });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
}
async function join(page: Page, name: string) {
  await page.goto("/");
  await page.getByLabel("Your display name").fill(name);
  await expect(page.getByText("Quiz ready to join.")).toBeVisible();
  await page.getByRole("button", { name: "Join the quiz" }).click();
  await expect(page.getByText("Live & synchronized")).toBeVisible();
}
async function select(page: Page, correct: boolean) {
  const before = await state(page);
  const question = before.currentQuestion!;
  const id = correct
    ? correctID(question.id)
    : question.options.find((option) => option.id !== correctID(question.id))!
        .id;
  await page
    .getByRole("radio")
    .and(page.locator(`input[value="${id}"]`))
    .check();
  return before;
}
async function next(page: Page) {
  // A user action may arrive repeatedly before Vue paints; it still advances
  // presentation once, never submits another answer or skips a server question.
  await page
    .getByRole("button", { name: /Next question|See your results/ })
    .evaluate((button: HTMLButtonElement) => {
      button.click();
      button.click();
      button.click();
    });
  await expect(
    page.locator(".question-content[inert], .question-panel[inert]"),
  ).toHaveCount(0);
  await expect(
    page.locator("#question-heading, #completion-heading"),
  ).toBeFocused();
}
async function feedbackFrames(
  page: Page,
  label: string,
  interrupt?: () => Promise<void>,
) {
  // Sample the real browser's CSS keyframes at deterministic times. Pausing
  // presentation must not affect the authoritative score or enablement of Next.
  for (const time of [0, 120, 360]) {
    await page.locator(".answer-feedback").evaluate((element, ms) => {
      for (const animation of element.getAnimations({ subtree: true })) {
        animation.pause();
        animation.currentTime = ms;
      }
    }, time);
    await capture(page, `${label}-${time}`);
    if (time === 120 && interrupt) {
      const playing = await page
        .locator(".answer-feedback")
        .evaluate((element) => {
          const animations = element.getAnimations({ subtree: true });
          for (const animation of animations) animation.play();
          return animations.some(
            (animation) => animation.playState === "running",
          );
        });
      expect(playing).toBe(true);
      await interrupt();
    }
  }
  await page.locator(".answer-feedback").evaluate((element) => {
    for (const animation of element.getAnimations({ subtree: true }))
      animation.finish();
  });
}

for (const variant of [
  { label: "desktop", width: 1440, reduced: false },
  { label: "mobile", width: 375, reduced: false },
  { label: "reduced-mobile", width: 375, reduced: true },
]) {
  test(`${variant.label}: motion preserves authoritative answers, focus, replay and live standings`, async ({
    browser,
  }) => {
    test.setTimeout(120000);
    const context = await browser.newContext({
      viewport: {
        width: variant.width,
        height: variant.width === 375 ? 812 : 1000,
      },
      reducedMotion: variant.reduced ? "reduce" : "no-preference",
    });
    const peerContext = await browser.newContext({ reducedMotion: "reduce" });
    const page = await context.newPage();
    const peer = await peerContext.newPage();
    const errors: string[] = [];
    const posted: unknown[] = [];
    const timeline: { name: string; target: string; time: number }[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.exposeFunction(
      "recordMotion",
      (entry: (typeof timeline)[number]) => timeline.push(entry),
    );
    await page.addInitScript(() => {
      document.addEventListener("animationstart", (event) => {
        const target = event.target as Element;
        void (
          window as unknown as {
            recordMotion: (value: unknown) => Promise<void>;
          }
        ).recordMotion({
          name: event.animationName,
          target: target.getAttribute("class") ?? "",
          time: Math.round(performance.now()),
        });
      });
      document.addEventListener("transitionrun", (event) => {
        const target = event.target as Element;
        if (
          event.propertyName === "transform" &&
          target.matches(
            ".standings-move, .question-content, .question-progress > span",
          )
        )
          void (
            window as unknown as {
              recordMotion: (value: unknown) => Promise<void>;
            }
          ).recordMotion({
            name: `transition:${event.propertyName}`,
            target:
              target.getAttribute("class") ??
              target.parentElement?.className ??
              "",
            time: Math.round(performance.now()),
          });
      });
    });
    page.on("request", (request) => {
      if (request.method() === "POST" && request.url().endsWith("/answers"))
        posted.push(request.postDataJSON());
    });
    try {
      let releasePreview!: () => void;
      const previewGate = new Promise<void>((resolve) => {
        releasePreview = resolve;
      });
      await page.route(
        "**/api/quizzes/VOCAB-DEMO",
        async (route) => {
          await previewGate;
          await route.continue();
        },
        { times: 1 },
      );
      await page.goto("/");
      await expect(page.getByText("Validating quiz…")).toBeVisible();
      const loadingNameY = (await page
        .getByLabel("Your display name")
        .boundingBox())!.y;
      await capture(page, `${variant.label}-validation-loading`);
      releasePreview();
      await expect(page.getByText("Quiz ready to join.")).toBeVisible();
      await expect(page.locator(".preview-loading")).toHaveCount(0);
      expect(
        Math.abs(
          (await page.getByLabel("Your display name").boundingBox())!.y -
            loadingNameY,
        ),
      ).toBeLessThan(5);
      await capture(page, `${variant.label}-preview`);
      await page
        .getByLabel("Your display name")
        .fill(`Motion ${variant.label}`);
      let releaseJoin!: () => void;
      const joinGate = new Promise<void>((resolve) => {
        releaseJoin = resolve;
      });
      await page.route(
        "**/api/quizzes/VOCAB-DEMO/join",
        async (route) => {
          await joinGate;
          await route.continue();
        },
        { times: 1 },
      );
      const joinWidth = (await page
        .getByRole("button", { name: "Join the quiz" })
        .boundingBox())!.width;
      await page.getByRole("button", { name: "Join the quiz" }).click();
      await expect(
        page.getByRole("button", { name: "Joining…" }),
      ).toBeDisabled();
      expect(
        (await page.getByRole("button", { name: "Joining…" }).boundingBox())!
          .width,
      ).toBe(joinWidth);
      await capture(page, `${variant.label}-joining`);
      releaseJoin();
      await expect(page.getByText("Live & synchronized")).toBeVisible();
      await expect(page.locator(".connection .live-dot")).toHaveCSS(
        "animation-name",
        "none",
      );
      await join(peer, `Peer ${variant.label}`);
      await page.getByRole("radio").first().locator("..").hover();
      await capture(page, `${variant.label}-hover`);
      const first = await select(page, true);
      await page.locator("input:checked").focus();
      await page.keyboard.press("Tab");
      await page.keyboard.press("Shift+Tab");
      await expect(
        page.locator(".answer-option:has(input:focus-visible)"),
      ).toHaveCSS("outline-width", "3px");
      await capture(page, `${variant.label}-selected`);
      let releaseAnswer!: () => void;
      const answerGate = new Promise<void>((resolve) => {
        releaseAnswer = resolve;
      });
      await page.route(
        "**/api/quizzes/VOCAB-DEMO/answers",
        async (route) => {
          await answerGate;
          await route.continue();
        },
        { times: 1 },
      );
      await page
        .getByRole("button", { name: "Check answer", exact: true })
        .evaluate((button: HTMLButtonElement) => {
          button.click();
          button.click();
        });
      await expect(
        page.getByRole("button", { name: "Checking…", exact: true }),
      ).toBeDisabled();
      await expect(page.locator("input:checked")).toBeDisabled();
      await expect(page.locator(".answer-feedback")).toHaveCount(0);
      await expect(page.getByTestId("personal-score")).toHaveText("0");
      if (variant.reduced)
        await expect(page.locator(".activity-indicator i").first()).toHaveCSS(
          "animation-name",
          "none",
        );
      await capture(page, `${variant.label}-checking`);
      releaseAnswer();
      await expect(
        page.getByRole("button", { name: "Next question" }),
      ).toBeEnabled();
      await expect(page.getByTestId("personal-score")).toHaveText("100");
      expect(posted).toHaveLength(1);
      if (!variant.reduced)
        await feedbackFrames(page, `${variant.label}-correct`, () =>
          context.setOffline(true),
        );
      else await capture(page, `${variant.label}-correct`);
      const accepted = await state(page);
      expect(accepted.answeredCount).toBe(first.answeredCount + 1);
      const beforeReplayMarks = timeline.filter(
        (event) => event.name === "confirm-mark",
      ).length;
      // A real reconnect reconciles the same receipt; it must not replay reward motion.
      await context.setOffline(true);
      await expect(
        page.getByText("Reconnecting…", { exact: true }),
      ).toBeVisible();
      await capture(page, `${variant.label}-reconnecting`);
      await context.setOffline(false);
      await expect(page.getByText("Live & synchronized")).toBeVisible();
      expect((await state(page)).receipts).toEqual(accepted.receipts);
      expect(
        timeline.filter((event) => event.name === "confirm-mark"),
      ).toHaveLength(beforeReplayMarks);
      await next(page);
      await expect(page.locator("#question-heading")).toHaveText(
        accepted.currentQuestion!.prompt,
      );
      await select(page, false);
      await page
        .getByRole("button", { name: "Check answer", exact: true })
        .click();
      await expect(
        page.getByRole("button", { name: "Next question" }),
      ).toBeEnabled();
      await expect(page.locator(".feedback-answers dt")).toHaveText([
        "Your answer",
        "Correct answer",
      ]);
      await expect(page.locator(".wrong-answer")).toBeVisible();
      await expect(page.locator(".revealed-answer")).toBeVisible();
      if (!variant.reduced)
        await feedbackFrames(page, `${variant.label}-incorrect`);
      else await capture(page, `${variant.label}-incorrect`);
      const confirmed = await state(page);
      await page.reload();
      await expect(page.getByText("Live & synchronized")).toBeVisible();
      await expect(page.locator(".answer-feedback")).toContainText(
        "Not quite.",
      );
      await expect(page.locator(".feedback-symbol")).toHaveCSS(
        "animation-name",
        "none",
      );
      expect((await state(page)).receipts).toEqual(confirmed.receipts);
      await next(page);

      if (variant.width === 375)
        await page.getByRole("tab", { name: /Standings/ }).click();
      const peerState = await state(peer);
      const peerRow = page.locator(
        `tr[data-participant-id="${peerState.participantId}"]`,
      );
      const oldPosition = await peerRow.evaluate((element) =>
        Array.from(element.parentElement!.children).indexOf(element),
      );
      const retainedRow = await peerRow.elementHandle();
      const moveCount = () =>
        timeline.filter(
          (event) =>
            event.name === "transition:transform" &&
            event.target.includes("standings-move"),
        ).length;
      const beforePeerMoves = moveCount();
      // Real peer accepts two answers, moving ahead of this participant.
      for (let i = 0; i < 2; i++) {
        await select(peer, true);
        await peer
          .getByRole("button", { name: "Check answer", exact: true })
          .click();
        await expect(
          peer.getByRole("button", { name: "Next question" }),
        ).toBeEnabled();
        await next(peer);
      }
      await expect(peerRow.locator(".score-cell")).toHaveText("200");
      expect(
        await peerRow.evaluate((element) =>
          Array.from(element.parentElement!.children).indexOf(element),
        ),
      ).toBeLessThan(oldPosition);
      expect(
        await retainedRow!.evaluate((element) => element.isConnected),
      ).toBe(true);
      if (!variant.reduced)
        await expect.poll(moveCount).toBeGreaterThan(beforePeerMoves);
      else expect(moveCount()).toBe(beforePeerMoves);
      await capture(page, `${variant.label}-standings`);
      await page.locator(".standings-scroll").evaluate((element) => {
        element.scrollTop = element.scrollHeight;
        element.dispatchEvent(new Event("scroll"));
      });
      await capture(page, `${variant.label}-standings-scroll`);
      if (variant.width === 375) {
        await page.getByRole("tab", { name: /Standings/ }).focus();
        await page.keyboard.press("Home");
        await expect(
          page.getByRole("tab", { name: "Question", exact: true }),
        ).toBeFocused();
      }
      for (let i = 2; i < 8; i++) {
        await select(page, true);
        await page
          .getByRole("button", { name: "Check answer", exact: true })
          .click();
        await expect(
          page.getByRole("button", { name: /Next question|See your results/ }),
        ).toBeEnabled();
        await next(page);
      }
      await expect(page.getByTestId("personal-score")).toHaveText("700");
      await capture(page, `${variant.label}-completion`);
      const retainedCompletion = await page
        .locator(".completion")
        .elementHandle();
      const rankAtCompletion = (await state(page)).currentRank;
      // Other participants continue after personal completion. Passing this
      // score updates the live rank without replaying the completion entrance.
      for (let i = 2; i < 8; i++) {
        await select(peer, true);
        await peer
          .getByRole("button", { name: "Check answer", exact: true })
          .click();
        await expect(
          peer.getByRole("button", { name: /Next question|See your results/ }),
        ).toBeEnabled();
        await next(peer);
      }
      await expect(
        page
          .locator(".result-grid > div")
          .filter({ hasText: "Current rank" })
          .locator("strong"),
      ).toHaveText(String(rankAtCompletion + 1));
      expect(
        await retainedCompletion!.evaluate(
          (element) =>
            element.isConnected &&
            !element.classList.contains("panel-enter-active"),
        ),
      ).toBe(true);
      await capture(page, `${variant.label}-completion-live-rank`);
      const beforeReview = await state(page);
      const beforeReviewRequests = posted.length;
      await page.getByText("Review missed words", { exact: false }).click();
      await expect(page.locator(".missed-review article")).toBeVisible();
      await capture(page, `${variant.label}-review`);
      expect(await state(page)).toEqual(beforeReview);
      expect(posted).toHaveLength(beforeReviewRequests);
      if (variant.reduced)
        expect(
          await page.evaluate(
            () =>
              document
                .getAnimations()
                .filter((animation) => animation.playState === "running")
                .length,
          ),
        ).toBe(0);
      expect(errors).toEqual([]);
      await writeFile(
        `${evidence}/${variant.label}-animation-events.json`,
        JSON.stringify(timeline, null, 2),
      );
    } finally {
      await context.close();
      await peerContext.close();
    }
  });
}
