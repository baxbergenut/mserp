"use client";

import { useEffect, useRef, useState } from "react";
import { Check, Pencil, X } from "lucide-react";
import { fetchDriverEscrowSetting, saveDriverEscrowSetting } from "@/app/lib/api";
import type { DriverEscrowSetting } from "@/app/lib/types";
import { usePermissions } from "@/app/lib/access";
import { controlClass } from "@/app/components/management/ManagementUI";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";

export function EscrowDefault() {
  const canManage = usePermissions().includes("escrow.write");
  const [setting, setSetting] = useState<DriverEscrowSetting | null>(null);
  const [amount, setAmount] = useState("");
  const [editing, setEditing] = useState(false);
  const [menu, setMenu] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const root = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    let cancelled = false;
    fetchDriverEscrowSetting().then(value => { if (!cancelled) { setSetting(value); setAmount(value.defaultAmount); setError(""); } })
      .catch(reason => { if (!cancelled) setError(reason instanceof Error ? reason.message : "Could not load the default"); });
    return () => { cancelled = true; };
  }, [attempt]);
  useEffect(() => { if (editing) { input.current?.focus(); input.current?.select(); } }, [editing]);
  useEffect(() => {
    if (!menu) return;
    const close = (event: PointerEvent) => { if (!root.current?.contains(event.target as Node)) setMenu(false); };
    document.addEventListener("pointerdown", close);
    return () => document.removeEventListener("pointerdown", close);
  }, [menu]);
  async function save() {
    if (!setting || saving) return;
    setSaving(true); setError("");
    try { const value = await saveDriverEscrowSetting({ defaultAmount: amount, version: setting.version }); setSetting(value); setAmount(value.defaultAmount); setEditing(false); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "Could not save the default"); }
    finally { setSaving(false); }
  }
  return <div ref={root} className="relative ml-auto text-xs" onKeyDown={event => { if (event.key === "Escape" && !saving) { setEditing(false); setMenu(false); setError(""); setAmount(setting?.defaultAmount ?? ""); } }}>
    {editing ? <form className="flex items-center gap-1.5" onSubmit={event => { event.preventDefault(); void save(); }}><label className="text-zinc-500" htmlFor="escrow-default">Default:</label><input id="escrow-default" ref={input} aria-label="Default driver escrow amount" className={`${controlClass} !w-28 !py-1.5`} type="number" required min="0.01" step="0.01" value={amount} onChange={event => setAmount(event.target.value)} disabled={saving} /><button aria-label="Save default" disabled={saving} className="rounded p-1.5 text-blue-400 hover:bg-zinc-800"><Check className="h-4 w-4" /></button><button type="button" aria-label="Cancel default edit" disabled={saving} className="rounded p-1.5 text-zinc-500 hover:bg-zinc-800" onClick={() => { setEditing(false); setError(""); setAmount(setting?.defaultAmount ?? ""); }}><X className="h-4 w-4" /></button></form>
      : <button type="button" aria-label="Escrow default" aria-haspopup={canManage ? "menu" : undefined} aria-expanded={canManage ? menu : undefined} className="rounded-lg px-2 py-2 text-zinc-400 hover:bg-zinc-900" disabled={!setting} onContextMenu={event => { if (canManage) { event.preventDefault(); setMenu(true); } }} onClick={() => { if (canManage) setMenu(value => !value); }} onKeyDown={event => { if (canManage && (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10"))) { event.preventDefault(); setMenu(true); } }}>Default: <span className="font-mono text-zinc-200">{setting ? decimalDisplay(hundredths(setting.defaultAmount), true) : "…"}</span></button>}
    {menu && !editing && <div role="menu" aria-label="Escrow default actions" className="absolute right-0 top-full z-20 mt-1 w-28 rounded-lg border border-zinc-700 bg-zinc-900 p-1 shadow-xl"><button autoFocus role="menuitem" className="flex w-full items-center gap-2 rounded px-2 py-2 text-zinc-200 hover:bg-zinc-800" onClick={() => { setMenu(false); setEditing(true); }}><Pencil className="h-3 w-3" />Edit</button></div>}
    {error && <div className="mt-1 max-w-80 text-xs text-red-400" role="alert">{error} <button className="text-blue-400" disabled={saving} onClick={() => setAttempt(value => value + 1)}>Reload default</button></div>}
  </div>;
}
