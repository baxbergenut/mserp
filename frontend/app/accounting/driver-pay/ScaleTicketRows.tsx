import { ChevronDown, RotateCcw } from "lucide-react";
import type { DriverPayDriver, DriverPayEdits } from "@/app/lib/types";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";
import { PayInput } from "./PayInput";
import { isScaleTicket, sourceAmountInput, validSourceAmount } from "./pay";

const magnitude = (amount: bigint) => amount < BigInt(0) ? -amount : amount;

export function ScaleTicketRows({ driver, edits, disabled, onEdit, expanded, onToggle }: {
  driver: DriverPayDriver;
  edits: DriverPayEdits;
  disabled: boolean;
  expanded: boolean;
  onToggle: () => void;
  onEdit: (update: (current: DriverPayEdits) => DriverPayEdits) => void;
}) {
  const tickets = [...(driver.autoCharges ?? []).filter(isScaleTicket), ...(driver.deferredScaleCharges ?? [])];
  const grouped = tickets.length > 1;
  const values = tickets.map(ticket => edits.scaleDeductions?.[ticket.source] ?? (driver.settlement?.finalized ? ticket.amount : ticket.scaleDue ?? ticket.amount));
  const validTotal = tickets.every((ticket, index) => validSourceAmount(values[index], ticket.scaleDue ?? ticket.amount));
  const total = values.reduce((sum, value) => sum + hundredths(value), BigInt(0));
  return <>
    {grouped && <tr>
      <td colSpan={2} className="border-b border-zinc-800 p-0">
        <button type="button" aria-expanded={expanded} onClick={onToggle}
          aria-label={`${driver.fullName}, CAT Scale, ${tickets.length} expenses`}
          className="flex min-h-8 w-full items-stretch text-left hover:bg-zinc-800/60 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-blue-500">
          <span className="flex w-[69.3548%] min-w-0 items-center gap-1.5 border-r border-zinc-800 px-2 text-zinc-300">
            <ChevronDown aria-hidden="true" className={`h-3 w-3 shrink-0 transition-transform ${expanded ? "" : "-rotate-90"}`} />
            <span className="truncate">CAT Scale</span>
            <span className="ml-auto shrink-0 rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] text-zinc-400">{tickets.length}</span>
          </span>
          <span aria-label="CAT Scale total" className={`flex flex-1 items-center justify-end px-2 font-mono text-xs ${!validTotal ? "text-amber-300" : total > BigInt(0) ? "text-emerald-300" : "text-red-300"}`}>
            {validTotal ? `${total > BigInt(0) ? "+" : ""}${decimalDisplay(total, true)}` : "Check amounts"}
          </span>
        </button>
      </td>
    </tr>}
    {(!grouped || expanded) && tickets.map(ticket => {
    const due = ticket.scaleDue ?? ticket.amount;
    const value = edits.scaleDeductions?.[ticket.source] ?? (driver.settlement?.finalized ? ticket.amount : due);
    const valid = validSourceAmount(value, due);
    const left = valid ? decimalDisplay(magnitude(hundredths(due) - hundredths(value)), true) : "—";
    const original = decimalDisplay(magnitude(hundredths(ticket.scaleOriginalAmount ?? due)), true);
    const balance = decimalDisplay(magnitude(hundredths(due)), true);
    const description = `Original ticket ${original}. Available for this statement ${balance}.`;
    const setAmount = (amount: string) => onEdit(current => ({
      ...current,
      scaleDeductions: { ...current.scaleDeductions, [ticket.source]: amount },
    }));
    return <tr key={ticket.source}>
      <td className={`h-8 border-b border-r border-zinc-800 pr-2 text-zinc-300 ${grouped ? "pl-6" : "pl-2"}`}>
        <div className="flex items-center justify-between gap-1">
          <span className="min-w-0 truncate" title={ticket.name}>{ticket.name}</span>
          <span className="ml-auto shrink-0 text-[10px] text-zinc-500" title={description}>Left: {left}</span>
          {!disabled && (!valid || hundredths(value) !== hundredths(due)) && <button type="button"
            aria-label={`${driver.fullName}, ${ticket.name}, reset to amount due`}
            title="Collect the full unpaid amount" onClick={() => setAmount(due)}
            className="rounded p-1 text-blue-400 hover:bg-zinc-800"><RotateCcw className="h-3 w-3" /></button>}
        </div>
      </td>
      <td className="border-b border-zinc-800 p-0">
        <PayInput disabled={disabled} aria-label={`${driver.fullName}, ${ticket.name}, deduction`}
          aria-invalid={!valid} inputMode="decimal" value={value}
          normalizeValue={amount => sourceAmountInput(amount, due)}
          onValueChange={setAmount}
          title={description}
          className={`h-8 w-full min-w-0 bg-transparent px-2 text-right font-mono text-xs outline-none focus:ring-1 focus:ring-inset focus:ring-blue-500 aria-invalid:bg-red-500/10 ${hundredths(due) > BigInt(0) ? "text-emerald-300" : "text-red-300"}`} />
      </td>
    </tr>;
  })}
  </>;
}
