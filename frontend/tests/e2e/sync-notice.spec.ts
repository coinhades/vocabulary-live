import { test, expect } from "@playwright/test";
import type { WebSocketRoute } from "@playwright/test";
import { mkdir, writeFile } from "node:fs/promises";
import { parseState } from "../../src/types/protocol";

const folder = "../docs/evidence/screenshots/sync-notice";
type NoticeEvidence = { insertions: number; messages: string[] };

for (const variant of [
  { name: "desktop", width: 1440, reduced: false },
  { name: "mobile", width: 375, reduced: false },
  { name: "reduced-mobile", width: 375, reduced: true },
]) {
  test(`${variant.name}: normal navigation never inserts a warning and a real interruption keeps one until synchronized`, async ({
    browser,
    baseURL,
  }) => {
    const context = await browser.newContext({
      baseURL,
      viewport: { width: variant.width, height: 1000 },
      reducedMotion: variant.reduced ? "reduce" : "no-preference",
    });
    const page = await context.newPage();
    let holdSnapshots = true;
    let active: WebSocketRoute | undefined;
    let held: { socket: WebSocketRoute; message: string | Buffer } | undefined;
    let connections = 0;
    await context.addInitScript(() => {
      const evidence = { insertions: 0, messages: [] as string[] };
      Object.assign(window, { syncNoticeEvidence: evidence });
      const seen = new WeakSet<Element>();
      // Observe insertions, not just the final state: a one-frame warning is a failure.
      new MutationObserver((records) => {
        for (const record of records)
          for (const node of record.addedNodes) {
            if (!(node instanceof Element)) continue;
            const notices = [node, ...node.querySelectorAll(".network-notice")];
            for (const notice of notices)
              if (notice.matches(".network-notice") && !seen.has(notice)) {
                seen.add(notice);
                evidence.insertions++;
                evidence.messages.push(notice.textContent ?? "");
              }
          }
      }).observe(document, { childList: true, subtree: true });
    });
    await page.routeWebSocket("**/api/quizzes/VOCAB-DEMO/live", (socket) => {
      connections++;
      active = socket;
      held = undefined;
      const server = socket.connectToServer();
      server.onMessage((message) => {
        if (active !== socket) return;
        if (holdSnapshots) held = { socket, message };
        else socket.send(message);
      });
    });
    const evidence = () =>
      page.evaluate(
        () =>
          (window as unknown as { syncNoticeEvidence: NoticeEvidence })
            .syncNoticeEvidence,
      );
    const release = () => {
      holdSnapshots = false;
      held?.socket.send(held.message);
      held = undefined;
    };
    const current = async () =>
      parseState(
        await (await page.request.get("/api/quizzes/VOCAB-DEMO/state")).json(),
      );
    const noWarning = async () => {
      await expect(page.locator(".network-notice")).toHaveCount(0);
      expect((await evidence()).insertions).toBe(0);
    };
    const initialSync = async () => {
      await expect.poll(() => !!held).toBe(true);
      await expect(page.locator(".connection")).toHaveAttribute(
        "data-state",
        "synchronizing",
      );
      await expect(page.getByText("Live & synchronized")).toHaveCount(0);
      // Allow actual painted frames while the first real server snapshot is withheld.
      await page.evaluate(
        () =>
          new Promise<void>((resolve) =>
            requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
          ),
      );
      await noWarning();
      release();
      await expect(page.getByText("Live & synchronized")).toBeVisible();
      await noWarning();
    };
    await mkdir(folder, { recursive: true });
    try {
      await page.goto("/");
      await page.getByLabel("Your display name").fill(`Notice ${variant.name}`);
      await expect(page.getByText("Quiz ready to join.")).toBeVisible();
      await noWarning();
      await page.getByRole("button", { name: "Join the quiz" }).click();
      await initialSync();
      await page.getByRole("radio").first().check();
      await page
        .getByRole("button", { name: "Check answer", exact: true })
        .click();
      await expect(
        page.getByRole("button", { name: "Next question" }),
      ).toBeEnabled();
      const accepted = (await current()).receipts;
      await noWarning();

      holdSnapshots = true;
      held = undefined;
      await page.reload();
      await initialSync();
      await expect(page.locator(".answer-feedback")).toBeVisible();
      expect((await current()).receipts).toEqual(accepted);
      await page.getByRole("button", { name: "Next question" }).click();
      await expect(page.getByRole("timer")).toHaveText(/\d+ seconds left/);
      await noWarning();
      await page.getByRole("button", { name: "All quizzes" }).click();
      await expect(
        page.getByRole("heading", { name: "Join a practice" }),
      ).toBeVisible();
      await expect(page.locator(".quiz-layout")).toHaveCount(0);
      await noWarning();
      await page.getByRole("button", { name: "Join the quiz" }).click();
      await expect(page.getByText("Live & synchronized")).toBeVisible();
      await noWarning();
      await page.screenshot({
        path: `${folder}/${variant.name}-normal.png`,
        fullPage: true,
        animations: "disabled",
      });
      const normal = await evidence();

      // A genuine closed socket must show a warning; a new open socket/HTTP
      // refresh must not briefly hide it before the fresh snapshot arrives.
      holdSnapshots = true;
      held = undefined;
      const beforeConnections = connections;
      await active!.close({ code: 1012, reason: "Test reconnect" });
      const notice = page.locator(".network-notice");
      await expect(notice).toBeVisible();
      await expect(notice).toContainText("last known scores");
      const warningElement = await notice.elementHandle();
      await expect.poll(() => connections).toBeGreaterThan(beforeConnections);
      await expect.poll(() => !!held).toBe(true);
      await expect(page.locator(".connection")).toHaveAttribute(
        "data-state",
        "synchronizing",
      );
      await page.getByRole("button", { name: "Retry synchronization" }).click();
      expect(await warningElement!.evaluate((el) => el.isConnected)).toBe(true);
      await expect(notice).toBeVisible();
      expect((await evidence()).insertions).toBe(1);
      await page.screenshot({
        path: `${folder}/${variant.name}-real-interruption.png`,
        fullPage: true,
        animations: "disabled",
      });
      release();
      await expect(page.getByText("Live & synchronized")).toBeVisible();
      await expect(notice).toHaveCount(0);
      expect((await current()).receipts).toEqual(accepted);
      const recovered = await evidence();
      expect(recovered.insertions).toBe(1);
      await writeFile(
        `${folder}/${variant.name}-checks.json`,
        JSON.stringify(
          { normal, recovered, connections, retainedReceipts: true },
          null,
          2,
        ),
      );
    } finally {
      await context.close();
    }
  });
}
