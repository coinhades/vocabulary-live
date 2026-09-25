<script setup lang="ts">
import { retire } from "../lib/motion";
withDefaults(defineProps<{ value: string | number; animate?: boolean }>(), {
  animate: true,
});
</script>

<template>
  <span class="changing-value">
    <Transition name="value" :css="animate" @before-leave="retire">
      <span :key="value" class="changing-value-current">{{ value }}</span>
    </Transition>
  </span>
</template>
