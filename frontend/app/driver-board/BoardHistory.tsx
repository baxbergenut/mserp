"use client";

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { History, RefreshCw, Undo2, X } from "lucide-react";
import { fetchDriverBoardHistory } from "@/app/lib/api";
import type { DriverBoardEvent, BoardLoad } from "@/app/lib/types";
import { formatETA } from "./board";

const labels: Record<string, string> = { currentLoad: "Current load", trailerNumber: "Trailer", status: "Status", destination: "Origin / destination", eta: "ETA", notes: "Notes", homeTime: "Home time", driverHome: "Driver home", loadPlan: "Load selection and queue" };
function historyValue(field: string, value: string) {
  if (field === "eta") return formatETA(value, true);
  if (field !== "loadPlan") return value || "Empty";
  try {
    const state = JSON.parse(value) as { current?: BoardLoad; orderLabels?: string[]; hidden?: string[]; destinationSource?: boolean; stopKey?: string };
    const stop = state.current?.stops.find(s => s.key === state.stopKey);
    return [`Current: ${state.current?.number || "Not selected"}`, `Order: ${state.orderLabels?.length ? state.orderLabels.join(" → ") : "Gross Board dates"}`, `Removed from queue: ${state.hidden?.length ?? 0}`, `Destination: ${state.destinationSource ? stop?.location || "Selected load stop" : "Manual"}`].join("\n");
  } catch { return "Load plan updated"; }
}
const buttonClass = "rounded-lg border border-zinc-700 px-3 py-2 text-xs text-zinc-200 hover:bg-zinc-800 disabled:opacity-40";

export function BoardHistory({ driverIds, title, canUndo, waiting, onUndo, onClose, onLoads }: {
  driverIds: string[]; title: string; canUndo: boolean; waiting: boolean;
  onUndo: (id: number, driverId: string) => Promise<void>; onClose: () => void;
  onLoads?: () => void;
}) {
  const scope = driverIds.join(",");
  const [events, setEvents] = useState<DriverBoardEvent[]>([]);
  const [cursor, setCursor] = useState(0);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  const [confirm, setConfirm] = useState<DriverBoardEvent | null>(null);
  const panel = useRef<HTMLElement>(null);
  const request = useRef(0);

  useEffect(() => {
    const generation = ++request.current;
    const result = scope ? fetchDriverBoardHistory(scope.split(",")) : Promise.resolve({ items: [], nextCursor: 0 });
    result.then(data => { if (generation === request.current) { setEvents(data.items); setCursor(data.nextCursor); } })
      .catch(err => { if (generation === request.current) setError(err instanceof Error ? err.message : "History could not be loaded."); })
      .finally(() => { if (generation === request.current) setLoading(false); });
    return () => { request.current += 1; };
  }, [scope, revision]);

  function refresh() {
    setLoading(true); setError(""); setEvents([]); setCursor(0); setConfirm(null);
    setRevision(value => value + 1);
  }

  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    panel.current?.querySelector<HTMLButtonElement>("button:not(:disabled)")?.focus();
    return () => { previous?.focus(); };
  }, []);
  useEffect(() => {
    function keyboard(event: KeyboardEvent) {
      if (event.key === "Escape" && !busy) { event.preventDefault(); onClose(); }
      if (event.key !== "Tab") return;
      const focusable = Array.from(panel.current?.querySelectorAll<HTMLElement>('button:not(:disabled),a[href],input:not(:disabled),select:not(:disabled),[tabindex="0"]') ?? []);
      const first = focusable[0], last = focusable.at(-1);
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
      if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
    }
    document.addEventListener("keydown", keyboard);
    return () => document.removeEventListener("keydown", keyboard);
  }, [onClose, busy]);

  async function more() {
    const generation = request.current;
    setLoading(true); setError("");
    try {
      const data = await fetchDriverBoardHistory(scope.split(","), cursor);
      if (generation === request.current) { setEvents(current => [...current, ...data.items]); setCursor(data.nextCursor); }
    } catch (err) { if (generation === request.current) setError(err instanceof Error ? err.message : "History could not be loaded."); }
    finally { if (generation === request.current) setLoading(false); }
  }
  async function undo() {
    if (!confirm) return;
    setBusy(true); setError("");
    try { await onUndo(confirm.id, confirm.driverId); refresh(); }
    catch (err) { setError(err instanceof Error ? err.message : "Could not undo. Reload the board and review the latest changes."); }
    finally { setBusy(false); }
  }
  return createPortal(<div className="fixed inset-0 z-50 flex justify-end bg-black/60" onMouseDown={e => { if (e.target === e.currentTarget && !busy) onClose(); }}>
    <section ref={panel} role="dialog" aria-modal="true" aria-label={title} className="flex h-dvh w-full max-w-xl flex-col border-l border-zinc-700 bg-zinc-950 shadow-2xl">
      <header className="flex items-center gap-3 border-b border-zinc-800 p-4">
        <History className="h-5 w-5 text-zinc-400" /><div className="min-w-0 flex-1"><h2 className="truncate font-semibold text-zinc-100">{title}</h2><p className="text-xs text-zinc-500">Saved changes · times shown in New York</p></div>
        {onLoads && <button className={buttonClass} disabled={busy} onClick={onLoads}>Loads</button>}
        <button aria-label="Refresh history" disabled={busy || loading} className={buttonClass} onClick={refresh}><RefreshCw className="h-4 w-4" /></button>
        <button aria-label="Close history" disabled={busy} className={buttonClass} onClick={onClose}><X className="h-4 w-4" /></button>
      </header>
      <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-4">
        <p className="text-xs text-zinc-500">History starts when this feature is installed. Existing values have no earlier edit history.</p>
        {waiting && <p role="status" className="text-xs text-amber-300">Undo is available after your board changes finish saving.</p>}
        {error && <p role="alert" className="rounded-lg bg-red-500/10 p-3 text-sm text-red-300">{error}</p>}
        {!loading && !events.length && !error && <p className="py-8 text-center text-sm text-zinc-400">No saved changes for these drivers yet.</p>}
        {events.map(event => <article key={event.id} className="rounded-lg border border-zinc-800 p-3">
          <div className="flex items-start justify-between gap-3"><div><h3 className="text-sm font-medium text-zinc-200">{event.driverName}</h3><p className="mt-1 text-xs text-zinc-400">{event.actorName} · {event.source === "profile" ? "Driver profile" : event.source === "undo" ? "Undo" : event.source === "board" ? "Status Board" : "System update"}</p><time className="text-[11px] text-zinc-500" dateTime={event.createdAt}>{new Date(event.createdAt).toLocaleString("en-US", { timeZone: "America/New_York", month: "short", day: "numeric", year: "numeric", hour: "numeric", minute: "2-digit", second: "2-digit" })}</time></div>
            {canUndo && <button className={buttonClass} disabled={busy || waiting || loading} onClick={() => setConfirm(event)} aria-label={`Undo change ${event.id}`}><Undo2 className="h-3.5 w-3.5" /></button>}
          </div>
          <dl className="mt-3 space-y-3">{Object.entries(event.after).map(([field, value]) => <div key={field}><dt className="text-xs font-medium text-zinc-400">{labels[field] ?? field}</dt><dd className="mt-1 grid grid-cols-[1fr_auto_1fr] gap-2 text-xs"><span className="whitespace-pre-wrap break-words text-zinc-500">{historyValue(field, event.before[field])}</span><span aria-label="changed to" className="text-zinc-600">→</span><span className="whitespace-pre-wrap break-words text-zinc-200">{historyValue(field, value)}</span></dd></div>)}</dl>
          {event.undoOf && <p className="mt-2 text-[11px] text-zinc-500">Reverses change #{event.undoOf}</p>}
        </article>)}
        {loading && <p role="status" className="text-sm text-zinc-400">Loading history…</p>}
        {cursor > 0 && <button disabled={loading || busy} onClick={() => void more()} className={`${buttonClass} w-full`}>Load older changes</button>}
      </div>
      {confirm && <footer className="space-y-3 border-t border-zinc-700 bg-zinc-900 p-4"><p className="text-sm text-zinc-200">Restore the previous {Object.keys(confirm.after).map(k => labels[k] ?? k).join(", ").toLowerCase()} for {confirm.driverName}?</p><p className="text-xs text-zinc-400">This creates a new history entry. If an affected field changed afterward, undo will be blocked.</p><div className="flex justify-end gap-2"><button disabled={busy} className={buttonClass} onClick={() => setConfirm(null)}>Cancel</button><button disabled={busy || waiting} className={buttonClass} onClick={() => void undo()}>{busy ? "Undoing…" : "Undo change"}</button></div></footer>}
    </section>
  </div>, document.body);
}
