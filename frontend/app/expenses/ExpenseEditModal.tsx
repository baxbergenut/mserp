"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { fetchExpense, fetchExpensesPage, fetchMaintenancePage, fetchDrivers, fetchTrucks, fetchInvestors, updateExpense } from "@/app/lib/api";
import { useExpenseCategoryAccess, usePermissions } from "@/app/lib/access";
import type { Driver, Expense, ExpenseInput, ExpensePage, Investor, Truck } from "@/app/lib/types";
import { ErrorBanner, Modal } from "@/app/components/management/ManagementUI";
import { ExpenseForm, expenseToInput } from "./ExpenseForm";

export function ExpenseEditModal({ id, onClose, onSaved }: { id: string; onClose: () => void; onSaved: () => Promise<void> }) {
  const access = useExpenseCategoryAccess();
  const permissions = usePermissions();
  const [data, setData] = useState<{ expense: Expense; options: ExpensePage["options"]; drivers: Driver[]; trucks: Truck[]; owners: Investor[] } | null>(null);
  const [form, setForm] = useState<ExpenseInput | null>(null);
  const [maintenanceId,setMaintenanceId]=useState<string | null>(null);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const canEdit = !!data && access.some(a => a.categoryId === data.expense.categoryId && a.canEdit);
  const fleet = permissions.includes("fleet.read");
  useEffect(() => {
    let cancelled = false;
    Promise.all([fetchExpense(id), fetchExpensesPage({ page: 1, pageSize: 1 }), fleet ? fetchDrivers() : Promise.resolve([]), fleet ? fetchTrucks() : Promise.resolve([]), fleet ? fetchInvestors() : Promise.resolve([])])
      .then(([expense, page, drivers, trucks, owners]) => {
        if (cancelled) return;
        // Preserve saved assignments when the user cannot browse the fleet directory.
        if (expense.driverId && !drivers.some(d => d.id === expense.driverId)) drivers.push({ id: expense.driverId, fullName: expense.driverName ?? "Saved driver" } as Driver);
        if (expense.truckId && !trucks.some(t => t.id === expense.truckId)) trucks.push({ id: expense.truckId, unitNumber: expense.unitNumber ?? "Saved truck" } as Truck);
        if (expense.ownerId && !owners.some(o => o.id === expense.ownerId)) owners.push({ id: expense.ownerId, fullName: expense.ownerName ?? "Saved owner" } as Investor);
        void fetchMaintenancePage({expenseId:id,pageSize:1}).then(result=>{if(!cancelled)setMaintenanceId(result.items[0]?.id ?? null);}).catch(()=>{});
        setData({ expense, options: page.options, drivers, trucks, owners }); setForm(expenseToInput(expense));
      }).catch(reason => { if (!cancelled) setError(reason instanceof Error ? reason.message : "Unable to load expense"); });
    return () => { cancelled = true; };
  }, [id, fleet]);

  async function save() {
    if (!form || !canEdit) return;
    setSaving(true); setError("");
    try { await updateExpense(id, form); await onSaved(); onClose(); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "Unable to save expense"); }
    finally { setSaving(false); }
  }
  return <Modal title={canEdit ? "Edit entry" : "Expense details"} isSaving={saving} submitLabel="Save changes" hideSubmit={!canEdit || !!maintenanceId} onClose={onClose} onSubmit={event => { event.preventDefault(); void save(); }}>
    {error && <ErrorBanner message={error} />}
    {!data && !error && <p role="status" className="text-zinc-500">Loading expense…</p>}
    {maintenanceId && <p className="mb-4 text-xs text-zinc-400">This charge belongs to a service visit. <Link className="text-blue-400" href={`/maintenance?recordId=${maintenanceId}`}>Open work order</Link></p>}
    {data && form && <fieldset disabled={!canEdit || saving || !!maintenanceId}><ExpenseForm value={form} onChange={setForm} options={data.options} originalCategory={{ id: data.expense.categoryId, name: data.expense.category }} allowedCategoryIds={Array.from(new Set([data.expense.categoryId, ...access.filter(a => a.canCreate).map(a => a.categoryId)]))} drivers={data.drivers} trucks={data.trucks} owners={data.owners} /></fieldset>}
  </Modal>;
}
