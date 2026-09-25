import type { QuestionClock } from "../types/protocol";

// Anchor Redis time to this page's monotonic clock. Local wall-clock and
// session-storage edits cannot extend a question's scoring deadline.
export function countdown(
  clock: QuestionClock | null,
  receivedAt: number,
  now: number,
  durationSeconds: number,
) {
  if (!clock) return { seconds: durationSeconds, fraction: 1 };
  const remaining = Math.max(
    0,
    Math.min(
      durationSeconds * 1000,
      clock.deadlineMs - clock.serverTimeMs - Math.max(0, now - receivedAt),
    ),
  );
  return {
    seconds: Math.ceil(remaining / 1000),
    fraction: remaining / (durationSeconds * 1000),
  };
}
