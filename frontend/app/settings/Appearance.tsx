"use client";

import { Check } from "lucide-react";
import { themes } from "@/app/lib/themes";
import { useTheme } from "@/app/components/ThemeProvider";

export function Appearance() {
  const { theme, choose, saving, error } = useTheme();
  return <section aria-labelledby="appearance-heading" className="space-y-4">
    <div><h2 id="appearance-heading">Color theme</h2><p className="mt-1 text-zinc-400">Saved to your account. Applies across all pages.</p></div>
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4" role="group" aria-label="Color theme">
      {themes.map(item => <button key={item.id} type="button" aria-pressed={theme === item.id} disabled={saving}
        onClick={() => void choose(item.id)}
        className={`theme-choice overflow-hidden rounded-lg border text-left transition-colors ${theme === item.id ? "border-accent ring-1 ring-accent" : "border-zinc-800 hover:border-zinc-500"}`}>
        <div aria-hidden="true" className="flex h-24 border-b border-zinc-800" style={{ background: item.background }}>
          <div className="w-1/4 space-y-2 p-3" style={{ background: item.sidebar }}>
            <div className="h-1.5 w-7 rounded" style={{ background: item.accent }} />
            {[0, 1, 2].map(i => <div key={i} className="h-1 w-full rounded opacity-40" style={{ background: item.foreground }} />)}
          </div>
          <div className="flex-1 space-y-2 p-3">
            <div className="h-2 w-1/3 rounded" style={{ background: item.foreground }} />
            <div className="flex gap-2">{[0, 1, 2].map(i => <div key={i} className="h-5 flex-1 rounded border" style={{ borderColor: `${item.foreground}40`, background: `${item.accent}18` }} />)}</div>
            <div className="h-1 w-full rounded opacity-30" style={{ background: item.foreground }} />
            <div className="h-1 w-3/4 rounded opacity-30" style={{ background: item.foreground }} />
          </div>
        </div>
        <div className="p-4"><div className="flex items-center justify-between gap-2 font-medium text-zinc-100">{item.name}{theme === item.id && <Check aria-hidden="true" className="text-accent" />}</div><p className="mt-1 text-xs text-zinc-400">{item.description}</p></div>
      </button>)}
    </div>
    <p role="status" className="text-xs text-zinc-400">{saving ? "Saving theme…" : `Current theme: ${themes.find(item => item.id === theme)?.name}`}</p>
    {error && <p role="alert" className="text-red-300">{error}</p>}
  </section>;
}
