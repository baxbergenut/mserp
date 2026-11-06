"use client";

import { PayInput } from "./PayInput";
import { carryBreakdown, validExpenseAmount } from "./pay";
import type { DriverPayDriver, DriverPayEdits } from "@/app/lib/types";
import { decimalDisplay, decimalInputValue, hundredths } from "@/app/gross-board/board";

export function ExpenseRowLabel({ id, name, date, onOpenExpense, disabled }: {
  id: string; name: string; date?: string; onOpenExpense?: (id: string) => void; disabled?: boolean;
}) {
  return <>
    <button type="button" disabled={!onOpenExpense || disabled} onClick={() => onOpenExpense?.(id)} className="min-w-0 truncate text-left hover:text-blue-300 hover:underline disabled:text-inherit disabled:no-underline" aria-label={`Open expense ${name}`}>{name}</button>
    {date && <span className="shrink-0 text-[10px] text-zinc-500">{`${date.slice(5, 7)}/${date.slice(8, 10)}`}</span>}
  </>;
}

export function ExpenseRows({ driver, edits, disabled, onEdit, onOpenExpense, expenseOpenDisabled }: {
  driver: DriverPayDriver; edits: DriverPayEdits; disabled: boolean;
  onOpenExpense?: (id: string) => void; expenseOpenDisabled?: boolean;
  onEdit: (update: (value: DriverPayEdits) => DriverPayEdits) => void;
}) {
  const rows = edits.expenseDeductions ?? [];
  if (!rows.length) return null;
  return <>
    {rows.map(row => {
      const isExpense = row.source !== "escrow";
      const accessorial = row.source === "accessorial";
      const credit = row.source === "reimburse" || accessorial;
      const label = accessorial ? `Accessorial · ${row.name}` : row.name;
      const valid = validExpenseAmount(row.amount, row.available);
      const value = valid && hundredths(row.amount) !== BigInt(0) ? `${credit ? "+" : "-"}${row.amount}` : row.amount;
      const balance = valid
        ? `Left: ${decimalDisplay(hundredths(row.openingBalance) - hundredths(row.amount), true)}`
        : `Available: ${decimalDisplay(hundredths(row.available), true)}`;
      return <tr key={`${row.source ?? "charge"}:${row.expenseId}`}>
        <td className="h-8 border-b border-r border-zinc-800 px-2 text-zinc-300" title={row.name}>
          <div className="flex items-center justify-between gap-1">
            {isExpense ? <ExpenseRowLabel id={row.expenseId} name={label} date={row.expenseDate} onOpenExpense={onOpenExpense} disabled={expenseOpenDisabled} /> : <span className="min-w-0 truncate">{row.name}</span>}
            <span className="ml-auto shrink-0 text-[10px] text-zinc-500">{balance}</span>
          </div>
        </td>
        <td className="border-b border-zinc-800 p-0 align-top"><PayInput disabled={disabled} aria-label={`${driver.fullName}, expense ${row.name}, ${accessorial ? "accessorial credit" : credit ? "reimbursement" : "deduction"}`} aria-invalid={!valid} inputMode="decimal" normalizeValue={value => {
          const amount = value.trim().replace(/^[+-]/, "");
          return decimalInputValue(amount, true) ?? amount;
        }} value={value}
          onValueChange={amount => onEdit(current => ({ ...current, expenseDeductions: current.expenseDeductions?.map(item => item.expenseId === row.expenseId && item.source === row.source ? { ...item, amount, ...((!item.source || item.source === "accessorial") ? { mode: "fixed" as const } : {}), apply: true } : item) }))}
          title={row.expenseDate < edits.weekStart ? carryBreakdown(BigInt(0), hundredths(row.openingBalance)) : undefined}
          className={`h-8 w-full min-w-0 bg-transparent px-2 text-right font-mono text-xs ${credit ? "text-emerald-300" : "text-red-300"} outline-none focus:ring-1 focus:ring-inset focus:ring-blue-500 aria-invalid:bg-red-500/10`} /></td>
      </tr>;
    })}
  </>;
}
