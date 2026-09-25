import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QuizMachine } from "./quiz-machine";
import type { Socket } from "./quiz-machine";
import type { Answer, Receipt, State, Question } from "../types/protocol";
import { parseState, parseReceipt, parseClock } from "../types/protocol";
import { ServiceError } from "./api";

// This is a deterministic fake server plan. Production has no full questions array.
const order = ["q6", "q2", "q8", "q1", "q4", "q7", "q3", "q5"];
function question(id: string): Question {
  return {
    id,
    prompt: `Prompt ${id}`,
    options: ["d", "b", "a", "c"].map((id) => ({ id, text: `Option ${id}` })),
    timing: { difficulty: "medium", durationSeconds: 30 },
  };
}
const policies = {
  mode: "LIVE_PRACTICE",
  feedbackPolicy: "IMMEDIATE",
  questionOrderPolicy: "PER_PARTICIPANT_SHUFFLED",
  answerOrderPolicy: "PER_PARTICIPANT_SHUFFLED",
  leaderboardPolicy: "LIVE_ALL",
  scoringPolicy: "FIRST_ANSWER_TIMED_100_50",
} as const;
function receipt(a: Answer, total = 100, version = 2): Receipt {
  return {
    submissionId: a.submissionId,
    epoch: a.epoch,
    questionId: a.questionId,
    quizId: "VOCAB-DEMO",
    selectedOptionId: a.optionId,
    correctness: a.optionId === "b",
    timedOut: false,
    pointsAwarded: a.optionId === "b" ? 100 : 0,
    totalScoreAtAcceptance: total,
    acceptedVersion: version,
    correctOptionId: "b",
    explanation: "A useful explanation.",
    question: question(a.questionId),
    questionNumber: order.indexOf(a.questionId) + 1,
    vocabulary: {
      word: "example",
      pronunciation: "/ɪɡˈzæmpəl/",
      partOfSpeech: "noun",
      definition: "An illustration.",
    },
  };
}
function accepted(n = 0, option = "b", total = (n + 1) * 100) {
  return receipt(
    {
      submissionId: `request-${n}`,
      epoch: "epoch1",
      questionId: order[n],
      optionId: option,
    },
    total,
    n + 2,
  );
}
function state(
  receipts: Receipt[] = [],
  version = receipts.length + 1,
  epoch = "epoch1",
  quizId = "VOCAB-DEMO",
): State {
  const current = order.find(
    (id) => !receipts.some((r) => r.questionId === id),
  );
  const score = receipts.reduce((sum, r) => sum + r.pointsAwarded, 0);
  return {
    quizId,
    title: "Test words",
    contentVersion: "content-v2",
    totalQuestions: 8,
    pointsPerCorrectAnswer: 100,
    pointsAfterTimeout: 50,
    policies,
    epoch,
    version,
    participantCount: 1,
    participantId: "p1",
    answeredCount: receipts.length,
    score,
    completed: !current,
    correctCount: receipts.filter((r) => r.correctness).length,
    accuracy:
      Math.round((receipts.filter((r) => r.correctness).length / 8) * 1000) /
      10,
    currentRank: 1,
    questionNumber: current ? order.indexOf(current) + 1 : 0,
    currentQuestion: current ? question(current) : null,
    leaderboard: [
      {
        participantId: "p1",
        displayName: "You",
        score,
        rank: 1,
        answeredCount: receipts.length,
      },
    ],
    receipts,
  };
}
function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}
function setup(storage = new Map<string, string>(), initial = state()) {
  const sockets: Socket[] = [];
  let current = initial;
  let uuid = 0;
  const clocks = new Map<string, number>();
  const transport = {
    start: vi.fn(async (_id: string, epoch: string, questionId: string) => {
      const key = `${epoch}.${questionId}`;
      const deadlineMs = clocks.get(key) ?? Date.now() + 30000;
      clocks.set(key, deadlineMs);
      return { epoch, questionId, deadlineMs, serverTimeMs: Date.now() };
    }),
    session: vi.fn(async () => "p1"),
    join: vi.fn(async () => structuredClone(current)),
    state: vi.fn(async () => structuredClone(current)),
    answer: vi.fn(async (_id: string, a: Answer) => {
      const previous = current.receipts.find(
        (r) => r.questionId === a.questionId,
      );
      if (previous) return { outcome: "replayed" as const, receipt: previous };
      const r = receipt(
        a,
        current.score + (a.optionId === "b" ? 100 : 0),
        current.version + 1,
      );
      current = state([...current.receipts, r], current.version + 1);
      return { outcome: "accepted" as const, receipt: r };
    }),
  };
  const machine = new QuizMachine({
    transport,
    storage: {
      getItem: (key) => storage.get(key) ?? null,
      setItem: (key, value) => {
        storage.set(key, value);
      },
      removeItem: (key) => {
        storage.delete(key);
      },
    },
    uuid: () => `00000000-0000-4000-8000-${String(++uuid).padStart(12, "0")}`,
    now: () => Date.now(),
    random: () => 0.5,
    socket: () => {
      const socket: Socket = {
        onopen: null,
        onmessage: null,
        onclose: null,
        onerror: null,
        close: vi.fn(),
      };
      sockets.push(socket);
      return socket;
    },
  });
  return {
    machine,
    transport,
    sockets,
    storage,
    current: (s: State) => {
      current = s;
    },
    read: () => structuredClone(current),
  };
}
async function start(s: ReturnType<typeof setup>) {
  await s.machine.join("VOCAB-DEMO", "Player");
  s.sockets.at(-1)?.onopen?.();
  s.machine.receive(s.read());
}

describe("authoritative practice state machine", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
  });

  it("keeps idle, first connection and normal private synchronization free of warnings", async () => {
    const s = setup();
    expect(s.machine.networkNotice).toBe("");
    await s.machine.join("VOCAB-DEMO", "Player");
    expect(s.machine.connection).toBe("connecting");
    expect(s.machine.networkNotice).toBe("");
    s.sockets[0].onopen?.();
    await vi.advanceTimersByTimeAsync(5000);
    expect(s.machine.connection).toBe("synchronizing");
    expect(s.machine.networkNotice).toBe("");
    s.machine.receive(s.read());
    const updated = state([accepted()]);
    const d = deferred<State>();
    s.transport.state.mockImplementationOnce(() => d.promise);
    s.machine.receive(updated);
    expect(s.machine.connection).toBe("synchronizing");
    expect(s.machine.networkNotice).toBe("");
    d.resolve(updated);
    await vi.advanceTimersByTimeAsync(0);
    s.machine.receive(updated);
    expect(s.machine.connection).toBe("live");
    expect(s.machine.networkNotice).toBe("");
    s.machine.leave();
    expect(s.machine.networkNotice).toBe("");
  });

  it("refreshes liveness without replacing unchanged state or standings", async () => {
    const s = setup();
    await start(s);
    const original = s.machine.state;
    const rows = original?.leaderboard;
    s.sockets[0].onclose?.();
    s.machine.receive(s.read());
    expect(s.machine.connection).toBe("live");
    expect(s.machine.state).toBe(original);
    expect(s.machine.state?.leaderboard).toBe(rows);
    expect(s.machine.networkNotice).toBe("");
  });

  it("coalesces public recovery requests while a private snapshot is in flight", async () => {
    const s = setup();
    await start(s);
    const d = deferred<State>();
    s.transport.state.mockImplementationOnce(() => d.promise);
    const updated = state([accepted()]);
    for (let i = 0; i < 20; i++) s.machine.receive(updated);
    expect(s.transport.state).toHaveBeenCalledTimes(1);
    d.resolve(updated);
    await vi.advanceTimersByTimeAsync(0);
    s.machine.receive(updated);
    expect(s.machine.connection).toBe("live");
    expect(s.machine.feedback?.submissionId).toBe(accepted().submissionId);
  });

  it("keeps accepted feedback and safe reload recovery when storage cleanup fails", async () => {
    const s = setup();
    await start(s);
    vi.spyOn(s.storage, "delete").mockImplementation(() => {
      throw new Error("Storage unavailable");
    });
    s.machine.selected = "b";
    await s.machine.submit();
    expect(s.machine.pending).toBeNull();
    expect(s.machine.feedback?.correctness).toBe(true);
    expect(s.machine.me?.score).toBe(100);
    const restored = setup(s.storage, s.read());
    await start(restored);
    expect(restored.machine.feedback).toEqual(s.machine.feedback);
    expect(restored.transport.answer).not.toHaveBeenCalled();
    expect(() => restored.machine.leave()).not.toThrow();
  });

  it("does not send an answer when its recovery intent cannot be persisted", async () => {
    const s = setup();
    await start(s);
    vi.spyOn(s.storage, "set").mockImplementation(() => {
      throw new Error("Quota exceeded");
    });
    s.machine.selected = "b";
    await s.machine.submit();
    expect(s.transport.answer).not.toHaveBeenCalled();
    expect(s.machine.pending).toBeNull();
    expect(s.machine.message).toContain("Browser storage is unavailable");
  });

  it("rejects inconsistent completion metrics and progress beyond published quiz size", () => {
    const valid = state([accepted()]);
    expect(() => parseState({ ...valid, correctCount: 0 })).toThrow();
    expect(() => parseState({ ...valid, accuracy: 90 })).toThrow();
    expect(() =>
      parseState({ ...valid, totalQuestions: 4, questionNumber: 7 }),
    ).toThrow();
  });

  it("keeps an outage warning through retries and clears it only after a current snapshot", async () => {
    const s = setup();
    await start(s);
    s.sockets[0].onclose?.();
    const warning = s.machine.networkNotice;
    expect(warning).toContain("last known scores");
    await vi.advanceTimersByTimeAsync(500);
    expect(s.machine.connection).toBe("connecting");
    expect(s.machine.networkNotice).toBe(warning);
    s.sockets.at(-1)?.onopen?.();
    expect(s.machine.connection).toBe("synchronizing");
    await s.machine.retry();
    expect(s.machine.networkNotice).toBe(warning);
    s.machine.receive(state([], 0));
    expect(s.machine.networkNotice).toBe(warning);
    s.machine.receive(s.read());
    expect(s.machine.connection).toBe("live");
    expect(s.machine.networkNotice).toBe("");
  });

  it("warns when initial synchronization stalls and cancels that warning when leaving", async () => {
    const s = setup();
    await s.machine.join("VOCAB-DEMO", "Player");
    s.sockets[0].onopen?.();
    await vi.advanceTimersByTimeAsync(6000);
    expect(s.machine.networkNotice).toContain("last known scores");
    s.machine.leave();
    expect(s.machine.networkNotice).toBe("");
    await s.machine.join("VOCAB-DEMO", "Player");
    expect(s.machine.connection).toBe("connecting");
    expect(s.machine.networkNotice).toBe("");
  });

  it("still shows explicit service errors and uses dedicated session recovery instead of a connection banner", async () => {
    const s = setup();
    await start(s);
    s.transport.state.mockRejectedValueOnce(new Error("Service offline"));
    await s.machine.retry();
    expect(s.machine.networkNotice).toContain("service could not be reached");
    s.sockets[0].onmessage?.({
      data: JSON.stringify({ type: "status", code: "SESSION_EXPIRED" }),
    });
    expect(s.machine.connection).toBe("expired");
    expect(s.machine.networkNotice).toBe("");
    expect(s.machine.message).toContain("session has expired");
  });

  it("keeps timer failure recoverable even while live scores arrive", async () => {
    const s = setup();
    s.transport.start.mockRejectedValueOnce(new Error("Lost start response"));
    await start(s);
    expect(s.machine.connection).toBe("live");
    expect(s.machine.clockMessage).toContain("Retry");
    expect(s.machine.networkNotice).toContain("question timer");
    expect(s.machine.canSelect).toBe(false);
    await s.machine.retry();
    expect(s.machine.clockMessage).toBe("");
    expect(s.machine.networkNotice).toBe("");
    expect(s.machine.canSelect).toBe(true);
  });

  it("starts the next clock only after feedback is explicitly dismissed", async () => {
    const s = setup();
    await start(s);
    const first = s.machine.clock;
    await s.machine.retry();
    expect(s.machine.clock).toBe(first);
    expect(s.transport.start).toHaveBeenCalledTimes(1);
    s.machine.selected = "b";
    await s.machine.submit();
    await vi.advanceTimersByTimeAsync(60000);
    await s.machine.retry();
    expect(s.transport.start).toHaveBeenCalledTimes(1);
    s.machine.next();
    await Promise.resolve();
    expect(s.transport.start).toHaveBeenCalledTimes(2);
    expect(s.machine.clock?.questionId).toBe("q2");
  });

  it("validates late 50-point receipts and restores them as correct feedback", async () => {
    const r = { ...accepted(0, "b", 50), timedOut: true, pointsAwarded: 50 };
    expect(parseReceipt(r)).toEqual(r);
    expect(() => parseReceipt({ ...r, pointsAwarded: 100 })).toThrow();
    expect(() => parseReceipt({ ...r, timedOut: false })).toThrow();
    const s = setup(new Map(), parseState(state([r])));
    await start(s);
    expect(s.machine.feedback?.pointsAwarded).toBe(50);
    expect(s.machine.correctCount).toBe(1);
    expect(s.machine.me?.score).toBe(50);
    expect(s.transport.start).not.toHaveBeenCalled();
    expect(
      parseClock({
        epoch: "epoch1",
        questionId: "q6",
        deadlineMs: 1000,
        serverTimeMs: 2000,
      }).deadlineMs,
    ).toBe(1000);
    expect(() =>
      parseClock({
        epoch: "epoch1",
        questionId: "q6",
        deadlineMs: 0,
        serverTimeMs: 2000,
      }),
    ).toThrow();
  });

  it("renders only the server current question and advances to its next ID after explicit Next", async () => {
    const s = setup();
    await start(s);
    expect(s.machine.question?.id).toBe("q6");
    expect(s.machine.state).not.toHaveProperty("questions");
    expect(s.machine.action).toBe("READY");
    s.machine.selected = "b";
    expect(s.machine.action).toBe("SELECTED");
    await s.machine.submit();
    expect(s.machine.action).toBe("FEEDBACK");
    expect(s.machine.question?.id).toBe("q6");
    expect(s.machine.state?.currentQuestion?.id).toBe("q2");
    s.machine.next();
    expect(s.machine.question?.id).toBe("q2");
    expect(s.machine.questionNumber).toBe(2);
  });

  it("requires a synchronized snapshot before calling a socket live, and rejects stale versions", async () => {
    const s = setup();
    await s.machine.join("VOCAB-DEMO", "Player");
    s.sockets[0].onopen?.();
    expect(s.machine.connection).toBe("synchronizing");
    s.machine.receive(state([], 3));
    s.machine.receive(state([], 2));
    expect(s.machine.state?.version).toBe(3);
    expect(s.machine.connection).toBe("live");
  });

  it("persists and freezes one intent before sending without optimistically scoring", async () => {
    const s = setup();
    await start(s);
    const d = deferred<{ outcome: "accepted"; receipt: Receipt }>();
    s.transport.answer.mockImplementationOnce(() => d.promise);
    s.machine.selected = "b";
    const submitting = s.machine.submit();
    expect(s.machine.action).toBe("SUBMITTING");
    expect(s.machine.canSelect).toBe(false);
    expect(s.machine.feedback).toBeNull();
    expect(s.machine.me?.score).toBe(0);
    expect(s.storage.has("vocabulary.pending.VOCAB-DEMO")).toBe(true);
    await s.machine.submit();
    expect(s.transport.answer).toHaveBeenCalledTimes(1);
    const r = receipt(s.transport.answer.mock.calls[0][1]);
    s.current(state([r]));
    d.resolve({ outcome: "accepted", receipt: r });
    await submitting;
    expect(s.machine.me?.score).toBe(100);
    expect(s.machine.pending).toBeNull();
  });

  it("checks private state before retrying exactly the same submission ID and payload", async () => {
    const s = setup();
    await start(s);
    s.transport.answer.mockRejectedValueOnce(new TypeError("connection lost"));
    s.machine.selected = "b";
    await s.machine.submit();
    const original = s.transport.answer.mock.calls[0][1];
    expect(s.machine.submission).toBe("unconfirmed");
    expect(s.machine.feedback).toBeNull();
    expect(s.machine.pending?.optionId).toBe("b");
    await s.machine.retry();
    expect(s.transport.state).toHaveBeenCalled();
    expect(s.transport.answer.mock.calls[1][1]).toEqual(original);
    expect(s.machine.pending).toBeNull();
  });

  it("recovers the original accepted receipt after a lost HTTP response without resending", async () => {
    const s = setup();
    await start(s);
    s.transport.answer.mockImplementationOnce(async (_id, a) => {
      s.current(state([receipt(a)]));
      throw new TypeError("response lost");
    });
    s.machine.selected = "b";
    await s.machine.submit();
    await s.machine.retry();
    expect(s.transport.answer).toHaveBeenCalledTimes(1);
    expect(s.machine.feedback).toEqual(s.read().receipts[0]);
    expect(s.machine.me?.score).toBe(100);
  });

  it("ignores a late failure when a private snapshot already confirmed the pending answer", async () => {
    const s = setup();
    await start(s);
    const d = deferred<void>();
    s.transport.answer.mockImplementationOnce(async () => {
      await d.promise;
      throw new TypeError("late response loss");
    });
    s.machine.selected = "b";
    const submitting = s.machine.submit();
    s.current(state([receipt(s.transport.answer.mock.calls[0][1])]));
    s.machine.receive(s.read());
    await vi.advanceTimersByTimeAsync(0);
    expect(s.machine.pending).toBeNull();
    d.resolve();
    await submitting;
    expect(s.machine.action).toBe("FEEDBACK");
    expect(s.machine.me?.score).toBe(100);
  });

  it("reload resumes pending intent and the exact option order without inventing a UUID", async () => {
    const s = setup();
    s.storage.set(
      "vocabulary.pending.VOCAB-DEMO",
      JSON.stringify({
        quizId: "VOCAB-DEMO",
        participantId: "p1",
        submissionId: "stable",
        epoch: "epoch1",
        questionId: "q6",
        optionId: "b",
      }),
    );
    await start(s);
    expect(s.transport.answer.mock.calls[0][1].submissionId).toBe("stable");
    expect(s.machine.feedback?.question.options).toEqual(
      question("q6").options,
    );
  });

  it("restores unread feedback across reload and remembers explicit dismissal", async () => {
    const s = setup();
    await start(s);
    s.machine.selected = "b";
    await s.machine.submit();
    s.machine.stop();
    const restored = setup(s.storage, s.read());
    await start(restored);
    expect(restored.machine.feedback?.questionId).toBe("q6");
    restored.machine.next();
    restored.machine.stop();
    const dismissed = setup(s.storage, s.read());
    await start(dismissed);
    expect(dismissed.machine.feedback).toBeNull();
    expect(dismissed.machine.question?.id).toBe("q2");
  });

  it("preserves reversible selection and question order through WebSocket loss and reconnect", async () => {
    const s = setup();
    await start(s);
    s.machine.selected = "c";
    const before = structuredClone(s.machine.question);
    s.sockets[0].onclose?.();
    expect(s.machine.selected).toBe("c");
    expect(s.machine.canSelect).toBe(true);
    await vi.advanceTimersByTimeAsync(500);
    s.sockets.at(-1)?.onopen?.();
    s.machine.receive(s.read());
    expect(s.machine.question).toEqual(before);
    expect(s.machine.selected).toBe("c");
  });

  it("preserves an unknown answer outcome when a socket update cannot be verified", async () => {
    const s = setup();
    await start(s);
    s.transport.answer.mockRejectedValueOnce(new TypeError("response lost"));
    s.machine.selected = "b";
    await s.machine.submit();
    const outcomeNotice = s.machine.message;
    expect(s.machine.submission).toBe("unconfirmed");
    s.sockets[0].onmessage?.({ data: "{malformed" });
    expect(s.machine.connection).toBe("reconnecting");
    expect(s.machine.networkMessage).toContain("could not be verified");
    expect(s.machine.message).toBe(outcomeNotice);
    expect(s.machine.selected).toBe("b");
    expect(s.machine.feedback).toBeNull();
    expect(s.transport.answer).toHaveBeenCalledTimes(1);
    await s.machine.retry();
    s.machine.receive(s.read());
    expect(s.machine.feedback?.correctness).toBe(true);
    expect(s.machine.networkMessage).toBe("");
    expect(s.machine.message).toBe("");
    expect(s.transport.answer.mock.calls[1]).toEqual(
      s.transport.answer.mock.calls[0],
    );
  });

  it("accepts an HTTP answer while standings reconnect and keeps synchronization separate", async () => {
    const s = setup();
    await start(s);
    s.sockets[0].onclose?.();
    s.machine.selected = "b";
    await s.machine.submit();
    expect(s.machine.me?.score).toBe(100);
    expect(s.machine.feedback?.correctness).toBe(true);
    expect(s.machine.connection).toBe("reconnecting");
  });

  it("wrong acceptance consumes progress once, remains visible, and awards zero", async () => {
    const s = setup();
    await start(s);
    s.machine.selected = "a";
    await s.machine.submit();
    expect(s.machine.feedback?.correctness).toBe(false);
    expect(s.machine.state?.answeredCount).toBe(1);
    expect(s.machine.me?.score).toBe(0);
    await s.machine.submit();
    expect(s.transport.answer).toHaveBeenCalledTimes(1);
    expect(s.machine.question?.id).toBe("q6");
    s.machine.next();
    expect(s.machine.question?.id).toBe("q2");
  });

  it("does not regress current score/progress when an older private read finishes after newer public progress", async () => {
    const s = setup();
    await start(s);
    const d = deferred<State>();
    s.transport.state.mockImplementationOnce(() => d.promise);
    const retry = s.machine.retry();
    s.current(state([accepted(0), accepted(1)]));
    s.machine.receive(s.read());
    await vi.advanceTimersByTimeAsync(0);
    d.resolve(state([accepted(0)]));
    await retry;
    expect(s.machine.me?.score).toBe(200);
    expect(s.machine.state?.answeredCount).toBe(2);
    expect(s.machine.state?.currentQuestion?.id).toBe("q8");
  });

  it("waits for authoritative next state even if the receipt arrived but private refresh failed", async () => {
    const s = setup();
    await start(s);
    s.transport.state.mockRejectedValueOnce(new TypeError("offline"));
    s.machine.selected = "b";
    await s.machine.submit();
    expect(s.machine.feedback?.correctness).toBe(true);
    expect(s.machine.canAdvance).toBe(false);
    s.machine.next();
    expect(s.machine.question?.id).toBe("q6");
    await s.machine.retry();
    expect(s.machine.canAdvance).toBe(true);
  });

  it("ignores late HTTP/socket callbacks after switching quizzes", async () => {
    const s = setup();
    await start(s);
    const oldMessage = s.sockets[0].onmessage;
    const d = deferred<State>();
    s.transport.state.mockImplementationOnce(() => d.promise);
    const retry = s.machine.retry();
    s.current(state([], 1, "travel-epoch", "TRAVEL-DEMO"));
    await s.machine.join("TRAVEL-DEMO", "Player");
    d.resolve(state([accepted()]));
    await retry;
    oldMessage?.({
      data: JSON.stringify({ type: "snapshot", snapshot: state([accepted()]) }),
    });
    expect(s.machine.state?.quizId).toBe("TRAVEL-DEMO");
    expect(s.machine.me?.score).toBe(0);
  });

  it("uses HTTP authority for foreign epochs and never replays an old-epoch saved intent", async () => {
    const s = setup();
    s.storage.set(
      "vocabulary.pending.VOCAB-DEMO",
      JSON.stringify({
        quizId: "VOCAB-DEMO",
        participantId: "p1",
        submissionId: "old",
        epoch: "older",
        questionId: "q6",
        optionId: "b",
      }),
    );
    await start(s);
    expect(s.transport.answer).not.toHaveBeenCalled();
    s.machine.receive(state([], 999, "older"));
    await vi.advanceTimersByTimeAsync(0);
    expect(s.machine.state?.epoch).toBe("epoch1");
  });

  it("completion and reading missed receipts never submit an answer or change competitive score", async () => {
    const receipts = order.map((_, i) =>
      accepted(i, i === 0 ? "a" : "b", i * 100),
    );
    const s = setup(new Map(), state(receipts));
    await start(s);
    expect(s.machine.complete).toBe(false); // Last feedback still awaits explicit Next.
    s.machine.next();
    expect(s.machine.action).toBe("COMPLETED");
    expect(s.machine.missed).toHaveLength(1);
    expect(s.machine.correctCount).toBe(7);
    const before = structuredClone(s.machine.state);
    await s.machine.submit();
    s.machine.next();
    expect(s.transport.answer).not.toHaveBeenCalled();
    expect(s.machine.state).toEqual(before);
  });

  it("requires explicit recovery instead of silently replacing an expired identity", async () => {
    const s = setup();
    await start(s);
    s.machine.leave();
    s.transport.session.mockRejectedValueOnce(
      new ServiceError({
        code: "UNAUTHENTICATED",
        message: "Expired",
        requestId: "r",
        retryable: false,
      }),
    );
    await s.machine.join("VOCAB-DEMO", "Player");
    expect(s.transport.session).toHaveBeenLastCalledWith(false, true);
    expect(s.machine.connection).toBe("expired");
    expect(s.transport.join).toHaveBeenCalledTimes(1);
    await s.machine.restartSession();
    expect(s.transport.session).toHaveBeenCalledWith(true);
  });

  it("bounds reconnection and cleans timers and sockets on leave", async () => {
    const s = setup();
    await start(s);
    await vi.advanceTimersByTimeAsync(6000);
    expect(s.machine.connection).toBe("reconnecting");
    s.machine.leave();
    const count = s.sockets.length;
    await vi.advanceTimersByTimeAsync(60000);
    expect(s.sockets).toHaveLength(count);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("validates current-state, policy, receipt and option-ID consistency at the network boundary", () => {
    expect(parseState(state())).toEqual(state());
    expect(parseState(state([accepted()]))).toEqual(state([accepted()]));
    for (const invalid of [
      { ...state(), participantCount: 7 },
      { ...state(), version: "1" },
      { ...state(), completed: true },
      { ...state(), score: 100 },
      {
        ...state(),
        policies: { ...policies, feedbackPolicy: "AFTER_COMPLETION" },
      },
    ])
      expect(() => parseState(invalid)).toThrow();
    const bad = state();
    bad.currentQuestion!.options[1].id = "d";
    expect(() => parseState(bad)).toThrow();
    expect(() =>
      parseReceipt({ ...accepted(), selectedOptionId: "array-position-1" }),
    ).toThrow();
    const wrongRank = state();
    wrongRank.leaderboard[0].rank = 2;
    expect(() => parseState(wrongRank)).toThrow();
  });
});
