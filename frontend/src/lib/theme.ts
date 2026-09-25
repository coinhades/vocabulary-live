export type Theme = "light" | "dark";

// public/theme.js applies the same rule before first paint.
export const themeStorageKey = "vocabulary.theme";
export const themeColors: Record<Theme, string> = {
  light: "#f6f5f0",
  dark: "#070c25",
};

// An explicit choice wins; otherwise follow the operating-system preference.
export function resolveTheme(saved: unknown, prefersLight: boolean): Theme {
  if (saved === "light" || saved === "dark") return saved;
  return prefersLight ? "light" : "dark";
}
