import { test, expect, request as apiRequest } from "@playwright/test";
import type { BrowserContext, Page, APIRequestContext } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import { parseState } from "../../src/types/protocol";

// Test-owned grading fixture, never imported by the application bundle.
const correctOptions = ["b", "a", "c", "d", "a", "c", "b", "d"];
const evidence = "../docs/evidence/screenshots/dark-theme";
const origin = `http://127.0.0.1:${process.env.E2E_PORT ?? "8080"}`;
function correctOption(questionId: string) {
  return correctOptions[Number(questionId.split("-q")[1]) - 1];
}
async function current(page: Page, quiz = "VOCAB-DEMO") {
  const response = await page.request.get(`/api/quizzes/${quiz}/state`);
  expect(response.ok()).toBe(true);
  return parseState(await response.json());
}
async function screenshot(page: Page, name: string) {
  await mkdir(evidence, { recursive: true });
  await page.screenshot({
    path: `${evidence}/${name}.png`,
    fullPage: true,
    animations: "disabled",
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
}
async function captureBoth(page: Page, name: string) {
  const previous = page.viewportSize()!;
  for (const [width, label] of [
    [1440, "desktop"],
    [375, "mobile"],
  ] as const) {
    await page.setViewportSize({ width, height: width === 375 ? 812 : 1000 });
    await expect(page.getByRole("tablist")).toHaveCount(
      width === 375 && (await page.locator(".quiz-layout").count()) ? 1 : 0,
    );
    await screenshot(page, `${label}-${name}`);
  }
  await page.setViewportSize(previous);
}
async function join(
  page: Page,
  name: string,
  quiz = "VOCAB-DEMO",
  live = true,
) {
  await page.goto("/");
  await page.getByLabel("Quiz code", { exact: true }).fill(quiz);
  await page.getByLabel("Your display name").fill(name);
  await expect(page.getByText("Quiz ready to join.")).toBeVisible();
  await page.getByRole("button", { name: "Join the quiz" }).click();
  await expect(page.locator("#question-heading")).toBeVisible();
  if (live) await expect(page.getByText("Live & synchronized")).toBeVisible();
}
async function choose(page: Page, correct = true, quiz = "VOCAB-DEMO") {
  const state = await current(page, quiz);
  const question = state.currentQuestion!;
  const option = correct
    ? correctOption(question.id)
    : question.options.find((o) => o.id !== correctOption(question.id))!.id;
  await page
    .getByRole("radio")
    .and(page.locator(`input[value="${option}"]`))
    .check();
  return { question, option, state };
}
async function check(page: Page, correct = true, quiz = "VOCAB-DEMO") {
  const intent = await choose(page, correct, quiz);
  await page.getByRole("button", { name: "Check answer", exact: true }).click();
  await expect(
    page.getByRole("button", { name: /Next question|See your results/ }),
  ).toBeEnabled();
  return intent;
}
async function advance(page: Page) {
  await page
    .getByRole("button", { name: /Next question|See your results/ })
    .click();
}
async function standings(page: Page) {
  if (page.viewportSize()!.width <= 760)
    await page.getByRole("tab", { name: /Standings/ }).click();
}
async function questionView(page: Page) {
  if (page.viewportSize()!.width <= 760)
    await page.getByRole("tab", { name: /Question|Results/ }).click();
}
async function apiPlayer(name: string, quiz = "VOCAB-DEMO") {
  const client = await apiRequest.newContext({
    baseURL: origin,
    extraHTTPHeaders: { Origin: origin },
  });
  expect((await client.post("/api/session", { data: {} })).status()).toBe(200);
  const response = await client.post(`/api/quizzes/${quiz}/join`, {
    data: { displayName: name },
  });
  expect(response.status()).toBe(200);
  return { client, state: parseState(await response.json()) };
}

test("expired browser cookie requires explicit recovery and retains the old participant", async ({
  page,
  context,
}) => {
  await join(page, "Returning player", "TRAVEL-DEMO");
  await check(page, true, "TRAVEL-DEMO");
  await expect(page.getByTestId("personal-score")).toHaveText("100");
  await context.clearCookies();
  await page.reload();
  await expect(
    page.getByText(
      "Your anonymous session has expired. Start a new session to continue.",
    ),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Start a new anonymous session" })
    .click();
  await expect(page.getByText("Live & synchronized")).toBeVisible();
  await expect(page.getByTestId("personal-score")).toHaveText("0");
  await expect(
    page.getByRole("row").filter({ hasText: "Returning player" }),
  ).toHaveCount(2);
});

test("three real players, persisted presentation, feedback, lost response, mobile standings and non-scoring review", async ({
  browser,
}) => {
  test.setTimeout(120000);
  const contexts: BrowserContext[] = [];
  const pages: Page[] = [];
  const errors: string[] = [];
  const publicMessages: string[] = [];
  const initialPayloads: string[] = [];
  let answerRequests = 0;
  for (let i = 0; i < 3; i++) {
    const context = await browser.newContext({
      viewport: { width: i === 2 ? 375 : 1440, height: i === 2 ? 812 : 1000 },
      reducedMotion: "reduce",
    });
    contexts.push(context);
    const page = await context.newPage();
    pages.push(page);
    page.on("pageerror", (e) => errors.push(e.message));
    page.on("websocket", (socket) =>
      socket.on("framereceived", (event) =>
        publicMessages.push(event.payload.toString()),
      ),
    );
    page.on("request", (r) => {
      if (r.method() === "POST" && r.url().endsWith("/answers"))
        answerRequests++;
    });
    await page.route(
      "**/api/quizzes/*/join",
      async (route) => {
        const response = await route.fetch();
        initialPayloads.push(await response.text());
        await route.fulfill({ response });
      },
      { times: 1 },
    );
  }
  try {
    const [alex, morgan, sam] = pages;
    await alex.goto("/");
    await expect(alex.getByText("Quiz ready to join.")).toBeVisible();
    await captureBoth(alex, "join");
    await join(alex, "Alex");
    await join(morgan, "Morgan");
    await join(sam, "Sam");
    await expect(
      sam.getByRole("tab", { name: "Question", exact: true }),
    ).toHaveAttribute("aria-selected", "true");
    await expect(sam.locator(".standings")).not.toBeVisible();
    for (const page of pages) {
      await standings(page);
      await expect(
        page.getByText("3 people have joined", { exact: false }),
      ).toBeVisible();
      for (const name of ["Alex", "Morgan", "Sam"])
        await expect(
          page.getByRole("row").filter({ hasText: name }),
        ).toBeVisible();
      await questionView(page);
    }
    expect(await alex.evaluate(() => document.cookie)).not.toContain(
      "vocab_session",
    );
    expect(
      (await contexts[0].cookies()).some(
        (c) => c.name === "vocab_session" && c.httpOnly && c.sameSite === "Lax",
      ),
    ).toBe(true);
    await expect(
      alex.getByRole("button", { name: "Check answer", exact: true }),
    ).toBeDisabled();

    // Reversible selection and visible keyboard focus before explicit confirmation.
    await choose(alex, false);
    const first = await choose(alex, true);
    await alex.locator(`input[value="${first.option}"]`).focus();
    await alex.keyboard.press("Tab");
    await alex.keyboard.press("Shift+Tab");
    await expect(
      alex.locator(".answer-option:has(input:focus-visible)"),
    ).toHaveCSS("outline-width", "3px");
    await expect(alex.getByTestId("personal-score")).toHaveText("0");
    await captureBoth(alex, "selected");
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    await alex.route(
      "**/api/quizzes/VOCAB-DEMO/answers",
      async (route) => {
        await gate;
        await route.continue();
      },
      { times: 1 },
    );
    await alex
      .getByRole("button", { name: "Check answer", exact: true })
      .click();
    await expect(
      alex.getByRole("button", { name: "Checking…", exact: true }),
    ).toBeDisabled();
    await expect(alex.locator("input[type=radio]").first()).toBeDisabled();
    await expect(alex.locator(".answer-feedback")).toHaveCount(0);
    await expect(alex.getByTestId("personal-score")).toHaveText("0");
    await captureBoth(alex, "submitting");
    release();
    await expect(
      alex.getByRole("button", { name: "Next question", exact: true }),
    ).toBeEnabled();
    await expect(alex.locator(".feedback-title")).toContainText("Correct");
    await captureBoth(alex, "correct");

    const samState = await current(sam);
    await sam.locator("input[type=radio]").first().focus();
    await sam.keyboard.press("ArrowRight");
    await expect(sam.locator("input[type=radio]").nth(1)).toBeChecked();
    await expect(sam.getByTestId("personal-score")).toHaveText("0");
    await sam
      .locator(`input[value="${correctOption(samState.currentQuestion!.id)}"]`)
      .focus();
    await sam.keyboard.press("Space");
    await sam.keyboard.press("Tab");
    await expect(
      sam.getByRole("button", { name: "Check answer", exact: true }),
    ).toBeFocused();
    await sam.keyboard.press("Enter");
    await expect(
      sam.getByRole("button", { name: "Next question" }),
    ).toBeEnabled();

    await check(morgan, false);
    await expect(morgan.locator(".answer-feedback")).toContainText(
      "Not quite.",
    );
    await expect(morgan.locator(".feedback-answers")).toContainText(
      "Your answer",
    );
    await expect(morgan.locator(".feedback-answers")).toContainText(
      "Correct answer",
    );
    await captureBoth(morgan, "incorrect");
    for (const page of pages) {
      await standings(page);
      for (const name of ["Alex", "Sam"]) {
        const row = page.getByRole("row").filter({ hasText: name });
        await expect(row.locator("td").first()).toHaveText("1");
        await expect(row).toContainText("100");
      }
      await expect(
        page
          .getByRole("row")
          .filter({ hasText: "Morgan" })
          .locator("td")
          .first(),
      ).toHaveText("3");
      await questionView(page);
    }

    await alex.reload();
    await expect(alex.getByText("Live & synchronized")).toBeVisible();
    await expect(alex.locator("#question-heading")).toHaveText(
      first.question.prompt,
    );
    const reloaded = await current(alex);
    expect(reloaded.receipts[0].question).toEqual(first.question);
    await expect(alex.locator("input:checked")).toHaveValue(first.option);
    await advance(alex);
    await expect(alex.locator("#question-heading")).toHaveText(
      reloaded.currentQuestion!.prompt,
    );

    let lostId = "";
    await alex.route(
      "**/api/quizzes/VOCAB-DEMO/answers",
      async (route) => {
        lostId = route.request().postDataJSON().submissionId;
        await route.fetch();
        await route.abort("failed");
      },
      { times: 1 },
    );
    const second = await choose(alex, false);
    await alex
      .getByRole("button", { name: "Check answer", exact: true })
      .click();
    await expect.poll(async () => (await current(alex)).answeredCount).toBe(2);
    await alex.reload();
    await expect(alex.getByText("Live & synchronized")).toBeVisible();
    await expect(alex.locator("#question-heading")).toHaveText(
      second.question.prompt,
    );
    await expect(alex.locator(".answer-feedback")).toContainText("Not quite.");
    const recovered = await current(alex);
    expect(recovered.receipts[1].submissionId).toBe(lostId);
    expect(
      await alex.evaluate(() =>
        sessionStorage.getItem("vocabulary.pending.VOCAB-DEMO"),
      ),
    ).toBeNull();
    await advance(alex);

    const beforeReconnect = await current(alex);
    const selected = await choose(alex);
    await contexts[0].setOffline(true);
    await expect(
      alex.getByText("Reconnecting…", { exact: true }),
    ).toBeVisible();
    await captureBoth(alex, "reconnecting");
    await expect(alex.locator("input:checked")).toHaveValue(selected.option);
    await contexts[0].setOffline(false);
    await expect(alex.getByText("Live & synchronized")).toBeVisible();
    expect((await current(alex)).currentQuestion).toEqual(
      beforeReconnect.currentQuestion,
    );
    await expect(alex.locator("input:checked")).toHaveValue(selected.option);

    // These participants are created through the real service and remain in the complete list.
    for (let i = 0; i < 24; i++) {
      const p = await apiPlayer(
        `Practice guest ${String(i + 1).padStart(2, "0")}`,
      );
      await p.client.dispose();
    }
    await expect(alex.getByRole("row")).toHaveCount(28);
    expect(
      await alex
        .locator(".standings-scroll")
        .evaluate((el) => el.scrollHeight > el.clientHeight),
    ).toBe(true);
    await screenshot(alex, "desktop-standings-scroll");
    await sam.getByRole("tab", { name: /Standings/ }).focus();
    await sam.keyboard.press("Home");
    await expect(
      sam.getByRole("tab", { name: "Question", exact: true }),
    ).toBeFocused();
    await sam.keyboard.press("End");
    await expect(sam.getByRole("tab", { name: /Standings/ })).toBeFocused();
    await expect(sam.locator(".standings")).toBeVisible();
    await sam.locator(".standings-scroll").evaluate((el) => {
      el.scrollTop = el.scrollHeight;
    });
    await screenshot(sam, "mobile-standings-scroll");

    for (let i = 2; i < 8; i++) {
      await check(alex);
      await advance(alex);
    }
    await expect(alex.locator("#completion-heading")).toBeVisible();
    await expect(alex.getByTestId("personal-score")).toHaveText("700");
    await expect(alex.locator(".result-grid")).toContainText("87.5%");
    await expect(alex.locator(".result-grid")).toContainText("Current rank");
    await captureBoth(alex, "completion");
    const beforeReview = await current(alex);
    const requestsBeforeReview = answerRequests;
    await alex
      .getByRole("button", { name: "Review words", exact: true })
      .click();
    await expect(alex.locator(".missed-review article")).toHaveCount(1);
    await expect(alex.locator(".missed-review")).toContainText(
      "Review does not change your score.",
    );
    await captureBoth(alex, "missed-review");
    expect(await current(alex)).toEqual(beforeReview);
    expect(answerRequests).toBe(requestsBeforeReview);
    await alex.setViewportSize({ width: 1440, height: 1000 });
    await alex.getByRole("button", { name: "View live standings" }).click();
    await expect(alex.locator(".standings")).toBeVisible();
    await expect(alex.getByRole("row")).toHaveCount(28);
    expect(initialPayloads).toHaveLength(3);
    expect(publicMessages.length).toBeGreaterThan(3);
    const privateKeys = new Set([
      "correctOptionId",
      "explanation",
      "selectedOptionId",
      "questionOrder",
      "optionOrders",
    ]);
    function inspectKeys(value: unknown) {
      if (value && typeof value === "object") {
        for (const [key, nested] of Object.entries(value)) {
          expect(privateKeys.has(key), `private field leaked: ${key}`).toBe(
            false,
          );
          inspectKeys(nested);
        }
      }
    }
    for (const payload of [...publicMessages, ...initialPayloads])
      inspectKeys(JSON.parse(payload));
    expect(errors).toEqual([]);
  } finally {
    await Promise.all(contexts.map((context) => context.close()));
  }
});

test("HTTP checking works during WebSocket-only failure and an unknown outcome retries the same intent", async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.clock.install();
  await page.routeWebSocket("**/api/quizzes/VOCAB-DEMO/live", (route) =>
    route.close(),
  );
  await join(page, "HTTP still works", "VOCAB-DEMO", false);
  // Hold automatic reconnects while exercising the separate manual retry path.
  await page.clock.pauseAt(new Date(Date.now() + 100));
  await expect(page.getByText("Live & synchronized")).not.toBeVisible();
  const before = await choose(page);
  const ids: string[] = [];
  await page.route("**/api/quizzes/VOCAB-DEMO/answers", async (route) => {
    ids.push(route.request().postDataJSON().submissionId);
    if (ids.length === 1) await route.abort("failed");
    else await route.continue();
  });
  await page.getByRole("button", { name: "Check answer", exact: true }).click();
  await page.clock.runFor(50);
  await expect(
    page.getByRole("button", { name: "Awaiting confirmation" }),
  ).toBeDisabled();
  await expect(page.locator(".answer-feedback")).toHaveCount(0);
  await expect(page.getByTestId("personal-score")).toHaveText("0");
  await expect(page.locator("input:checked")).toHaveValue(before.option);
  await page.getByRole("button", { name: "Check & retry safely" }).click();
  await expect(
    page.getByRole("button", { name: "Next question" }),
  ).toBeEnabled();
  expect(ids).toHaveLength(2);
  expect(ids[1]).toBe(ids[0]);
  await page.clock.resume();
  await expect(page.getByTestId("personal-score")).toHaveText("100");
  await expect(
    page.getByText("Standings synchronizing · last known scores"),
  ).toBeVisible();
});

test("inline validating, invalid quiz, service unavailable and safe display-name rendering", async ({
  page,
}) => {
  await page.goto("/");
  await page.getByLabel("Quiz code", { exact: true }).fill("UNKNOWN");
  await expect(page.getByText("Validating quiz…")).toBeVisible();
  await expect(page.getByText("That quiz code was not found.")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Join the quiz" }),
  ).toBeDisabled();
  await page.route("**/api/quizzes/VOCAB-DEMO", (route) =>
    route.abort("failed"),
  );
  await page.getByLabel("Quiz code", { exact: true }).fill("VOCAB-DEMO");
  await expect(
    page.getByText("Quiz service unavailable. Please try again."),
  ).toBeVisible();
  await page.unroute("**/api/quizzes/VOCAB-DEMO");
  await page.getByRole("button", { name: "Retry validation" }).click();
  await expect(page.getByText("Quiz ready to join.")).toBeVisible();
  await page
    .getByLabel("Your display name")
    .fill("<img src=x onerror=alert(1)>");
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  await page.route(
    "**/api/quizzes/VOCAB-DEMO/join",
    async (route) => {
      await gate;
      await route.continue();
    },
    { times: 1 },
  );
  await page.getByRole("button", { name: "Join the quiz" }).click();
  await expect(page.getByRole("button", { name: "Joining…" })).toBeDisabled();
  release();
  await expect(page.getByText("Live & synchronized")).toBeVisible();
  await expect(
    page.getByRole("row").filter({ hasText: "<img src=x onerror=alert(1)>" }),
  ).toBeVisible();
  expect(await page.locator("tbody img").count()).toBe(0);
});

test("full room is inline and an existing member can resume unchanged", async ({
  page,
}) => {
  test.setTimeout(120000);
  const keeper = await apiPlayer("Room keeper", "TRAVEL-DEMO");
  const clients: APIRequestContext[] = [keeper.client];
  try {
    for (let i = keeper.state.participantCount; i < 200; i++) {
      const p = await apiPlayer(`Capacity guest ${i}`, "TRAVEL-DEMO");
      await p.client.dispose();
    }
    await page.goto("/");
    await page
      .getByRole("button", { name: "TRAVEL-DEMO", exact: true })
      .click();
    await page.getByLabel("Your display name").fill("Late arrival");
    await expect(page.getByText("Quiz ready to join.")).toBeVisible();
    await page.getByRole("button", { name: "Join the quiz" }).click();
    await expect(
      page.getByText("This demo quiz has reached its 200-person capacity."),
    ).toBeVisible();
    const resumed = await keeper.client.post("/api/quizzes/TRAVEL-DEMO/join", {
      data: { displayName: "Changed name" },
    });
    expect(resumed.status()).toBe(200);
    const state = parseState(await resumed.json());
    expect(state.currentQuestion).toEqual(keeper.state.currentQuestion);
    expect(state.participantId).toBe(keeper.state.participantId);
    expect(state.participantCount).toBe(200);
  } finally {
    await Promise.all(clients.map((client) => client.dispose()));
  }
});
