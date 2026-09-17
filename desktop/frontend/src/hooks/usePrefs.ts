import { useEffect, useState } from "react";
import { getLang, type Lang } from "~/lib/i18n";
import type { ThemeId } from "~/lib/themes";

export function usePrefs() {
  const [lang, setLang] = useState<Lang>(getLang);
  const [theme, setTheme] = useState<ThemeId>(
    () => (localStorage.getItem("aether.theme") as ThemeId) || "aether",
  );

  useEffect(() => {
    const sync = () => {
      setLang(getLang());
      setTheme((localStorage.getItem("aether.theme") as ThemeId) || "aether");
    };
    window.addEventListener("aether:prefs", sync);
    return () => window.removeEventListener("aether:prefs", sync);
  }, []);

  return { lang, theme };
}
