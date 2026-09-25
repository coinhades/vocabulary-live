import { afterEach, describe, expect, it, vi } from "vitest";
import { requestJSON, ServiceError } from "./http";
import {
  learningAudio,
  learningRequest,
  onLearningCredentialsRejected,
} from "./learning-api";

afterEach(() => vi.unstubAllGlobals());

describe("HTTP boundary", () => {
  it("sends one scoped request with cancellation and no automatic retry", async () => {
    const abort = new AbortController();
    const fetch = vi.fn().mockResolvedValue(Response.json({ ok: true }));
    vi.stubGlobal("fetch", fetch);
    const parse = vi.fn((v: unknown) => v);
    expect(await requestJSON("/session", parse, {}, abort.signal)).toEqual({
      ok: true,
    });
    expect(fetch).toHaveBeenCalledTimes(1);
    const options = fetch.mock.calls[0][1] as RequestInit;
    expect(options).toMatchObject({
      method: "POST",
      credentials: "same-origin",
      cache: "no-store",
      body: "{}",
    });
    abort.abort();
    expect(options.signal?.aborted).toBe(true);
    expect(parse).toHaveBeenCalledOnce();
  });

  it("rejects a proxy HTML response without passing it to the application", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response("<html>Gateway error</html>", {
          status: 502,
          headers: { "Content-Type": "text/html" },
        }),
      ),
    );
    const parse = vi.fn();
    await expect(requestJSON("/state", parse)).rejects.toThrow(
      "unexpected response",
    );
    expect(parse).not.toHaveBeenCalled();
  });

  it("cancels oversized response streams before buffering the rest", async () => {
    const cancel = vi.fn();
    const body = new ReadableStream({
      pull(controller) {
        controller.enqueue(new Uint8Array(16));
      },
      cancel,
    });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(body, {
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    await expect(
      requestJSON("/state", (v) => v, undefined, undefined, 6000, 20),
    ).rejects.toThrow("too large");
    expect(cancel).toHaveBeenCalledOnce();
  });

  it("retains structured failures and notifies learning credential consumers", async () => {
    const detail = {
      code: "AI_CREDENTIALS_INVALID",
      message: "Learning is unavailable.",
      requestId: "r1",
      retryable: true,
    };
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(Response.json({ error: detail }, { status: 503 })),
    );
    const listener = vi.fn();
    const stop = onLearningCredentialsRejected(listener);
    try {
      await expect(learningRequest("/example", (v) => v, {})).rejects.toEqual(
        new ServiceError(detail),
      );
      expect(listener).toHaveBeenCalledOnce();
    } finally {
      stop();
    }
  });

  it("rejects malformed JSON and oversized audio", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(
        new Response("{", { headers: { "Content-Type": "application/json" } }),
      )
      .mockResolvedValueOnce(
        new Response(new Uint8Array(1048577), {
          headers: { "Content-Type": "audio/mpeg" },
        }),
      );
    vi.stubGlobal("fetch", fetch);
    await expect(requestJSON("/state", (v) => v)).rejects.toThrow();
    await expect(
      learningAudio("/pronunciation", "epoch", new AbortController().signal),
    ).rejects.toThrow("too large");
  });
});
