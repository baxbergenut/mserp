"use client";

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { X } from "lucide-react";
import { controlClass } from "@/app/components/management/ManagementUI";
import { saveEscrowRelease } from "@/app/lib/api";
import type { Escrow, EscrowRelease } from "@/app/lib/types";
import { currentChargeWeek } from "@/app/accounting/driver-charges/charges";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";

export function EscrowReleaseForm({ escrow, release, anchor, onClose, onSaved }: {
  escrow: Escrow; release?: EscrowRelease; anchor: { left: number; bottom: number };
  onClose: () => void; onSaved: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [id] = useState(() => release?.id ?? crypto.randomUUID());
  const [amount, setAmount] = useState(release?.amount ?? "");
  const [week, setWeek] = useState(release?.weekStart ?? currentChargeWeek());
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const available = hundredths(escrow.heldAmount) + hundredths(release?.amount ?? "0");
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    const element = dialog.current; element?.showModal();
    return () => { element?.close(); previous?.focus(); };
  }, []);
  async function save(cancelled = false) {
    if (saving) return;
    setSaving(true); setError("");
    try {
      await saveEscrowRelease(escrow.id, { id, amount: cancelled ? release!.amount : amount, weekStart: cancelled ? release!.weekStart : week, version: release?.version ?? 0, escrowVersion: escrow.version, cancelled });
      onSaved();
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Could not save release"); }
    finally { setSaving(false); }
  }
  return createPortal(<dialog ref={dialog} aria-label={release ? "Edit escrow release" : "Release escrow"}
    onCancel={event => { event.preventDefault(); if (!saving) onClose(); }}
    className="mserp-ui fixed m-0 w-80 max-w-[calc(100vw-24px)] rounded-xl border border-zinc-700 bg-zinc-950 p-4 text-zinc-200 shadow-2xl backdrop:bg-black/30"
    style={{ left: Math.max(12, Math.min(anchor.left, window.innerWidth - 332)), top: Math.max(12, Math.min(anchor.bottom + 8, window.innerHeight - 400)) }}>
    <div className="mb-3 flex items-start gap-3"><div className="min-w-0 flex-1"><h2 className="text-sm font-semibold">{release ? "Edit release" : "Release escrow"}</h2><p className="mt-1 truncate text-xs text-zinc-500">{escrow.driverName}</p></div><button type="button" aria-label="Close release" disabled={saving} onClick={onClose} className="p-1 text-zinc-500 hover:text-zinc-200"><X className="h-4 w-4" /></button></div>
    <p className="mb-3 text-xs text-zinc-400">Available <span className="float-right font-mono text-zinc-200">{decimalDisplay(available, true)}</span></p>
    <form onSubmit={event => { event.preventDefault(); void save(); }} className="space-y-3">
      <label className="block text-xs text-zinc-400">Amount ($)<input autoFocus aria-label="Release amount" className={`${controlClass} mt-1`} type="number" min="0.01" step="0.01" required max={String(Number(available) / 100)} value={amount} disabled={saving} onChange={event => setAmount(event.target.value)} /></label>
      <label className="block text-xs text-zinc-400">Week of<input aria-label="Release week" className={`${controlClass} mt-1`} type="date" required min={currentChargeWeek()} max="2100-12-27" step="7" value={week} disabled={saving} onChange={event => setWeek(event.target.value)} /></label>
      {error && <p role="alert" className="text-xs text-red-400">{error}</p>}
      <div className="flex items-center justify-between gap-2 pt-1">{release ? <button type="button" disabled={saving} className="text-xs text-red-400 disabled:opacity-40" onClick={() => void save(true)}>Cancel release</button> : <span />}
        <button disabled={saving} className="rounded-lg bg-blue-600 px-3 py-2 text-xs text-white disabled:opacity-40">{saving ? "Saving…" : release ? "Save release" : "Release"}</button></div>
    </form>
  </dialog>, document.body);
}
