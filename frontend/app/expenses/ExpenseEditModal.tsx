"use client";
import type { DriverOption, TruckOption, InvestorOption } from "@/app/lib/types";


import { useEffect, useState } from "react";
import { fetchExpense, fetchExpensesPage, fetchDriverOptions, fetchTruckOptions, fetchInvestorOptions, updateExpense } from "@/app/lib/api";
import { useExpenseCategoryAccess } from "@/app/lib/access";
import type { Driver, Expense, ExpenseInput, ExpensePage, Investor, Truck } from "@/app/lib/types";
import { ErrorBanner, Modal } from "@/app/components/management/ManagementUI";
import { ExpenseForm, expenseToInput } from "./ExpenseForm";
import { applyExpenseCategoryRules } from "./catalog";
import { ExpenseDocuments } from "./ExpenseDocuments";

export function ExpenseEditModal({ id, onClose, onSaved }: { id: string; onClose: () => void; onSaved: () => Promise<void> }) {
  const access = useExpenseCategoryAccess();

  const [data, setData] = useState<{ expense: Expense; options: ExpensePage["options"]; drivers: DriverOption[]; trucks: TruckOption[]; owners: InvestorOption[] } | null>(null);
  const [form, setForm] = useState<ExpenseInput | null>(null);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const canEdit = !!data && (data.expense.canEdit ?? access.some(a => a.categoryId === data.expense.categoryId && a.canEdit));
  const fleet = true;
  useEffect(() => {
    let cancelled = false;
    Promise.all([fetchExpense(id), fetchExpensesPage({ page: 1, pageSize: 1 }), fleet ? fetchDriverOptions() : Promise.resolve([]), fleet ? fetchTruckOptions() : Promise.resolve([]), fleet ? fetchInvestorOptions() : Promise.resolve([])])
      .then(([expense, page, drivers, trucks, owners]) => {
        if (cancelled) return;
        // Preserve saved assignments when the user cannot browse the fleet directory.
        if (expense.driverId && !drivers.some(d => d.id === expense.driverId)) drivers.push({ id: expense.driverId, fullName: expense.driverName ?? "Saved driver" } as Driver);
        if (expense.truckId && !trucks.some(t => t.id === expense.truckId)) trucks.push({ id: expense.truckId, unitNumber: expense.unitNumber ?? "Saved truck" } as Truck);
        if (expense.ownerId && !owners.some(o => o.id === expense.ownerId)) owners.push({ id: expense.ownerId, fullName: expense.ownerName ?? "Saved owner" } as Investor);
        setData({ expense, options: page.options, drivers, trucks, owners }); setForm(expenseToInput(expense));
      }).catch(reason => { if (!cancelled) setError(reason instanceof Error ? reason.message : "Unable to load expense"); });
    return () => { cancelled = true; };
  }, [id, fleet]);

  async function save() {
    if (!form || !canEdit || !data) return;
    setSaving(true); setError("");
    try { await updateExpense(id, applyExpenseCategoryRules(form, data.options.settings)); await onSaved(); onClose(); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "Unable to save expense"); }
    finally { setSaving(false); }
  }
  return <Modal title={canEdit ? "Edit entry" : "Expense details"} isSaving={saving} submitLabel="Save changes" hideSubmit={!canEdit} onClose={onClose} onSubmit={event => { event.preventDefault(); void save(); }}>
    {error && <ErrorBanner message={error} />}
    {!data && !error && <p role="status" className="text-zinc-500">Loading expense…</p>}
    {data && form && <fieldset disabled={!canEdit || saving}><ExpenseForm value={form} onChange={setForm} options={data.options} originalCategory={{ id: data.expense.categoryId, name: data.expense.category }} allowedCategoryIds={Array.from(new Set([data.expense.categoryId, ...access.filter(a => a.canCreate).map(a => a.categoryId)]))} drivers={data.drivers} trucks={data.trucks} owners={data.owners} /></fieldset>}
    {data && <ExpenseDocuments key={id} id={id} />}
  </Modal>;
}
