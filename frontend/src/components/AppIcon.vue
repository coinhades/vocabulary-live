<script setup lang="ts">
defineProps<{
  name:
    | "people"
    | "chart"
    | "bolt"
    | "clock"
    | "trophy"
    | "bulb"
    | "spark"
    | "book"
    | "wifi"
    | "logout"
    | "refresh"
    | "sun"
    | "moon"
    | "check"
    | "close"
    | "chevron-left"
    | "chevron-right"
    | "speaker";
}>();
</script>

<template>
  <svg
    class="app-icon"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.7"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <g v-if="name === 'people'">
      <circle cx="12" cy="7" r="3" />
      <path
        d="M6 21v-3a6 6 0 0 1 12 0v3M5 5a3 3 0 0 0 0 6m14-6a3 3 0 0 1 0 6M2 19v-2a4 4 0 0 1 3-4m17 6v-2a4 4 0 0 0-3-4"
      />
    </g>
    <g v-else-if="name === 'chart'" fill="currentColor" stroke="none">
      <rect x="3" y="13" width="4" height="8" rx="1" />
      <rect x="10" y="8" width="4" height="13" rx="1" />
      <rect x="17" y="3" width="4" height="18" rx="1" />
    </g>
    <path
      v-else-if="name === 'bolt'"
      d="m14 2-10 12h7l-1 8 10-13h-7z"
      fill="currentColor"
      stroke="none"
    />
    <g v-else-if="name === 'clock'">
      <circle cx="12" cy="12" r="9" />
      <path d="M12 6v6h5" />
    </g>
    <g v-else-if="name === 'trophy'">
      <path
        d="M7 3h10v7a5 5 0 0 1-10 0zm0 1H3v3a5 5 0 0 0 5 5m9-8h4v3a5 5 0 0 1-5 5M12 15v5m-5 1h10"
      />
    </g>
    <g v-else-if="name === 'bulb'">
      <path
        d="M9 18h6m-5 3h4m-6-8a6 6 0 1 1 8 0c-1 1-1 2-1 3H9c0-1 0-2-1-3M12 1v1M2 10H1m22 0h-1M4 3l1 1m15-1-1 1"
      />
    </g>
    <path
      v-else-if="name === 'book'"
      d="M12 6.5C10 5 7 4.5 3 5v13c4-.5 7 0 9 1.5 2-1.5 5-2 9-1.5V5c-4-.5-7 0-9 1.5zm0 0V19"
    />
    <g v-else-if="name === 'wifi'">
      <path
        d="M2.5 9a14 14 0 0 1 19 0M5.5 12.5a9.5 9.5 0 0 1 13 0M8.5 16a5 5 0 0 1 7 0"
      />
      <circle cx="12" cy="19.5" r="1.2" fill="currentColor" stroke="none" />
    </g>
    <path
      v-else-if="name === 'logout'"
      d="M9 4H6a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h3m7-4 4-4-4-4m4 4H10"
    />
    <path
      v-else-if="name === 'refresh'"
      d="M20 11a8 8 0 0 0-14.3-4.9L4 8m0-4v4h4m-4 5a8 8 0 0 0 14.3 4.9L20 16m0 4v-4h-4"
    />
    <g v-else-if="name === 'sun'">
      <circle cx="12" cy="12" r="4" />
      <path
        d="M12 2v2m0 16v2M4.9 4.9l1.4 1.4m11.4 11.4 1.4 1.4M2 12h2m16 0h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"
      />
    </g>
    <path
      v-else-if="name === 'moon'"
      d="M20 14.5A8.5 8.5 0 0 1 9.5 4a8.5 8.5 0 1 0 10.5 10.5z"
    />
    <g v-else-if="name === 'speaker'">
      <path d="M11 4 6 8H3v8h3l5 4z" />
      <path class="speaker-wave speaker-wave-near" d="M15 8a6 6 0 0 1 0 8" />
      <path class="speaker-wave speaker-wave-far" d="M18 5a10 10 0 0 1 0 14" />
    </g>
    <path v-else-if="name === 'chevron-left'" d="m14 6-6 6 6 6" />
    <path v-else-if="name === 'chevron-right'" d="m10 6 6 6-6 6" />
    <path v-else-if="name === 'check'" d="m5 12.5 4.5 4.5L19 7.5" />
    <path v-else-if="name === 'close'" d="M6 6l12 12M18 6 6 18" />
    <path
      v-else
      d="m12 2 2.5 7.5L22 12l-7.5 2.5L12 22l-2.5-7.5L2 12l7.5-2.5z"
    />
  </svg>
</template>
