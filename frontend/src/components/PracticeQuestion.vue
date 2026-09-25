<script setup lang="ts">
import { computed } from "vue";
import type { Question, Receipt } from "../types/protocol";
import { useTheme } from "../composables/useTheme";
import { retire } from "../lib/motion";
import AppIcon from "./AppIcon.vue";
import ChangingValue from "./ChangingValue.vue";
import QuestionPrompt from "./QuestionPrompt.vue";
import PronunciationButton from "./PronunciationButton.vue";
import AnswerFeedback from "./AnswerFeedback.vue";
import AnswerOptions from "./AnswerOptions.vue";
import LearningPanel from "./LearningPanel.vue";
const props = defineProps<{
  question: Question;
  quizId: string;
  questionNumber: number;
  totalQuestions: number;
  score: number;
  feedback: Receipt | null;
  pending: boolean;
  canSelect: boolean;
  canAdvance: boolean;
  completed: boolean;
  submission: "idle" | "checking" | "unconfirmed";
  freshFeedback: boolean;
}>();
const selected = defineModel<string>("selected", { required: true });
const emit = defineEmits<{ submit: []; advance: [] }>();
const { light } = useTheme();
const wordLabel = computed(() => {
  const difficulty = props.question.timing.difficulty;
  return `${difficulty.charAt(0).toUpperCase()}${difficulty.slice(1)} word`;
});
</script>
<template>
  <section
    class="question-panel panel"
    :class="{
      'has-feedback': !!feedback,
      'feedback-incorrect': !!feedback && !feedback.correctness,
    }"
    aria-labelledby="question-heading"
  >
    <div v-show="!feedback" class="question-meta">
      <span class="eyebrow">{{ light ? wordLabel : quizId }}</span
      ><span v-if="light" class="score-chip"
        ><AppIcon name="trophy" /><strong
          ><ChangingValue data-testid="personal-score" :value="score"
        /></strong>
        points</span
      ><ChangingValue
        v-else
        :value="`Question ${questionNumber} of ${totalQuestions}`"
      />
    </div>
    <div class="question-stage">
      <Transition name="question" @before-leave="retire">
        <div :key="question.id" class="question-content">
          <div class="question-title-line">
            <h2
              id="question-heading"
              tabindex="-1"
              :class="{ 'sr-only': !!feedback }"
            >
              <QuestionPrompt
                :text="question.prompt"
                :word="question.pronunciationText"
              />
            </h2>
            <PronunciationButton
              v-if="!feedback && question.pronunciationText"
              :word="question.pronunciationText"
              :question-id="question.id"
            />
          </div>
          <p v-show="!feedback" class="secondary question-instruction">
            {{
              feedback
                ? "Your answer has been recorded."
                : pending
                  ? "Your choice is locked while we confirm your answer."
                  : "Choose an answer. You can change it before checking."
            }}
          </p>
          <AnswerOptions
            v-show="!feedback"
            v-model="selected"
            :options="question.options"
            :legend="`${question.prompt} Choose one answer.`"
            :disabled="!canSelect"
            :checking="pending"
          />
          <div class="submission-status" role="status" aria-live="polite">
            <span v-if="submission === 'checking'">Checking your answer…</span>
            <span v-else-if="submission === 'unconfirmed'"
              >Result not yet confirmed. Your choice is saved.</span
            >
          </div>
          <Transition
            name="feedback"
            :css="freshFeedback"
            @before-leave="retire"
          >
            <AnswerFeedback
              v-if="feedback"
              :key="feedback.submissionId"
              :receipt="feedback"
              :animate="freshFeedback"
              announce
            />
          </Transition>
          <LearningPanel
            v-if="feedback"
            :key="feedback.submissionId"
            :receipt="feedback"
          >
            <button
              type="button"
              class="button primary feedback-next"
              :disabled="!canAdvance"
              @click="emit('advance')"
            >
              {{
                !canAdvance
                  ? "Synchronizing…"
                  : completed
                    ? "See your results"
                    : "Next question"
              }}
              <span class="button-arrow" aria-hidden="true">→</span>
            </button>
          </LearningPanel>
          <div v-else class="question-footer">
            <span class="fine-print"
              >Correct: 100 points before zero · 50 after.</span
            >
            <button
              class="button primary"
              :aria-busy="submission === 'checking'"
              :disabled="!selected || !canSelect"
              @click="emit('submit')"
            >
              <ChangingValue
                :value="
                  submission === 'checking'
                    ? 'Checking…'
                    : pending
                      ? 'Awaiting confirmation'
                      : 'Check answer'
                "
              />
              <span
                v-if="submission === 'checking'"
                class="activity-indicator"
                aria-hidden="true"
                ><i></i><i></i><i></i
              ></span>
              <span v-else class="button-arrow" aria-hidden="true">→</span>
            </button>
          </div>
          <p v-if="feedback" class="feedback-reading-note">
            Take a moment to read the explanation.
          </p>
        </div>
      </Transition>
    </div>
  </section>
</template>
