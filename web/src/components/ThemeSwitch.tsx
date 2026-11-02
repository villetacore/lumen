import { useState } from "react";
import { IconButton } from "@villetacore/velessoft-ui";
import Icon from "./Icon";
import { getTheme, saveTheme, type Theme } from "../lib/theme";
const order: Theme[] = ["system", "light", "dark"];
const labels = { system: "как в системе", light: "светлая", dark: "тёмная" };
export default function ThemeSwitch() {
  const [theme, setTheme] = useState(getTheme);
  return <IconButton variant="ghost" size="sm" label={`Тема: ${labels[theme]}. Переключить`} onClick={() => {
    const next = order[(order.indexOf(theme) + 1) % order.length];
    saveTheme(next); setTheme(next);
  }}><Icon name={theme === "dark" ? "moon" : theme === "light" ? "sun" : "theme"} size={16} /></IconButton>;
}
