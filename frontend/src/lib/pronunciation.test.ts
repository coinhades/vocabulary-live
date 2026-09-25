import { describe, it, expect, vi } from "vitest";
import { PronunciationController, type AudioState } from "./pronunciation";
function setup() {
  const audio = {
    pause: vi.fn(),
    load: vi.fn(),
    removeAttribute: vi.fn(),
    play: vi.fn().mockResolvedValue(undefined),
    currentTime: 0,
    src: "",
    onended: null as (() => void) | null,
    onerror: null as (() => void) | null,
  };
  const fetch = vi
    .fn()
    .mockResolvedValue(new Blob(["valid-test-audio"], { type: "audio/mpeg" }));
  const revokeURL = vi.fn();
  const state: AudioState = { key: "", status: "idle", message: "" };
  const controller = new PronunciationController(state, {
    createAudio: () => audio as unknown as HTMLAudioElement,
    fetch,
    createURL: vi.fn(() => "blob:test"),
    revokeURL,
  });
  return { audio, fetch, revokeURL, state, controller };
}
describe("app pronunciation", () => {
  it("retains blocked audio and replays/restarts without generation", async () => {
    const s = setup();
    s.audio.play.mockRejectedValueOnce(
      new DOMException("blocked", "NotAllowedError"),
    );
    await s.controller.play("word", "/audio", "epoch");
    expect(s.state.status).toBe("ready-to-play");
    expect(s.state.message).toBe("Ready — tap to play.");
    await s.controller.play("word", "/audio", "epoch");
    expect(s.state.status).toBe("playing");
    await s.controller.play("word", "/audio", "epoch");
    expect(s.fetch).toHaveBeenCalledTimes(1);
    expect(s.audio.currentTime).toBe(0);
    expect(s.revokeURL).not.toHaveBeenCalled();
    s.controller.clear();
    expect(s.revokeURL).toHaveBeenCalledWith("blob:test");
  });
  it("suppresses repeated loading and ignores responses after navigation", async () => {
    const s = setup();
    let resolve!: (b: Blob) => void;
    s.fetch.mockImplementation(() => new Promise<Blob>((r) => (resolve = r)));
    const p = s.controller.play("old", "/audio", "epoch");
    await s.controller.play("old", "/audio", "epoch");
    expect(s.fetch).toHaveBeenCalledTimes(1);
    s.controller.stop();
    resolve(new Blob(["late"]));
    await p;
    expect(s.audio.play).not.toHaveBeenCalled();
    expect(s.state.status).toBe("idle");
  });
  it("handles decoder errors and detaches listeners on cleanup", async () => {
    const s = setup();
    await s.controller.play("word", "/audio", "epoch");
    s.audio.onerror?.();
    expect(s.state.status).toBe("error");
    expect(s.revokeURL).toHaveBeenCalledTimes(1);
    s.controller.clear();
    expect(s.audio.onended).toBeNull();
    expect(s.audio.onerror).toBeNull();
  });
  it("does not replace a decoder failure with a late play outcome", async () => {
    const s = setup();
    s.audio.play.mockImplementationOnce(async () => {
      s.audio.onerror?.();
      throw new DOMException("decode failed", "NotSupportedError");
    });
    await s.controller.play("word", "/audio", "epoch");
    expect(s.state.status).toBe("error");
    expect(s.state.message).toContain("decoded");
    expect(s.revokeURL).toHaveBeenCalledTimes(1);
  });
});
