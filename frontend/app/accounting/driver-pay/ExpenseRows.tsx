"use client";

import { PayInput } from "./PayInput";
import { carryBreakdown } from "./pay";
import type { DriverPayDriver, DriverPayEdits } from "@/app/lib/types";
import { decimalDisplay, hundredths, validDecimal } from "@/app/gross-board/board";

export function ExpenseRows({ driver, edits, disabled, onEdit }: {
  driver: DriverPayDriver; edits: DriverPayEdits; disabled: boolean;
  onEdit: (update: (value: DriverPayEdits) => DriverPayEdits) => void;
}) {
  const rows = edits.expenseDeductions ?? [];
  if (!rows.length) return null;
  return <>
    {rows.map(row => {
      const valid = row.amount.trim() !== "" && validDecimal(row.amount) && hundredths(row.amount) >= BigInt(0) && hundredths(row.amount) <= hundredths(row.available);
      const value = row.amount === "" || (validDecimal(row.amount) && hundredths(row.amount) === BigInt(0)) ? row.amount : `-${row.amount}`;
      const left = valid ? decimalDisplay(hundredths(row.openingBalance) - hundredths(row.amount), true) : "—";
      return <tr key={row.expenseId}>
        <td className="h-8 border-b border-r border-zinc-800 px-2 text-zinc-300" title={row.name}>
          <div className="flex items-center justify-between gap-1"><span className="min-w-0 truncate">{row.name}</span><span className="ml-auto shrink-0 text-[10px] text-zinc-500" title={`Remaining ${left}`}>Left: {left}</span></div>
        </td>
        <td className="border-b border-zinc-800 p-0 align-top"><PayInput disabled={disabled} aria-label={`${driver.fullName}, expense ${row.name}, deduction`} aria-invalid={!valid} inputMode="decimal" value={value}
          onValueChange={value => { const amount = value.replace(/^-/, ""); onEdit(current => ({ ...current, expenseDeductions: current.expenseDeductions?.map(item => item.expenseId === row.expenseId ? { ...item, amount, apply: true } : item) })); }}
          title={row.expenseDate < edits.weekStart ? carryBreakdown(BigInt(0), hundredths(row.openingBalance)) : undefined}
          className="h-8 w-full min-w-0 bg-transparent px-2 text-right font-mono text-xs text-red-300 outline-none focus:ring-1 focus:ring-inset focus:ring-blue-500 aria-invalid:bg-red-500/10" /></td>
      </tr>;
    })}
  </>;
}
