"use client";

import { createContext, useCallback, useContext, useEffect, useState } from "react";
import { fetchStatusColors, saveStatusColors } from "@/app/lib/api";
import type { StatusColorSettings } from "@/app/lib/types";
import { defaultStatusColors, statusSwatch } from "@/app/lib/statusColors";
import { useTheme } from "./ThemeProvider";

const Context = createContext<{
  settings: StatusColorSettings | null; error: string; loading: boolean;
  reload: () => Promise<void>; save: (input: StatusColorSettings) => Promise<void>;
} | null>(null);

export function StatusColorsProvider({ children }: { children: React.ReactNode }) {
  const { theme } = useTheme();
  const [settings, setSettings] = useState<StatusColorSettings | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const reload = useCallback(async () => {
    try { const next = await fetchStatusColors(); setSettings(previous => !previous || next.version >= previous.version ? next : previous); setError(""); }
    catch (e) { setError(e instanceof Error ? e.message : "Could not load status colors"); }
    finally { setLoading(false); }
  }, []);
  useEffect(() => {
    let active = true;
    fetchStatusColors().then(value => { if (active) { setSettings(value); setError(""); } }).catch(e => { if (active) setError(e instanceof Error ? e.message : "Could not load status colors"); }).finally(() => { if (active) setLoading(false); });
    const refresh = () => { if (document.visibilityState === "visible") void reload(); };
    const timer = setInterval(refresh, 30000);
    window.addEventListener("focus", refresh); document.addEventListener("visibilitychange", refresh);
    return () => { active = false; clearInterval(timer); window.removeEventListener("focus", refresh); document.removeEventListener("visibilitychange", refresh); };
  }, [reload]);
  useEffect(() => {
    const root = document.documentElement;
    const surface = getComputedStyle(root).getPropertyValue("--card").trim() || "#111113";
    const colors = { ...defaultStatusColors, ...settings?.colors };
    for (const [key, base] of Object.entries(colors)) {
      const swatch = statusSwatch(base, surface);
      root.style.setProperty(`--status-${key}`, base);
      root.style.setProperty(`--status-${key}-bg`, swatch.background);
      root.style.setProperty(`--status-${key}-fg`, swatch.color);
    }
    return () => { for (const key of Object.keys(colors)) for (const suffix of ["", "-bg", "-fg"]) root.style.removeProperty(`--status-${key}${suffix}`); };
  }, [settings, theme]);
  async function save(input: StatusColorSettings) {
    const next = await saveStatusColors(input);
    setSettings(previous => !previous || next.version >= previous.version ? next : previous);
    setError("");
  }
  return <Context.Provider value={{ settings, error, loading, reload, save }}>{children}</Context.Provider>;
}
export function useStatusColors() { const value = useContext(Context); if (!value) throw new Error("StatusColorsProvider is required"); return value; }
