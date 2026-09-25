<script setup lang="ts">
import { nextTick, onUnmounted, ref, useId, watch } from "vue";
import AppIcon from "./AppIcon.vue";
const props = defineProps<{
  open: boolean;
  title: string;
  word: string;
  partOfSpeech: string;
}>();
const emit = defineEmits<{ close: [] }>();
const titleId = useId();
const dialog = ref<HTMLDialogElement>();
let opener: HTMLElement | null = null;
let overflow: string | undefined;
function restore() {
  if (overflow === undefined) return;
  document.documentElement.style.overflow = overflow;
  overflow = undefined;
  if (
    opener?.isConnected &&
    !opener.closest("[inert]") &&
    !opener.matches(":disabled")
  )
    opener.focus({ preventScroll: true });
  else
    (
      document.querySelector<HTMLElement>(
        ".question-content:not([inert]) .feedback-next",
      ) ??
      document.querySelector<HTMLElement>("#question-heading:not(.sr-only)")
    )?.focus({ preventScroll: true });
  opener = null;
}
watch(
  () => props.open,
  async (open) => {
    await nextTick();
    const el = dialog.value;
    if (!el || open !== props.open) return;
    if (open && !el.open) {
      opener =
        document.activeElement instanceof HTMLElement
          ? document.activeElement
          : null;
      overflow = document.documentElement.style.overflow;
      document.documentElement.style.overflow = "hidden";
      el.showModal();
      el.querySelector<HTMLButtonElement>("[data-modal-close]")?.focus({
        preventScroll: true,
      });
    } else if (!open && el.open) {
      el.close();
      restore();
    }
  },
  { flush: "post" },
);
function backdrop(event: MouseEvent) {
  if (event.target !== dialog.value || !dialog.value) return;
  const rect = dialog.value.getBoundingClientRect();
  if (
    event.clientX < rect.left ||
    event.clientX > rect.right ||
    event.clientY < rect.top ||
    event.clientY > rect.bottom
  )
    emit("close");
}
function containFocus(event: KeyboardEvent) {
  if (event.key !== "Tab" || !dialog.value) return;
  const controls = [
    ...dialog.value.querySelectorAll<HTMLElement>(
      "button:not(:disabled), select:not(:disabled), input:not(:disabled), textarea:not(:disabled), a[href], [tabindex]:not([tabindex='-1'])",
    ),
  ].filter((el) => el.getClientRects().length > 0 && !el.closest("[inert]"));
  const first = controls[0],
    last = controls.at(-1);
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last?.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first?.focus();
  }
}
onUnmounted(() => {
  dialog.value?.close();
  restore();
});
</script>
<template>
  <Teleport to="body">
    <dialog
      ref="dialog"
      class="learning-dialog"
      :aria-labelledby="titleId"
      @cancel.prevent="emit('close')"
      @click="backdrop"
      @keydown="containFocus"
      @close="!dialog?.open && restore()"
    >
      <header class="dialog-header">
        <div>
          <p class="dialog-word">
            {{ word }} <span>{{ partOfSpeech }}</span>
          </p>
          <h2 :id="titleId">{{ title }}</h2>
        </div>
        <button
          type="button"
          class="icon-button dialog-close"
          data-modal-close
          aria-label="Close learning window"
          @click="emit('close')"
        >
          <AppIcon name="close" />
        </button>
      </header>
      <div class="dialog-body"><slot /></div>
      <footer class="dialog-footer"><slot name="footer" /></footer>
    </dialog>
  </Teleport>
</template>
