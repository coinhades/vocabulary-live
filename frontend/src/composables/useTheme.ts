import { computed, readonly, ref } from "vue";
import { resolveTheme, themeColors, themeStorageKey } from "../lib/theme";
import type { Theme } from "../lib/theme";

const system = matchMedia("(prefers-color-scheme: light)");
function saved() {
  try {
    return localStorage.getItem(themeStorageKey);
  } catch {
    return null;
  }
}
const theme = ref<Theme>(resolveTheme(saved(), system.matches));
function apply(value: Theme) {
  theme.value = value;
  document.documentElement.dataset.theme = value;
  document
    .querySelector('meta[name="theme-color"]')
    ?.setAttribute("content", themeColors[value]);
}
apply(theme.value);
system.addEventListener("change", (event) => {
  if (!saved()) apply(resolveTheme(null, event.matches));
});
addEventListener("storage", (event) => {
  if (event.key === themeStorageKey)
    apply(resolveTheme(event.newValue, system.matches));
});

// Theme is presentation only; switching never touches quiz or session state.
export function useTheme() {
  return {
    theme: readonly(theme),
    light: computed(() => theme.value === "light"),
    toggle() {
      const next = theme.value === "light" ? "dark" : "light";
      try {
        localStorage.setItem(themeStorageKey, next);
      } catch {
        /* The choice still applies to this page. */
      }
      apply(next);
    },
  };
}
