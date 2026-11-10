"use client";
import { usePermissions } from "@/app/lib/access";
import { useState } from "react";
import { reduceEscrowBalance, saveEscrowOpening } from "@/app/lib/api";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";
import type { Escrow } from "@/app/lib/types";
import { Modal, Field, controlClass, ErrorBanner } from "@/app/components/management/ManagementUI";

export function EscrowCorrectionForm({ escrow, reduce, onClose, onSaved }: {
 escrow: Escrow; reduce: boolean; onClose: () => void; onSaved: () => void;
}) {
 const canEdit = usePermissions().includes("escrow.edit");
 const [amount, setAmount] = useState(reduce ? "" : escrow.openingPaid);
 const [reason, setReason] = useState("");
 const [saving, setSaving] = useState(false);
 const [error, setError] = useState("");
 async function save() {
  if (!canEdit || saving) return;
  setSaving(true); setError("");
  try {
   if (reduce) await reduceEscrowBalance(escrow.id, { balance: amount, version: escrow.version, reason });
   else await saveEscrowOpening(escrow.id, { openingPaid: amount, version: escrow.version, reason });
   onSaved();
  }
  catch(e) { setError(e instanceof Error ? e.message : "Could not save escrow correction"); }
  finally { setSaving(false); }
 }
 const maxBalance = (Number(hundredths(escrow.heldAmount) - BigInt(1)) / 100).toFixed(2);
 return <Modal title={`${escrow.driverName} · ${reduce ? "Reduce escrow balance" : "Previously paid"}`} isSaving={saving} onClose={onClose} submitLabel="Save correction" onSubmit={event => { event.preventDefault(); void save(); }}><div className="space-y-4">
  {error && <ErrorBanner message={error} />}
  {reduce && <div className="space-y-1 text-sm">
    <p>Current balance <span className="font-mono">{decimalDisplay(hundredths(escrow.heldAmount), true)}</span></p>
    <p className="text-xs text-zinc-400">This records a balance correction. If collection is active, the reduction becomes eligible for collection next week.</p>
  </div>}
  <Field label={reduce ? "New balance ($)" : "Previously paid ($)"}><input type="number" min="0" max={reduce ? maxBalance : escrow.amount} step="0.01" required className={controlClass} value={amount} onChange={event => setAmount(event.target.value)} /></Field>
  <Field label="Correction reason"><textarea required maxLength={5000} rows={3} className={controlClass} value={reason} onChange={event => setReason(event.target.value)} /></Field>
 </div></Modal>;
}
