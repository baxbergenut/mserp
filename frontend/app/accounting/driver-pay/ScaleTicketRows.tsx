import type { DriverPayDriver, DriverPayEdits } from "@/app/lib/types";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";
import { PayInput } from "./PayInput";
import { isScaleTicket, scaleTicketAmountValid, sourceAmountInput } from "./pay";

export function ScaleTicketRows({ driver, edits, disabled, onEdit }: {
  driver: DriverPayDriver;
  edits: DriverPayEdits;
  disabled: boolean;
  onEdit: (update: (current: DriverPayEdits) => DriverPayEdits) => void;
}) {
  const tickets = [...(driver.autoCharges ?? []).filter(isScaleTicket), ...(driver.deferredScaleCharges ?? [])];
  return tickets.map(ticket => {
    const value = edits.scaleDeductions?.[ticket.source] ?? ticket.amount;
    const valid = scaleTicketAmountValid(value, ticket.amount);
    const deferred = valid && hundredths(value) === BigInt(0) && hundredths(ticket.amount) !== BigInt(0);
    const due = decimalDisplay(hundredths(ticket.amount), true);
    return <tr key={ticket.source}>
      <td className="h-8 border-b border-r border-zinc-800 px-2 text-zinc-300">
        <div className="flex items-center justify-between gap-1">
          <span className="min-w-0 truncate" title={ticket.name}>{ticket.name}</span>
          {deferred && <span className="shrink-0 text-[10px] text-zinc-400">Deferred</span>}
        </div>
      </td>
      <td className="border-b border-zinc-800 p-0">
        <PayInput disabled={disabled} aria-label={`${driver.fullName}, ${ticket.name}, deduction`}
          aria-invalid={!valid} inputMode="decimal" value={value}
          normalizeValue={amount => sourceAmountInput(amount, ticket.amount)}
          onValueChange={amount => onEdit(current => ({
            ...current,
            scaleDeductions: { ...current.scaleDeductions, [ticket.source]: amount },
          }))}
          title={`Enter zero to defer this ticket, or ${due} to collect it in full.`}
          className="h-8 w-full min-w-0 bg-transparent px-2 text-right font-mono text-xs text-red-300 outline-none focus:ring-1 focus:ring-inset focus:ring-blue-500 aria-invalid:bg-red-500/10" />
      </td>
    </tr>;
  });
}
