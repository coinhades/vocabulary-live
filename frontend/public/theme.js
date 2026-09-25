/* global document, localStorage, matchMedia */
// Applies the saved or system theme before first paint, so a light-theme
// visitor never sees a dark frame. src/lib/theme.ts owns the same rule.
(() => {
  let theme = null;
  try {
    theme = localStorage.getItem("vocabulary.theme");
  } catch {
    /* Storage can be unavailable; fall back to the system preference. */
  }
  if (theme !== "light" && theme !== "dark")
    theme = matchMedia("(prefers-color-scheme: light)").matches
      ? "light"
      : "dark";
  document.documentElement.dataset.theme = theme;
  document
    .querySelector('meta[name="theme-color"]')
    ?.setAttribute("content", theme === "light" ? "#f6f5f0" : "#070c25");
})();
