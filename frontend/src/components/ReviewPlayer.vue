<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from "vue";
import type { ReviewState } from "../types/learning";
import { parseReview, object, bool } from "../types/learning";
import { learningRequest, learningError } from "../lib/learning-api";
import { useLearning } from "../composables/useLearning";
import PronunciationButton from "./PronunciationButton.vue";
import AnswerOptions from "./AnswerOptions.vue";
const props = defineProps<{ review: ReviewState }>();
const emit = defineEmits<{ update: [ReviewState]; close: [] }>();
const learning = useLearning();
const selected = ref(""),
  busy = ref(false),
  hintBusy = ref(false),
  error = ref(""),
  hintError = ref(""),
  reported = ref(false);
let mutationAbort: AbortController | undefined,
  hintAbort: AbortController | undefined;
let sequence = 0;
let hintSequence = 0;
function cancelHint() {
  hintSequence++;
  hintAbort?.abort();
  hintBusy.value = false;
}
const intents = new Map<string, string>();
const card = computed(() => props.review.currentCard);
watch(
  () => card.value?.id,
  () => {
    selected.value = props.review.feedback?.selectedOptionId ?? "";
    error.value = "";
    hintError.value = "";
    reported.value = false;
    cancelHint();
    learning.audio.stop();
  },
  { immediate: true },
);
onUnmounted(() => {
  sequence++;
  mutationAbort?.abort();
  cancelHint();
  learning.audio.stop();
});
function current(result: ReviewState) {
  return (
    result.reviewSessionId === props.review.reviewSessionId &&
    result.epoch === props.review.epoch &&
    result.version >= props.review.version
  );
}
async function act(operation: "answers" | "advance", skip = false) {
  if (busy.value || !card.value) return;
  const c = card.value;
  cancelHint();
  hintError.value = "";
  const storageKey = `vocabulary.review-intent.${props.review.reviewSessionId}.${c.id}.${operation}`;
  const body = {
    epoch: props.review.epoch,
    ...(operation === "answers"
      ? { optionId: skip ? "" : selected.value, skip }
      : {}),
  };
  const signature = JSON.stringify([c.id, operation, body]);
  let actionId = intents.get(signature) ?? crypto.randomUUID();
  try {
    const saved = sessionStorage.getItem(storageKey);
    if (saved) {
      const prior: unknown = JSON.parse(saved);
      const p = object(prior);
      if (p.signature === signature && typeof p.actionId === "string")
        actionId = p.actionId;
    }
    sessionStorage.setItem(storageKey, JSON.stringify({ signature, actionId }));
  } catch {
    /* Business uniqueness still protects the card. */
  }
  intents.set(signature, actionId);
  busy.value = true;
  error.value = "";
  mutationAbort = new AbortController();
  const seq = sequence;
  try {
    const result = await learningRequest(
      `/reviews/${props.review.reviewSessionId}/cards/${c.id}/${operation}`,
      parseReview,
      { ...body, clientActionId: actionId },
      mutationAbort.signal,
    );
    if (seq !== sequence || !current(result)) return;
    emit("update", result);
    try {
      sessionStorage.removeItem(storageKey);
    } catch {
      /* storage optional */
    }
    if (operation === "advance") {
      await nextTick();
      document.getElementById("review-question-heading")?.focus();
    }
  } catch (e) {
    if (seq === sequence) error.value = learningError(e);
  } finally {
    if (seq === sequence) busy.value = false;
  }
}
async function hint() {
  if (hintBusy.value || !card.value) return;
  const id = card.value.id;
  hintBusy.value = true;
  hintError.value = "";
  hintAbort = new AbortController();
  const seq = sequence;
  const hintSeq = ++hintSequence;
  try {
    const result = await learningRequest(
      `/reviews/${props.review.reviewSessionId}/cards/${id}/hint`,
      parseReview,
      { epoch: props.review.epoch },
      hintAbort.signal,
    );
    if (
      seq === sequence &&
      hintSeq === hintSequence &&
      card.value?.id === id &&
      current(result)
    )
      emit("update", result);
  } catch (e) {
    if (
      seq === sequence &&
      hintSeq === hintSequence &&
      !hintAbort.signal.aborted
    )
      hintError.value = learningError(e);
  } finally {
    if (seq === sequence && hintSeq === hintSequence) hintBusy.value = false;
  }
}
async function report() {
  if (!card.value) return;
  const reportCard = card.value.id,
    reportSequence = sequence;
  try {
    await learningRequest(
      `/learning-artifacts/${card.value.artifactId}/reports`,
      (v) => bool(object(v).reported),
      { reason: "confusing" },
    );
    if (reportSequence === sequence && card.value?.id === reportCard)
      reported.value = true;
  } catch (e) {
    if (reportSequence === sequence && card.value?.id === reportCard)
      error.value = learningError(e);
  }
}
</script>
<template>
  <section class="review-player panel" aria-labelledby="private-review-heading">
    <div class="learning-heading">
      <h2 id="private-review-heading">Unscored review</h2>
      <button type="button" class="learning-button" @click="emit('close')">
        Back to results
      </button>
    </div>
    <p class="review-policy">Practice review - no leaderboard points.</p>
    <p class="learning-disclosure">
      Missed words first; a review mistake may add one reinforcement. Up to six
      cards. Saved for 24 hours.
    </p>
    <p v-if="review.fallback" class="learning-disclosure">
      {{ review.fallback }}
    </p>
    <template v-if="card">
      <p class="eyebrow">
        CARD {{ review.position }} OF {{ review.totalCards }}
        <span v-if="card.reinforcement">· Another look</span>
      </p>
      <div class="word-title-line">
        <h3>{{ card.word }}</h3>
        <PronunciationButton :word="card.word" :question-id="card.questionId" />
      </div>
      <h3 id="review-question-heading" tabindex="-1" class="review-prompt">
        {{ card.prompt }}
      </h3>
      <p class="learning-disclosure">
        {{
          card.source === "ai"
            ? "AI-generated review context · canonical choices"
            : "Canonical practice question"
        }}
      </p>
      <AnswerOptions
        v-model="selected"
        variant="review"
        :options="card.options"
        legend="Choose a review answer"
        :disabled="!!review.feedback || busy"
      />
      <div v-if="review.feedback" class="review-feedback" role="status">
        <strong>{{
          review.feedback.status === "skipped"
            ? "Skipped — take another look when ready."
            : review.feedback.correct
              ? "Correct in review."
              : "Take another look."
        }}</strong>
        <p>Canonical meaning: {{ review.feedback.definition }}</p>
        <p>
          Correct choice:
          {{
            card.options.find((o) => o.id === review.feedback?.correctOptionId)
              ?.text
          }}
        </p>
        <p>{{ review.feedback.explanation }}</p>
        <p v-if="review.feedback.hintUsed" class="learning-disclosure">
          You used a hint on this card.
        </p>
      </div>
      <div v-if="review.hint" class="learning-content">
        <h3>AI hint</h3>
        <p>{{ review.hint.data.hint }}</p>
      </div>
      <p v-if="hintBusy" role="status">
        Preparing a hint…
        <button type="button" class="text-button" @click="cancelHint">
          Cancel hint
        </button>
      </p>
      <p v-if="hintError" class="learning-error" role="status">
        {{ hintError }}
      </p>
      <div class="learning-actions review-actions">
        <button
          v-if="review.feedback"
          type="button"
          class="button primary"
          :disabled="busy"
          @click="act('advance')"
        >
          {{
            busy
              ? "Saving…"
              : review.position === review.totalCards
                ? "Finish review"
                : "Next review card"
          }}</button
        ><template v-else
          ><button
            type="button"
            class="button primary"
            :disabled="busy || !selected"
            @click="act('answers')"
          >
            {{ busy ? "Checking…" : "Check review answer" }}</button
          ><button
            type="button"
            class="learning-button"
            :disabled="busy"
            @click="act('answers', true)"
          >
            Skip</button
          ><button
            v-if="learning.capabilities.value.textConfigured && !review.hint"
            type="button"
            class="learning-button"
            :disabled="hintBusy"
            @click="hint"
          >
            Give me a hint
          </button></template
        >
      </div>
      <button
        type="button"
        class="report-button"
        :disabled="reported"
        @click="report"
      >
        {{ reported ? "Report received" : "This question is confusing" }}
      </button>
    </template>
    <div v-else class="review-finished">
      <h3 id="review-question-heading" tabindex="-1">Review complete</h3>
      <p>
        {{ review.checked }} checked · {{ review.skipped }} skipped ·
        {{ review.hintUsed }} hints used
      </p>
      <p>{{ review.needsAnotherLook }} cards marked for another look.</p>
      <p>Your competitive score is unchanged.</p>
    </div>
    <p v-if="error" class="learning-error" role="status">
      {{ error }} Your review is saved on the server; retry the same action or
      return to results.
    </p>
  </section>
</template>
