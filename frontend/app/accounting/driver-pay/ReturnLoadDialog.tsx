"use client";

import { useState } from "react";
import { Modal, controlClass } from "@/app/components/management/ManagementUI";
import { transferPayLoadWeek } from "@/app/lib/api";
import type { OutgoingLoadTransfer } from "@/app/lib/types";
import { shortDate } from "@/app/gross-board/board";

export type ReturnLoadTarget = Pick<OutgoingLoadTransfer, "driverId" | "loadNumber" | "sourceDate" | "sourceSlot" | "boardVersion" | "transferVersion">;

export function ReturnLoadDialog({ target, investor, onClose, onReturned }: {
  target: ReturnLoadTarget; investor: boolean; onClose: () => void; onReturned: () => void;
}) {
  const [reason, setReason] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  return <Modal title="Return to original date" description={`${target.loadNumber} · ${shortDate(target.sourceDate)}`} submitLabel="Return load" isSaving={saving} onClose={onClose} onSubmit={async event => {
    event.preventDefault(); if (saving || !reason.trim()) return;
    setSaving(true); setError("");
    try {
      await transferPayLoadWeek(investor, { driverId: target.driverId, date: target.sourceDate, slot: target.sourceSlot, boardVersion: target.boardVersion, transferVersion: target.transferVersion, mode: "restore", reason: reason.trim() });
    } catch (err) { setError(err instanceof Error ? err.message : "Unable to return load"); setSaving(false); return; }
    onClose(); onReturned();
  }}>
    <p className="mb-3 text-sm text-zinc-300">Return this load to {shortDate(target.sourceDate)} on Gross Board and Driver / Investor Pay. Affected finalized statements must be reopened first.</p>
    <label className="block text-xs text-zinc-400">Reason<textarea required maxLength={1000} rows={2} disabled={saving} value={reason} onChange={event => setReason(event.target.value)} className={`${controlClass} mt-1`} /></label>
    {error && <p role="alert" className="mt-3 text-xs text-red-300">{error}</p>}
  </Modal>;
}
