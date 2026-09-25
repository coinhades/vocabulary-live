<script setup lang="ts">
import { computed, onUnmounted, ref, watch, useId } from "vue";
import type { Receipt } from "../types/protocol";
import type { Artifact, Explanation, Example } from "../types/learning";
import {
  parseArtifact,
  parseExplanation,
  parseExample,
} from "../types/learning";
import { learningRequest, learningError } from "../lib/learning-api";
import { boundedCachePut, useLearning } from "../composables/useLearning";
import AppIcon from "./AppIcon.vue";
import LearningDialog from "./LearningDialog.vue";
const props = defineProps<{ receipt: Receipt }>();
const learning = useLearning();
const contextId = useId();
const tab = ref<"explanation" | "examples" | null>(null);
const context = ref("daily-life");
const variant = ref(0);
const explanation = ref<Artifact<Explanation> | null>(null);
const examples = ref<Record<string, Artifact<Example>[]>>({});
const busy = ref(false);
const error = ref("");
const retryNext = ref(false);
let abort: AbortController | undefined;
let sequence = 0;
const intents = new Map<string, string>();
const example = computed(() => examples.value[context.value]?.[variant.value]);
const count = computed(() => examples.value[context.value]?.length ?? 0);
const scopeKey = computed(() =>
  JSON.stringify([learning.scope.value, props.receipt.submissionId]),
);
function cancel() {
  sequence++;
  abort?.abort();
  busy.value = false;
}
function close() {
  cancel();
  tab.value = null;
  error.value = "";
}
function open(kind: "explanation" | "examples") {
  cancel();
  error.value = "";
  tab.value = kind;
  if (kind === "explanation") void load(kind);
}
watch(scopeKey, () => {
  close();
  explanation.value = null;
  examples.value = {};
  intents.clear();
});
watch(
  () => learning.capabilities.value.textConfigured,
  (available) => {
    if (!available) close();
  },
);
watch(context, () => {
  cancel();
  variant.value = 0;
  error.value = "";
  retryNext.value = false;
});
onUnmounted(cancel);
async function load(kind: "explanation" | "examples", next = false) {
  if (busy.value || !learning.capabilities.value.textConfigured) return;
  tab.value = kind;
  error.value = "";
  retryNext.value = next;
  const index = kind === "examples" && next ? count.value : variant.value;
  if (index >= 3) return;
  const cacheKey = JSON.stringify([
    scopeKey.value,
    kind,
    kind === "examples" ? context.value : "",
    index,
  ]);
  const cached = learning.cache.get(cacheKey);
  if (cached) {
    if (kind === "explanation")
      explanation.value = parseArtifact(cached, parseExplanation);
    else {
      const list = (examples.value[context.value] ??= []);
      list[index] = parseArtifact(cached, parseExample);
      variant.value = index;
    }
    return;
  }
  const scope = learning.scope.value;
  if (!scope) return;
  const id = intents.get(cacheKey) ?? crypto.randomUUID();
  intents.set(cacheKey, id);
  cancel();
  const token = sequence;
  abort = new AbortController();
  busy.value = true;
  try {
    const raw = await learningRequest(
      `/quizzes/${encodeURIComponent(scope.quizId)}/questions/${encodeURIComponent(props.receipt.questionId)}/${kind}`,
      (v) => v,
      {
        epoch: scope.epoch,
        clientActionId: id,
        ...(kind === "examples"
          ? { context: context.value, variant: index }
          : {}),
      },
      abort.signal,
    );
    if (
      token !== sequence ||
      scopeKey.value !== JSON.stringify([scope, props.receipt.submissionId])
    )
      return;
    const result =
      kind === "explanation"
        ? parseArtifact(raw, parseExplanation)
        : parseArtifact(raw, parseExample);
    if (
      result.epoch !== scope.epoch ||
      result.questionId !== props.receipt.questionId ||
      result.acceptedReceiptId !== props.receipt.submissionId ||
      result.contentVersion !== scope.contentVersion
    )
      throw new Error("Outdated learning response");
    boundedCachePut(learning.cache, cacheKey, raw);
    if (kind === "explanation")
      explanation.value = parseArtifact(raw, parseExplanation);
    else {
      const list = (examples.value[context.value] ??= []);
      list[index] = parseArtifact(raw, parseExample);
      variant.value = index;
    }
  } catch (e) {
    if (token === sequence) error.value = learningError(e);
  } finally {
    if (token === sequence) busy.value = false;
  }
}
</script>
<template>
  <div
    v-if="learning.capabilities.value.textConfigured || $slots.default"
    class="learning-panel"
    :class="{ 'has-advance': !!$slots.default }"
  >
    <div
      class="feedback-actions"
      :class="{
        'has-learning': learning.capabilities.value.textConfigured,
        'has-next': !!$slots.default,
      }"
      aria-label="Question actions"
    >
      <template v-if="learning.capabilities.value.textConfigured">
        <button
          type="button"
          class="learning-button"
          aria-haspopup="dialog"
          :aria-expanded="tab === 'explanation'"
          @click="open('explanation')"
        >
          Explain why
        </button>
        <button
          type="button"
          class="learning-button"
          aria-haspopup="dialog"
          :aria-expanded="tab === 'examples'"
          @click="open('examples')"
        >
          Examples
        </button>
      </template>
      <slot />
    </div>
    <LearningDialog
      :open="tab !== null"
      :title="
        tab === 'explanation' ? 'Why this answer?' : 'Examples in context'
      "
      :word="receipt.vocabulary.word"
      :part-of-speech="receipt.vocabulary.partOfSpeech"
      @close="close"
    >
      <template v-if="tab === 'examples'">
        <div class="example-toolbar">
          <span class="example-context-hint">Make it familiar</span>
          <div class="context-select">
            <label :for="contextId">Context</label>
            <select :id="contextId" v-model="context">
              <option value="daily-life">Daily life</option>
              <option value="work">Work</option>
              <option value="travel">Travel</option>
              <option value="school">School</option>
            </select>
          </div>
        </div>
        <div class="example-card">
          <div v-if="example" class="generated-text">
            <p class="example-main">{{ example.data.sentence }}</p>
            <p v-if="example.data.usageNote" class="example-usage">
              {{ example.data.usageNote }}
            </p>
          </div>
          <div v-else class="example-empty">
            <AppIcon name="book" />
            <h3>See it in a sentence</h3>
            <p>Choose a context and create an example.</p>
          </div>
        </div>
      </template>
      <div v-else-if="explanation" class="generated-text explanation-body">
        <p>{{ explanation.data.explanation }}</p>
        <p v-if="explanation.data.contrast" class="explanation-contrast">
          {{ explanation.data.contrast }}
        </p>
        <div class="explanation-example">
          <span class="eyebrow">IN A SENTENCE</span>
          <p>{{ explanation.data.example }}</p>
        </div>
      </div>
      <div v-else class="explanation-placeholder" aria-hidden="true">
        <span></span><span></span><span></span>
      </div>
      <p v-if="busy" class="learning-status" role="status">
        <span class="audio-spinner" aria-hidden="true"></span>
        {{
          tab === "explanation"
            ? "Preparing an explanation…"
            : "Creating an example…"
        }}
        <button type="button" class="text-button" @click="cancel">
          Cancel
        </button>
      </p>
      <p v-if="error" class="learning-error" role="status">
        {{ error }}
        <button
          type="button"
          class="text-button"
          @click="load(tab!, retryNext)"
        >
          Retry
        </button>
      </p>
      <template #footer>
        <template v-if="tab === 'examples'">
          <nav class="example-pagination" aria-label="Saved examples">
            <button
              type="button"
              class="icon-button"
              aria-label="Previous example"
              :disabled="variant === 0 || busy || !example"
              @click="variant--"
            >
              <AppIcon name="chevron-left" />
            </button>
            <span class="example-count" aria-live="polite"
              >{{ example ? variant + 1 : 0 }}<span> / {{ count }}</span></span
            >
            <button
              type="button"
              class="icon-button"
              aria-label="Next example"
              :disabled="variant >= count - 1 || busy"
              @click="variant++"
            >
              <AppIcon name="chevron-right" />
            </button>
          </nav>
          <button
            type="button"
            class="button primary example-create"
            :aria-disabled="busy || count >= 3"
            @click="load('examples', !!example)"
          >
            <AppIcon :name="busy ? 'clock' : 'refresh'" />{{
              busy
                ? "Creating…"
                : count >= 3
                  ? "3 examples saved"
                  : example
                    ? "Another example"
                    : "Create example"
            }}
          </button>
        </template>
        <button
          v-else
          type="button"
          class="button primary dialog-done"
          @click="close"
        >
          Back to question <span aria-hidden="true">→</span>
        </button>
      </template>
    </LearningDialog>
  </div>
</template>
