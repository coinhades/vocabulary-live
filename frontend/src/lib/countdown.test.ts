import { describe, expect, it } from "vitest";
import { countdown } from "./countdown";
const clock = {
  epoch: "epoch",
  questionId: "word",
  serverTimeMs: 100000,
  deadlineMs: 120000,
};
describe("authoritative countdown presentation", () => {
  it("moves the bar continuously within a displayed second", () => {
    const first = countdown(clock, 1000, 1500, 20);
    const next = countdown(clock, 1000, 1516, 20);
    expect(first.seconds).toBe(next.seconds);
    expect(next.fraction).toBeLessThan(first.fraction);
    expect(first.fraction - next.fraction).toBeCloseTo(0.016 / 20);
  });
  it("uses elapsed monotonic time and catches up after a background-tab gap", () => {
    expect(countdown(clock, 1000, 16000, 20)).toEqual({
      seconds: 5,
      fraction: 0.25,
    });
    expect(countdown(clock, 1000, 99000, 20)).toEqual({
      seconds: 0,
      fraction: 0,
    });
  });
  it("restores the remaining server deadline without granting new time", () => {
    const restored = { ...clock, serverTimeMs: 119500 };
    expect(countdown(restored, 10, 10, 20)).toEqual({
      seconds: 1,
      fraction: 0.025,
    });
    expect(countdown(restored, 10, 510, 20)).toEqual({
      seconds: 0,
      fraction: 0,
    });
  });
  it("bounds progress while waiting for the server or after expiry", () => {
    expect(countdown(null, 0, 9000, 20)).toEqual({ seconds: 20, fraction: 1 });
    expect(countdown({ ...clock, serverTimeMs: 130000 }, 0, 1, 20)).toEqual({
      seconds: 0,
      fraction: 0,
    });
  });
});
