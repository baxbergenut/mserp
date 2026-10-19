"use client";

import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { parseETA } from "./board";
import type { BoardAutomation, BoardLoadAction } from "@/app/lib/types";

export function ETAEditor({ name, value, anchor, onApply, onClose, automation, waiting, onAction }: {
  name: string; value: string; anchor: HTMLButtonElement; onApply: (value: string) => void; onClose: () => void;
  automation?: BoardAutomation; waiting?: boolean; onAction?: (action: BoardLoadAction) => Promise<void>;
}) {
  const initial = parseETA(value);
  const [miles, setMiles] = useState(initial?.miles ?? "");
  const [time, setTime] = useState(initial?.miles ? "" : initial?.time ?? "");
  const [manualStopTime, setManualStopTime] = useState(automation?.manualStopTime ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function act(action: BoardLoadAction) {
    if (!onAction || busy || waiting) return;
    setBusy(true); setError("");
    try { await onAction(action); } catch (err) { setError(err instanceof Error ? err.message : "Could not save stop time"); }
    finally { setBusy(false); }
  }
  const panel = useRef<HTMLFormElement>(null);
  useLayoutEffect(() => {
    const place = () => {
      const element = panel.current;
      if (!element) return;
      const rect = anchor.getBoundingClientRect();
      const width = Math.min(300, window.innerWidth - 16);
      element.style.width = `${width}px`;
      element.style.left = `${Math.max(8, Math.min(rect.right - width, window.innerWidth - width - 8))}px`;
      const height = element.offsetHeight;
      const top = rect.bottom + height + 6 <= window.innerHeight - 8 ? rect.bottom + 6 : rect.top - height - 6;
      element.style.top = `${Math.max(8, Math.min(top, window.innerHeight - height - 8))}px`;
    };
    place();
    panel.current?.querySelector("input")?.focus({ preventScroll: true });
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    return () => {
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
      // Restore after React removes the focused input, without stealing focus
      // from another field clicked outside the popup.
      queueMicrotask(() => { if (document.activeElement === document.body) anchor.focus({ preventScroll: true }); });
    };
  }, [anchor]);
  useEffect(() => {
    const outside = (event: PointerEvent) => { if (!panel.current?.contains(event.target as Node) && event.target !== anchor) onClose(); };
    document.addEventListener("pointerdown", outside);
    return () => document.removeEventListener("pointerdown", outside);
  }, [anchor, onClose]);
  const inputClass = "mt-1 block h-8 w-full min-w-0 rounded border border-zinc-700 bg-zinc-900 px-2 text-xs text-zinc-100 outline-none focus:border-blue-500 [color-scheme:dark]";
  const buttonClass = "rounded px-2 py-1.5 text-xs text-zinc-300 hover:bg-zinc-800";
  return createPortal(<form ref={panel} role="dialog" aria-label={`${name} · ETA`} className="fixed z-50 max-h-[calc(100dvh-16px)] overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-950 p-3 shadow-xl" onKeyDown={event => {
    if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); onClose(); }
  }} onBlur={event => {
    if (event.relatedTarget && !event.currentTarget.contains(event.relatedTarget as Node)) onClose();
  }} onSubmit={event => {
    event.preventDefault();
    const next = time || (miles ? `${Number(miles)} mi` : "");
    if (!busy && !waiting && (!next || parseETA(next))) onApply(next);
  }}>
    <div className="mb-3 flex items-center justify-between text-xs"><span className="font-medium text-zinc-100">ETA</span><span className="text-zinc-500">New York time</span></div>
    {automation?.appointment?.label && <p className="mb-2 text-xs text-zinc-300" title={automation.appointment.detail}>{automation.appointment.label} · {automation.appointment.kind === "manual" ? "Manual stop time" : "DataTruck appointment"}</p>}
    {error && <p role="alert" className="mb-2 text-xs text-red-400">{error}</p>}
    {automation?.context && (automation.stopType === "pickup" || automation.stopType === "delivery") && <div className="mb-3"><label className="block text-[11px] text-zinc-400">Manual {automation.stopType} time<input aria-label={`Manual ${automation.stopType} time`} type="text" maxLength={80} placeholder="e.g. 14:00 MON" value={manualStopTime} disabled={busy || waiting} onChange={e => setManualStopTime(e.target.value)} className={inputClass} /></label><div className="mt-1 flex items-center justify-between"><span className="text-[10px] text-zinc-500">Overrides the DataTruck time for this stop.</span><button type="button" disabled={busy || waiting || !onAction || manualStopTime.trim() === (automation.manualStopTime ?? "")} className="rounded px-2 py-1 text-xs text-blue-400 hover:bg-zinc-800 disabled:text-zinc-600" onClick={() => void act({ action: "stop_time", manualStopTime: manualStopTime.trim() })}>Save time</button></div></div>}
    <fieldset disabled={busy || waiting}>
    {!initial && value && <p className="mb-2 break-words text-xs text-amber-200">Existing ETA: {value}. Enter a time or miles to replace it.</p>}
    <div className="grid grid-cols-[1.1fr_1fr] gap-2">
      <label className="text-[11px] text-zinc-400">Miles left<input type="number" min="0" max="9999999" step="0.01" value={miles} onChange={e => { setMiles(e.target.value); setTime(""); }} className={inputClass} /></label>
      <label className="text-[11px] text-zinc-400">Arrival time<input type="time" step={60} value={time} onChange={e => { setTime(e.target.value); setMiles(""); }} className={inputClass} /></label>
    </div>
    <div className="mt-3 flex items-center gap-1"><button type="button" className={`${buttonClass} mr-auto`} onClick={() => onApply("")}>Clear ETA</button><button type="button" className={buttonClass} onClick={onClose}>Cancel</button><button type="submit" className="rounded bg-blue-600 px-2 py-1.5 text-xs text-white hover:bg-blue-500">Apply ETA</button></div>
    </fieldset>
  </form>, document.body);
}
