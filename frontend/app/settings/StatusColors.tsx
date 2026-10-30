"use client";

import { useState } from "react";
import { useStatusColors } from "@/app/components/StatusColorsProvider";
import { defaultStatusColors, statusDefinitions, statusSwatch } from "@/app/lib/statusColors";
import type { StatusColorSettings } from "@/app/lib/types";
import { controlClass } from "@/app/components/management/ManagementUI";

export function StatusColors() {
  const { settings, error: loadError, loading, reload, save } = useStatusColors();
  const [draft, setDraft] = useState<StatusColorSettings | null>(null);
  const [busy, setBusy] = useState(false), [error, setError] = useState(""), [notice, setNotice] = useState("");
  const current = draft ?? settings;
  const stale = !!draft && !!settings && draft.version !== settings.version;
  if (!current) return <div className="space-y-3"><p role={loadError ? "alert" : "status"}>{loadError || "Loading status colors…"}</p>{!loading && <button className="ui-button" onClick={() => void reload()}>Retry</button>}</div>;
  return <section className="space-y-4" aria-label="Status colors">
    <div className="flex flex-wrap items-center justify-between gap-3"><p className="text-zinc-400">Company-wide colors. Changes apply to every user and page.</p><div className="flex flex-wrap gap-2">
      <button className="ui-button" disabled={busy} onClick={() => { setDraft({ colors: {}, version: current.version }); setNotice(""); }}>Restore defaults</button>
      {draft && <button className="ui-button" disabled={busy} onClick={() => { setDraft(null); setError(""); }}>Discard changes</button>}
      <button className="ui-button ui-button-primary" disabled={!draft || busy || stale} onClick={async () => { setBusy(true); setError(""); setNotice(""); try { await save(current); setDraft(null); setNotice("Saved"); } catch (e) { setError(e instanceof Error ? e.message : "Could not save status colors"); } finally { setBusy(false); } }}>{busy ? "Saving…" : "Save colors"}</button>
    </div></div>
    {(error || loadError) && <p role="alert" className="text-red-300">{error || loadError}<button className="ui-button ml-3" disabled={busy} onClick={() => void reload()}>Reload saved colors</button></p>}
    {stale && <p role="alert" className="text-amber-300">Colors changed in another window. Discard your changes to load the latest version.</p>}
    {notice && <p role="status" className="text-emerald-400">{notice}</p>}
    <div className="overflow-x-auto rounded-lg border border-zinc-800"><table className="w-full text-left"><thead><tr><th className="px-4 py-2">Status</th><th className="px-4 py-2">Color</th><th className="px-4 py-2">Light / dark preview</th></tr></thead><tbody>{statusDefinitions.map(([key, label]) => {
      const color = current.colors[key] ?? defaultStatusColors[key];
      return <tr key={key}><td className="px-4 py-2">{label}</td><td className="px-4 py-2"><label className="inline-flex items-center gap-2"><input type="color" aria-label={`${label} color`} className={`${controlClass} !w-12 !p-1`} disabled={busy} value={color} onChange={e => { setDraft({ ...current, colors: { ...current.colors, [key]: e.target.value } }); setNotice(""); }} /><span className="text-xs text-zinc-400">{color.toUpperCase()}</span></label></td><td className="px-4 py-2"><div className="flex flex-wrap gap-2">{["#ffffff", "#111113"].map(surface => <span key={surface} className="inline-flex rounded p-1" style={{ background: surface }}><span className="rounded px-2 py-0.5 text-xs" style={statusSwatch(color, surface)}>{label}</span></span>)}</div></td></tr>;
    })}</tbody></table></div>
  </section>;
}
