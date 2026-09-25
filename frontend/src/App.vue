<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from "vue";
import Standings from "./components/Standings.vue";
import PracticeQuestion from "./components/PracticeQuestion.vue";
import JoinForm from "./components/JoinForm.vue";
import CompletionPanel from "./components/CompletionPanel.vue";
import ChangingValue from "./components/ChangingValue.vue";
import AppIcon from "./components/AppIcon.vue";
import QuestionTimer from "./components/QuestionTimer.vue";
import { provideLearning } from "./composables/useLearning";
import { retire, hide, reveal } from "./lib/motion";
import { useQuiz } from "./composables/useQuiz";
import { useTheme } from "./composables/useTheme";

const quiz = useQuiz();
const learning = provideLearning(
  computed(() =>
    quiz.state
      ? {
          participantId: quiz.state.participantId,
          quizId: quiz.state.quizId,
          epoch: quiz.state.epoch,
          contentVersion: quiz.state.contentVersion,
        }
      : null,
  ),
);
watch(
  () =>
    JSON.stringify([
      quiz.question?.id,
      quiz.feedback?.submissionId,
      quiz.complete,
    ]),
  () => learning.audio.stop(),
);
const { light, toggle: toggleTheme } = useTheme();
const requestedCode = new URLSearchParams(location.search).get("quiz");
const code = ref(
  requestedCode && /^[A-Z0-9-]{1,64}$/.test(requestedCode)
    ? requestedCode
    : "VOCAB-DEMO",
);
const name = ref("");
const mobile = ref(false);
const mobileView = ref<"question" | "standings">("question");
const showCompletionStandings = ref(false);
const freshFeedback = ref(false);
// Animate only a newly resolved intent in this mounted view, never a restored
// receipt or repeated reconciliation of the same accepted submission.
watch(
  () => quiz.feedback?.submissionId,
  (id) => {
    freshFeedback.value =
      !!id && quiz.pending?.questionId === quiz.feedback?.questionId;
  },
  { flush: "sync" },
);
let media: MediaQueryList | undefined;
const updateViewport = () => {
  mobile.value = media?.matches ?? false;
};
const labels = {
  idle: "Ready when you are",
  connecting: "Synchronizing…",
  synchronizing: "Synchronizing…",
  live: "Live & synchronized",
  reconnecting: "Reconnecting…",
  stale: "Standings delayed",
  unavailable: "Service unavailable",
  expired: "Session expired",
  reset: "Quiz reset",
};
const status = computed(() =>
  light.value && quiz.connection === "live" ? "Live" : labels[quiz.connection],
);
const connectionActive = computed(() =>
  ["connecting", "synchronizing", "reconnecting"].includes(quiz.connection),
);
const showRecovery = computed(
  () => quiz.connection === "expired" || quiz.connection === "reset",
);
const facts = computed(() =>
  light.value
    ? ([
        {
          icon: "book",
          title: "Everyday words",
          detail: "Real-world vocabulary",
        },
        {
          icon: "people",
          title: "Live together",
          detail: "Same questions, live",
        },
        {
          icon: "chart",
          title: "Track progress",
          detail: "See how you compare",
        },
      ] as const)
    : ([
        { icon: "people", title: "Real people", detail: "Real-time practice" },
        {
          icon: "chart",
          title: "Everyday words",
          detail: "Deeper understanding",
        },
        {
          icon: "bolt",
          title: "A sharper you",
          detail: "One question at a time",
        },
      ] as const),
);
const answeredPercent = computed(() =>
  quiz.state
    ? Math.round((quiz.state.answeredCount / quiz.state.totalQuestions) * 100)
    : 0,
);
async function join() {
  mobileView.value = "question";
  showCompletionStandings.value = false;
  await quiz.join(code.value, name.value);
}
async function advance() {
  quiz.next();
  await nextTick();
  document
    .querySelector<HTMLElement>(
      quiz.complete
        ? ".completion:not([inert]) #completion-heading"
        : ".question-content:not([inert]) #question-heading",
    )
    ?.focus();
}
function switchTab(event: KeyboardEvent) {
  if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
  event.preventDefault();
  mobileView.value =
    event.key === "Home"
      ? "question"
      : event.key === "End"
        ? "standings"
        : mobileView.value === "question"
          ? "standings"
          : "question";
  void nextTick(() =>
    document.getElementById(`tab-${mobileView.value}`)?.focus(),
  );
}
onMounted(() => {
  media = matchMedia("(max-width: 760px)");
  updateViewport();
  media.addEventListener("change", updateViewport);
  if (requestedCode) return;
  try {
    const raw = sessionStorage.getItem("vocabulary.last");
    if (!raw) return;
    const last: unknown = JSON.parse(raw);
    if (
      last &&
      typeof last === "object" &&
      "quizId" in last &&
      "displayName" in last &&
      typeof last.quizId === "string" &&
      typeof last.displayName === "string"
    ) {
      code.value = last.quizId;
      name.value = last.displayName;
      void join();
    }
  } catch {
    /* Manual join remains available if saved navigation is invalid. */
  }
});
onUnmounted(() => media?.removeEventListener("change", updateViewport));
</script>

<template>
  <div class="app-shell" :class="{ 'is-practicing': !!quiz.state }">
    <header class="site-header">
      <a
        class="brand"
        href="/"
        aria-label="Vocabulary Live home"
        @click.prevent="quiz.leave()"
      >
        <span class="brand-mark" aria-hidden="true"
          ><img src="/art/brand.png" alt="" /></span
        ><span>Vocabulary <strong>Live</strong></span> </a
      ><span class="header-actions"
        ><span class="assessment">Engineering assessment demo</span
        ><button
          type="button"
          class="theme-toggle"
          :aria-label="light ? 'Switch to dark theme' : 'Switch to light theme'"
          :title="light ? 'Switch to dark theme' : 'Switch to light theme'"
          @click="toggleTheme"
        >
          <AppIcon :name="light ? 'moon' : 'sun'" /></button
        ><button
          v-if="light && quiz.state"
          type="button"
          class="header-leave"
          @click="quiz.leave()"
        >
          <AppIcon :name="quiz.complete ? 'refresh' : 'logout'" />{{
            quiz.complete ? "New quiz" : "Leave"
          }}
        </button></span
      >
    </header>
    <div class="page-stage">
      <Transition name="page" @before-leave="retire">
        <main v-if="!quiz.state" key="join" class="join-layout">
          <section class="intro">
            <div class="intro-text">
              <p class="eyebrow">
                <span class="tiny-line"></span>LIVE PRACTICE CHALLENGE
              </p>
              <h1>
                A little practice.<br /><em
                  >A <span>sharper<br />vocabulary.</span></em
                >
              </h1>
              <img
                v-if="light"
                class="join-illustration"
                src="/art/learner.webp"
                alt=""
                width="826"
                height="976"
              />
              <p class="intro-copy">
                <span>Take your time. Learn from each answer.</span>
                <span>See your progress alongside everyone else.</span>
              </p>
            </div>
            <div class="join-facts">
              <div v-for="fact in facts" :key="fact.title">
                <AppIcon :name="fact.icon" /><strong>{{ fact.title }}</strong
                ><span>{{ fact.detail }}</span>
              </div>
            </div>
          </section>
          <JoinForm
            v-model:code="code"
            v-model:name="name"
            :busy="quiz.busy"
            :message="quiz.message"
            :expired="quiz.connection === 'expired'"
            @join="join"
            @restart="quiz.restartSession()"
          />
        </main>

        <main
          v-else
          key="practice"
          class="quiz-layout"
          :class="{ 'is-complete': quiz.complete }"
        >
          <div v-if="light" class="practice-status">
            <p class="room-status">
              <span
                class="connection"
                :class="{
                  connected: quiz.connection === 'live',
                  'is-active': connectionActive,
                }"
                :data-state="quiz.connection"
                role="status"
                ><span class="live-dot" aria-hidden="true"></span
                >{{ status }}</span
              ><span class="room-total"
                >· {{ quiz.state.participantCount }}
                {{
                  quiz.state.participantCount === 1 ? "person" : "people"
                }}</span
              >
            </p>
            <div v-if="!quiz.complete" class="practice-meter">
              <strong
                ><ChangingValue
                  :value="`Question ${quiz.questionNumber} of ${quiz.state.totalQuestions}`"
              /></strong>
              <div
                class="question-progress"
                role="progressbar"
                :aria-valuenow="quiz.state.answeredCount"
                :aria-valuemax="quiz.state.totalQuestions"
                :aria-valuemin="0"
                aria-label="Questions answered"
              >
                <span
                  :style="{
                    transform: `scaleX(${quiz.state.answeredCount / quiz.state.totalQuestions})`,
                  }"
                ></span>
              </div>
              <span class="practice-percent">{{ answeredPercent }}%</span>
            </div>
          </div>
          <div v-if="!light" class="quiz-topline">
            <button class="text-button back" @click="quiz.leave()">
              ← All quizzes
            </button>
            <span class="room-count"
              ><span class="live-dot" aria-hidden="true"></span
              >{{ quiz.state.participantCount }}
              {{ quiz.state.participantCount === 1 ? "person" : "people" }}
              joined</span
            >
          </div>
          <div v-if="!light" class="quiz-heading">
            <div class="practice-progress">
              <div
                class="question-orbit"
                :aria-label="`Question ${quiz.questionNumber} of ${quiz.state.totalQuestions}`"
              >
                <small>{{ quiz.complete ? "Complete" : "Question" }}</small
                ><strong
                  >{{
                    quiz.complete
                      ? quiz.state.totalQuestions
                      : quiz.questionNumber
                  }}/{{ quiz.state.totalQuestions }}</strong
                >
              </div>
              <div class="practice-track">
                <h1>{{ quiz.state.title }}</h1>
                <div
                  class="question-progress"
                  role="progressbar"
                  :aria-valuenow="quiz.state.answeredCount"
                  :aria-valuemax="quiz.state.totalQuestions"
                  :aria-valuemin="0"
                  aria-label="Questions answered"
                >
                  <span
                    :style="{
                      transform: `scaleX(${quiz.state.answeredCount / quiz.state.totalQuestions})`,
                    }"
                  ></span>
                </div>
              </div>
            </div>
            <span
              class="connection"
              :class="{
                connected: quiz.connection === 'live',
                'is-active': connectionActive,
              }"
              :data-state="quiz.connection"
              role="status"
            >
              <span class="live-dot" aria-hidden="true"></span
              >{{ status }}</span
            >
            <div class="score-summary">
              <AppIcon name="trophy" /><strong
                ><ChangingValue
                  data-testid="personal-score"
                  :value="quiz.me?.score ?? quiz.state.score"
                />
                <small> points</small></strong
              >
            </div>
          </div>
          <Transition name="notice" @before-leave="retire">
            <div v-if="quiz.networkNotice" class="network-notice" role="status">
              {{ quiz.networkNotice }}
              <button
                v-if="!quiz.pending"
                class="text-button"
                :disabled="quiz.busy"
                @click="quiz.retry()"
              >
                Retry synchronization
              </button>
            </div>
          </Transition>
          <Transition name="notice" @before-leave="retire">
            <div
              v-if="quiz.message || showRecovery"
              class="notice"
              :class="{ caution: quiz.connection !== 'live' }"
              role="status"
            >
              <span>{{ quiz.message }}</span>
              <button
                v-if="quiz.connection === 'expired'"
                class="text-button"
                :disabled="quiz.busy"
                @click="quiz.restartSession()"
              >
                Start a new anonymous session
              </button>
              <button
                v-else-if="quiz.connection === 'reset'"
                class="text-button"
                @click="quiz.join(quiz.quizId, quiz.displayName)"
              >
                Rejoin this quiz
              </button>
              <button
                v-else-if="
                  quiz.submission === 'unconfirmed' ||
                  quiz.connection === 'stale' ||
                  quiz.connection === 'unavailable'
                "
                class="text-button"
                :disabled="quiz.submission === 'checking'"
                @click="quiz.retry()"
              >
                Check &amp; retry safely
              </button>
            </div>
          </Transition>
          <div
            v-if="mobile"
            class="mobile-tabs"
            role="tablist"
            aria-label="Practice views"
            @keydown="switchTab"
          >
            <button
              id="tab-question"
              role="tab"
              :aria-selected="mobileView === 'question'"
              aria-controls="question-view"
              :tabindex="mobileView === 'question' ? 0 : -1"
              @click="mobileView = 'question'"
            >
              {{ quiz.complete ? "Results" : "Question" }}
            </button>
            <button
              id="tab-standings"
              role="tab"
              :aria-selected="mobileView === 'standings'"
              aria-controls="standings-view"
              :tabindex="mobileView === 'standings' ? 0 : -1"
              @click="mobileView = 'standings'"
            >
              Standings <span>({{ quiz.state.participantCount }})</span>
            </button>
          </div>
          <div
            class="play-grid"
            :class="{
              'showing-feedback': !!quiz.feedback,
              'showing-completion': quiz.complete,
            }"
          >
            <Transition name="tab" @before-leave="hide" @before-enter="reveal">
              <div
                v-show="!mobile || mobileView === 'question'"
                id="question-view"
                :class="{
                  'motion-hidden': mobile && mobileView !== 'question',
                }"
                :role="mobile ? 'tabpanel' : undefined"
                :aria-labelledby="mobile ? 'tab-question' : undefined"
              >
                <div class="panel-stage">
                  <Transition name="panel" @before-leave="retire">
                    <PracticeQuestion
                      v-if="!quiz.complete && quiz.question"
                      key="question"
                      v-model:selected="quiz.selected"
                      :question="quiz.question"
                      :quiz-id="quiz.quizId"
                      :question-number="quiz.questionNumber"
                      :total-questions="quiz.state.totalQuestions"
                      :score="quiz.me?.score ?? quiz.state.score"
                      :feedback="quiz.feedback"
                      :pending="!!quiz.pending"
                      :can-select="quiz.canSelect"
                      :can-advance="quiz.canAdvance"
                      :completed="quiz.state.completed"
                      :submission="quiz.submission"
                      :fresh-feedback="freshFeedback"
                      @submit="quiz.submit()"
                      @advance="advance"
                    />
                    <CompletionPanel
                      v-else-if="quiz.complete"
                      key="completion"
                      :state="quiz.state"
                      :me="quiz.me"
                      :live="quiz.connection === 'live'"
                      @leave="quiz.leave()"
                    />
                  </Transition>
                </div>
                <QuestionTimer
                  v-if="!quiz.feedback && !quiz.complete && quiz.question"
                  :timing="quiz.question.timing"
                  :clock="
                    quiz.clock?.epoch === quiz.state.epoch &&
                    quiz.clock?.questionId === quiz.question.id
                      ? quiz.clock
                      : null
                  "
                  :received-at="quiz.clockReceivedAt"
                  :checking="!!quiz.pending"
                />
              </div>
            </Transition>
            <Transition name="tab" @before-leave="hide" @before-enter="reveal">
              <div
                v-show="!mobile || mobileView === 'standings'"
                id="standings-view"
                :class="{
                  'motion-hidden': mobile && mobileView !== 'standings',
                }"
                :role="mobile ? 'tabpanel' : undefined"
                :aria-labelledby="mobile ? 'tab-standings' : undefined"
              >
                <button
                  v-if="quiz.complete && !mobile"
                  class="completion-standings-toggle text-button"
                  :aria-expanded="showCompletionStandings"
                  aria-controls="completed-standings"
                  @click="showCompletionStandings = !showCompletionStandings"
                >
                  {{
                    showCompletionStandings
                      ? "Hide live standings"
                      : "View live standings"
                  }}
                  <span aria-hidden="true">{{
                    showCompletionStandings ? "↑" : "↓"
                  }}</span>
                </button>
                <div
                  v-show="!quiz.complete || mobile || showCompletionStandings"
                  :id="quiz.complete ? 'completed-standings' : undefined"
                >
                  <Standings
                    :rows="quiz.state.leaderboard"
                    :participant-id="quiz.state.participantId"
                    :count="quiz.state.participantCount"
                    :live="quiz.connection === 'live'"
                    :total="quiz.state.totalQuestions"
                    :active="
                      mobile
                        ? mobileView === 'standings'
                        : !quiz.complete || showCompletionStandings
                    "
                  />
                </div>
                <div v-if="quiz.feedback && !light" class="encouragement panel">
                  <AppIcon
                    :name="quiz.feedback.correctness ? 'bulb' : 'spark'"
                  />
                  <p>
                    <strong>{{
                      quiz.feedback.correctness
                        ? "Great job!"
                        : "That’s part of learning!"
                    }}</strong
                    ><span>{{
                      quiz.feedback.correctness
                        ? "Keep building your vocabulary."
                        : "Every mistake makes your vocabulary stronger."
                    }}</span>
                  </p>
                </div>
                <div v-else-if="!light" class="quote-card panel">
                  <span aria-hidden="true">“</span>
                  <p>Better words. Brighter conversations.</p>
                </div>
              </div>
            </Transition>
          </div>
        </main>
      </Transition>
    </div>
  </div>
</template>
