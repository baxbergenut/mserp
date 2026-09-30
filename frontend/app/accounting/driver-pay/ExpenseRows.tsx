"use client";

import type { DriverPayDriver, DriverPayEdits } from "@/app/lib/types";
import { hundredths, validDecimal } from "@/app/gross-board/board";

export function ExpenseRows({ driver, edits, disabled, onEdit }: {
  driver: DriverPayDriver; edits: DriverPayEdits; disabled: boolean;
  onEdit: (update: (value: DriverPayEdits) => DriverPayEdits) => void;
}) {
  const rows = edits.expenseDeductions ?? [];
  if (!rows.length) return null;
  return <>
    {rows.map(row => {
      const valid = row.amount.trim() !== "" && validDecimal(row.amount) && hundredths(row.amount) >= BigInt(0) && hundredths(row.amount) <= hundredths(row.available);
      return <tr key={row.expenseId}>
        <td className="h-8 border-b border-r border-zinc-800 px-2 text-zinc-300" title={row.name}>
          <div className="flex items-center gap-2"><span className="min-w-0 flex-1 truncate">{row.name}</span><span className="shrink-0 text-[9px] text-blue-400">{row.category === "Penalties" ? "Penalty" : "Expense"}</span></div>
        </td>
        <td className="border-b border-zinc-800 p-0 align-top"><input disabled={disabled} aria-label={`${driver.fullName}, expense ${row.name}, deduction`} aria-invalid={!valid} inputMode="decimal" value={row.amount}
          onChange={event => { const amount = event.target.value; onEdit(current => ({ ...current, expenseDeductions: current.expenseDeductions?.map(item => item.expenseId === row.expenseId ? { ...item, amount, apply: true } : item) })); }}
          title="Positive amount to deduct this week. Zero carries the full balance forward. Changes save automatically."
          className="h-8 w-full min-w-0 bg-transparent px-2 text-right font-mono text-xs text-red-300 outline-none focus:ring-1 focus:ring-inset focus:ring-blue-500 aria-invalid:bg-red-500/10" /></td>
      </tr>;
    })}
  </>;
}
