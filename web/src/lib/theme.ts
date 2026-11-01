export type Theme = "system" | "light" | "dark";
export function getTheme(): Theme {
  try {
    const saved = localStorage.getItem("lumen-theme");
    if (saved === "light" || saved === "dark") return saved;
  } catch { /* Storage is optional. */ }
  return "system";
}
export function applyTheme(theme: Theme) {
  if (theme === "system") delete document.documentElement.dataset.theme;
  else document.documentElement.dataset.theme = theme;
}
export function saveTheme(theme: Theme) {
  applyTheme(theme);
  try { localStorage.setItem("lumen-theme", theme); } catch { /* Storage is optional. */ }
}
