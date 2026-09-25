<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import type { Row } from "../types/protocol";
import ChangingValue from "./ChangingValue.vue";
import AppIcon from "./AppIcon.vue";
import { useTheme } from "../composables/useTheme";
const { light } = useTheme();
const props = defineProps<{
  rows: Row[];
  participantId: string;
  count: number;
  live: boolean;
  total: number;
  active: boolean;
}>();
const root = ref<HTMLElement>();
function avatar(id: string) {
  let hash = 0;
  for (const character of id)
    hash = (hash * 31 + character.charCodeAt(0)) >>> 0;
  return hash % 6;
}
const scrolling = ref(false);
const onScreen = ref(true);
const ownRankChange = ref(0);
let observer: IntersectionObserver | undefined;
let scrollTimer: ReturnType<typeof setTimeout> | undefined;
let settleAfter = 0;
// The full list remains present. Dense rooms update immediately instead of
// spending a frame budget moving hundreds of rows, including offscreen ones.
const canMove = computed(
  () =>
    props.active &&
    props.live &&
    onScreen.value &&
    !scrolling.value &&
    props.rows.length <= 40,
);
watch(
  () => [props.rows, props.live] as const,
  ([rows, live], [previous, wasLive]) => {
    const before = previous.find(
      (row) => row.participantId === props.participantId,
    );
    const after = rows.find((row) => row.participantId === props.participantId);
    if (
      live &&
      wasLive &&
      canMove.value &&
      before &&
      after &&
      before.rank !== after.rank
    )
      ownRankChange.value++;
  },
);
function pauseForScroll() {
  scrolling.value = true;
  clearTimeout(scrollTimer);
  scrollTimer = setTimeout(() => {
    scrolling.value = false;
  }, settleAfter);
}
onMounted(() => {
  settleAfter = Number.parseFloat(
    getComputedStyle(document.documentElement).getPropertyValue(
      "--motion-scroll-idle",
    ),
  );
  observer = new IntersectionObserver(([entry]) => {
    onScreen.value = entry.isIntersecting;
  });
  if (root.value) observer.observe(root.value);
});
onUnmounted(() => {
  clearTimeout(scrollTimer);
  observer?.disconnect();
});
</script>

<template>
  <aside
    ref="root"
    class="standings panel"
    :class="{ 'standings-motion-paused': !canMove }"
    aria-labelledby="standings-title"
  >
    <div class="panel-heading">
      <div>
        <h2 id="standings-title">Live Leaderboard</h2>
      </div>
      <span v-if="light" class="standings-count"
        ><AppIcon name="people" />{{ count
        }}<span class="sr-only">
          {{ count === 1 ? "person has" : "people have" }} joined</span
        ></span
      >
      <span
        v-else
        class="live-dot"
        :class="{ muted: !live }"
        aria-hidden="true"
      ></span>
    </div>
    <p v-if="!light" class="standings-caption">
      {{ count }} {{ count === 1 ? "person has" : "people have" }} joined ·
      everyone counts
    </p>
    <p v-if="!live && !light" class="standings-sync">
      Standings synchronizing · last known scores
    </p>
    <div
      class="standings-scroll"
      tabindex="0"
      aria-label="All participants, scroll to see more"
      @scroll.passive="pauseForScroll"
      @wheel.passive="pauseForScroll"
      @touchmove.passive="pauseForScroll"
    >
      <table>
        <thead>
          <tr>
            <th scope="col">Rank</th>
            <th scope="col">{{ light ? "Name" : "Player" }}</th>
            <th scope="col" class="score-cell">
              {{ light ? "Score" : "Points" }}
            </th>
            <th v-if="light" scope="col" class="answered-cell">Answered</th>
          </tr>
        </thead>
        <TransitionGroup
          tag="tbody"
          name="standings-row"
          :css="canMove"
          :move-class="canMove ? 'standings-move' : 'standings-static'"
        >
          <tr
            v-for="row in rows"
            :key="row.participantId"
            :class="{ 'is-you': row.participantId === participantId }"
            :data-participant-id="row.participantId"
          >
            <td class="rank" :data-rank="row.rank">
              <span
                v-if="row.participantId === participantId && ownRankChange"
                :key="ownRankChange"
                class="row-ack"
                aria-hidden="true"
              ></span>
              <AppIcon
                v-if="light && row.rank === 1"
                name="trophy"
                class="rank-trophy"
              /><span class="rank-number">{{ row.rank }}</span>
            </td>
            <th scope="row">
              <span
                v-if="row.participantId === participantId && ownRankChange"
                :key="ownRankChange"
                class="row-ack"
                aria-hidden="true"
              ></span>
              <span
                class="player-avatar"
                :class="`avatar-${avatar(row.participantId)}`"
                aria-hidden="true"
              ></span
              ><span class="player-name">{{ row.displayName }}</span
              ><span v-if="row.participantId === participantId" class="you-tag"
                >You</span
              ><small v-if="!light"
                >{{ row.answeredCount }} / {{ total }} answered</small
              >
            </th>
            <td class="score-cell">
              <span
                v-if="row.participantId === participantId && ownRankChange"
                :key="ownRankChange"
                class="row-ack"
                aria-hidden="true"
              ></span>
              <strong
                ><ChangingValue :value="row.score" :animate="canMove"
              /></strong>
            </td>
            <td v-if="light" class="answered-cell">
              <span
                v-if="row.participantId === participantId && ownRankChange"
                :key="ownRankChange"
                class="row-ack"
                aria-hidden="true"
              ></span
              >{{ row.answeredCount }}<small>/{{ total }}</small>
            </td>
          </tr>
        </TransitionGroup>
      </table>
    </div>
    <div v-if="light" class="standings-sync-card" :class="{ stale: !live }">
      <AppIcon name="wifi" />
      <p>
        <strong>{{
          live ? "Standings are live" : "Standings synchronizing"
        }}</strong
        ><span>{{
          live
            ? "Same questions for everyone. Equal points share a rank."
            : "Showing last known scores. Everyone stays listed."
        }}</span>
      </p>
    </div>
    <p v-else class="fine-print standings-note">
      Equal points share a rank. Scores stay when someone disconnects.
    </p>
  </aside>
</template>
