"use client";

import { useState } from "react";
import type { DriverPayLoad, LoadWeekTransferMode } from "@/app/lib/types";
import { addDays, monday, shortDate } from "@/app/gross-board/board";
import { Modal, controlClass } from "@/app/components/management/ManagementUI";

export function LoadTransferDialog({ load, finalized, onClose, onTransfer }: {
  load: DriverPayLoad; finalized: boolean; onClose: () => void;
  onTransfer: (mode: LoadWeekTransferMode, reason: string) => Promise<void>;
}) {
  const [mode, setMode] = useState<LoadWeekTransferMode>(load.transferVersion ? (load.date === load.sourceDate ? "dispatch_only" : "everyone") : finalized ? "dispatch_only" : "everyone");
  const [reason, setReason] = useState("Late delivery after Monday 3 p.m.");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const original = load.sourceDate || load.date;
  const week = monday(new Date(`${original}T12:00:00`));
  const next = addDays(week, 7);
  return <Modal title={load.transferVersion ? "Change load transfer" : "Move to next Monday"} description={`${load.loadNumber} · ${shortDate(original)} → ${shortDate(next)}`} isSaving={saving} submitLabel="Save transfer" onClose={onClose} onSubmit={async event => {
    event.preventDefault(); if (saving || !reason.trim()) return;
    setSaving(true); setError("");
    try { await onTransfer(mode, reason.trim()); onClose(); }
    catch (err) { setError(err instanceof Error ? err.message : "Unable to transfer load"); }
    finally { setSaving(false); }
  }}>
    <fieldset disabled={saving} className="space-y-3 text-xs text-zinc-300">
      <legend className="mb-3 text-zinc-400">Dispatcher gross moves to the week of {shortDate(next)}. Choose the settlement week.</legend>
      <label className="flex items-start gap-2"><input type="radio" name="transfer-mode" checked={mode === "everyone"} disabled={finalized} onChange={() => setMode("everyone")} /><span><strong className="font-medium">Move for everyone</strong><span className="mt-1 block text-zinc-500">Driver and investor statements also move to {shortDate(next)}–{shortDate(addDays(next, 6))}.</span></span></label>
      <label className="flex items-start gap-2"><input type="radio" name="transfer-mode" checked={mode === "dispatch_only"} onChange={() => setMode("dispatch_only")} /><span><strong className="font-medium">Keep settlement this week</strong><span className="mt-1 block text-zinc-500">Driver and investor statements stay in the original week, {shortDate(week)}–{shortDate(addDays(week, 6))}.</span></span></label>
      {!!load.transferVersion && <label className="flex items-start gap-2"><input type="radio" name="transfer-mode" checked={mode === "restore"} onChange={() => { setMode("restore"); setReason(""); }} /><span>Restore original placement for everyone</span></label>}
      <p className="text-zinc-500">The original dispatcher retains credit. This records a manual decision, not a verified delivery time.</p>
      {finalized && <p className="text-amber-300">Settlement changes require reopening affected driver and investor statements. Dispatcher-only changes can leave finalized statements unchanged.</p>}
      <label className="block text-zinc-400">Reason<textarea required maxLength={1000} rows={2} value={reason} onChange={event => setReason(event.target.value)} className={`${controlClass} mt-1`} /></label>
    </fieldset>
    {error && <p role="alert" className="mt-3 text-xs text-red-300">{error}</p>}
  </Modal>;
}
