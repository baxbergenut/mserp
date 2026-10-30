"use client";

import { useState } from "react";
import { useStatusColors } from "@/app/components/StatusColorsProvider";
import { contrast, statusDefinitions, statusPair, type StatusColorKey } from "@/app/lib/statusColors";
import type { StatusColorSettings } from "@/app/lib/types";
import { controlClass } from "@/app/components/management/ManagementUI";

export function StatusColors() {
  const { settings, error: loadError, loading, reload, save } = useStatusColors();
  const [draft, setDraft] = useState<StatusColorSettings | null>(null);
  const [busy, setBusy] = useState(false), [error, setError] = useState(""), [notice, setNotice] = useState("");
  const current = draft ?? settings;
  const stale = !!draft && !!settings && draft.version !== settings.version;
  if (!current) return <div className="space-y-3"><p role={loadError ? "alert" : "status"}>{loadError || "Loading status colors…"}</p>{!loading && <button className="ui-button" onClick={() => void reload()}>Retry</button>}</div>;
  function editColor(key: StatusColorKey, part: "foreground" | "background", value: string) {
    if (!current) return;
    const next = { ...statusPair(key, current.colors), [part]: value };
    const colors = { ...current.colors, [key]: next.background, [`${key}.fg`]: next.foreground, [`${key}.bg`]: next.background };
    if (key === "driver_ready") { delete colors.truck_ready; delete colors["truck_ready.fg"]; delete colors["truck_ready.bg"]; }
    setDraft({ ...current, colors }); setNotice("");
  }
  return <section className="space-y-4" aria-label="Status colors">
    <div className="flex flex-wrap items-center justify-between gap-3"><p className="text-zinc-400">One foreground/background pair per status, shared across every page. Defaults match the original Status Board.</p><div className="flex flex-wrap gap-2">
      <button className="ui-button" disabled={busy} onClick={() => { setDraft({ colors: {}, version: current.version }); setNotice(""); }}>Restore defaults</button>
      {draft && <button className="ui-button" disabled={busy} onClick={() => { setDraft(null); setError(""); }}>Discard changes</button>}
      <button className="ui-button ui-button-primary" disabled={!draft || busy || stale} onClick={async () => { setBusy(true); setError(""); setNotice(""); try { await save(current); setDraft(null); setNotice("Saved"); } catch (e) { setError(e instanceof Error ? e.message : "Could not save status colors"); } finally { setBusy(false); } }}>{busy ? "Saving…" : "Save colors"}</button>
    </div></div>
    {(error || loadError) && <p role="alert" className="text-red-300">{error || loadError}<button className="ui-button ml-3" disabled={busy} onClick={() => void reload()}>Reload saved colors</button></p>}
    {stale && <p role="alert" className="text-amber-300">Colors changed in another window. Discard your changes to load the latest version.</p>}
    {notice && <p role="status" className="text-emerald-400">{notice}</p>}
    <div className="overflow-x-auto rounded-lg border border-zinc-800"><table className="w-full text-left"><thead><tr><th className="px-4 py-2">Status</th><th className="px-4 py-2">Foreground (text)</th><th className="px-4 py-2">Background (fill)</th><th className="px-4 py-2">Light / dark preview</th></tr></thead><tbody>{statusDefinitions.map(([key, label]) => {
      const colors = statusPair(key, current.colors);
      return <tr key={key}><td className="px-4 py-2">{label}</td>{(["foreground", "background"] as const).map(part => <td key={part} className="px-4 py-2"><label className="inline-flex items-center gap-2"><input type="color" aria-label={`${label} ${part} color`} className={`${controlClass} !w-12 !p-1`} disabled={busy} value={colors[part]} onChange={e => editColor(key, part, e.target.value)} /><span className="text-xs text-zinc-400">{colors[part].toUpperCase()}</span></label></td>)}<td className="px-4 py-2"><div className="flex flex-wrap gap-2">{["#ffffff", "#111113"].map(surface => <span key={surface} className="inline-flex rounded p-1" style={{ background: surface }}><span className="rounded px-2 py-0.5 text-xs" style={{ background: colors.background, color: colors.foreground }}>{label}</span></span>)}</div><span className="text-xs text-zinc-500">Contrast {contrast(colors.foreground, colors.background).toFixed(1)}:1</span></td></tr>;
    })}</tbody></table></div>
  </section>;
}
