<script setup lang="ts">
import { computed, onUnmounted } from "vue";
import { useLearning } from "../composables/useLearning";
import AppIcon from "./AppIcon.vue";
const props = defineProps<{ word: string; questionId: string }>();
const learning = useLearning();
const key = computed(() =>
  JSON.stringify([learning.scope.value, props.questionId]),
);
const state = computed(() =>
  !learning.capabilities.value.speechConfigured
    ? "unavailable"
    : learning.audio.state.key === key.value
      ? learning.audio.state.status
      : "idle",
);
onUnmounted(() => {
  if (learning.audio.state.key === key.value) learning.audio.stop();
});
const label = computed(() => {
  if (state.value === "loading")
    return `Preparing pronunciation for ${props.word}`;
  if (state.value === "playing") return `Replay ${props.word}`;
  if (state.value === "ready-to-play") return `Play ${props.word} again`;
  if (state.value === "error") return `Retry pronunciation of ${props.word}`;
  return `Listen to ${props.word}`;
});
function listen() {
  if (state.value === "loading" || state.value === "unavailable") return;
  const s = learning.scope.value;
  if (s)
    void learning.audio.play(
      key.value,
      `/quizzes/${encodeURIComponent(s.quizId)}/questions/${encodeURIComponent(props.questionId)}/pronunciation`,
      s.epoch,
    );
}
</script>
<template>
  <span
    v-if="state !== 'unavailable'"
    class="pronunciation-control"
    :data-state="state"
  >
    <button
      type="button"
      class="speaker-button"
      :aria-label="label"
      :title="state === 'error' ? learning.audio.state.message : label"
      :aria-disabled="state === 'loading'"
      :aria-busy="state === 'loading'"
      @click.stop="listen"
    >
      <span
        v-if="state === 'loading'"
        class="audio-spinner"
        aria-hidden="true"
      ></span>
      <AppIcon
        v-else
        :name="
          state === 'ready-to-play' || state === 'error' ? 'refresh' : 'speaker'
        "
      />
    </button>
    <span class="sr-only" role="status">{{
      state === "error"
        ? "Pronunciation could not play. You can retry."
        : state === "playing"
          ? "Playing pronunciation."
          : state === "loading"
            ? "Preparing pronunciation."
            : ""
    }}</span>
  </span>
</template>
