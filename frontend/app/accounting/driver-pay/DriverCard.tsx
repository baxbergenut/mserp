"use client";

import { Fragment, memo, useState } from "react";
import Link from "next/link";
import { AlertTriangle, ChevronDown, RotateCcw } from "lucide-react";
import type { DriverPayAdjustment, DriverPayDriver, DriverPayEdits } from "@/app/lib/types";
import { Modal, controlClass } from "@/app/components/management/ManagementUI";
import { addDays, decimalDisplay, hundredths, shortDate, validDecimal } from "@/app/gross-board/board";
import { costAmount, costRows, driverTotals } from "./pay";
import { ChargeActions, GeneratedChargeRows } from "./ChargeRows";
import { ExpenseRows } from "./ExpenseRows";
import { CommentButton } from "./CommentButton";

export const payButtonClass = "inline-flex items-center justify-center gap-2 rounded-lg border border-zinc-700/70 px-3 py-2 text-xs text-zinc-300 hover:bg-zinc-800 focus-visible:outline-2 focus-visible:outline-blue-500 disabled:cursor-not-allowed disabled:opacity-40";
const weekdays = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const amount = (value: string, money = false) => value === "" ? "—" : decimalDisplay(hundredths(value), money);
export const tariffLabel = (driver: DriverPayDriver) => {
  const rate = driver.payRate.includes(".") ? driver.payRate.replace(/0+$/, "").replace(/\.$/, "") : driver.payRate;
  return driver.payType === "cpm" ? `$${rate}/mile` : `${rate}% of driver gross`;
};
const columns = [
  ["Day", 82], ["Load number", 116], ["Pickup date", 94], ["Pickup location", 120],
  ["Delivery location", 120], ["Original rate", 95], ["Driver gross", 95],
  ["Total miles", 78], ["Loaded miles", 78], ["Deadhead miles", 88], ["Driver fee", 92], ["Reimbursements/charges", 215], ["Amount", 95], ["Comments", 36],
] as const;
const cell = "h-8 border-b border-r border-zinc-800/70 px-2 py-0 align-middle";
const numeric = `${cell} text-right font-mono tabular-nums whitespace-nowrap`;

type Props = {
  returnToPay?: boolean;
 onSettlement?: (reopen: boolean) => void;
 settlementDisabled?: boolean;
  driver: DriverPayDriver;
  edits: DriverPayEdits;
  disabled: boolean;
  open: boolean;
  onToggle: (id: string) => void;
  chargeActionsDisabled: boolean;
  onReload: () => Promise<void>;
  onEdit: (id: string, update: (edits: DriverPayEdits) => DriverPayEdits) => void;
};

export const DriverCard = memo(function DriverCard({ driver, edits, disabled, open, onToggle, onEdit, chargeActionsDisabled, onReload, onSettlement, settlementDisabled, returnToPay }: Props) {
  const [comment, setComment] = useState<{ key: string | null; label: string; value: string } | null>(null);
  const totals = driverTotals(driver, edits);
  const edit = (update: (value: DriverPayEdits) => DriverPayEdits) => onEdit(driver.id, update);
  const allFeesMissing = driver.loads.length > 0 && totals.missingFees === driver.loads.length;
  const incomplete = totals.review > 0;
  const netAdjustments = totals.addition + totals.reimbursement - totals.deduction + totals.costs + totals.generated - totals.expenses;
  const payable = allFeesMissing ? "Needs review" : decimalDisplay(totals.payable, true);
  const editAdjustment = (index: number, update: (item: DriverPayAdjustment) => DriverPayAdjustment) => {
    edit(current => {
      const adjustments = Array.from({ length: Math.max(current.adjustments.length, index + 1) }, (_, row) => current.adjustments[row] ?? { id: crypto.randomUUID(), kind: "reimbursement" as const, name: "", note: "", amount: "" });
      adjustments[index] = update(adjustments[index]);
      return { ...current, adjustments };
    });
  };
  const weekRowCount = weekdays.reduce((count, _, index) => count + Math.max(1, driver.loads.filter(load => load.date === addDays(edits.weekStart, index)).length), 0);
  const visibleCostRows = driver.payType === "cpm" ? [] : costRows;
  const adjustmentCells = <td colSpan={2} rowSpan={weekRowCount} className="border-b border-r border-zinc-800/70 bg-zinc-950 p-0 align-top">
    <div data-payroll-scroll={`${driver.id}:adjustments`} className="h-56 overflow-y-auto overscroll-contain" role="region" aria-label={`${driver.fullName} reimbursements and charges`} tabIndex={0}>
      <table className="w-full table-fixed border-separate border-spacing-0 text-xs"><colgroup><col style={{ width: `${215 / 310 * 100}%` }} /><col style={{ width: `${95 / 310 * 100}%` }} /></colgroup>
        <tbody>{visibleCostRows.map(({ key, label }, index) => {
          const value = costAmount(driver, edits, key);
          const manual = edits[`${key}Override`] != null;
          const source = amount(driver[`${key}Total`], true);
          const description = `Weekly ${label.toLowerCase()} total: ${source}. ${key === "fuel" ? "Diesel, by merchant-local purchase date." : "Matched historical truck tolls, by crossing date; includes credits."} ${driver.isOwnerOperator && driver.payType === "gross_percentage" ? "Automatically deducted from pay." : "Company expense: no automatic deduction."}`;
          return <tr key={key} className="sticky z-10 bg-zinc-950" style={{ top: index * 32 }}>
            <td className={`${cell} text-zinc-300`}><div className="flex items-center justify-between gap-1"><span title={description}>{label}</span><span className="ml-auto text-[10px] text-zinc-500" title={description}>Source {source}</span>{manual && <button type="button" disabled={disabled} aria-label={`${driver.fullName}, ${label}, reset to automatic`} title="Reset to automatic amount" onClick={() => edit(current => ({ ...current, [`${key}Override`]: null }))} className="rounded p-1 text-blue-400 hover:bg-zinc-800 disabled:opacity-40"><RotateCcw className="h-3 w-3" /></button>}</div></td>
            <td className={`${cell} !border-r-0 !px-0`}><input aria-label={`${driver.fullName}, ${label}, amount`} aria-invalid={!value.trim() || !validDecimal(value)} disabled={disabled} inputMode="decimal" value={value} title={`${manual ? "Manual override" : "Automatic amount"}. Positive: reimbursement. Negative: charge. Zero: no charge.`} onChange={event => { const value = event.target.value; edit(current => ({ ...current, [`${key}Override`]: value })); }} className={`h-[31px] w-full min-w-0 bg-transparent px-2 text-right font-mono text-xs outline-none focus:bg-blue-500/10 focus:ring-1 focus:ring-inset focus:ring-blue-500 aria-invalid:bg-red-500/10 ${value.startsWith("-") ? "text-red-300" : hundredths(value) > BigInt(0) ? "text-emerald-300" : "text-zinc-400"}`} /></td>
          </tr>;
        })}<ExpenseRows driver={driver} edits={edits} disabled={disabled} onEdit={edit} /><GeneratedChargeRows driver={driver} edits={edits} disabled={disabled} onEdit={edit} />{Array.from({ length: Math.min(100, Math.max(7 - visibleCostRows.length, edits.adjustments.length + 1)) }, (_, index) => {
          const item = edits.adjustments[index];
          const name = item?.name ?? "";
          const value = item ? `${item.kind === "deduction" ? "-" : ""}${item.amount}` : "";
          const empty = !name.trim() && !item?.amount && item?.kind !== "deduction";
          return <tr key={index} className="hover:bg-zinc-800/20">
            <td className={`${cell} !px-0`}><input aria-label={`${driver.fullName}, adjustment ${index + 1}, name`} aria-invalid={!empty && !name.trim()} disabled={disabled} maxLength={200} value={name} title={name} onChange={event => { const name = event.target.value; editAdjustment(index, current => ({ ...current, name })); }} className="h-[31px] w-full min-w-0 bg-transparent px-2 text-xs text-zinc-300 outline-none focus:bg-blue-500/10 focus:ring-1 focus:ring-inset focus:ring-blue-500" /></td>
            <td className={`${cell} !border-r-0 !px-0`}><input aria-label={`${driver.fullName}, adjustment ${index + 1}, amount`} aria-invalid={!empty && (!validDecimal(item?.amount ?? "", true) || hundredths(item?.amount ?? "") <= BigInt(0))} disabled={disabled} inputMode="decimal" value={value} title="Positive: reimbursement. Negative: charge. Clear both cells to remove." onChange={event => {
              const value = event.target.value;
              editAdjustment(index, current => ({ ...current, amount: value.startsWith("-") ? value.slice(1) : value, kind: value.startsWith("-") ? "deduction" : "reimbursement" }));
            }} className={`h-[31px] w-full min-w-0 bg-transparent px-2 text-right font-mono text-xs outline-none focus:bg-blue-500/10 focus:ring-1 focus:ring-inset focus:ring-blue-500 aria-invalid:bg-red-500/10 ${item?.kind === "deduction" ? "text-red-300" : "text-emerald-300"}`} /></td>
          </tr>;
        })}</tbody>
      </table>
    </div>
  </td>;

  return <>
    <tr className={`h-9 cursor-pointer text-xs text-zinc-300 hover:bg-zinc-800/30 ${open ? "bg-zinc-800/30" : "bg-card"}`} onClick={() => onToggle(driver.id)}>
      <th scope="row" className="border-b border-zinc-800 px-3 py-0 text-left font-medium">
        <button type="button" aria-expanded={open} aria-controls={`driver-${driver.id}`} className="flex h-9 w-full items-center gap-2 text-left text-zinc-100 focus-visible:outline-2 focus-visible:outline-blue-500">
          <ChevronDown className={`h-3.5 w-3.5 shrink-0 text-zinc-500 ${open ? "rotate-180" : ""}`} />
          <span className="truncate">{driver.fullName}</span>{driver.settlement?.finalized && <span className="text-[10px] text-emerald-400">Finalized</span>}
          {incomplete && <span title={`${totals.review} loads need review`} className="inline-flex shrink-0 items-center gap-1 text-[10px] text-amber-300"><AlertTriangle className="h-3 w-3" />{totals.review}</span>}
        </button>
      </th>
      <td className="border-b border-zinc-800 px-3">{driver.truckUnit || "—"}</td>
      <td className="border-b border-zinc-800 px-3">{driver.isOwnerOperator ? "Owner operator" : "Company driver"}</td>
      <td className="border-b border-zinc-800 px-3">{tariffLabel(driver)}</td>
      <td className="border-b border-zinc-800 px-3 text-zinc-500">{driver.dispatcherName}</td>
      <td className="border-b border-zinc-800 px-3 text-right font-mono">{driver.loads.length}</td>
      <td title={incomplete ? "Provisional payable: highlighted loads need review" : "Total payable"} className={`border-b border-zinc-800 px-3 text-right font-mono font-medium ${incomplete ? "text-amber-200" : "text-zinc-100"}`}>{payable}</td>
    </tr>
    {open && <tr><td colSpan={7} className="border-b border-zinc-700 p-0">
      <div id={`driver-${driver.id}`}>
        {onSettlement && <div className="flex items-center justify-between border-b border-zinc-800 px-3 py-2"><span className="text-xs text-zinc-500">{driver.settlement?.finalized ? `Finalized by ${driver.settlement.finalizedBy}` : driver.settlement ? "Reopened for corrections" : "Draft settlement"}</span><button type="button" disabled={settlementDisabled} className={payButtonClass} onClick={() => onSettlement(!!driver.settlement?.finalized)}>{driver.settlement?.finalized ? "Reopen driver" : "Finalize driver"}</button></div>}
        <ChargeActions driver={driver} edits={edits} disabled={chargeActionsDisabled} onReload={onReload} />
 {totals.payable < BigInt(0) && <p role="status" className="px-3 py-2 text-xs text-amber-300">Negative payable — review deductions before confirming. Unpaid expense balances carry forward; other negative pay does not.</p>}
        <div data-payroll-scroll={`${driver.id}:loads`} className="overflow-x-auto" role="region" aria-label={`${driver.fullName} weekly loads`} tabIndex={0}>
          <table className="w-full min-w-[1504px] table-fixed border-separate border-spacing-0 bg-zinc-950/30 text-left text-xs">
            <colgroup>{columns.map(([label, width]) => <col key={label} style={{ width }} />)}</colgroup>
            <thead className="bg-zinc-900 text-[10px] text-zinc-400"><tr>{columns.map(([label], index) => <th key={label}
              title={label === "Driver gross" ? "Gross Board driver gross used for percentage pay" : undefined}
              className={`${cell} font-medium ${index === 0 ? "sticky left-0 z-10 bg-zinc-900" : ""} ${index >= 5 && index !== 11 ? "text-right" : ""}`}>
              {label === "Comments" ? <span className="sr-only">Comments</span> : label}
            </th>)}</tr></thead>
            <tbody>{weekdays.map((day, dayIndex) => {
              const date = addDays(edits.weekStart, dayIndex);
              const loads = driver.loads.filter(load => load.date === date);
              const dayLabel = <>{day}<span className="ml-2 text-[10px] text-zinc-600">{shortDate(date)}</span></>;
              return <Fragment key={date}>{loads.length ? loads.map((load, index) => <tr key={load.commentKey} className={load.loadRecordId === null ? "bg-amber-500/10" : load.issues.length ? "bg-amber-500/[0.04]" : "hover:bg-zinc-800/15"}>
                {index === 0 && <th rowSpan={loads.length} scope="rowgroup" className={`${cell} sticky left-0 z-10 bg-zinc-950 font-medium text-zinc-400`}>{dayLabel}</th>}
                <td className={`${cell} text-zinc-200`}><div className="flex items-center gap-1.5"><Link data-payroll-load={returnToPay || undefined} href={`/gross-board?${new URLSearchParams({ driverId: driver.id, date: load.date, slot: String(load.slot), loadNumber: load.loadNumber, ...(returnToPay ? { from: "driver-pay" } : {}) })}`} prefetch={false} className="truncate text-blue-400 underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-blue-500" title={`Open ${load.loadNumber} in Gross Board · ${load.date} · load ${load.slot + 1}`}>{load.loadNumber}</Link>{load.issues.length > 0 && <span title={load.issues.join("\n")} aria-label={load.issues.join(". ")} className="shrink-0 text-amber-300"><AlertTriangle className="h-3 w-3" /></span>}</div></td>
                <td className={`${cell} text-zinc-400`}>{load.pickupDate || "—"}</td>
                <td className={`${cell} text-zinc-400`}><div className="truncate" title={load.pickupLocation}>{load.pickupLocation || "—"}</div></td>
                <td className={`${cell} text-zinc-400`}><div className="truncate" title={load.deliveryLocation}>{load.deliveryLocation || "—"}</div></td>
                <td className={`${numeric} text-zinc-300`}>{amount(load.originalRate, true)}</td>
                <td className={`${numeric} text-zinc-300`}>{amount(load.driverGross, true)}</td>
                <td className={`${numeric} text-zinc-400`}>{amount(load.totalMiles)}</td>
                <td className={`${numeric} text-zinc-400`}>{amount(load.loadedMiles)}</td>
                <td className={`${numeric} text-zinc-400`}>{amount(load.deadheadMiles)}</td>
                <td className={`${numeric} font-medium text-zinc-200`}>{amount(load.fee, true)}</td>
                {dayIndex === 0 && index === 0 && adjustmentCells}
                <td className={`${cell} !px-1 text-center`}><CommentButton label={`${driver.fullName}, ${load.loadNumber}, ${load.date}, comment`} value={edits.comments[load.commentKey] ?? ""} disabled={disabled}
                  onClick={() => setComment({ key: load.commentKey, label: `${load.loadNumber} · ${load.date}`, value: edits.comments[load.commentKey] ?? "" })} /></td>
              </tr>) : <tr><th scope="row" className={`${cell} sticky left-0 bg-zinc-950 font-medium text-zinc-400`}>{dayLabel}</th><td colSpan={10} className={`${cell} text-zinc-600`}>No load</td>{dayIndex === 0 && adjustmentCells}<td className={cell} /></tr>}</Fragment>;
            })}</tbody>
            <tbody>
              <tr className="bg-blue-500/5 font-mono text-zinc-200"><th colSpan={5} className={`${cell} text-left font-sans font-medium`}>Weekly totals{incomplete ? " · provisional" : ""}</th>
                {[totals.original, totals.gross, totals.totalMiles, totals.loadedMiles, totals.deadheadMiles, totals.fee].map((value, index) => <Fragment key={index}><td className={numeric}>{index === 5 && allFeesMissing ? "—" : decimalDisplay(value, index < 2 || index === 5)}</td></Fragment>)}<td className={`${cell} font-sans text-[10px] text-zinc-500`}>Net adjustments</td><td className={numeric}>{decimalDisplay(netAdjustments, true)}</td><td className={`${cell} !px-1 text-center`}><CommentButton label={`${driver.fullName}, weekly comment`} value={edits.notes} disabled={disabled} onClick={() => setComment({ key: null, label: "Weekly comment", value: edits.notes })} /></td>
              </tr>
            </tbody>

          </table>
        </div>
      </div>
    </td></tr>}

    {comment && <Modal title={comment.value.trim() ? "Edit comment" : "Add comment"} description={`${driver.fullName} · ${comment.label}`} isSaving={false} submitLabel="Save comment" onClose={() => setComment(null)} onSubmit={event => {
      event.preventDefault();
      edit(current => comment.key === null ? { ...current, notes: comment.value } : { ...current, comments: { ...current.comments, [comment.key]: comment.value } });
      setComment(null);
    }}><label className="block text-xs text-zinc-400">Comment<textarea autoFocus aria-label="Comment" rows={5} maxLength={comment.key === null ? 5000 : 2000} value={comment.value} onChange={event => setComment({ ...comment, value: event.target.value })} className={`${controlClass} mt-2 resize-y`} placeholder="Add a comment…" /></label></Modal>}
  </>;
});
