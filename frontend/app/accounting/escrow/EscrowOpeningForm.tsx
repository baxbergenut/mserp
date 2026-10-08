"use client";
import { useState } from "react";
import { saveEscrowOpening } from "@/app/lib/api";
import type { Escrow } from "@/app/lib/types";
import { Modal, Field, controlClass, ErrorBanner } from "@/app/components/management/ManagementUI";

export function EscrowOpeningForm({ escrow, onClose, onSaved }: { escrow: Escrow; onClose: () => void; onSaved: () => void }) {
 const [amount, setAmount] = useState(escrow.openingPaid);
 const [reason, setReason] = useState("");
 const [saving, setSaving] = useState(false);
 const [error, setError] = useState("");
 async function save() {
  if (saving) return;
  setSaving(true); setError("");
  try { await saveEscrowOpening(escrow.id, { openingPaid: amount, version: escrow.version, reason }); onSaved(); }
  catch(e) { setError(e instanceof Error ? e.message : "Could not save previously paid amount"); }
  finally { setSaving(false); }
 }
 return <Modal title={`${escrow.driverName} · Previously paid`} isSaving={saving} onClose={onClose} submitLabel="Save correction" onSubmit={event => { event.preventDefault(); void save(); }}><div className="space-y-4">
  {error && <ErrorBanner message={error} />}
  <Field label="Previously paid ($)"><input type="number" min="0" max={escrow.amount} step="0.01" required className={controlClass} value={amount} onChange={event => setAmount(event.target.value)} /></Field>
  <Field label="Correction reason"><textarea required maxLength={5000} rows={3} className={controlClass} value={reason} onChange={event => setReason(event.target.value)} /></Field>
 </div></Modal>;
}
