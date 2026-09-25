<script setup lang="ts">
import { computed } from "vue";
import { questionPromptParts } from "../lib/question-prompt";
const props = defineProps<{ text: string; word?: string }>();
const parts = computed(() => questionPromptParts(props.text, props.word));
</script>
<template>
  <span
    ><template v-for="(part, index) in parts" :key="index"
      ><em v-if="part.emphasized">{{ part.text }}</em
      ><template v-else>{{ part.text }}</template></template
    ></span
  >
</template>
