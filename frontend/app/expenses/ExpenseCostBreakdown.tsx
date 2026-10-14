"use client";

import { useEffect, useRef, useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import type { ExpenseInput } from "../lib/types";
import { controlClass } from "../components/management/ManagementUI";
import { centsLabel, costBreakdownError, expenseCents } from "../lib/expenseCosts";

export function ExpenseNameSelect({ value, names, onChange, label = "Name", className = controlClass }: {
  value: string; names: string[]; onChange: (value: string) => void; label?: string; className?: string;
}) {
  const [customValue, setCustomValue] = useState<string | null>(null);
  const defaultName = names.find(name => name.toLowerCase() === value.trim().toLowerCase());
  const customMode = (customValue !== null && customValue === value) || (!defaultName && !!value);
  return <div className="space-y-2">
    <select aria-label={label} required value={customMode ? "__custom" : defaultName ?? ""} className={className} onChange={event => {
      setCustomValue(event.target.value === "__custom" ? "" : null); onChange(event.target.value === "__custom" ? "" : event.target.value);
    }}>
      <option value="">Select name</option>
      {names.map(name => <option key={name} value={name}>{name}</option>)}
      <option value="__custom">Custom name…</option>
    </select>
    {customMode && <input aria-label={`${label} custom`} required pattern=".*\S.*" maxLength={100} value={value} onChange={event => { setCustomValue(event.target.value); onChange(event.target.value); }} className={className} placeholder="Enter custom name" />}
  </div>;
}

export function ExpenseCostBreakdown({ value, names, onChange, label = "Cost breakdown" }: {
  value: ExpenseInput; names: string[]; onChange: (value: ExpenseInput) => void; label?: string;
}) {
  const rows = value.costBreakdown ?? [];
  const firstAmount = useRef<HTMLInputElement>(null);
  const error = costBreakdownError(value);
  useEffect(() => { firstAmount.current?.setCustomValidity(error); }, [error, rows.length]);
  const total = rows.reduce((sum, row) => sum + (expenseCents(row.amount) ?? BigInt(0)), BigInt(0));
  const entered = expenseCents(value.amount);
  return <section aria-label={label} className="min-w-0 space-y-3">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h3 className="text-[13px] font-semibold text-zinc-200">{label}</h3>
      <button type="button" disabled={rows.length >= 100} className="inline-flex items-center gap-1 rounded-lg border border-zinc-700 px-2 py-1.5 text-xs text-zinc-400 hover:bg-zinc-800 disabled:opacity-40" onClick={() => onChange({ ...value, costBreakdown: [...rows, { name: "", amount: "" }] })}>
        <Plus className="h-3.5 w-3.5" /> Add cost
      </button>
    </div>
    {rows.map((row, index) => <div key={index} className="grid grid-cols-[minmax(0,1fr)_7rem_auto] items-start gap-2">
      <ExpenseNameSelect value={row.name} names={names} label={`${label} ${index + 1} name`} onChange={name => onChange({ ...value, costBreakdown: rows.map((item, i) => i === index ? { ...item, name } : item) })} />
      <input ref={index === 0 ? firstAmount : undefined} required aria-label={`${label} ${index + 1} amount`} type="number" step="0.01" value={row.amount} className={controlClass} placeholder="0.00" onChange={event => onChange({ ...value, costBreakdown: rows.map((item, i) => i === index ? { ...item, amount: event.target.value } : item) })} />
      <button type="button" aria-label={`Remove ${label.toLowerCase()} ${index + 1}`} className="rounded-md p-2 text-zinc-500 hover:bg-red-500/10 hover:text-red-400" onClick={() => onChange({ ...value, costBreakdown: rows.filter((_, i) => i !== index) })}><Trash2 className="h-4 w-4" /></button>
    </div>)}
    {rows.length > 0 && <div aria-live="polite" className="space-y-1 text-xs">
      <p className="text-zinc-400">Breakdown total: {centsLabel(total)}{entered !== null && total !== entered ? ` · Difference: ${centsLabel(entered - total)}` : ""}</p>
      {error ? <p className="text-red-400">{error}</p> : <p className="text-emerald-400">Matches entered amount</p>}
    </div>}
  </section>;
}
