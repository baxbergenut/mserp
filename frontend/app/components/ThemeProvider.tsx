"use client";

import { createContext, useContext, useLayoutEffect, useRef, useState } from "react";
import { saveTheme } from "@/app/lib/api";
import { normalizeTheme, type ThemeId } from "@/app/lib/themes";

const ThemeContext = createContext<{
  theme: ThemeId;
  saving: boolean;
  error: string;
  choose: (theme: ThemeId) => Promise<void>;
} | null>(null);

export function ThemeProvider({ initialTheme, children }: { initialTheme: string; children: React.ReactNode }) {
  const [theme, setTheme] = useState(() => normalizeTheme(initialTheme));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const pending = useRef(false);

  // Apply before the authenticated UI paints, including portals attached to body.
  useLayoutEffect(() => {
    document.documentElement.dataset.theme = theme;
    return () => { delete document.documentElement.dataset.theme; };
  }, [theme]);

  async function choose(next: ThemeId) {
    if (pending.current || next === theme) return;
    const previous = theme;
    pending.current = true;
    setSaving(true);
    setError("");
    setTheme(next);
    try {
      await saveTheme(next);
    } catch (e) {
      setTheme(previous);
      setError(e instanceof Error ? e.message : "Could not save theme. Please try again.");
    } finally {
      pending.current = false;
      setSaving(false);
    }
  }

  return <ThemeContext.Provider value={{ theme, saving, error, choose }}>{children}</ThemeContext.Provider>;
}

export function useTheme() {
  const context = useContext(ThemeContext);
  if (!context) throw new Error("ThemeProvider is required");
  return context;
}
