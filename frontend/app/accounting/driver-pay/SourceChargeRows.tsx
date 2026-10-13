"use client";

import { RotateCcw } from "lucide-react";
import type { DriverPayDriver, DriverPayEdits } from "@/app/lib/types";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";
import { PayInput } from "./PayInput";
import { carryBreakdown, sourceAmountInput, validSourceAmount } from "./pay";

export function SourceChargeRows({ driver, edits, disabled, onEdit }: {
  driver: DriverPayDriver; edits: DriverPayEdits; disabled: boolean;
  onEdit: (update: (value: DriverPayEdits) => DriverPayEdits) => void;
}) {
  return <>{(edits.sourceCharges ?? []).filter(row => driver.autoCharges?.some(a => a.source === row.source)).map(row => {
    const magnitude = (value: bigint) => value < BigInt(0) ? -value : value;
    const due = hundredths(row.due), collected = hundredths(row.amount);
    const valid = validSourceAmount(row.amount, row.due);
    const description = carryBreakdown(magnitude(hundredths(row.base)), magnitude(hundredths(row.carry))) ?? `Due ${decimalDisplay(magnitude(due), true)}`;
    const setAmount = (amount: string) => onEdit(current => ({ ...current, sourceCharges: current.sourceCharges?.map(item => item.source === row.source && item.ownerId === row.ownerId ? { ...item, amount } : item) }));
    return <tr key={`${row.source}:${row.ownerId}`}>
      <td className="h-8 border-b border-r border-zinc-800 px-2 text-zinc-300"><div className="flex items-center justify-between gap-1"><span className="min-w-0 truncate" title={description}>{row.name}</span><span className="ml-auto shrink-0 text-[10px] text-zinc-500" title={description}>Left {valid ? decimalDisplay(magnitude(due - collected), true) : "—"}</span>{row.amount !== row.due && <button type="button" disabled={disabled} aria-label={`${driver.fullName}, ${row.name}, reset to amount due`} title="Collect the full amount due" onClick={() => setAmount(row.due)} className="rounded p-1 text-blue-400 hover:bg-zinc-800 disabled:opacity-40"><RotateCcw className="h-3 w-3" /></button>}</div></td>
      <td className="border-b border-zinc-800 p-0"><PayInput disabled={disabled} aria-label={`${driver.fullName}, ${row.name}, amount`} aria-invalid={!valid} inputMode="decimal" value={row.amount} normalizeValue={value => sourceAmountInput(value, row.due)} onValueChange={setAmount} title={`${description}. Enter zero to defer; the unpaid amount carries forward.`} className={`h-8 w-full min-w-0 bg-transparent px-2 text-right font-mono text-xs outline-none focus:ring-1 focus:ring-inset focus:ring-blue-500 aria-invalid:bg-red-500/10 ${due > BigInt(0) ? "text-emerald-300" : "text-red-300"}`} /></td>
    </tr>;
  })}</>;
}
