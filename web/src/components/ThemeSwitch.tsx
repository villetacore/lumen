import { useState } from "react";
import Icon from "./Icon";
import { getTheme, saveTheme, type Theme } from "../lib/theme";
const order: Theme[] = ["system", "light", "dark"];
const labels = { system: "как в системе", light: "светлая", dark: "тёмная" };
export default function ThemeSwitch() {
  const [theme, setTheme] = useState(getTheme);
  return <button className="theme-switch" onClick={() => {
    const next = order[(order.indexOf(theme) + 1) % order.length];
    saveTheme(next); setTheme(next);
  }} aria-label={`Тема: ${labels[theme]}. Переключить`}><Icon name={theme === "dark" ? "moon" : theme === "light" ? "sun" : "theme"} />Тема: {labels[theme]}</button>;
}
