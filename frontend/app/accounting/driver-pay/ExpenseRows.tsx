"use client";

import type { DriverPayDriver, DriverPayEdits } from "@/app/lib/types";
import { decimalDisplay, hundredths, validDecimal } from "@/app/gross-board/board";

export function ExpenseRows({ driver, edits, disabled, onEdit }: {
  driver: DriverPayDriver; edits: DriverPayEdits; disabled: boolean;
  onEdit: (update: (value: DriverPayEdits) => DriverPayEdits) => void;
}) {
  const rows = edits.expenseDeductions ?? [];
  if (!rows.length) return null;
  return <>
    <tr><td colSpan={2} className="border-b border-zinc-800 px-2 py-1 text-[10px] text-zinc-500">
      Driver expenses · enter positive deductions. Saved amounts reduce the balance.
      {rows.some(row => !row.saved && !row.apply) && <button type="button" disabled={disabled} className="mt-1 block text-blue-400 hover:underline disabled:opacity-40"
        onClick={() => onEdit(current => ({ ...current, expenseDeductions: current.expenseDeductions?.map(row => ({ ...row, apply: true })) }))}>Save expense deductions</button>}
    </td></tr>
    {rows.map(row => {
      const valid = row.amount.trim() !== "" && validDecimal(row.amount) && hundredths(row.amount) >= BigInt(0) && hundredths(row.amount) <= hundredths(row.available);
      const remaining = hundredths(row.openingBalance) - hundredths(row.amount);
      return <tr key={row.expenseId}>
        <td className="border-b border-r border-zinc-800 px-2 py-1 text-zinc-300" title={`${row.expenseDate} · ${row.name}. Total ${decimalDisplay(hundredths(row.total), true)}. Available ${decimalDisplay(hundredths(row.available), true)}.`}>
          <div className="truncate">{row.name}</div>
          <div className="text-[10px] text-zinc-500">{row.expenseDate} · {row.apply ? "Saving…" : row.saved ? "Saved" : "Not saved"}</div>
          <div className="text-[10px] text-zinc-500">Remaining after this week: {valid ? decimalDisplay(remaining, true) : "—"}</div>
        </td>
        <td className="border-b border-zinc-800 p-0 align-top"><input disabled={disabled} aria-label={`${driver.fullName}, expense ${row.name}, deduction`} aria-invalid={!valid} inputMode="decimal" value={row.amount}
          onChange={event => { const amount = event.target.value; onEdit(current => ({ ...current, expenseDeductions: current.expenseDeductions?.map(item => item.expenseId === row.expenseId ? { ...item, amount, apply: true } : item) })); }}
          title="Positive amount to deduct this week. Zero carries the full balance forward. Changes save automatically."
          className="h-8 w-full min-w-0 bg-transparent px-2 text-right font-mono text-xs text-red-300 outline-none focus:ring-1 focus:ring-inset focus:ring-blue-500 aria-invalid:bg-red-500/10" /></td>
      </tr>;
    })}
  </>;
}
