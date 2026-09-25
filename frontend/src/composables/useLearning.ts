import {
  inject,
  onMounted,
  onUnmounted,
  provide,
  reactive,
  ref,
  watch,
  type InjectionKey,
  type Ref,
} from "vue";
import type { Capabilities, LearningScope } from "../types/learning";
import { parseCapabilities } from "../types/learning";
import {
  learningAudio,
  learningRequest,
  onLearningCredentialsRejected,
} from "../lib/learning-api";
import { PronunciationController, type AudioState } from "../lib/pronunciation";
interface LearningContext {
  scope: Ref<LearningScope | null>;
  capabilities: Ref<Capabilities>;
  audio: PronunciationController;
  cache: Map<string, unknown>;
}
const key: InjectionKey<LearningContext> = Symbol("learning");
export function provideLearning(scope: Ref<LearningScope | null>) {
  const capabilities = ref<Capabilities>({
    speechConfigured: false,
    textConfigured: false,
    promptVersion: "learning-v1",
  });
  const audio = new PronunciationController(
    reactive<AudioState>({ key: "", status: "idle", message: "" }),
    {
      createAudio: () => new Audio(),
      fetch: learningAudio,
      createURL: URL.createObjectURL,
      revokeURL: URL.revokeObjectURL,
    },
  );
  const cache = new Map<string, unknown>();
  watch(
    () => JSON.stringify(scope.value),
    () => {
      audio.clear();
      cache.clear();
    },
  );
  const abort = new AbortController();
  let capabilitySequence = 0;
  let refreshing = false;
  let interval: ReturnType<typeof setInterval> | undefined;
  const stopListening = onLearningCredentialsRejected(() => {
    capabilitySequence++;
    capabilities.value = {
      ...capabilities.value,
      speechConfigured: false,
      textConfigured: false,
    };
    audio.clear();
    cache.clear();
  });
  const refresh = () => {
    if (refreshing || abort.signal.aborted) return;
    refreshing = true;
    const sequence = ++capabilitySequence;
    void learningRequest(
      "/learning-capabilities",
      parseCapabilities,
      undefined,
      abort.signal,
    )
      .then((v) => {
        if (sequence !== capabilitySequence) return;
        if (!v.speechConfigured) audio.clear();
        if (!v.textConfigured) cache.clear();
        capabilities.value = v;
      })
      .catch(() => {
        /* Canonical practice remains usable. */
      })
      .finally(() => {
        refreshing = false;
      });
  };
  onMounted(() => {
    refresh();
    interval = setInterval(refresh, 60000);
    window.addEventListener("focus", refresh);
  });
  onUnmounted(() => {
    abort.abort();
    stopListening();
    clearInterval(interval);
    window.removeEventListener("focus", refresh);
    audio.clear();
    cache.clear();
  });
  const context = { scope, capabilities, audio, cache };
  provide(key, context);
  return context;
}
export function useLearning() {
  const context = inject(key);
  if (!context) throw new Error("Learning context missing");
  return context;
}
export function boundedCachePut(
  cache: Map<string, unknown>,
  key: string,
  value: unknown,
) {
  cache.delete(key);
  while (cache.size >= 128) {
    const first = cache.keys().next().value;
    if (first) cache.delete(first);
    else break;
  }
  cache.set(key, value);
}
