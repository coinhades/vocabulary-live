<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import type { QuestionTiming, QuestionClock } from "../types/protocol";
import { countdown } from "../lib/countdown";
import AppIcon from "./AppIcon.vue";

const props = defineProps<{
  timing: QuestionTiming;
  clock: QuestionClock | null;
  receivedAt: number;
  checking: boolean;
}>();
const now = ref(performance.now());
const progress = computed(() =>
  countdown(
    props.clock,
    props.receivedAt,
    now.value,
    props.timing.durationSeconds,
  ),
);
let frame = 0;
let interval: ReturnType<typeof setInterval> | undefined;
let media: MediaQueryList | undefined;
function tick(time: number) {
  now.value = time;
  if (progress.value.seconds > 0) frame = requestAnimationFrame(tick);
}
function schedule() {
  cancelAnimationFrame(frame);
  clearInterval(interval);
  now.value = performance.now();
  if (
    !props.clock ||
    props.checking ||
    document.hidden ||
    progress.value.seconds === 0
  )
    return;
  if (media?.matches)
    interval = setInterval(() => {
      now.value = performance.now();
      if (progress.value.seconds === 0) clearInterval(interval);
    }, 1000);
  else frame = requestAnimationFrame(tick);
}
onMounted(() => {
  media = matchMedia("(prefers-reduced-motion: reduce)");
  media.addEventListener("change", schedule);
  document.addEventListener("visibilitychange", schedule);
  schedule();
});
watch(() => [props.clock, props.receivedAt, props.checking], schedule);
onUnmounted(() => {
  cancelAnimationFrame(frame);
  clearInterval(interval);
  media?.removeEventListener("change", schedule);
  document.removeEventListener("visibilitychange", schedule);
});
</script>

<template>
  <div
    class="question-timer"
    :class="{ 'time-low': !!clock && progress.seconds <= 5 && !checking }"
  >
    <div class="timer-strip">
      <AppIcon name="clock" />
      <span class="timer-label" role="timer" aria-live="off">{{
        checking
          ? "Checking answer…"
          : !clock
            ? "Starting timer…"
            : progress.seconds === 0
              ? "Time’s up"
              : `${progress.seconds} seconds left`
      }}</span>
      <div
        class="timer-track"
        role="progressbar"
        :aria-label="`${timing.difficulty} word practice timer`"
        :aria-valuemin="0"
        :aria-valuemax="timing.durationSeconds"
        :aria-valuenow="progress.seconds"
        :aria-valuetext="`${progress.seconds} of ${timing.durationSeconds} seconds remaining`"
      >
        <span :style="{ transform: `scaleX(${progress.fraction})` }"></span>
      </div>
    </div>
    <p
      v-if="clock && progress.seconds === 0 && !checking"
      class="timer-expiry"
      role="status"
    >
      You can still earn 50 points for a correct answer.
    </p>
  </div>
</template>
