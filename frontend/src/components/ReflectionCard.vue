<script setup lang="ts">
import { onUnmounted, ref } from "vue";
import type { State } from "../types/protocol";
import type { Artifact, Reflection } from "../types/learning";
import { parseArtifact, parseReflection } from "../types/learning";
import { learningRequest, learningError } from "../lib/learning-api";
import { boundedCachePut, useLearning } from "../composables/useLearning";
const props = defineProps<{ state: State }>();
const learning = useLearning();
const result = ref<Artifact<Reflection> | null>(null),
  busy = ref(false),
  error = ref("");
let abort: AbortController | undefined;
let sequence = 0;
const actionId = crypto.randomUUID();
function cancel() {
  sequence++;
  abort?.abort();
  busy.value = false;
}
onUnmounted(cancel);
async function load() {
  if (busy.value) return;
  const scope = learning.scope.value;
  if (!scope) return;
  const key = JSON.stringify([scope, "reflection"]);
  const cached = learning.cache.get(key);
  if (cached) {
    result.value = parseArtifact(cached, parseReflection);
    return;
  }
  cancel();
  const seq = sequence;
  abort = new AbortController();
  busy.value = true;
  error.value = "";
  try {
    const a = await learningRequest(
      `/quizzes/${encodeURIComponent(scope.quizId)}/learning-reflection`,
      (v) => parseArtifact(v, parseReflection),
      { epoch: scope.epoch, clientActionId: actionId },
      abort.signal,
    );
    if (seq !== sequence) return;
    if (
      a.epoch !== scope.epoch ||
      a.contentVersion !== scope.contentVersion ||
      a.data.suggestions.some(
        (s) => !props.state.receipts.some((r) => r.questionId === s.questionId),
      )
    )
      throw new Error("Invalid reflection references");
    result.value = a;
    boundedCachePut(learning.cache, key, a);
  } catch (e) {
    if (seq === sequence) error.value = learningError(e);
  } finally {
    if (seq === sequence) busy.value = false;
  }
}
function word(id: string) {
  return (
    props.state.receipts.find((r) => r.questionId === id)?.vocabulary.word ?? ""
  );
}
</script>
<template>
  <section
    v-if="learning.capabilities.value.textConfigured"
    class="learning-content reflection-card"
    aria-label="Optional study suggestions"
  >
    <h3>Keep learning</h3>
    <p class="learning-disclosure">
      Optional suggestions based only on this completed practice.
    </p>
    <button
      v-if="!result"
      type="button"
      class="learning-button"
      :disabled="busy"
      @click="load"
    >
      Get study suggestions
    </button>
    <p v-if="busy" role="status">
      Preparing study suggestions…
      <button type="button" class="text-button" @click="cancel">Cancel</button>
    </p>
    <p v-if="error" class="learning-error" role="status">
      {{ error }}
      <button type="button" class="text-button" @click="load">
        Retry suggestions
      </button>
    </p>
    <div v-if="result" class="generated-text">
      <p>{{ result.data.summary }}</p>
      <ul class="study-suggestions">
        <li v-for="item in result.data.suggestions" :key="item.questionId">
          <strong>{{ word(item.questionId) }}</strong>
          <p>{{ item.step }}</p>
        </li>
      </ul>
      <p class="learning-disclosure">
        AI reflection · This is not a proficiency assessment.
      </p>
    </div>
  </section>
</template>
