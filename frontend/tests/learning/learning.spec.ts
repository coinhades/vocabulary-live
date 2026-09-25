import { test, expect, type Page } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import { parseState } from "../../src/types/protocol";
const folder = "../docs/evidence/screenshots/learning-ui-polish";
test.beforeEach(async ({ request }) => {
  await mkdir(folder, { recursive: true });
  await request.post("/__test/provider", { data: { mode: "", delayMS: 0 } });
});
test.afterEach(async ({ request }) => {
  await request.post("/__test/provider", { data: { mode: "", delayMS: 0 } });
});
async function state(page: Page) {
  return parseState(
    await (await page.request.get("/api/quizzes/VOCAB-DEMO/state")).json(),
  );
}
async function join(page: Page, name: string) {
  await page.goto("/");
  await page.getByLabel("Your display name").fill(name);
  await page.getByRole("button", { name: "Join the quiz" }).click();
  await expect(page.getByRole("radio").first()).toBeVisible();
}
async function capture(page: Page, name: string) {
  await page.evaluate(() => document.fonts.ready);
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
    path: `${folder}/${name}.png`,
    fullPage: !(await page.locator("dialog[open]").count()),
    animations: "disabled",
  });
}
for (const theme of ["dark", "light"] as const) {
  test(`${theme}: private learning, speaker playback, responsive panels and live second learner`, async ({
    browser,
  }) => {
    test.setTimeout(120000);
    await mkdir(folder, { recursive: true });
    const context = await browser.newContext({
      colorScheme: theme,
      viewport: { width: 1440, height: 900 },
    });
    const other = await browser.newContext({ colorScheme: theme });
    const page = await context.newPage(),
      peer = await other.newPage();
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.goto("/");
    await expect(page.locator(".quiz-preview:not([inert])")).toContainText(
      "8 questions",
    );
    const surface = await page.locator(".join-panel").evaluate((el) => ({
      shadow: getComputedStyle(el).boxShadow,
      blur: getComputedStyle(el).backdropFilter,
    }));
    expect(surface.shadow).not.toBe("none");
    if (theme === "dark") expect(surface.blur).toContain("blur(");
    await capture(page, `${theme}-join`);
    await join(page, "A learner with a long name");
    await join(peer, "Another learner");
    await page.getByRole("radio").first().check();
    await page
      .getByRole("button", { name: "Check answer", exact: true })
      .click();
    await expect(page.locator(".word-title-line")).toBeVisible();
    const before = await state(page);
    const word = before.receipts[0].vocabulary.word;
    const speaker = page.locator(".word-title-line .speaker-button");
    await expect(speaker).toHaveAccessibleName(`Listen to ${word}`);
    const heading = await page.locator(".word-title-line h3").boundingBox(),
      button = await speaker.boundingBox();
    expect(button!.x).toBeGreaterThan(heading!.x + heading!.width);
    await page.request.post("/__test/provider", { data: { delayMS: 350 } });
    await speaker.click();
    await expect(
      page.locator(
        '.pronunciation-control[data-state="loading"] .speaker-button',
      ),
    ).toBeVisible();
    await expect(
      page.locator(
        '.pronunciation-control[data-state="playing"] .speaker-button',
      ),
    ).toBeVisible();
    await capture(page, `${theme}-playing`);
    await expect(speaker).toHaveAccessibleName(`Replay ${word}`);
    const calls = (await (await page.request.get("/__test/provider")).json())
      .calls;
    await speaker.click();
    expect(
      (await (await page.request.get("/__test/provider")).json()).calls,
    ).toBe(calls);
    const standingsHeight = (await page.locator(".standings").boundingBox())!
      .height;
    const peerBefore = await state(peer);
    await page
      .getByRole("button", { name: "Explain why", exact: true })
      .click();
    await expect(page.getByText("Preparing an explanation…")).toBeVisible();
    await peer.getByRole("radio").first().check();
    await peer
      .getByRole("button", { name: "Check answer", exact: true })
      .click();
    await expect(
      page.locator(
        `.standings [data-participant-id="${peerBefore.participantId}"]`,
      ),
    ).toContainText(theme === "dark" ? "1 / 8 answered" : "1/8");
    await expect(
      page.locator(".learning-dialog .generated-text"),
    ).toBeVisible();
    expect(
      (await page.locator(".standings").boundingBox())!.height,
    ).toBeCloseTo(standingsHeight, 0);
    await expect(page.locator(".feedback-next")).toBeEnabled();
    for (const [width, height] of [
      [1440, 900],
      [1280, 800],
      [768, 1024],
      [390, 844],
      [375, 812],
    ]) {
      await page.setViewportSize({ width, height });
      if (width <= 760)
        await expect(page.locator("#standings-view")).not.toBeVisible();
      await capture(page, `${theme}-explanation-${width}`);
      const dialogBox = await page.getByRole("dialog").boundingBox();
      expect(dialogBox!.x + dialogBox!.width / 2).toBeCloseTo(width / 2, 0);
      expect(dialogBox!.y + dialogBox!.height / 2).toBeCloseTo(height / 2, 0);
      await page.getByRole("button", { name: "Close learning window" }).click();
      const actions = await page
        .locator(".feedback-actions > button")
        .evaluateAll((elements) =>
          elements.map((el) => el.getBoundingClientRect()),
        );
      expect(actions).toHaveLength(3);
      expect(actions[0].width).toBeCloseTo(actions[1].width, 0);
      expect(actions[2].width).toBeCloseTo(2 * actions[0].width, 0);
      expect(actions[0].y).toBe(actions[2].y);
      await capture(page, `${theme}-feedback-${width}`);
      const wordBox = await page.locator(".word-title-line h3").boundingBox();
      const speakerBox = await speaker.boundingBox();
      expect(speakerBox!.x).toBeGreaterThan(wordBox!.x + wordBox!.width);
      expect(speakerBox!.y + speakerBox!.height / 2).toBeCloseTo(
        wordBox!.y + wordBox!.height / 2,
        0,
      );
      await page
        .getByRole("button", { name: "Explain why", exact: true })
        .click();
    }
    await page.getByRole("button", { name: "Close learning window" }).click();
    await page.getByRole("button", { name: "Examples", exact: true }).click();
    await page.getByLabel("Context", { exact: true }).selectOption("travel");
    await page.getByRole("button", { name: "Create example" }).click();
    await expect(page.locator(".generated-text")).toContainText("travel");
    await page.getByRole("button", { name: "Another example" }).click();
    await expect(page.getByText("2 / 2", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Previous example" }).click();
    await expect(page.locator(".example-count")).toHaveText("1 / 2");
    await page
      .getByRole("button", { name: "Next example", exact: true })
      .click();
    await expect(page.locator(".example-count")).toHaveText("2 / 2");
    await capture(page, `${theme}-examples-mobile`);
    await page.setViewportSize({ width: 1440, height: 900 });
    await capture(page, `${theme}-examples-desktop`);
    await expect(
      page.getByText(
        /Ready — tap|AI-generated voice|Provider retention|AI-generated learning content|Report confusing/,
      ),
    ).toHaveCount(0);
    const after = await state(page);
    expect(after.receipts).toEqual(before.receipts);
    expect(after.score).toBe(before.score);
    expect(after.currentQuestion).toEqual(before.currentQuestion);
    expect(errors).toEqual([]);
    await page.request.post("/__test/provider", { data: { delayMS: 0 } });
    await context.close();
    await other.close();
  });
}
test("autoplay block retains clip; timeout stays local; navigation ignores late output", async ({
  page,
}) => {
  await page.addInitScript(() => {
    const original = HTMLMediaElement.prototype.play;
    let blocked = false;
    HTMLMediaElement.prototype.play = function () {
      if (!blocked) {
        blocked = true;
        return Promise.reject(
          new DOMException("test blocked", "NotAllowedError"),
        );
      }
      return original.call(this);
    };
  });
  await join(page, "Recovery learner");
  await page.getByRole("radio").first().check();
  await page.getByRole("button", { name: "Check answer", exact: true }).click();
  await expect(page.locator(".word-title-line")).toBeVisible();
  await page.request.post("/__test/provider", { data: { delayMS: 0 } });
  await page.locator(".word-title-line .speaker-button").click();
  await expect(
    page.locator(
      '.pronunciation-control[data-state="ready-to-play"] .speaker-button',
    ),
  ).toBeVisible();
  await expect(
    page.locator(".word-title-line .speaker-button"),
  ).toHaveAccessibleName(/^Play .+ again$/);
  const calls = (await (await page.request.get("/__test/provider")).json())
    .calls;
  await page.locator(".word-title-line .speaker-button").click();
  await expect(
    page.locator(
      '.pronunciation-control[data-state="playing"] .speaker-button',
    ),
  ).toBeVisible();
  expect(
    (await (await page.request.get("/__test/provider")).json()).calls,
  ).toBe(calls);
  await page.request.post("/__test/provider", { data: { mode: "timeout" } });
  await page.getByRole("button", { name: "Explain why" }).click();
  await expect(page.locator(".learning-error")).toContainText("too long");
  await expect(page.locator(".feedback-next")).toBeEnabled();
  await page.request.post("/__test/provider", { data: { delayMS: 2000 } });
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await page.getByRole("button", { name: "Close learning window" }).click();
  await page
    .getByRole("button", { name: "Next question", exact: true })
    .click();
  await expect(page.getByRole("radio").first()).toBeVisible();
  await expect(page.locator(".learning-dialog[open]")).toHaveCount(0);
  await expect(page.locator("#question-heading")).toBeFocused();
  await page.request.post("/__test/provider", { data: { delayMS: 0 } });
});

test("rejected credentials hide all learning controls, preserve feedback and recover", async ({
  page,
}) => {
  await page.request.post("/__test/provider", {
    data: { mode: "invalid-key" },
  });
  await join(page, "Credential checks");
  await expect(page.locator(".speaker-button")).toHaveCount(0);
  await page.getByRole("radio").first().check();
  await page.getByRole("button", { name: "Check answer", exact: true }).click();
  await expect(page.locator(".answer-feedback")).toBeVisible();
  const before = await state(page);
  const calls = (await (await page.request.get("/__test/provider")).json())
    .calls;
  await expect(page.getByRole("button", { name: "Explain why" })).toHaveCount(
    0,
  );
  await expect(
    page.getByRole("button", { name: "Examples", exact: true }),
  ).toHaveCount(0);
  const actions = await page.locator(".feedback-actions").boundingBox();
  expect(
    (await page.locator(".feedback-next").boundingBox())!.width,
  ).toBeCloseTo(actions!.width, 0);

  await page.request.post("/__test/provider", { data: { mode: "" } });
  await page.reload();
  await expect(page.getByRole("button", { name: "Explain why" })).toBeVisible();
  await expect(page.locator(".speaker-button")).toBeVisible();
  // A rejected credential after successful validation hides all controls together.
  await page.request.post("/__test/provider", {
    data: { mode: "invalid-key" },
  });
  await page.getByRole("button", { name: "Explain why" }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.locator(".speaker-button")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Explain why" })).toHaveCount(
    0,
  );
  await expect(
    page.getByRole("button", { name: "Examples", exact: true }),
  ).toHaveCount(0);
  await expect(page.locator(".feedback-next")).toBeFocused();
  expect(
    (await (await page.request.get("/__test/provider")).json()).calls,
  ).toBe(calls);
  const after = await state(page);
  expect(after.receipts).toEqual(before.receipts);
  expect(after.score).toBe(before.score);
  expect(after.currentQuestion).toEqual(before.currentQuestion);
  // Focus refresh also detects credential changes without a generation action.
  await page.request.post("/__test/provider", { data: { mode: "" } });
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect(page.getByRole("button", { name: "Explain why" })).toBeVisible();
  await page.request.post("/__test/provider", {
    data: { mode: "invalid-key" },
  });
  await page.evaluate(() => window.dispatchEvent(new Event("focus")));
  await expect(page.locator(".speaker-button")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Explain why" })).toHaveCount(
    0,
  );
  expect(
    (await (await page.request.get("/__test/provider")).json()).calls,
  ).toBe(calls);
  await page.getByRole("button", { name: "Next question" }).click();
  await expect(page.getByRole("radio").first()).toBeVisible();
});
