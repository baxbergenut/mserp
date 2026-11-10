"use client";

import { useState } from "react";
import { ErrorBanner, Field, Modal, controlClass } from "@/app/components/management/ManagementUI";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";
import { usePermissions } from "@/app/lib/access";
import { saveEscrowBalance } from "@/app/lib/api";
import type { Escrow } from "@/app/lib/types";

export function EscrowCorrectionForm({ escrow, onClose, onSaved }: {
  escrow: Escrow;
  onClose: () => void;
  onSaved: () => void;
}) {
  const canEdit = usePermissions().includes("escrow.edit");
  const [balance, setBalance] = useState(escrow.heldAmount);
  const [reason, setReason] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const current = hundredths(escrow.heldAmount);
  const target = hundredths(balance);

  async function save() {
    if (!canEdit || saving || target === current || !reason.trim()) return;
    setSaving(true);
    setError("");
    try {
      await saveEscrowBalance(escrow.id, { balance, version: escrow.version, reason });
      onSaved();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not change escrow balance");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      title="Change balance"
      description={escrow.driverName}
      isSaving={saving}
      submitLabel="Save balance"
      submitDisabled={!canEdit || target === current || !reason.trim()}
      onClose={onClose}
      onSubmit={event => { event.preventDefault(); void save(); }}
    >
      <div className="space-y-4">
        {error && <ErrorBanner message={error} />}
        <p className="text-sm">Current balance <span className="font-mono">{decimalDisplay(current, true)}</span></p>
        <Field label="New balance ($)">
          <input type="number" min="0" max={escrow.amount} step="0.01" required disabled={saving}
            className={controlClass} value={balance} onChange={event => setBalance(event.target.value)} />
        </Field>
        {balance !== "" && target < current && !escrow.collectionStopped && (
          <p className="text-xs text-zinc-400">The reduction becomes eligible for collection next week.</p>
        )}
        <Field label="Correction reason">
          <textarea required maxLength={5000} rows={3} disabled={saving} className={controlClass}
            value={reason} onChange={event => setReason(event.target.value)} />
        </Field>
      </div>
    </Modal>
  );
}
