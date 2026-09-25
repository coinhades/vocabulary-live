<script setup lang="ts">
import type { Option } from "../types/protocol";

withDefaults(
  defineProps<{
    options: Option[];
    legend: string;
    disabled: boolean;
    checking?: boolean;
    variant?: "practice" | "review";
  }>(),
  { variant: "practice", checking: false },
);
const selected = defineModel<string>({ required: true });
</script>

<template>
  <fieldset
    :class="[
      variant === 'review' ? 'review-options' : 'options',
      { 'is-checking': checking },
    ]"
    :disabled="disabled"
  >
    <legend class="sr-only">{{ legend }}</legend>
    <label
      v-for="(option, index) in options"
      :key="option.id"
      :class="
        variant === 'review'
          ? ['review-option', { 'is-selected': selected === option.id }]
          : ['answer-option', { selected: selected === option.id }]
      "
    >
      <input
        v-model="selected"
        type="radio"
        :name="variant === 'review' ? 'review-option' : 'answer'"
        :value="option.id"
      />
      <span class="option-letter" aria-hidden="true">{{
        String.fromCharCode(65 + index)
      }}</span>
      <span class="option-text">{{ option.text }}</span>
    </label>
  </fieldset>
</template>
