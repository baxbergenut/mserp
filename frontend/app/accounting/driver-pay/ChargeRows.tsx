"use client";

import { useState } from "react";
import Link from "next/link";
import { confirmDriverCharges } from "@/app/lib/api";
import type { ChargeOccurrence, DriverPayDriver, DriverPayEdits } from "@/app/lib/types";
import { Modal, controlClass, ErrorBanner } from "@/app/components/management/ManagementUI";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";
import { currentChargeWeek } from "../driver-charges/charges";

export function ChargeActions({ driver, edits, disabled, onReload }: { driver: DriverPayDriver; edits: DriverPayEdits; disabled: boolean; onReload: () => Promise<void> }) {
  const [selection, setSelection] = useState<ChargeOccurrence[] | null>(null);
  const [reopen, setReopen] = useState(false);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const rows = (edits.generatedCharges ?? []).filter(r => r.kind === "installment" && hundredths(r.amount) < BigInt(0));
  const button = "rounded border border-zinc-700 px-2 py-1 text-xs text-zinc-300 hover:bg-zinc-800 disabled:opacity-40";
  return <div className="flex flex-wrap items-center gap-2 border-b border-zinc-800 px-3 py-2">
    <Link className="text-xs text-blue-400" href={`/accounting/driver-charges?driverId=${driver.id}&action=recurring`}>Assign recurring charge</Link><span className="text-zinc-700">·</span><Link className="text-xs text-blue-400" href={`/accounting/driver-charges?driverId=${driver.id}&action=installment`}>New installment plan</Link>
    {rows.some(r => !r.confirmedAt) && <button type="button" disabled={disabled || edits.weekStart > currentChargeWeek()} className={button} title={disabled ? "Wait until weekly edits are saved" : "Record deductions actually withheld"} onClick={() => { setSelection(rows.filter(r => !r.confirmedAt)); setReopen(false); setError(""); }}>Confirm installment deductions</button>}
    {rows.some(r => r.confirmedAt) && <button type="button" disabled={disabled} className={button} onClick={() => { setSelection(rows.filter(r => r.confirmedAt)); setReopen(true); setReason(""); setError(""); }}>Reopen deductions</button>}
    {selection && <Modal title={reopen ? "Reopen installment deductions" : "Confirm installment deductions"} description={`${driver.fullName} · week of ${edits.weekStart}. ${reopen ? "Reverses collection and unlocks these rows; amounts remain scheduled." : "Confirm only amounts actually withheld. This does not finalize payroll."}`} isSaving={busy} submitLabel={reopen ? "Reopen selected" : "Confirm selected"} onClose={() => setSelection(null)} onSubmit={e => { e.preventDefault(); if (!selection.length) { setError("Select at least one deduction"); return; } setBusy(true); setError(""); void confirmDriverCharges(driver.id, edits.weekStart, selection, reopen ? reason : undefined).then(async () => { await onReload(); setSelection(null); }).catch(err => setError(err.message)).finally(() => setBusy(false)); }}><div className="space-y-3">{error && <ErrorBanner message={error} />}{rows.filter(r => Boolean(r.confirmedAt) === reopen).map(row => <label key={row.scheduleId} className="flex items-center gap-3 text-sm text-zinc-300"><input type="checkbox" checked={selection.some(r => r.scheduleId === row.scheduleId)} onChange={e => setSelection(e.target.checked ? [...selection, row] : selection.filter(r => r.scheduleId !== row.scheduleId))} />{row.name}<span className="ml-auto font-mono">{decimalDisplay(hundredths(row.amount), true)}</span></label>)}{reopen && <label className="block text-sm text-zinc-400">Reason<textarea required maxLength={2000} className={`${controlClass} mt-2`} value={reason} onChange={e => setReason(e.target.value)} /></label>}</div></Modal>}
  </div>;
}

export function GeneratedChargeRows({ driver, edits, disabled, onEdit }: { driver: DriverPayDriver; edits: DriverPayEdits; disabled: boolean; onEdit: (update: (edits: DriverPayEdits) => DriverPayEdits) => void }) {
  function edit(id: string, update: Partial<ChargeOccurrence>) { onEdit(current => ({ ...current, generatedCharges: current.generatedCharges?.map(r => r.scheduleId === id ? { ...r, ...update } : r) })); }
  return <>{(edits.generatedCharges ?? []).map(row => <tr key={row.scheduleId} className="bg-blue-500/[0.03]">
    <td className="h-8 border-b border-r border-zinc-800/70 p-0"><div className="flex items-center"><input aria-label={`${driver.fullName}, ${row.name}, charge name`} disabled={disabled || !!row.confirmedAt} maxLength={200} value={row.name} onChange={e => edit(row.scheduleId, { name: e.target.value, reset: false, overridden: true })} className="h-[31px] w-full min-w-0 bg-transparent px-2 text-xs text-zinc-300 outline-none focus:bg-blue-500/10 disabled:opacity-70" /><Link aria-label={`${row.name} source`} title={`${row.kind === "recurring" ? "Recurring" : "Installment"}${row.confirmedAt ? " · confirmed" : ""}. Scheduled ${row.scheduledAmount}`} className="pr-1 text-[9px] text-blue-400" href={`/accounting/driver-charges?driverId=${driver.id}&tab=${row.kind}&scheduleId=${row.scheduleId}`}>{row.confirmedAt ? "Confirmed" : row.kind === "recurring" ? "Recurring" : "Installment"}</Link>{!row.confirmedAt && <><button type="button" disabled={disabled} title="Skip this week" aria-label={`${row.name}, skip this week`} className="p-1 text-[10px] text-zinc-500 hover:text-zinc-100" onClick={() => edit(row.scheduleId, { amount: "0.00", overridden: true, reset: false })}>0</button><button type="button" disabled={disabled} title="Reset to scheduled amount" aria-label={`${row.name}, reset to scheduled amount`} className="p-1 text-xs text-blue-400" onClick={() => edit(row.scheduleId, { amount: row.scheduledAmount, overridden: false, reset: true })}>↺</button></>}</div></td>
    <td className="h-8 border-b border-zinc-800/70 p-0"><input aria-label={`${driver.fullName}, ${row.name}, charge amount`} disabled={disabled || !!row.confirmedAt} inputMode="decimal" value={row.amount} onChange={e => edit(row.scheduleId, { amount: e.target.value, overridden: true, reset: false })} className={`h-[31px] w-full min-w-0 bg-transparent px-2 text-right font-mono text-xs outline-none focus:bg-blue-500/10 ${row.amount.startsWith("-") ? "text-red-300" : "text-emerald-300"}`} /></td>
  </tr>)}</>;
}
