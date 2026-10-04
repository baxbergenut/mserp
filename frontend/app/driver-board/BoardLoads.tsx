"use client";

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { ArrowDown, ArrowUp, History, RefreshCw, X } from "lucide-react";
import { compactLoadLocation } from "@/app/lib/usStates";
import { fetchBoardLoads } from "@/app/lib/api";
import type { BoardLoad, BoardLoads as Loads, BoardLoadAction } from "@/app/lib/types";
import { controlClass } from "@/app/components/management/ManagementUI";

const buttonClass = "rounded-lg border border-zinc-700 px-3 py-2 text-xs text-zinc-200 hover:bg-zinc-800 disabled:opacity-40";
const time = (value: string) => value ? new Date(value).toLocaleString("en-US", { timeZone: "America/New_York", month: "short", day: "numeric", hour: "numeric", minute: "2-digit" }) : "Unknown";

export function BoardLoads({ driverId, name, initial, canEdit, waiting, onChange, onClose, onHistory }: {
  driverId: string; name: string; initial: Loads; canEdit: boolean; waiting: boolean;
  onChange: (id: string, view: Loads, action: BoardLoadAction) => Promise<Loads>;
  onClose: () => void; onHistory: () => void;
}) {
  const [view, setView] = useState(initial);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [select, setSelect] = useState<BoardLoad | null>(null);
  const [stop, setStop] = useState("");
  const [clear, setClear] = useState(false);
  const panel = useRef<HTMLElement>(null);
  const locked = busy || waiting || !canEdit;
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    panel.current?.querySelector<HTMLButtonElement>("button:not(:disabled)")?.focus();
    return () => previous?.focus();
  }, []);
  useEffect(() => {
    function keyboard(event: KeyboardEvent) {
      if (event.key === "Escape" && !busy) { event.preventDefault(); onClose(); }
      if (event.key !== "Tab") return;
      const items = Array.from(panel.current?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),a[href]') ?? []);
      if (event.shiftKey && document.activeElement === items[0]) { event.preventDefault(); items.at(-1)?.focus(); }
      if (!event.shiftKey && document.activeElement === items.at(-1)) { event.preventDefault(); items[0]?.focus(); }
    }
    document.addEventListener("keydown", keyboard); return () => document.removeEventListener("keydown", keyboard);
  }, [busy, onClose]);
  async function refresh() {
    setBusy(true); setError(""); setSelect(null); setClear(false);
    try { setView(await fetchBoardLoads(driverId)); }
    catch (err) { setError(err instanceof Error ? err.message : "Could not load plans."); }
    finally { setBusy(false); }
  }
  async function act(action: BoardLoadAction) {
    if (locked) return;
    setBusy(true); setError("");
    try { setView(await onChange(driverId, view, action)); setSelect(null); setClear(false); }
    catch (err) { setError(err instanceof Error ? err.message : "Could not save. Reload the board and review its current values."); }
    finally { setBusy(false); }
  }
  function move(index: number, offset: number) {
    const order = view.next.map(p => p.planId);
    [order[index], order[index + offset]] = [order[index + offset], order[index]];
    void act({ action: "order", order });
  }
  function choose(load: BoardLoad) { setSelect(load); setClear(false); setStop(load.stops.find(s => s.type === "pickup" && s.location)?.key ?? ""); }
  function detail(load: BoardLoad) {
    return <><p className="mt-1 text-xs text-zinc-500">Gross Board: {load.date} · slot {load.slot + 1}{load.loadId !== null ? ` · DataTruck #${load.loadId}` : " · Unmatched plan"}</p>
      {load.warning && <p className="mt-2 text-xs text-amber-300">{load.warning}</p>}
      {load.loadId !== null && <p className="mt-1 text-[11px] text-zinc-500">DataTruck: {load.sourceStatus || "No status"} · last synced {time(load.syncedAt)} NY</p>}
      {load.stops.length > 0 && <ol className="mt-3 space-y-2">{load.stops.map((s, i) => <li key={`${s.key}-${i}`} className={`rounded-md px-2 py-1.5 text-xs ${view.current?.planId === load.planId && view.stopKey === s.key ? "bg-blue-500/15 text-blue-200" : "bg-zinc-900 text-zinc-400"}`}><span className="capitalize">{i + 1}. {s.type || "Stop"}</span> · {compactLoadLocation(s.location) || "Location unavailable"}{s.appointment && <span className="mt-1 block text-[11px] text-zinc-500">Appointment: {time(s.appointment)} NY</span>}</li>)}</ol>}
    </>;
  }
  return createPortal(<div className="fixed inset-0 z-50 flex justify-end bg-black/60" onMouseDown={e => { if (e.target === e.currentTarget && !busy) onClose(); }}>
    <section ref={panel} role="dialog" aria-modal="true" aria-label={`${name} · Loads`} className="flex h-dvh w-full max-w-xl flex-col border-l border-zinc-700 bg-zinc-950 shadow-2xl">
      <header className="flex items-center gap-2 border-b border-zinc-800 p-4"><h2 className="min-w-0 flex-1 truncate font-semibold text-zinc-100">{name} · Loads</h2><button className={buttonClass} onClick={onHistory} disabled={busy}><History className="inline h-3.5 w-3.5" /> History</button><button className={buttonClass} disabled={busy} aria-label="Close loads" onClick={onClose}><X className="h-4 w-4" /></button></header>
      <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-4">
        <p className="text-xs text-zinc-500">Planned loads from Gross Board across weeks. Current load and stop are explicitly selected. Appointments are separate from your editable ETA.</p>
        {waiting && <p className="text-xs text-amber-300">Wait for board edits to finish saving before changing loads.</p>}
        {error && <p role="alert" className="rounded-lg bg-red-500/10 p-3 text-sm text-red-300">{error}</p>}
        <div className="flex items-center justify-between gap-2"><span className="text-xs text-zinc-400">Upcoming from {view.fromDate} · New York</span><button className={buttonClass} disabled={busy} onClick={() => void refresh()}><RefreshCw className="inline h-3.5 w-3.5" /> Refresh loads</button></div>
        <section className="rounded-lg border border-blue-500/30 p-3"><h3 className="text-xs font-medium uppercase tracking-wide text-blue-300">Current load</h3>{view.current ? <>
          <p className="mt-2 text-sm font-semibold text-zinc-100">{view.current.number}</p>{detail(view.current)}
          <label className="mt-3 block text-xs text-zinc-400">Destination source<select aria-label="Current load destination source" disabled={locked} value={view.destinationSource ? view.stopKey : ""} onChange={e => void act(e.target.value ? { action: "source", stopKey: e.target.value } : { action: "manual" })} className={`${controlClass} mt-1`}><option value="">Manual destination</option>{view.current.stops.filter(s => s.location).map((s, i) => <option key={`${s.key}-${i}`} value={s.key}>{s.type} · {compactLoadLocation(s.location)}</option>)}</select></label>
          {canEdit && <button className={`${buttonClass} mt-3`} disabled={locked} onClick={() => { setClear(true); setSelect(null); }}>Clear current selection</button>}
        </> : <p className="mt-2 text-sm text-zinc-400">No linked current load. Existing free-text dispatch details remain editable.</p>}</section>
        <div className="flex items-center justify-between"><h3 className="text-sm font-medium text-zinc-200">Next loads · {view.customOrder ? "Custom order" : "Planned date / slot order"}</h3>{view.customOrder && canEdit && <button className={buttonClass} disabled={locked} onClick={() => void act({ action: "reset_order" })}>Reset order</button>}</div>
        {!view.next.length && <p className="text-sm text-zinc-500">No next loads in this plan window.</p>}
        {view.next.map((load, i) => <article key={load.planId} className="rounded-lg border border-zinc-800 p-3"><div className="flex items-center gap-2"><h4 className="min-w-0 flex-1 break-words text-sm font-medium text-zinc-200">{i + 1}. {load.number}</h4>{canEdit && <><button aria-label={`Move ${load.number} up`} className={buttonClass} disabled={locked || i === 0} onClick={() => move(i, -1)}><ArrowUp className="h-3 w-3" /></button><button aria-label={`Move ${load.number} down`} className={buttonClass} disabled={locked || i === view.next.length - 1} onClick={() => move(i, 1)}><ArrowDown className="h-3 w-3" /></button></>}</div>{detail(load)}{canEdit && <div className="mt-3 flex gap-2"><button className={buttonClass} disabled={locked} onClick={() => choose(load)}>Set current</button><button className={buttonClass} disabled={locked} onClick={() => void act({ action: "hide", planId: load.planId })}>Remove from queue</button></div>}</article>)}
        {view.earlier.length > 0 && <details><summary className="cursor-pointer text-xs text-zinc-400">Earlier unfinished plans ({view.earlier.length})</summary><p className="mt-2 text-xs text-zinc-500">Planned before today; completion is not confirmed. Select one as current if the driver is still working on it.</p>{view.earlier.map(load => <article key={load.planId} className="mt-3 rounded-lg border border-zinc-800 p-3"><h4 className="text-sm font-medium text-zinc-300">{load.number}</h4>{detail(load)}{canEdit && <div className="mt-3 flex gap-2"><button className={buttonClass} disabled={locked} onClick={() => choose(load)}>Set current</button><button className={buttonClass} disabled={locked} onClick={() => void act({ action: "hide", planId: load.planId })}>Remove from queue</button></div>}</article>)}</details>}
        {view.hidden.length > 0 && <details><summary className="cursor-pointer text-xs text-zinc-400">Removed from queue ({view.hidden.length})</summary><p className="mt-2 text-xs text-zinc-500">Gross Board plans remain unchanged.</p>{view.hidden.map(load => <div key={load.planId} className="mt-2 flex items-center justify-between gap-2 text-sm text-zinc-400"><span>{load.number} · {load.date}</span>{canEdit && <button className={buttonClass} disabled={locked} onClick={() => void act({ action: "restore", planId: load.planId })}>Restore</button>}</div>)}</details>}
        {view.unavailable.length > 0 && <details><summary className="cursor-pointer text-xs text-zinc-400">Delivered or cancelled ({view.unavailable.length})</summary>{view.unavailable.map(load => <article key={load.planId} className="mt-3 border-t border-zinc-800 pt-3 text-sm text-zinc-400">{load.number}{detail(load)}</article>)}</details>}
      </div>
      {(select || clear) && <footer className="space-y-3 border-t border-zinc-700 bg-zinc-900 p-4"><p className="text-sm text-zinc-200">{select ? `Make ${select.number} the current load?` : "Clear the current load selection and its displayed load number?"}</p>{select && <label className="block text-xs text-zinc-400">Current stop<select aria-label="Choose current stop" className={`${controlClass} mt-1`} value={stop} disabled={busy} onChange={e => setStop(e.target.value)}><option value="">Keep manual destination</option>{select.stops.filter(s => s.location).map((s, i) => <option key={`${s.key}-${i}`} value={s.key}>{s.type} · {compactLoadLocation(s.location)}</option>)}</select></label>}<p className="text-xs text-zinc-500">Status and ETA stay unchanged. This does not confirm pickup or delivery.</p><div className="flex justify-end gap-2"><button className={buttonClass} disabled={busy} onClick={() => { setSelect(null); setClear(false); }}>Cancel</button><button className={buttonClass} disabled={locked} onClick={() => void act(select ? { action: "select", planId: select.planId, stopKey: stop } : { action: "clear" })}>{busy ? "Saving…" : select ? "Confirm current load" : "Clear selection"}</button></div></footer>}
    </section>
  </div>, document.body);
}
