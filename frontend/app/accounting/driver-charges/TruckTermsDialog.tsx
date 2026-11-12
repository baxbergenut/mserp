"use client";

import type { ReactNode } from "react";
import type { TruckTerm } from "@/app/lib/types";
import { controlClass, ErrorBanner, Field, Modal } from "@/app/components/management/ManagementUI";

export function TruckTermsDialog({ unitNumber, term, ownerField, saving, error, onShareChange, onSave, onClose }: {
  unitNumber: string;
  term: TruckTerm;
  ownerField: ReactNode;
  saving: boolean;
  error: string;
  onShareChange: (share: string) => void;
  onSave: () => void;
  onClose: () => void;
}) {
  return <Modal
    title={`Truck ${unitNumber} · settlement terms`}
    description={`Effective ${term.weekStart}`}
    isSaving={saving}
    submitLabel="Save terms"
    onClose={onClose}
    onSubmit={event => { event.preventDefault(); onSave(); }}
  >
    <div className="space-y-4">
      {error && <ErrorBanner message={error} />}
      {ownerField}
      <Field label="Investor share of driver gross (%)">
        <input required type="number" min="0.0001" max="100" step="0.0001" value={term.sharePercent} disabled={saving}
          onChange={event => onShareChange(event.target.value)} className={controlClass} />
      </Field>
    </div>
  </Modal>;
}
