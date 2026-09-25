<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import type { State } from "../types/protocol";
import type { ReviewState } from "../types/learning";
import { parseReview } from "../types/learning";
import { learningRequest, learningError } from "../lib/learning-api";
import { useLearning } from "../composables/useLearning";
import ReviewPlayer from "./ReviewPlayer.vue";
const props = defineProps<{ state: State }>();
const emit = defineEmits<{ active: [boolean] }>();
const learning = useLearning();
const review = ref<ReviewState | null>(null),
  open = ref(false),
  busy = ref(false),
  useAI = ref(false),
  error = ref("");
let abort: AbortController | undefined;
let sequence = 0;
watch(open, (active) => emit("active", active));
let lastPayload = "",
  actionId = "";
onMounted(async () => {
  abort = new AbortController();
  const seq = sequence;
  try {
    const r = await learningRequest(
      `/quizzes/${encodeURIComponent(props.state.quizId)}/review`,
      (v) => (v === null ? null : parseReview(v)),
      undefined,
      abort.signal,
    );
    if (seq === sequence) review.value = r;
  } catch {
    /* Starting/recovering explicitly remains available. */
  }
});
onUnmounted(() => {
  sequence++;
  abort?.abort();
});
async function start() {
  if (review.value) {
    open.value = true;
    await nextTick();
    document.getElementById("review-question-heading")?.focus();
    return;
  }
  if (busy.value) return;
  const body = {
    epoch: props.state.epoch,
    extraPractice: props.state.correctCount === props.state.totalQuestions,
    useAI: useAI.value,
  };
  const signature = JSON.stringify(body);
  if (signature !== lastPayload) {
    actionId = crypto.randomUUID();
    lastPayload = signature;
  }
  abort?.abort();
  abort = new AbortController();
  const seq = ++sequence;
  busy.value = true;
  error.value = "";
  try {
    const r = await learningRequest(
      `/quizzes/${encodeURIComponent(props.state.quizId)}/reviews`,
      parseReview,
      { ...body, clientActionId: actionId },
      abort.signal,
    );
    if (seq !== sequence) return;
    if (
      r.epoch !== props.state.epoch ||
      r.contentVersion !== props.state.contentVersion
    )
      throw new Error("Outdated review");
    review.value = r;
    open.value = true;
    await nextTick();
    document.getElementById("review-question-heading")?.focus();
  } catch (e) {
    if (seq === sequence) error.value = learningError(e);
  } finally {
    if (seq === sequence) busy.value = false;
  }
}
</script>
<template>
  <div class="review-launcher">
    <ReviewPlayer
      v-if="open && review"
      :review="review"
      @update="review = $event"
      @close="open = false"
    />
    <section
      v-else
      class="learning-content"
      aria-label="Private practice review"
    >
      <h3>
        {{
          state.correctCount === state.totalQuestions
            ? "Optional extra practice"
            : "Practice the words you missed"
        }}
      </h3>
      <p>
        Private review uses completed words. It never changes leaderboard
        points.
      </p>
      <label
        v-if="!review && learning.capabilities.value.textConfigured"
        class="review-ai-choice"
        ><input v-model="useAI" type="checkbox" /> Create new contexts with
        AI</label
      ><button
        type="button"
        class="learning-button"
        :disabled="busy"
        @click="start"
      >
        {{
          busy
            ? "Creating review questions…"
            : review
              ? "Resume practice review"
              : state.correctCount === state.totalQuestions
                ? "Start extra practice"
                : "Start practice review"
        }}
      </button>
      <p v-if="busy" role="status">Creating your private review…</p>
      <p v-if="error" class="learning-error" role="status">{{ error }}</p>
      <p v-if="useAI" class="learning-disclosure">
        Requested vocabulary context is sent to OpenAI. Provider retention
        policies apply. Invalid or unavailable generation uses canonical
        questions.
      </p>
    </section>
  </div>
</template>
