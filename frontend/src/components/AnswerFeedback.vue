<script setup lang="ts">
import { computed } from "vue";
import type { Receipt } from "../types/protocol";
import { learningWord } from "../lib/learning-word";
import { useTheme } from "../composables/useTheme";
import AppIcon from "./AppIcon.vue";
import PronunciationButton from "./PronunciationButton.vue";
const props = defineProps<{
  receipt: Receipt;
  announce?: boolean;
  animate?: boolean;
}>();
const { light } = useTheme();
const word = computed(() => learningWord(props.receipt));
function answer(id: string) {
  const index = props.receipt.question.options.findIndex((o) => o.id === id);
  return {
    letter: String.fromCharCode(65 + index),
    text: props.receipt.question.options[index]?.text ?? "",
  };
}
</script>

<template>
  <div
    class="answer-feedback"
    :class="{ incorrect: !receipt.correctness, 'feedback-fresh': animate }"
    :role="announce ? 'status' : undefined"
    :aria-live="announce ? 'polite' : undefined"
    :aria-atomic="announce ? true : undefined"
  >
    <div class="feedback-title">
      <span v-if="light" class="feedback-symbol feedback-icon"
        ><AppIcon :name="receipt.correctness ? 'check' : 'close'"
      /></span>
      <img
        v-else
        class="feedback-symbol"
        :src="receipt.correctness ? '/art/correct.webp' : '/art/incorrect.webp'"
        alt=""
      />
      <strong class="feedback-verdict">{{
        receipt.correctness ? "Correct!" : "Not quite."
      }}</strong>
      <span class="feedback-points">{{
        receipt.correctness
          ? "+" + receipt.pointsAwarded + " points"
          : light
            ? "That’s part of learning. Compare the answers below."
            : "The correct answer is:"
      }}</span>
      <span v-if="receipt.correctness && receipt.timedOut" class="late-credit"
        >Correct answer after time ran out.</span
      >
      <span
        v-if="light && receipt.correctness"
        class="feedback-sparkles"
        aria-hidden="true"
      ></span>
    </div>
    <dl class="feedback-answers">
      <template v-if="!receipt.correctness"
        ><dt>Your answer</dt>
        <dd class="feedback-answer wrong-answer">
          <span class="option-letter">{{
            answer(receipt.selectedOptionId).letter
          }}</span>
          <span class="answer-text">{{
            answer(receipt.selectedOptionId).text
          }}</span>
        </dd></template
      >
      <dt
        :class="{ 'sr-only': receipt.correctness }"
        class="correct-answer-label"
      >
        {{ receipt.correctness ? "Your answer" : "Correct answer" }}
      </dt>
      <dd class="feedback-answer revealed-answer">
        <span class="option-letter">{{
          answer(receipt.correctOptionId).letter
        }}</span>
        <span class="answer-text">{{
          answer(receipt.correctOptionId).text
        }}</span>
      </dd>
    </dl>
    <div class="word-definition panel">
      <div class="word-heading">
        <div class="word-title-line">
          <h3>{{ word }}</h3>
          <PronunciationButton :word="word" :question-id="receipt.questionId" />
        </div>
        <span class="word-meta">
          <span class="word-pronunciation">{{
            receipt.vocabulary.pronunciation
          }}</span>
          <span class="word-part">{{
            light
              ? receipt.vocabulary.partOfSpeech
              : `(${receipt.vocabulary.partOfSpeech})`
          }}</span>
        </span>
      </div>
      <p v-if="light" class="definition-label">Meaning</p>
      <p class="definition-meaning">
        {{ receipt.vocabulary.definition }}
      </p>
      <div v-if="light" class="explanation-box">
        <p class="definition-label">Explanation</p>
        <p class="feedback-explanation">{{ receipt.explanation }}</p>
      </div>
      <p v-else class="feedback-explanation">{{ receipt.explanation }}</p>
      <p v-if="receipt.vocabulary.example" class="feedback-explanation">
        <strong>Example:</strong> {{ receipt.vocabulary.example }}
      </p>
    </div>
  </div>
</template>
