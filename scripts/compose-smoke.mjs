// Local review check. Credential stays in memory and never enters output/files.
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { writeFileSync } from "node:fs";
const base = process.env.COMPOSE_TEST_URL ?? "http://127.0.0.1:18080";
const project = process.env.COMPOSE_PROJECT_NAME ?? "elsa-verify";
let cookie = "";
async function call(path, body) {
  const response = await fetch(`${base}${path}`, {
    method: body === undefined ? "GET" : "POST",
    headers: {
      Origin: base,
      ...(cookie ? { Cookie: cookie } : {}),
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(5000),
  });
  assert.equal(response.status, 200, `${path} HTTP status`);
  if (path.startsWith("/api"))
    assert.equal(response.headers.get("cache-control"), "no-store");
  if (response.headers.has("set-cookie")) {
    const value = response.headers.get("set-cookie");
    assert.match(value, /HttpOnly/);
    cookie = value.split(";")[0];
  }
  return response.json();
}
await call("/readyz");
const html = await fetch(base).then((r) => r.text());
assert.match(html, /Vocabulary Live/);
assert.match(html, /\/assets\//);
const assets = [...html.matchAll(/(?:src|href)="(\/assets\/[^\"]+)"/g)];
for (const [, asset] of assets)
  assert.equal((await fetch(base + asset)).status, 200);
await call("/api/session", {});
const joined = await call("/api/quizzes/VOCAB-DEMO/join", {
  displayName: "Restart check",
});
const answer = {
  submissionId: crypto.randomUUID(),
  epoch: joined.epoch,
  questionId: joined.currentQuestion.id,
  optionId: joined.currentQuestion.options[0].id,
};
const clock = await call("/api/quizzes/VOCAB-DEMO/start", {
  epoch: joined.epoch,
  questionId: joined.currentQuestion.id,
});
const accepted = await call("/api/quizzes/VOCAB-DEMO/answers", answer);
assert.equal(accepted.outcome, "accepted");
assert.deepEqual(accepted.receipt.question, joined.currentQuestion);
const before = await call("/api/quizzes/VOCAB-DEMO/state");
const restart = spawnSync(
  "docker",
  ["compose", "-p", project, "restart", "redis", "app"],
  { stdio: "inherit", env: process.env },
);
assert.equal(restart.status, 0);
const deadline = Date.now() + 30000;
while (true) {
  try {
    await call("/readyz");
    break;
  } catch (error) {
    if (Date.now() >= deadline) throw error;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
}
const after = await call("/api/quizzes/VOCAB-DEMO/state");
assert.deepEqual(after, before);
const replay = await call("/api/quizzes/VOCAB-DEMO/answers", answer);
assert.equal(replay.outcome, "replayed");
assert.deepEqual(replay.receipt, accepted.receipt);
const resumedClock = await call("/api/quizzes/VOCAB-DEMO/start", {
  epoch: joined.epoch,
  questionId: joined.currentQuestion.id,
});
assert.equal(resumedClock.deadlineMs, clock.deadlineMs);
const metrics = await fetch(base + "/metrics").then((r) => r.text());
for (const metric of [
  "quiz_requests_total",
  "quiz_answers_total",
  "quiz_active_sockets",
  "quiz_answer_duration_seconds",
])
  assert.ok(metrics.includes(metric));
assert.ok(!metrics.includes(cookie));
writeFileSync(
  process.env.COMPOSE_EVIDENCE_PATH ?? "docs/evidence/compose-restart.txt",
  "PASS: container readiness, SPA assets, authenticated join/answer, Redis and app restart, identical retained private snapshot, immutable receipt replay, metrics.\nCredentials were held in memory and not recorded.\n",
);
console.log("Compose smoke and retained-state restart passed.");
