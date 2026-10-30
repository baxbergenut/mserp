"use client";

import { employmentTypeLabel } from "@/app/lib/driverType";
import { Fragment, memo, useCallback, useState } from "react";
import { IntentLink as Link } from "@/app/components/IntentLink";
import { AlertTriangle, ChevronDown, RotateCcw, MoreHorizontal, ArrowRight, X } from "lucide-react";
import type { DriverPayAdjustment, DriverPayDriver, DriverPayEdits, DriverPayLoad, LoadWeekTransferMode, PayLoadPlacementInput, PayLoadPlacementResult } from "@/app/lib/types";
import { Modal, controlClass } from "@/app/components/management/ManagementUI";
import { addDays, dayStatuses, decimalDisplay, hundredths, shortDate, validDecimal } from "@/app/gross-board/board";
import { activeLayovers, carryBreakdown, centsAmount, costAmount, costRows, driverTotals, sourceAmountInput, validSourceAmount } from "./pay";
import { ChargeActions, GeneratedChargeRows } from "./ChargeRows";
import { SourceChargeRows } from "./SourceChargeRows";
import { ExpenseRowLabel, ExpenseRows } from "./ExpenseRows";
import { StatementMenu } from "./StatementMenu";
import { PaySourceCell } from "./PaySourceCell";
import { copyPayCell, editPayCell, navigatePayGrid, selectPayCell } from "./payGrid";
import { PayInput } from "./PayInput";
import styles from "./payGrid.module.css";
import { LoadTransferDialog } from "./LoadTransferDialog";
import { CommentButton } from "./CommentButton";
import { CostTransactionsDialog } from "./CostTransactionsDialog";

export const payButtonClass = "inline-flex items-center justify-center gap-2 rounded-lg border border-zinc-700/70 px-3 py-2 text-xs text-zinc-300 hover:bg-zinc-800 focus-visible:outline-2 focus-visible:outline-blue-500 disabled:cursor-not-allowed disabled:opacity-40";
const weekdays = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const amount = (value: string, money = false) => value === "" ? "—" : decimalDisplay(hundredths(value), money);
export const tariffLabel = (driver: DriverPayDriver) => {
  if (!driver.payRate) return "Setup required";
  const rate = driver.payRate.includes(".") ? driver.payRate.replace(/0+$/, "").replace(/\.$/, "") : driver.payRate;
  return driver.payType === "cpm" ? `$${rate}/mile` : `${rate}% of driver gross`;
};
const columns = [
  ["Day", 82], ["Load number", 116], ["Pickup date", 94], ["Pickup location", 120],
  ["Delivery location", 120], ["Total gross", 95], ["Driver gross", 95],
  ["Total miles", 78], ["Loaded miles", 78], ["Deadhead miles", 88], ["Driver fee", 92], ["Reimbursements/charges", 215], ["Amount", 95], ["Comments", 36],
] as const;
const cell = "h-8 border-b border-r border-zinc-800/70 px-2 py-0 align-middle";
const numeric = `${cell} text-right font-mono tabular-nums whitespace-nowrap`;

export type DriverCardProps = {
  onPlacement?: (input: PayLoadPlacementInput) => Promise<PayLoadPlacementResult | void>;
  placementDisabled?: boolean;
 onTransfer?: (load: DriverPayLoad, mode: LoadWeekTransferMode, reason: string) => Promise<void>;
 transferDisabled?: boolean;
  detailError?: string;
  onRetryDetails?: () => void;
  onOpenExpense?: (id: string) => void;
  expenseOpenDisabled?: boolean;
  onAcceptSource?: (load: DriverPayLoad, field: "originalRate" | "totalMiles") => Promise<void>;
  sourceActionDisabled?: boolean;
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

export const DriverCard = memo(function DriverCard({ onPlacement, placementDisabled, onTransfer, transferDisabled, driver, edits, disabled, open, onToggle, onEdit, chargeActionsDisabled, onReload, onSettlement, settlementDisabled, returnToPay, onAcceptSource, sourceActionDisabled, onOpenExpense, expenseOpenDisabled, detailError, onRetryDetails }: DriverCardProps) {
  const [extraNumber, setExtraNumber] = useState("");
  const [extraChoices, setExtraChoices] = useState<PayLoadPlacementResult["choices"]>([]);
  const [extraChoice, setExtraChoice] = useState("");
  const [placementError, setPlacementError] = useState("");
  const [placing, setPlacing] = useState(false);
  const changeLoad = async (action: PayLoadPlacementInput["action"], load?: DriverPayLoad, driverGross?: string) => {
    if (!onPlacement || placementDisabled || placing) return;
    setPlacing(true); setPlacementError("");
    try {
      const choice = action === "add" && extraChoice !== "" ? extraChoices[Number(extraChoice)] : undefined;
      const result = await onPlacement({ driverId: driver.id, weekStart: edits.weekStart, action, loadNumber: action === "add" ? extraNumber : load?.loadNumber, loadRecordId: load?.loadRecordId ?? undefined, loadTripId: choice?.loadTripId ?? load?.loadTripId, sourceDriverId: choice?.sourceDriverId, commentKey: load?.commentKey, boardVersion: load?.boardVersion ?? 0, placementVersion: load?.payrollPlacementVersion ?? 0, driverGross });
      if (result?.choices) { setExtraChoices(result.choices); setExtraChoice(""); return; }
      if (action === "add") { setExtraNumber(""); setExtraChoices([]); setExtraChoice(""); }
    } catch (error) { setPlacementError(error instanceof Error ? error.message : "Unable to change this statement load"); }
    finally { setPlacing(false); }
  };
  const [transfer, setTransfer] = useState<DriverPayLoad | null>(null);
  const [costDetails, setCostDetails] = useState<"fuel" | "toll" | null>(null);
  const [comment, setComment] = useState<{ key: string | null; label: string; value: string } | null>(null);
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const closeMenu = useCallback(() => setMenu(null), []);
  const totals = driverTotals(driver, edits);
  const investor = !!driver.investorId;
  const statementName = driver.fullName;
  const employmentLabel = driver.displayEmploymentTypes?.length
    ? driver.displayEmploymentTypes.map(employmentTypeLabel).join(" / ")
    : employmentTypeLabel(driver.employmentType ?? (driver.isOwnerOperator ? "owner_operator" : "company"));
  const operatingDrivers = (driver.operatingDrivers ?? (driver.autoCharges ?? []).filter(c => c.source.startsWith("driver:")).map(c => ({ id: c.source.slice(7), name: c.name.replace(/^Driver earnings · /, "") }))).filter(person => person.id !== driver.profileDriverId);
  const profileHref = investor ? `/investors/detail?id=${driver.investorId}` : `/drivers/detail?id=${driver.id}`;
  const displayColumns = columns.map(([label, width]) => [investor && label === "Driver fee" ? "Investor share" : label, width] as const);
  const edit = (update: (value: DriverPayEdits) => DriverPayEdits) => onEdit(driver.id, update);
  const loadCount = driver.summary?.loads ?? driver.loads.length;
  const homeBadge = driver.homeWeek && loadCount === 0 ? <span title="Home every day of this week, with no loads" className="shrink-0 whitespace-nowrap rounded-full bg-violet-500/15 px-2 py-0.5 text-[10px] font-medium text-violet-300">Home</span> : null;
  const allFeesMissing = loadCount > 0 && totals.missingFees === loadCount;
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
  const extraLoads = driver.loads.filter(load => load.payrollSupplement);
  const weekRowCount = weekdays.reduce((count, _, index) => count + Math.max(1, driver.loads.filter(load => !load.payrollSupplement && load.date === addDays(edits.weekStart, index)).length), 0) + extraLoads.length + (onPlacement ? 1 : 0);
  const visibleCostRows = costRows.filter(({ key }) => {
    const carry = hundredths(edits.costs?.[`${key}Carry`] ?? "0");
    if (driver.payType === "cpm" && carry === BigInt(0)) return false;
    return investor || (driver.employmentType ?? (driver.isOwnerOperator ? "owner_operator" : "company")) === "owner_operator"
      || carry !== BigInt(0) || hundredths(edits.costs?.[`${key}Due`] ?? "0") !== BigInt(0) || hundredths(costAmount(driver, edits, key)) !== BigInt(0);
  });
  const layovers = activeLayovers(driver, edits);
  const fixedChargeRows = layovers.length + visibleCostRows.length + (driver.autoCharges?.length ?? 0) + (edits.expenseDeductions?.length ?? 0) + (edits.generatedCharges?.length ?? 0);
  const adjustmentCells = open ? <td colSpan={2} rowSpan={weekRowCount} className="pay-adjustment-span relative border-r border-zinc-800/70 bg-zinc-950 p-0 align-top">
    <div data-payroll-scroll={`${driver.id}:adjustments`} className="absolute inset-0 overflow-y-auto overscroll-contain" role="region" aria-label={`${driver.fullName} reimbursements and charges`} tabIndex={0}>
      <table className="w-full table-fixed border-separate border-spacing-0 text-xs"><colgroup><col style={{ width: `${215 / 310 * 100}%` }} /><col style={{ width: `${95 / 310 * 100}%` }} /></colgroup>
        <tbody>{layovers.map(row => <tr key={`layover:${row.date}`}>
          <td className={`${cell} text-zinc-300`}><div className="flex items-center justify-between gap-1"><span title={`Shop on ${row.date} · driver bonus`}>Layover · {shortDate(row.date)}</span><button type="button" disabled={disabled} aria-label={`${driver.fullName}, remove layover for ${row.date}`} title="Remove this day's layover from this week" onClick={() => edit(current => ({ ...current, layoverExcludedDates: [...(current.layoverExcludedDates ?? []), row.date] }))} className="rounded p-1 text-zinc-500 hover:bg-zinc-800 hover:text-red-300 disabled:opacity-40"><X className="h-4 w-4" /></button></div></td>
          <td className={`${numeric} !border-r-0 text-emerald-300`}>+{amount(row.amount, true)}</td>
        </tr>)}{visibleCostRows.map(({ key, label }, index) => {
          const value = costAmount(driver, edits, key);
          const manual = edits[`${key}Override`] != null;
          const source = amount(driver[`${key}Total`], true);
          const due = hundredths(edits.costs?.[`${key}Due`] ?? driver[`${key}Total`] ?? "0");
          const left = due + hundredths(value);
          const carry = edits.costs?.[`${key}Carry`] ?? "0";
          const description = carryBreakdown(due - hundredths(carry), hundredths(carry)) ?? `Source ${source}`;
          return <tr key={key} className="sticky z-10 bg-zinc-950" style={{ top: index * 32 }}>
            <td className={`${cell} text-zinc-300`}><div className="flex items-center justify-between gap-1"><button type="button" title={description} aria-label={`${driver.fullName}, ${label} transactions`} onClick={() => setCostDetails(key)} className="text-left hover:text-blue-300 hover:underline focus-visible:outline-2 focus-visible:outline-blue-500"><span>{label}</span>{hundredths(carry) !== BigInt(0) && <span className="ml-1 text-[10px] text-amber-300">+ carry</span>}</button><span className="ml-auto text-[10px] text-zinc-500" title={description}>Left: {decimalDisplay(left, true)}</span>{manual && <button type="button" disabled={disabled} aria-label={`${driver.fullName}, ${label}, reset to automatic`} title="Reset to automatic amount" onClick={() => edit(current => ({ ...current, [`${key}Override`]: null }))} className="rounded p-1 text-blue-400 hover:bg-zinc-800 disabled:opacity-40"><RotateCcw className="h-3 w-3" /></button>}</div></td>
            <td className={`${cell} !border-r-0 !px-0`}><PayInput aria-label={`${driver.fullName}, ${label}, amount`} aria-invalid={!validSourceAmount(value, centsAmount(-due))} normalizeValue={value => sourceAmountInput(value, centsAmount(-due))} disabled={disabled} inputMode="decimal" value={value} title={description} onValueChange={value => { edit(current => ({ ...current, [`${key}Override`]: value })); }} className={`h-[31px] w-full min-w-0 bg-transparent px-2 text-right font-mono text-xs outline-none focus:bg-blue-500/10 focus:ring-1 focus:ring-inset focus:ring-blue-500 aria-invalid:bg-red-500/10 ${value.startsWith("-") ? "text-red-300" : hundredths(value) > BigInt(0) ? "text-emerald-300" : "text-zinc-400"}`} /></td>
          </tr>;
        })}{(driver.autoCharges ?? []).filter(row => !edits.sourceCharges?.some(source => source.source === row.source)).map(row => <tr key={row.source}><td className={`${cell} text-zinc-300`}>{row.source.startsWith("expense-reimburse:") ? <div className="flex items-center justify-between gap-1"><ExpenseRowLabel id={row.source.slice("expense-reimburse:".length)} name={row.name} date={row.expenseDate} onOpenExpense={onOpenExpense} disabled={expenseOpenDisabled} /></div> : <span title={row.source.startsWith("driver:") ? driver.loads.filter(l => l.sourceDriverId === row.source.slice(7)).map(l => `${l.loadNumber}: ${amount(l.driverFee ?? "", true)}`).join("\n") : row.source.startsWith("escrow-release:") ? "Manage in Escrow or the driver’s Escrow tab" : row.source.startsWith("weighmytruck:") ? "CAT Scale ticket · manage in WeighMyTruck" : "Recurring truck charge"}>{row.name}</span>}</td><td aria-label={row.source.startsWith("escrow-release:") ? `${driver.fullName}, escrow release` : undefined} className={`${numeric} !border-r-0 ${hundredths(row.amount) > BigInt(0) ? "text-emerald-300" : "text-red-300"}`}>{hundredths(row.amount) > BigInt(0) ? "+" : ""}{amount(row.amount, true)}</td></tr>)}<SourceChargeRows driver={driver} edits={edits} disabled={disabled} onEdit={edit} /><ExpenseRows onOpenExpense={onOpenExpense} expenseOpenDisabled={expenseOpenDisabled} driver={driver} edits={edits} disabled={disabled} onEdit={edit} /><GeneratedChargeRows driver={driver} edits={edits} disabled={disabled} onEdit={edit} />{Array.from({ length: Math.min(100, Math.max(weekRowCount - fixedChargeRows, edits.adjustments.length + 1)) }, (_, index) => {
          const item = edits.adjustments[index];
          const name = item?.name ?? "";
          const value = item ? `${item.kind === "deduction" ? "-" : ""}${item.amount}` : "";
          const empty = !name.trim() && !item?.amount && item?.kind !== "deduction";
          return <tr key={index} className="hover:bg-zinc-800/20">
            <td className={`${cell} !px-0`}><PayInput aria-label={`${driver.fullName}, adjustment ${index + 1}, name`} aria-invalid={!empty && !name.trim()} disabled={disabled} maxLength={200} value={name} title={item?.legacySheets?`${name} · ${item.note}`:name} onValueChange={name => { editAdjustment(index, current => ({ ...current, name })); }} className="h-[31px] w-full min-w-0 bg-transparent px-2 text-xs text-zinc-300 outline-none focus:bg-blue-500/10 focus:ring-1 focus:ring-inset focus:ring-blue-500" /></td>
            <td className={`${cell} !border-r-0 !px-0`}><PayInput aria-label={`${driver.fullName}, adjustment ${index + 1}, amount`} aria-invalid={!empty && (!validDecimal(item?.amount ?? "", true) || hundredths(item?.amount ?? "") <= BigInt(0))} disabled={disabled} inputMode="decimal" value={value} normalizeValue={item?.kind === "deduction" ? value => value.startsWith("+") ? value.slice(1) : sourceAmountInput(value, "-1") : undefined} title="Positive: reimbursement. Negative: charge. Existing charges keep their minus sign; enter + explicitly to change to reimbursement. Clear both cells to remove." onValueChange={value => {
              editAdjustment(index, current => ({ ...current, amount: value.startsWith("-") ? value.slice(1) : value, kind: value.startsWith("-") ? "deduction" : "reimbursement" }));
            }} className={`h-[31px] w-full min-w-0 bg-transparent px-2 text-right font-mono text-xs outline-none focus:bg-blue-500/10 focus:ring-1 focus:ring-inset focus:ring-blue-500 aria-invalid:bg-red-500/10 ${item?.kind === "deduction" ? "text-red-300" : "text-emerald-300"}`} /></td>
          </tr>;
        })}</tbody>
      </table>
    </div>
  </td> : null;

  return <>
    <tr className={`h-9 cursor-pointer text-xs text-zinc-300 ${open ? "bg-zinc-950" : "bg-zinc-900 hover:bg-zinc-800/70"}`} onClick={() => onToggle(driver.id)} onContextMenu={event => { if (onSettlement) { event.preventDefault(); setMenu({ x: event.clientX, y: event.clientY }); } }} onKeyDown={event => { if (onSettlement && (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10"))) { event.preventDefault(); const rect = event.currentTarget.getBoundingClientRect(); setMenu({ x: rect.left + 12, y: rect.bottom }); } }}>
      <th scope="row" className="border-b border-zinc-800 px-3 py-0 text-left font-medium">
        <div className="flex h-9 w-full items-center gap-2 text-left text-zinc-100">
          <button type="button" aria-label={investor ? `${statementName} ${driver.truckUnit}` : statementName} aria-expanded={open} aria-controls={`driver-${driver.id}`} className="shrink-0 rounded focus-visible:outline-2 focus-visible:outline-blue-500">
          <ChevronDown className={`h-3.5 w-3.5 shrink-0 text-zinc-500 ${open ? "rotate-180" : ""}`} />
          </button>
          <Link href={profileHref} onClick={event => event.stopPropagation()} className="truncate underline-offset-2 hover:text-blue-300 hover:underline focus-visible:outline-2 focus-visible:outline-blue-500">{statementName}</Link>{(!investor || operatingDrivers.length === 0) && homeBadge}{driver.settlement?.finalized && <span title={driver.settlement.override ? "Finalized with override" : "Finalized"} aria-label={driver.settlement.override ? "Finalized with override" : "Finalized"} className="shrink-0 whitespace-nowrap text-[10px] text-emerald-400">{driver.settlement.override ? "Finalized*" : "Finalized"}</span>}
          {!investor && driver.teamDrivers?.map(person => <Link key={person.id} title={`Team driver: ${person.name}. This statement uses the main driver's tariff.`} href={`/drivers/detail?id=${person.id}`} onClick={event => event.stopPropagation()} className="truncate text-[10px] text-blue-400">+ {person.name}</Link>)}
          {incomplete && <span title={`${totals.review} loads need review`} className="inline-flex shrink-0 items-center gap-1 text-[10px] text-amber-300"><AlertTriangle className="h-3 w-3" />{totals.review}</span>}
        </div>
      </th>
      <td className="border-b border-zinc-800 px-3">{driver.truckId && driver.truckUnit ? <Link href={`/trucks/detail?id=${driver.truckId}`} onClick={event => event.stopPropagation()} className="font-mono underline-offset-2 hover:text-blue-300 hover:underline focus-visible:outline-2 focus-visible:outline-blue-500">{driver.truckUnit}</Link> : driver.truckUnit || "—"}</td>
      <td className="border-b border-zinc-800 px-3">{investor ? <div className="flex items-center gap-2">{operatingDrivers.length ? operatingDrivers.map((person, index) => <Fragment key={person.id}>{index > 0 && ", "}<Link href={`/drivers/detail?id=${person.id}`} onClick={event => event.stopPropagation()}>{person.name}</Link></Fragment>) : "—"}{operatingDrivers.length > 0 && homeBadge}</div> : employmentLabel}</td>
      {!investor && <td className="border-b border-zinc-800 px-3">{tariffLabel(driver)}</td>}
      <td className="border-b border-zinc-800 px-3 text-zinc-500">{driver.dispatcherName || "—"}</td>
      {investor && <td className="border-b border-zinc-800 px-3">{tariffLabel(driver)}</td>}
      <td className="border-b border-zinc-800 px-3 text-right font-mono">{loadCount}</td>
      <td title={incomplete ? "Provisional payable: highlighted loads need review" : "Total payable"} className={`border-b border-zinc-800 px-3 text-right font-mono font-medium ${incomplete ? "text-amber-200" : "text-zinc-100"}`}>{payable}</td>
      <td className="border-b border-zinc-800 px-3 text-zinc-400 break-words">{driver.settlement?.finalized ? driver.settlement.finalizedBy || "Unknown" : "—"}</td>
    </tr>
    {open && driver.summary && <tr><td colSpan={8} className="border-b border-zinc-800 px-4 py-5 text-xs text-zinc-500"><div id={`driver-${driver.id}`} role={detailError ? "alert" : "status"}>{detailError || "Loading statement…"}{detailError && <button type="button" className={`${payButtonClass} ml-3`} onClick={onRetryDetails}>Try again</button>}</div></td></tr>}
    {open && !driver.summary && <tr><td colSpan={8} className="border-b border-zinc-700 bg-zinc-950 p-0">
      <div id={`driver-${driver.id}`}>
        {driver.issues?.map((issue, index) => <p key={index} role="alert" className="px-3 py-2 text-xs text-amber-300">{issue}</p>)}
        {driver.teamNotice && <p role="status" className="px-3 py-2 text-xs text-amber-300">{driver.teamNotice}</p>}
        {placementError && <p role="alert" className="px-3 py-2 text-xs text-red-300">{placementError}</p>}
        <ChargeActions driver={driver} edits={edits} disabled={chargeActionsDisabled} onReload={onReload} />
 {totals.payable < BigInt(0) && <p role="status" className="px-3 py-2 text-xs text-amber-300">Negative payable — adjust deductions to what was collected. Unpaid balances carry forward.</p>}
        <div data-payroll-scroll={`${driver.id}:loads`} className="overflow-x-auto" role="region" aria-label={`${driver.fullName} weekly loads`} tabIndex={0}>
          <table onMouseDown={selectPayCell} onDoubleClick={editPayCell} onCopy={copyPayCell} onKeyDown={navigatePayGrid} className={`${styles.grid} w-full min-w-[1504px] table-fixed border-separate border-spacing-0 text-left text-xs`}>
            <colgroup>{displayColumns.map(([label, width]) => <col key={label} style={{ width }} />)}</colgroup>
            <thead className="bg-zinc-950 text-[10px] text-zinc-400"><tr>{displayColumns.map(([label], index) => <th key={label}
              title={label === "Driver gross" ? "Gross Board driver gross used for percentage pay" : undefined}
              className={`${cell} !bg-zinc-950 font-medium ${index === 0 ? "sticky left-0 z-10 bg-zinc-950" : ""} ${index >= 5 && index !== 11 ? "text-right" : ""}`}>
              {label === "Comments" ? <span className="sr-only">Comments</span> : label}
            </th>)}</tr></thead>
            <tbody>{[...weekdays, ...(extraLoads.length ? ["Extra"] : [])].map((day, dayIndex) => {
              const date = addDays(edits.weekStart, dayIndex);
              const loads = day === "Extra" ? extraLoads : driver.loads.filter(load => !load.payrollSupplement && load.date === date);
              const statuses = [...new Set((driver.dayStatuses ?? []).filter(item => item.date === date).map(item => item.status))];
              const statusColor = statuses.length === 1 ? dayStatuses.find(item => item.value === statuses[0])?.color : undefined;
              const dayLabel = <>{day}{day !== "Extra" && <span className="ml-2 text-[10px] text-zinc-600">{shortDate(date)}</span>}</>;
              return <Fragment key={date}>{loads.length ? loads.map((load, index) => <tr key={load.commentKey} onContextMenu={event => {
 const day=new Date(`${load.sourceDate || load.date}T12:00:00Z`).getUTCDay();
 if(onTransfer && !load.payrollSupplement && !transferDisabled && (day===0 || day===6) && load.loadRecordId!==null && !load.tripReview){event.preventDefault();setTransfer(load);}
 }} className={load.loadRecordId === null ? "bg-amber-500/10" : load.issues.length ? "bg-amber-500/[0.04]" : "hover:bg-zinc-800/15"}>
                {index === 0 && <th rowSpan={loads.length} scope="rowgroup" className={`${cell} sticky left-0 z-10 bg-zinc-950 font-medium text-zinc-400`}>{dayLabel}</th>}
                <td className={`${cell} text-zinc-200`}><div className="flex items-center gap-1.5"><>{load.payrollSupplement ? <span title={`Service date: ${load.sourceDate || load.date}`} className="truncate">{load.loadNumber}</span> : <Link data-payroll-load={returnToPay || undefined} href={`/gross-board?${new URLSearchParams({ driverId: load.sourceDriverId ?? driver.id, date: load.dispatchDate || load.date, slot: String(load.transferVersion ? load.dispatchSlot : load.slot), loadNumber: load.loadNumber, ...(returnToPay ? { from: "driver-pay" } : {}) })}`} prefetch={false} className="truncate text-blue-400 underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-blue-500" title={`Open ${load.loadNumber} in Gross Board · ${load.date} · load ${load.slot + 1}`}>{load.loadNumber}</Link>}</>{onPlacement && load.loadRecordId !== null && (load.notReady || load.payrollSupplement) && <button type="button" disabled={placementDisabled || placing} aria-label={`Remove ${load.loadNumber} from statement`} title="Remove from payroll only; keep Gross Board unchanged" onClick={() => void changeLoad("remove", load)} className="shrink-0 rounded text-zinc-500 hover:text-red-300 disabled:opacity-40"><X className="h-3.5 w-3.5" /></button>}{onTransfer && !load.payrollSupplement && [0,6].includes(new Date(`${load.sourceDate || load.date}T12:00:00Z`).getUTCDay()) && load.loadRecordId!==null && !load.tripReview && <button type="button" disabled={transferDisabled} aria-label={`Move ${load.loadNumber} to next week`} title="Move or change load week" onClick={() => setTransfer(load)} className="shrink-0 rounded text-zinc-500 hover:text-blue-300 disabled:opacity-40"><MoreHorizontal className="h-3.5 w-3.5" /></button>}{load.issues.length > 0 && <span title={load.issues.join("\n")} aria-label={load.issues.join(". ")} className="shrink-0 text-amber-300"><AlertTriangle className="h-3 w-3" /></span>}{!!load.transferVersion && <span className="shrink-0 text-blue-300" aria-label={load.date === load.sourceDate ? "Dispatcher: next week" : "Moved from previous week"} title={`Original: ${load.sourceDate}. Dispatcher gross: ${load.dispatchDate}. Credited to ${load.creditedDispatcherName || "Unassigned"}.`}><ArrowRight className="h-3 w-3" /></span>}</div></td>
                <td className={`${cell} text-zinc-400`}>{load.pickupDate || "—"}</td>
                <td className={`${cell} text-zinc-400`}><div className="truncate" title={load.pickupLocation}>{load.pickupLocation || "—"}</div></td>
                <td className={`${cell} text-zinc-400`}><div className="truncate" title={load.deliveryLocation}>{load.deliveryLocation || "—"}</div></td>
                <PaySourceCell load={load} field="originalRate" className={numeric} disabled={!!sourceActionDisabled} onAccept={onAcceptSource} />
                <td className={`${numeric} text-zinc-300`}>{load.payrollSupplement && onPlacement ? <input key={`${load.commentKey}:${load.payrollPlacementVersion}`} data-grid-ignore aria-label={`${load.loadNumber}, driver gross`} defaultValue={load.driverGross} inputMode="decimal" disabled={placementDisabled || placing} className="w-full bg-transparent text-right outline-none focus:ring-1 focus:ring-blue-500 disabled:opacity-60" placeholder="Enter gross" onKeyDown={event => { if (event.key === "Enter") { event.preventDefault(); event.currentTarget.blur(); } if (event.key === "Escape") { event.currentTarget.value = load.driverGross; event.currentTarget.blur(); } }} onBlur={event => { if (event.currentTarget.value !== load.driverGross) void changeLoad("gross", load, event.currentTarget.value); }} /> : amount(load.driverGross, true)}</td>
                <PaySourceCell load={load} field="totalMiles" className={numeric} disabled={!!sourceActionDisabled} onAccept={onAcceptSource} />
                <td className={`${numeric} text-zinc-400`}>{amount(load.loadedMiles)}</td>
                <td className={`${numeric} text-zinc-400`}>{amount(load.deadheadMiles)}</td>
                <td className={`${numeric} font-medium text-zinc-200`}>{amount(load.fee, true)}</td>
                {dayIndex === 0 && index === 0 && adjustmentCells}
                <td className={`${cell} !px-1 text-center`}><CommentButton label={`${driver.fullName}, ${load.loadNumber}, ${load.date}, comment`} value={edits.comments[load.commentKey] ?? ""} disabled={disabled}
                  onClick={() => setComment({ key: load.commentKey, label: `${load.loadNumber} · ${load.date}`, value: edits.comments[load.commentKey] ?? "" })} /></td>
              </tr>) : <tr><th scope="row" className={`${cell} sticky left-0 bg-zinc-950 font-medium text-zinc-400`}>{dayLabel}</th><td colSpan={10} className={`${cell} text-center ${statusColor ?? "text-zinc-600"}`}>
                {statuses.length === 1 ? statuses[0] : statuses.length ? <div className="flex items-center justify-center gap-2">{statuses.map(status => <span key={status} className={`rounded px-2 font-medium ${dayStatuses.find(item => item.value === status)?.color ?? ""}`}>{status}</span>)}</div> : "No load"}
              </td>{dayIndex === 0 && adjustmentCells}<td className={cell} /></tr>}</Fragment>;
            })}{onPlacement && <tr data-pay-extra-row data-grid-ignore><th scope="row" className={`${cell} sticky left-0 bg-zinc-950 font-medium text-zinc-400`}>Add load</th><td className={cell}><input aria-label={`${driver.fullName}, extra load number`} value={extraNumber} onChange={event => { setExtraNumber(event.target.value); setExtraChoices([]); setExtraChoice(""); }} disabled={placementDisabled || placing} maxLength={200} placeholder="Load ID" className="w-full bg-transparent text-zinc-200 outline-none focus:ring-1 focus:ring-blue-500 disabled:opacity-40" onKeyDown={event => { if (event.key === "Enter") { event.preventDefault(); event.stopPropagation(); if (extraNumber.trim()) void changeLoad("add"); } }} /></td><td colSpan={9} className={`${cell} text-zinc-500`}>{extraChoices.length > 0 && <select aria-label="Load trip allocation" value={extraChoice} disabled={placementDisabled || placing} onChange={event => setExtraChoice(event.target.value)} className="mr-3 max-w-80 bg-zinc-900 text-zinc-200"><option value="">Choose trip…</option>{extraChoices.map((choice,index) => <option key={index} value={index}>{choice.tripNumber || (choice.loadTripId ? `Trip ${choice.loadTripId}` : "Full load")} · {choice.driverName} · {choice.sourceDate}</option>)}</select>}<button type="button" disabled={placementDisabled || placing || !extraNumber.trim() || (extraChoices.length > 0 && extraChoice === "")} onClick={() => void changeLoad("add")} className="mr-3 rounded border border-zinc-700 px-2 text-zinc-300 hover:bg-zinc-800 disabled:opacity-40">{placing ? "Saving…" : "Add to statement"}</button>Delivered or invoiced · not in a finalized statement</td><td className={cell} /></tr>}</tbody>
            <tbody>
              <tr data-pay-totals className="font-mono text-zinc-200"><th colSpan={5} className={`${cell} text-left font-sans font-medium`}>Weekly totals{incomplete ? " · provisional" : ""}</th>
                {[totals.original, totals.gross, totals.totalMiles, totals.loadedMiles, totals.deadheadMiles, totals.fee].map((value, index) => <Fragment key={index}><td className={numeric}>{index === 5 && allFeesMissing ? "—" : decimalDisplay(value, index < 2 || index === 5)}</td></Fragment>)}<td className={`${cell} font-sans text-[10px] text-zinc-500`}>Net adjustments</td><td className={numeric}>{decimalDisplay(netAdjustments, true)}</td><td className={`${cell} !px-1 text-center`}><CommentButton label={`${driver.fullName}, weekly comment`} value={edits.notes} disabled={disabled} onClick={() => setComment({ key: null, label: "Weekly comment", value: edits.notes })} /></td>
              </tr>
            </tbody>

          </table>
        </div>
      </div>
    </td></tr>}

    {menu && onSettlement && <StatementMenu x={menu.x} y={menu.y} label={`${driver.settlement?.finalized ? "Reopen" : "Finalize"} ${investor ? "truck" : "driver"}`} disabled={settlementDisabled} onClose={closeMenu} onSelect={() => onSettlement(!!driver.settlement?.finalized)} />}
    {costDetails && <CostTransactionsDialog kind={investor ? "investor" : "driver"} id={driver.id} name={driver.fullName} weekStart={edits.weekStart} cost={costDetails} onClose={() => setCostDetails(null)} />}
    {transfer && onTransfer && <LoadTransferDialog load={transfer} finalized={!!driver.settlement?.finalized} onClose={() => setTransfer(null)} onTransfer={(mode,reason) => onTransfer(transfer,mode,reason)} />}
    {comment && <Modal title={comment.value.trim() ? "Edit comment" : "Add comment"} description={`${driver.fullName} · ${comment.label}`} isSaving={false} submitLabel="Save comment" onClose={() => setComment(null)} onSubmit={event => {
      event.preventDefault();
      edit(current => comment.key === null ? { ...current, notes: comment.value } : { ...current, comments: { ...current.comments, [comment.key]: comment.value } });
      setComment(null);
    }}><label className="block text-xs text-zinc-400">Comment<textarea autoFocus aria-label="Comment" rows={5} maxLength={comment.key === null ? 5000 : 2000} value={comment.value} onChange={event => setComment({ ...comment, value: event.target.value })} className={`${controlClass} mt-2 resize-y`} placeholder="Add a comment…" /></label></Modal>}
  </>;
});
