<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { api, ServiceError } from "../lib/api";
import type { Metadata } from "../types/protocol";
import ChangingValue from "./ChangingValue.vue";
import { retire } from "../lib/motion";
import { useTheme } from "../composables/useTheme";
const { light } = useTheme();
const props = defineProps<{
  code: string;
  name: string;
  busy: boolean;
  message: string;
  expired: boolean;
}>();
const emit = defineEmits<{
  "update:code": [value: string];
  "update:name": [value: string];
  join: [];
  restart: [];
}>();
const preview = ref<Metadata | null>(null);
const validation = ref<"validating" | "ready" | "invalid" | "unavailable">(
  "validating",
);
const validationMessage = ref("");
const previewCode = ref("");
let generation = 0;
let timer: ReturnType<typeof setTimeout> | undefined;
let abort: AbortController | undefined;
const normalized = computed(() => props.code.trim().toUpperCase());
const valid = computed(
  () => validation.value === "ready" && previewCode.value === normalized.value,
);
async function validate(token = ++generation) {
  clearTimeout(timer);
  abort?.abort();
  abort = new AbortController();
  const code = normalized.value;
  preview.value = null;
  validation.value = "validating";
  validationMessage.value = "Validating quiz…";
  if (!/^[A-Z0-9-]{1,64}$/.test(code)) {
    validation.value = "invalid";
    validationMessage.value = code
      ? "Use letters, numbers and hyphens for the quiz code."
      : "Enter a quiz code or choose a demo.";
    return;
  }
  try {
    const metadata = await api.preview(code, abort.signal);
    if (token !== generation) return;
    preview.value = metadata;
    previewCode.value = code;
    validation.value = "ready";
    validationMessage.value = "Quiz ready to join.";
  } catch (error) {
    if (token !== generation) return;
    validation.value =
      error instanceof ServiceError && error.detail.code === "QUIZ_NOT_FOUND"
        ? "invalid"
        : "unavailable";
    validationMessage.value =
      validation.value === "invalid"
        ? "That quiz code was not found."
        : "Quiz service unavailable. Please try again.";
  }
}
watch(
  normalized,
  () => {
    const token = ++generation;
    clearTimeout(timer);
    abort?.abort();
    validation.value = "validating";
    validationMessage.value = "Validating quiz…";
    preview.value = null;
    timer = setTimeout(() => void validate(token), 250);
  },
  { immediate: true },
);
onUnmounted(() => {
  generation++;
  clearTimeout(timer);
  abort?.abort();
});
</script>

<template>
  <section class="join-panel panel" aria-labelledby="join-title">
    <h2 id="join-title">Join a practice</h2>
    <p class="secondary">Choose a quiz and a name for the standings.</p>
    <form @submit.prevent="valid && !busy && name.trim() && emit('join')">
      <label for="quiz-code">Quiz code</label>
      <input
        id="quiz-code"
        :value="code"
        name="quizCode"
        required
        maxlength="64"
        autocomplete="off"
        autocapitalize="characters"
        spellcheck="false"
        :disabled="busy"
        :aria-invalid="validation === 'invalid'"
        aria-describedby="available-codes validation-message"
        @input="emit('update:code', ($event.target as HTMLInputElement).value)"
      />
      <div id="available-codes" class="demo-codes">
        <span>Try a demo</span>
        <button
          v-for="demo in ['VOCAB-DEMO', 'TRAVEL-DEMO']"
          :key="demo"
          type="button"
          :disabled="busy"
          :class="{ chosen: normalized === demo }"
          :aria-pressed="normalized === demo"
          @click="emit('update:code', demo)"
        >
          {{ demo }}
        </button>
      </div>
      <div class="preview-stage" :aria-busy="validation === 'validating'">
        <Transition name="preview" @before-leave="retire">
          <div v-if="valid && preview" :key="previewCode" class="quiz-preview">
            <p id="validation-message" class="sr-only" role="status">
              {{ validationMessage }}
            </p>
            <h3>{{ preview.title }}</h3>
            <ul>
              <li>
                <strong>{{ preview.totalQuestions }} questions</strong>per
                practice
              </li>
              <li>
                <strong>{{ preview.pointsPerCorrectAnswer }} points</strong
                >before time runs out
              </li>
              <li>
                20–40 seconds per word · {{ preview.pointsAfterTimeout }} points
                for a correct answer after zero
              </li>
            </ul>
            <p>
              Incorrect answers earn 0. Questions are shuffled for each
              participant.
            </p>
          </div>
          <div
            v-else-if="validation === 'validating'"
            key="loading"
            class="quiz-preview preview-loading"
          >
            <div class="skeleton-line skeleton-title" aria-hidden="true"></div>
            <div class="skeleton-line" aria-hidden="true"></div>
            <div class="skeleton-line skeleton-short" aria-hidden="true"></div>
            <p id="validation-message" role="status">{{ validationMessage }}</p>
          </div>
          <div v-else key="error" class="quiz-preview preview-error">
            <p id="validation-message" class="field-error" role="status">
              {{ validationMessage }}
            </p>
            <button
              v-if="validation === 'unavailable'"
              type="button"
              class="text-button"
              @click="validate()"
            >
              Retry validation
            </button>
          </div>
        </Transition>
      </div>
      <label for="display-name">Your display name</label>
      <input
        id="display-name"
        :value="name"
        name="displayName"
        placeholder="e.g. Alex"
        required
        maxlength="32"
        autocomplete="nickname"
        :disabled="busy"
        aria-describedby="name-hint"
        @input="emit('update:name', ($event.target as HTMLInputElement).value)"
      />
      <p id="name-hint" class="field-hint">
        No account needed. Your first checked answer counts.
      </p>
      <button
        class="button primary join-button"
        type="submit"
        :disabled="busy || !name.trim() || !valid"
        :aria-busy="busy"
      >
        <ChangingValue :value="busy ? 'Joining…' : 'Join the quiz'" />
        <span v-if="busy" class="activity-indicator" aria-hidden="true"
          ><i></i><i></i><i></i
        ></span>
        <span v-else class="button-arrow" aria-hidden="true">{{
          light ? "→" : "↗"
        }}</span>
      </button>
    </form>
    <Transition name="notice" @before-leave="retire">
      <div v-if="message" class="notice caution" role="status">
        {{ message }}
        <button
          v-if="expired"
          class="text-button"
          :disabled="busy"
          @click="emit('restart')"
        >
          Start a new anonymous session
        </button>
      </div>
    </Transition>
    <p class="fine-print">Two demo rooms · up to 200 people per room</p>
  </section>
</template>
