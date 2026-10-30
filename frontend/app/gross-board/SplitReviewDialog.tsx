"use client";

import { useEffect, useState } from "react";
import { fetchLoadSplit } from "@/app/lib/api";
import type { GrossBoardEntry, LoadSplitWorkspace } from "@/app/lib/types";
import { BoardDialog } from "./BoardDialog";
import { amountDisplay, decimalDisplay, hundredths, validDecimal } from "./board";

const key = (e: GrossBoardEntry) => `${e.driverId}/${e.date}/${e.slot}`;
const inputClass = "w-full min-w-20 rounded border border-zinc-700 bg-zinc-900 px-2 py-2 text-sm disabled:opacity-60";

export function SplitReviewDialog({ entry, driverName, disabled, onClose, onApply }: {
  entry: GrossBoardEntry; driverName: string; disabled: boolean; onClose: () => void;
  onApply: (next: GrossBoardEntry) => void;
}) {
  const [workspace, setWorkspace] = useState<LoadSplitWorkspace | null>(null);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    fetchLoadSplit(entry.loadRecordId!, controller.signal).then(result => {
      if (controller.signal.aborted) return;
      const pending = entry.splitReview;
      const entries = pending?.entries ?? result.entries;
      const current = { driverName, entry: { ...entry, splitReview: undefined } };
      const merged = entries.some(item => key(item.entry) === key(entry))
        ? entries.map(item => key(item.entry) === key(entry) ? current : item) : [...entries, current];
      const allocations = result.trips.map(trip => {
        const saved = (pending?.allocations ?? result.allocations).find(a => a.tripId === trip.id);
        const linked = merged.find(item => item.entry.loadTripId === trip.id)?.entry;
        return { tripId: trip.id, originalRate: saved?.originalRate ?? linked?.enteredOriginalRate ?? "" };
      });
      setWorkspace({ ...result, ...(pending ?? {}), entries: merged, allocations });
    }).catch(err => { if (!controller.signal.aborted) setError(err instanceof Error ? err.message : "Unable to load split"); });
    return () => controller.abort();
    // Freeze the opening draft; source and entry versions are checked at save.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [entry.loadRecordId, retry]);
  const total = workspace?.allocations.reduce((sum, a) => sum + (validDecimal(a.originalRate) ? hundredths(a.originalRate) : BigInt(0)), BigInt(0)) ?? BigInt(0);
  const difference = workspace ? hundredths(workspace.total) - total : BigInt(0);
  const valid = !!workspace && workspace.allocations.length > 1 && difference === BigInt(0)
    && workspace.allocations.every(a => a.originalRate !== "" && validDecimal(a.originalRate))
    && workspace.entries.every(item => !!item.entry.loadTripId && validDecimal(item.entry.driverRate)) && !!workspace.reason.trim();
  function apply() {
    if (!workspace || !valid || disabled) return;
    const current = workspace.entries.find(item => key(item.entry) === key(entry))!.entry;
    const allocation = workspace.allocations.find(a => a.tripId === current.loadTripId)!;
    const trip = workspace.trips.find(t => t.id === current.loadTripId)!;
    onApply({ ...entry, loadTripId: current.loadTripId, driverRate: current.driverRate,
      originalRate: allocation.originalRate, enteredOriginalRate: allocation.originalRate, systemOriginalRate: allocation.originalRate,
      miles: trip.miles, enteredMiles: trip.miles, systemMiles: trip.miles, tripReview: false, tripReviewReason: "",
      acceptSystemValues: true, splitReview: { loadId: workspace.loadId, version: workspace.version, sourceHash: workspace.sourceHash,
        reason: workspace.reason, allocations: workspace.allocations, entries: workspace.entries } });
    onClose();
  }
  return <BoardDialog title={`Review load ${entry.loadNumber}`} onClose={onClose}>
    {error && <p role="alert" className="mb-3 text-red-300">{error}<button type="button" className="ml-3 underline" onClick={() => { setError(""); setRetry(n => n + 1); }}>Retry</button></p>}
    {!workspace && !error && <p role="status">Loading split…</p>}
    {workspace && <div onKeyDown={event => { if (event.key === "Enter" && event.target instanceof HTMLInputElement) event.preventDefault(); }}>
      <p className="mb-4 text-zinc-400">Allocate the order&apos;s {amountDisplay(workspace.total, true)} original gross across its trips. Driver gross is entered separately. DataTruck amounts are references.</p>
      {workspace.reviewReason && <p role="status" className="mb-4 rounded border border-amber-500/30 bg-amber-500/10 p-3 text-amber-300">{workspace.reviewReason}</p>}
      <table className="w-full text-left text-sm"><thead className="text-zinc-400"><tr><th className="py-2">Trip</th><th className="px-2 text-right">DataTruck amount</th><th className="w-36 pl-2">Original gross</th></tr></thead>
        <tbody>{workspace.trips.map((trip, index) => <tr key={`${trip.id}:${index}`} className="border-t border-zinc-800">
          <td className="py-3"><div>{trip.number || "Unknown trip"}</div><div className="text-xs text-zinc-400">{trip.driverName} · {trip.miles || "—"} mi</div></td>
          <td className="px-2 text-right font-mono">{amountDisplay(trip.originalRate, true) || "—"}</td>
          <td className="pl-2"><input aria-label={`Original gross for ${trip.number}`} className={inputClass} inputMode="decimal" disabled={disabled}
            value={workspace.allocations[index].originalRate} onChange={event => setWorkspace({ ...workspace, allocations: workspace.allocations.map((a, i) => i === index ? { ...a, originalRate: event.target.value } : a) })} /></td>
        </tr>)}</tbody></table>
      <p role="status" className={`my-3 text-right ${difference === BigInt(0) ? "text-emerald-300" : "text-amber-300"}`}>Allocated {decimalDisplay(total, true)} · Remaining {decimalDisplay(difference, true)}</p>
      <table className="mt-5 w-full text-left text-sm"><thead className="text-zinc-400"><tr><th className="py-2">Clustie driver</th><th className="px-2">Selected trip</th><th className="w-36 pl-2">Driver gross</th></tr></thead>
        <tbody>{workspace.entries.map((item, index) => <tr key={key(item.entry)} className="border-t border-zinc-800">
          <td className="py-3">{item.driverName}<div className="text-xs text-zinc-400">{item.entry.date} · Load {item.entry.slot + 1}</div></td>
          <td className="px-2"><select aria-label={`Trip for ${item.driverName}, ${item.entry.date}, ${item.entry.slot}`} className={inputClass} disabled={disabled} value={item.entry.loadTripId ?? ""}
            onChange={event => setWorkspace({ ...workspace, entries: workspace.entries.map((row, i) => i === index ? { ...row, entry: { ...row.entry, loadTripId: Number(event.target.value) || null } } : row) })}>
            <option value="">Select trip</option>{workspace.trips.map(t => <option key={t.id} value={t.id}>{t.number} · {t.driverName}</option>)}
          </select></td>
          <td className="pl-2"><input aria-label={`Driver gross for ${item.driverName}, ${item.entry.date}, ${item.entry.slot}`} className={inputClass} inputMode="decimal" disabled={disabled} value={item.entry.driverRate}
            onChange={event => setWorkspace({ ...workspace, entries: workspace.entries.map((row, i) => i === index ? { ...row, entry: { ...row.entry, driverRate: event.target.value } } : row) })} /></td>
        </tr>)}</tbody></table>
      <p className="mt-2 text-xs text-zinc-400">Driver gross does not have to equal order revenue. Existing mileage is verified against DataTruck. All linked entries save together.</p>
      <label className="mt-5 block text-zinc-400">Review note<input aria-label="Split review note" className={`${inputClass} mt-2`} maxLength={2000} disabled={disabled} value={workspace.reason} onChange={event => setWorkspace({ ...workspace, reason: event.target.value })} placeholder="For example: confirmed against the weekly sheet" /></label>
      {!disabled && <button type="button" disabled={!valid} onClick={apply} className="mt-5 rounded-lg bg-blue-600 px-4 py-2 text-white disabled:opacity-40">Apply split</button>}
    </div>}
  </BoardDialog>;
}
