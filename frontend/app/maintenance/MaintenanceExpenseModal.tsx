"use client";

import { useEffect, useState } from "react";
import { fetchMaintenance } from "@/app/lib/api";
import { ExpenseEditModal } from "@/app/expenses/ExpenseEditModal";
import { ErrorBanner, Modal } from "@/app/components/management/ManagementUI";
import type { MaintenanceRecord } from "@/app/lib/types";

export function MaintenanceExpenseModal({ recordId, onClose, onSaved }: { recordId: string; onClose: () => void; onSaved: () => Promise<void> }) {
  const [record, setRecord] = useState<MaintenanceRecord | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    fetchMaintenance(recordId).then(value => { if (active) setRecord(value); }).catch(reason => { if (active) setError(reason.message); });
    return () => { active = false; };
  }, [recordId]);
  if (record?.expenseId) return <ExpenseEditModal id={record.expenseId} onClose={onClose} onSaved={onSaved} />;
  return <Modal title="Service history" submitLabel="Done" hideSubmit cancelLabel="Done" isSaving={false} onClose={onClose} onSubmit={event => event.preventDefault()}>
    {error ? <ErrorBanner message={error} /> : !record ? <p role="status">Loading maintenance expense…</p> : <div className="space-y-3 text-sm text-zinc-300"><p>{record.notes || record.services.map(s => s.name).join(", ")}</p><p>{record.serviceDate || "Date unknown"} · {record.odometer?.toLocaleString() ?? "Odometer unknown"}</p><p className="text-xs text-zinc-500">Program tracking record. No expense was recorded.</p></div>}
  </Modal>;
}
