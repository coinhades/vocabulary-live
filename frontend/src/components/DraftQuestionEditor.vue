<script setup lang="ts">
import { useId } from "vue";
import { levels, type ItemContent } from "../types/authoring";
const content = defineModel<ItemContent>({ required: true });
defineProps<{
  number: number;
  approved: boolean;
  dirty: boolean;
  busy: boolean;
  locked: boolean;
  textConfigured: boolean;
  canApprove: boolean;
}>();
const emit = defineEmits<{ approve: []; regenerate: [] }>();
const id = useId();
function tags(event: Event) {
  content.value.tags = (event.target as HTMLInputElement).value
    .split(",")
    .map((v) => v.trim())
    .filter(Boolean);
}
function acceptTags() {
  content.value.tags = [
    ...new Set([...content.value.tags, ...content.value.suggestedTags]),
  ].slice(0, 5);
}
</script>
<template>
  <article class="studio-question panel">
    <div class="learning-heading">
      <h3>
        Question {{ number }} <span>· {{ content.word }}</span>
      </h3>
      <span class="studio-status">{{
        dirty
          ? "Unsaved · needs review"
          : approved
            ? "Approved"
            : "Needs review"
      }}</span>
    </div>
    <fieldset :disabled="locked || busy" class="studio-fields">
      <legend class="sr-only">Question {{ number }} content</legend>
      <label
        >Headword<input v-model="content.word" maxlength="80" required
      /></label>
      <label
        >Intended sense<input v-model="content.sense" maxlength="200" required
      /></label>
      <label
        >Part of speech<select v-model="content.partOfSpeech">
          <option v-for="v in ['noun', 'verb', 'adjective', 'adverb']" :key="v">
            {{ v }}
          </option>
        </select></label
      >
      <label
        >Pronunciation (IPA, optional)<input
          v-model="content.pronunciation"
          maxlength="100"
      /></label>
      <label class="studio-wide"
        >Canonical meaning<textarea
          v-model="content.meaning"
          maxlength="600"
          rows="2"
          required
        />
      </label>
      <label class="studio-wide"
        >Question stem<textarea
          v-model="content.prompt"
          maxlength="320"
          rows="2"
          required
        />
      </label>
      <fieldset class="studio-options studio-wide">
        <legend>Answer choices</legend>
        <label v-for="(_, index) in content.options" :key="index"
          >Option {{ String.fromCharCode(65 + index)
          }}<input v-model="content.options[index]" maxlength="240" required
        /></label>
      </fieldset>
      <label
        >Correct option<select v-model.number="content.correctIndex">
          <option
            v-for="(_, index) in content.options"
            :key="index"
            :value="index"
          >
            Option {{ String.fromCharCode(65 + index) }}
          </option>
        </select></label
      >
      <label
        >Difficulty / timer<select v-model="content.difficulty">
          <option value="easy">Easy · 20 seconds</option>
          <option value="medium">Medium · 30 seconds</option>
          <option value="hard">Hard · 40 seconds</option>
        </select></label
      >
      <label class="studio-wide"
        >Canonical explanation<textarea
          v-model="content.explanation"
          maxlength="800"
          rows="3"
          required
        />
      </label>
      <label class="studio-wide"
        >Example sentence<textarea
          v-model="content.example"
          maxlength="280"
          rows="2"
          required
        />
      </label>
      <label
        >Target level<select v-model="content.level">
          <option v-for="v in levels" :key="v">{{ v }}</option>
        </select></label
      >
      <label
        >Reviewed tags (comma separated)<input
          :value="content.tags.join(', ')"
          maxlength="204"
          @change="tags"
      /></label>
      <div class="studio-wide studio-suggestions">
        <p>Suggested tags: {{ content.suggestedTags.join(", ") || "None" }}</p>
        <button
          v-if="content.suggestedTags.length"
          type="button"
          class="learning-button"
          @click="acceptTags"
        >
          Accept suggested tags
        </button>
      </div>
      <div class="studio-wide studio-check">
        <input
          :id="`${id}-pronunciation`"
          v-model="content.pronunciationVisible"
          type="checkbox"
        /><label :for="`${id}-pronunciation`"
          >Allow pronunciation before answering. The exact headword must appear
          in the stem.</label
        >
      </div>
    </fieldset>
    <div v-if="!locked" class="learning-actions">
      <button
        type="button"
        class="learning-button"
        :disabled="busy || !textConfigured"
        @click="emit('regenerate')"
      >
        Preview a new version</button
      ><button
        type="button"
        class="learning-button"
        :disabled="busy || dirty || approved || !canApprove"
        @click="emit('approve')"
      >
        Approve question {{ number }}
      </button>
    </div>
    <p class="learning-disclosure">
      {{
        dirty
          ? "Save changes before approving."
          : "Review the sense, key, distractors and teaching text before approving."
      }}
    </p>
  </article>
</template>
