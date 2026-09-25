<script setup lang="ts">
import { computed, ref } from "vue";
import type { State, Row } from "../types/protocol";
import AnswerFeedback from "./AnswerFeedback.vue";
import ChangingValue from "./ChangingValue.vue";
import AppIcon from "./AppIcon.vue";
import ReflectionCard from "./ReflectionCard.vue";
import ReviewLauncher from "./ReviewLauncher.vue";
import LearningPanel from "./LearningPanel.vue";
import { learningWord, learningDefinition } from "../lib/learning-word";
import { useTheme } from "../composables/useTheme";
const props = defineProps<{ state: State; me?: Row; live: boolean }>();
defineEmits<{ leave: [] }>();
const { light } = useTheme();
const correct = computed(() => props.state.correctCount);
const missed = computed(() =>
  props.state.receipts.filter((r) => !r.correctness),
);
const accuracy = computed(() => props.state.accuracy);
const answered = computed(() =>
  [...props.state.receipts].sort((a, b) => a.questionNumber - b.questionNumber),
);
const counts = ["No", "One", "Two", "Three", "Four", "Five", "Six", "Seven"];
const reviewTitle = computed(() => {
  const n = missed.value.length;
  if (!n) return "Every word answered correctly";
  return `${counts[n] ?? n} new word${n === 1 ? "" : "s"} to review`;
});
const review = ref<HTMLDetailsElement>();
const reviewing = ref(false);
function reviewWords() {
  if (review.value) {
    review.value.open = true;
    review.value.querySelector("summary")?.focus();
    review.value.scrollIntoView({ block: "nearest" });
  }
}
</script>

<template>
  <section
    class="completion"
    :class="{ 'is-reviewing': reviewing }"
    :aria-labelledby="
      reviewing ? 'private-review-heading' : 'completion-heading'
    "
  >
    <div v-show="!reviewing" class="completion-celebration">
      <img class="completion-icon" src="/art/trophy.webp" alt="" />
      <h2 id="completion-heading" tabindex="-1">
        {{
          light ? `Great job, ${me?.displayName ?? "you"}!` : "Quiz complete!"
        }}
      </h2>
      <p class="completion-greeting">
        {{
          light
            ? "You completed the practice."
            : `Great effort, ${me?.displayName ?? "you"}!`
        }}
      </p>
      <div v-if="light" class="result-grid result-cards">
        <div class="correct-result">
          <span class="result-badge"><AppIcon name="check" /></span
          ><strong>{{ correct }} / {{ state.totalQuestions }}</strong
          ><span>Correct answers</span><small>{{ accuracy }}% accuracy</small>
        </div>
        <div class="score-result">
          <AppIcon name="chart" /><strong>{{ me?.score ?? state.score }}</strong
          ><span>Total score</span>
        </div>
        <div class="rank-result">
          <AppIcon name="trophy" /><strong
            >#<ChangingValue
              :value="me?.rank ?? state.currentRank"
              :animate="live" /></strong
          ><span>{{ live ? "Current rank" : "Last known rank" }}</span
          ><small
            >out of {{ state.participantCount }}
            {{ state.participantCount === 1 ? "person" : "people" }}</small
          >
        </div>
      </div>
      <div v-else class="result-grid">
        <div>
          <AppIcon name="trophy" /><strong
            >{{ correct }}<small>/{{ state.totalQuestions }}</small></strong
          ><span>Correct</span>
        </div>
        <div>
          <AppIcon name="chart" /><strong>{{ me?.score ?? state.score }}</strong
          ><span>Total points</span>
        </div>
        <div>
          <AppIcon name="spark" /><strong>{{ accuracy }}<small>%</small></strong
          ><span>Accuracy</span>
        </div>
        <div class="rank-result">
          <strong
            ><ChangingValue
              :value="me?.rank ?? state.currentRank"
              :animate="live" /></strong
          ><span>{{ live ? "Current rank" : "Last known rank" }}</span>
        </div>
      </div>
      <div class="quote-card panel">
        <span aria-hidden="true">“</span>
        <p v-if="light">
          “A bigger vocabulary opens a brighter you.”<span
            >Keep practicing!</span
          >
        </p>
        <p v-else>A better vocabulary<br />leads to brighter opportunities.</p>
      </div>
      <div v-if="light" class="completion-actions">
        <button
          v-if="missed.length"
          class="button primary"
          @click="reviewWords"
        >
          Review missed words <span aria-hidden="true">→</span></button
        ><button
          class="button secondary-button"
          aria-label="Explore another quiz"
          @click="$emit('leave')"
        >
          Try another quiz
        </button>
      </div>
      <p class="completion-note">
        Standings remain live while others continue. Equal scores share a rank;
        this is your <strong>current position</strong>.
      </p>
    </div>
    <div class="completion-learning">
      <section
        v-if="light"
        v-show="!reviewing"
        class="question-summary panel"
        aria-labelledby="summary-heading"
      >
        <h3 id="summary-heading">Question summary</h3>
        <ol class="summary-list">
          <li v-for="receipt in answered" :key="receipt.questionId">
            <span class="summary-number">{{ receipt.questionNumber }}</span
            ><span
              class="summary-result"
              :class="receipt.correctness ? 'is-correct' : 'is-incorrect'"
              >{{ receipt.correctness ? "correct" : "incorrect" }}</span
            ><AppIcon
              :name="receipt.correctness ? 'check' : 'close'"
              :class="receipt.correctness ? 'is-correct' : 'is-incorrect'"
            /><span class="summary-word">{{ learningWord(receipt) }}</span>
          </li>
        </ol>
        <div class="review-tip">
          <AppIcon name="bulb" />
          <p>
            <strong>{{ reviewTitle }}</strong
            ><span>{{
              missed.length
                ? "Focus on the words you missed to make them stick."
                : "No missed words to review this time."
            }}</span>
          </p>
        </div>
        <details v-if="missed.length" ref="review" class="missed-review">
          <summary>
            Review missed words <span>({{ missed.length }})</span>
          </summary>
          <p class="field-hint">
            For learning only. Review does not change your score.
          </p>
          <article v-for="receipt in missed" :key="receipt.questionId">
            <p class="eyebrow">QUESTION {{ receipt.questionNumber }}</p>
            <h4>{{ receipt.question.prompt }}</h4>
            <AnswerFeedback :receipt="receipt" />
            <LearningPanel :receipt="receipt" />
          </article>
        </details>
      </section>
      <section
        v-else
        v-show="!reviewing"
        class="review-summary panel"
        aria-labelledby="review-heading"
      >
        <h3>Your performance</h3>
        <div class="performance-row">
          <span>Correct</span>
          <div class="performance-track">
            <i :style="{ width: accuracy + '%' }"></i>
          </div>
          <b>{{ correct }}</b
          ><span>{{ accuracy }}%</span>
        </div>
        <div class="performance-row performance-incorrect">
          <span>Incorrect</span>
          <div class="performance-track">
            <i :style="{ width: 100 - accuracy + '%' }"></i>
          </div>
          <b>{{ missed.length }}</b
          ><span>{{ Math.round((100 - accuracy) * 10) / 10 }}%</span>
        </div>
        <h3 id="review-heading">Words to revisit</h3>
        <p v-if="!missed.length" class="perfect-note">
          Every answer correct. No missed words to review.
        </p>
        <ol v-else class="missed-word-list">
          <li v-for="receipt in missed" :key="receipt.questionId">
            <span
              ><strong>{{ learningWord(receipt) }}</strong
              ><small>{{ learningDefinition(receipt) }}</small></span
            ><small class="previous-answer"
              >Your answer:
              {{
                receipt.question.options.find(
                  (o) => o.id === receipt.selectedOptionId,
                )?.text
              }}</small
            >
          </li>
        </ol>
        <details v-if="missed.length" ref="review" class="missed-review">
          <summary>
            Review missed words <span>({{ missed.length }})</span>
          </summary>
          <p class="field-hint">
            For learning only. Review does not change your score.
          </p>
          <article v-for="receipt in missed" :key="receipt.questionId">
            <p class="eyebrow">QUESTION {{ receipt.questionNumber }}</p>
            <h4>{{ receipt.question.prompt }}</h4>
            <AnswerFeedback :receipt="receipt" />
            <LearningPanel :receipt="receipt" />
          </article>
        </details>
      </section>
      <ReviewLauncher :state="state" @active="reviewing = $event" />
      <ReflectionCard v-show="!reviewing" :state="state" />
      <div v-if="!light" v-show="!reviewing" class="completion-actions">
        <button
          v-if="missed.length"
          class="button secondary-button"
          @click="reviewWords"
        >
          Review words</button
        ><button
          class="button primary"
          aria-label="Explore another quiz"
          @click="$emit('leave')"
        >
          Try another quiz <span aria-hidden="true">→</span>
        </button>
      </div>
    </div>
  </section>
</template>
