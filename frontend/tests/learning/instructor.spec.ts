import { test, expect, type Page } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import { parseDraft, parseSummary } from "../../src/types/authoring";
import { parseState } from "../../src/types/protocol";
const secret = "test-only-operator-secret-7b210415ac74e91f";
async function login(page: Page) {
  await page.goto("/instructor");
  await page.getByLabel("Operator secret").fill(secret);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "A new practice" }),
  ).toBeVisible();
}
for (const theme of ["dark", "light"] as const) {
  test(`${theme}: instructor edits, conflict recovery, approval and real new quiz`, async ({
    browser,
  }) => {
    test.setTimeout(120000);
    const ctx = await browser.newContext({
        colorScheme: theme,
        viewport: { width: 1440, height: 900 },
      }),
      page = await ctx.newPage();
    const errors: string[] = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await login(page);
    expect(
      await page.evaluate(
        () => JSON.stringify(localStorage) + JSON.stringify(sessionStorage),
      ),
    ).not.toContain(secret);
    expect(await page.evaluate(() => document.cookie)).not.toContain(
      "vocab_operator",
    );
    await page
      .getByLabel("Topic", { exact: true })
      .fill(`${theme} travel practice`);
    await page
      .getByRole("button", { name: "Generate draft", exact: true })
      .click();
    await expect(page.locator(".studio-question")).toHaveCount(4);
    await expect(
      page.getByRole("button", { name: "Publish as new quiz" }),
    ).toBeDisabled();
    const rows = (
      (await (
        await page.request.get("/api/instructor/drafts")
      ).json()) as unknown[]
    ).map(parseSummary);
    const id = rows.find((d) => d.title === `${theme} travel practice`)!.id;
    const endpoint = `/api/instructor/drafts/${id}`;
    const original = parseDraft(
      await (await page.request.get(endpoint)).json(),
    );
    const first = page.locator(".studio-question").first();
    await first
      .getByLabel("Intended sense", { exact: true })
      .fill("A locally reviewed travel meaning");
    await first.getByRole("button", { name: "Accept suggested tags" }).click();
    await expect(
      first.getByRole("button", { name: "Approve question 1", exact: true }),
    ).toBeDisabled();
    // A second tab changes saved content; saving local edits must visibly conflict.
    if (theme === "dark") {
      const remote = await page.request.post(endpoint, {
        headers: { Origin: "http://127.0.0.1:18082" },
        data: {
          expectedVersion: original.version,
          clientActionId: crypto.randomUUID(),
          title: "Remote title",
          items: original.items.map((i) => ({ id: i.id, content: i.content })),
        },
      });
      expect(remote.ok()).toBe(true);
      await page
        .getByRole("button", { name: "Save changes", exact: true })
        .click();
      await expect(
        page.getByRole("region", { name: "Version conflict" }),
      ).toBeVisible();
      await expect(
        first.getByLabel("Intended sense", { exact: true }),
      ).toHaveValue("A locally reviewed travel meaning");
      await page
        .getByRole("button", { name: "Keep my edits", exact: true })
        .click();
    }
    await page
      .getByRole("button", { name: "Save changes", exact: true })
      .click();
    await expect(
      page.getByRole("button", { name: "Save changes", exact: true }),
    ).toBeDisabled();
    // A preview cannot silently replace an unsaved edit in another question.
    if (theme === "dark") {
      await page
        .locator(".studio-question")
        .nth(1)
        .getByLabel("Canonical explanation", { exact: true })
        .fill("Locally edited explanation awaiting review.");
      await first
        .getByRole("button", { name: "Preview a new version" })
        .click();
      await expect(
        page.getByRole("region", { name: "Regeneration preview" }),
      ).toBeVisible();
      await expect(
        first.getByLabel("Question stem", { exact: true }),
      ).not.toHaveValue(/^New preview:/);
      await page
        .getByRole("button", { name: "Use new version", exact: true })
        .click();
      await expect(
        first.getByLabel("Question stem", { exact: true }),
      ).toHaveValue(/^New preview:/);
      await expect(
        page
          .locator(".studio-question")
          .nth(1)
          .getByLabel("Canonical explanation", { exact: true }),
      ).toHaveValue("Locally edited explanation awaiting review.");
      await page
        .getByRole("button", { name: "Save changes", exact: true })
        .click();
    }
    for (let i = 1; i <= 4; i++) {
      await page
        .getByRole("button", { name: `Approve question ${i}`, exact: true })
        .click();
      await expect(
        page.getByRole("button", {
          name: `Approve question ${i}`,
          exact: true,
        }),
      ).toBeDisabled();
    }
    await expect(
      page.getByRole("button", { name: "Publish as new quiz" }),
    ).toBeDisabled();
    await page
      .getByLabel(
        "I have reviewed every question, its answer key and teaching content for this practice.",
      )
      .check();
    await expect(
      page.getByRole("button", { name: "Publish as new quiz" }),
    ).toBeEnabled();
    await mkdir("../docs/evidence/screenshots/ai-learning/p4", {
      recursive: true,
    });
    for (const [width, height] of [
      [1440, 900],
      [1280, 800],
      [768, 1024],
      [390, 844],
      [375, 812],
    ]) {
      await page.setViewportSize({ width, height });
      await first.scrollIntoViewIfNeeded();
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
      ).toBe(true);
      await page.screenshot({
        path: `../docs/evidence/screenshots/ai-learning/p4/${theme}-studio-${width}.png`,
        animations: "disabled",
      });
    }
    await page.getByRole("button", { name: "Publish as new quiz" }).click();
    await expect(
      page.getByRole("heading", { name: "Your new quiz is ready" }),
    ).toBeVisible();
    const draft = parseDraft(await (await page.request.get(endpoint)).json());
    expect(draft.status).toBe("published");
    expect(draft.publishedQuizId).toMatch(/^QUIZ-/);
    const popup = page.waitForEvent("popup");
    await page.getByRole("link", { name: "Open new quiz" }).click();
    const learner = await popup;
    await learner.getByLabel("Your display name").fill("New quiz learner");
    await learner.getByRole("button", { name: "Join the quiz" }).click();
    await expect(learner.getByRole("radio").first()).toBeVisible();
    const quizPath = `/api/quizzes/${draft.publishedQuizId}/state`;
    const before = parseState(
      await (await learner.request.get(quizPath)).json(),
    );
    expect(before.totalQuestions).toBe(4);
    const peerCtx = await browser.newContext(),
      peer = await peerCtx.newPage();
    await peer.goto(`/?quiz=${draft.publishedQuizId}`);
    await peer.getByLabel("Your display name").fill("Second new learner");
    await peer.getByRole("button", { name: "Join the quiz" }).click();
    await expect(peer.getByRole("radio").first()).toBeVisible();
    const peerState = parseState(
      await (await peer.request.get(quizPath)).json(),
    );
    expect(peerState.contentVersion).toBe(before.contentVersion);
    expect(peerState.totalQuestions).toBe(4);
    await expect(
      peer.getByText("Live & synchronized", { exact: true }),
    ).toBeVisible();
    const choiceByID = new Map(
      draft.items.map((i) => [
        `word-${i.id}`,
        String.fromCharCode(97 + i.content.correctIndex),
      ]),
    );
    for (let i = 0; i < 4; i++) {
      const s = parseState(await (await learner.request.get(quizPath)).json());
      await learner
        .getByRole("radio")
        .and(
          learner.locator(
            `input[value="${choiceByID.get(s.currentQuestion!.id)}"]`,
          ),
        )
        .check();
      await learner
        .getByRole("button", { name: "Check answer", exact: true })
        .click();
      await learner
        .getByRole("button", { name: /Next question|See your results/ })
        .click();
    }
    await expect(learner.locator("#completion-heading")).toBeVisible();
    const final = parseState(
      await (await learner.request.get(quizPath)).json(),
    );
    expect(final.correctCount).toBe(4);
    expect(final.accuracy).toBe(100);
    await page.reload();
    await page
      .getByRole("button", { name: new RegExp(`${theme} travel practice`) })
      .click();
    await expect(page.locator(".studio-code")).toHaveText(
      draft.publishedQuizId,
    );
    await page.getByRole("button", { name: "Load recent reports" }).click();
    await expect(page.getByText("No reports to review.")).toBeVisible();
    expect(errors).toEqual([]);
    await peerCtx.close();
    await ctx.close();
  });
}
