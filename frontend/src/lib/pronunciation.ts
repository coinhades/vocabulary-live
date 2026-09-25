export type AudioStatus =
  "unavailable" | "idle" | "loading" | "ready-to-play" | "playing" | "error";
export interface AudioState {
  key: string;
  status: AudioStatus;
  message: string;
}
export interface AudioDependencies {
  createAudio: () => HTMLAudioElement;
  fetch: (path: string, epoch: string, signal: AbortSignal) => Promise<Blob>;
  createURL: (blob: Blob) => string;
  revokeURL: (url: string) => void;
}
interface Clip {
  url: string;
  size: number;
  expires: number;
}
// One controller per mounted app, one active audio element, bounded memory only.
export class PronunciationController {
  private audio?: HTMLAudioElement;
  private clips = new Map<string, Clip>();
  private abort?: AbortController;
  private sequence = 0;
  private bytes = 0;
  constructor(
    readonly state: AudioState,
    private deps: AudioDependencies,
  ) {}
  stop() {
    this.sequence++;
    this.abort?.abort();
    this.abort = undefined;
    if (this.audio) {
      this.audio.pause();
      this.audio.onended = null;
      this.audio.onerror = null;
      this.audio.removeAttribute("src");
      this.audio.load();
    }
    this.state.key = "";
    this.state.status = "idle";
    this.state.message = "";
  }
  clear() {
    this.stop();
    for (const c of this.clips.values()) this.deps.revokeURL(c.url);
    this.clips.clear();
    this.bytes = 0;
  }
  private remove(key: string) {
    const c = this.clips.get(key);
    if (c) {
      this.deps.revokeURL(c.url);
      this.bytes -= c.size;
      this.clips.delete(key);
    }
  }
  async play(key: string, path: string, epoch: string) {
    if (this.state.key === key && this.state.status === "loading") return;
    this.stop();
    const sequence = this.sequence;
    this.state.key = key;
    let clip = this.clips.get(key);
    if (clip && clip.expires < Date.now()) {
      this.remove(key);
      clip = undefined;
    }
    if (!clip) {
      const abort = new AbortController();
      this.abort = abort;
      this.state.status = "loading";
      this.state.message = "Preparing pronunciation…";
      try {
        const blob = await this.deps.fetch(path, epoch, abort.signal);
        if (sequence !== this.sequence) return;
        while (this.clips.size >= 32 || this.bytes + blob.size > 16 * 1048576) {
          const oldest = this.clips.keys().next().value;
          if (!oldest) break;
          this.remove(oldest);
        }
        clip = {
          url: this.deps.createURL(blob),
          size: blob.size,
          expires: Date.now() + 86400000,
        };
        this.clips.set(key, clip);
        this.bytes += blob.size;
      } catch (e) {
        if (sequence !== this.sequence) return;
        this.state.status = "error";
        this.state.message =
          e instanceof Error
            ? e.message
            : "Pronunciation is unavailable. Try again.";
        return;
      }
    } else {
      this.clips.delete(key);
      this.clips.set(key, clip);
    }
    if (sequence !== this.sequence) return;
    const audio = (this.audio ??= this.deps.createAudio());
    audio.src = clip.url;
    audio.currentTime = 0;
    audio.onended = () => {
      if (sequence === this.sequence) {
        this.state.status = "ready-to-play";
        this.state.message = "Ready — tap to play again.";
      }
    };
    audio.onerror = () => {
      if (sequence === this.sequence) {
        audio.pause();
        audio.removeAttribute("src");
        this.remove(key);
        this.state.status = "error";
        this.state.message = "This audio could not be decoded. Tap to retry.";
      }
    };
    try {
      await audio.play();
      if (sequence === this.sequence && this.state.status !== "error") {
        this.state.status = "playing";
        this.state.message = "Playing pronunciation.";
      }
    } catch (e) {
      if (sequence !== this.sequence || this.state.status === "error") return;
      this.state.status = "ready-to-play";
      this.state.message =
        e instanceof DOMException && e.name === "NotAllowedError"
          ? "Ready — tap to play."
          : "Playback paused. Tap to play again.";
    }
  }
}
