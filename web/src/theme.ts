export type Theme = "light" | "dark";

const KEY = "ghosttraffic-theme";

function system(): Theme {
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

export function currentTheme(): Theme {
  const saved = localStorage.getItem(KEY);
  return saved === "light" || saved === "dark" ? saved : system();
}

export function applyTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme;
}

export function chooseTheme(theme: Theme) {
  localStorage.setItem(KEY, theme);
  applyTheme(theme);
}
