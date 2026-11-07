import type { DriverPayAdjustment, DriverPayDriver, DriverPayEdits, DriverPayWeek } from "@/app/lib/types";
import { decimalDisplay, hundredths, validDecimal } from "@/app/gross-board/board";

export function payStatementReady(report: DriverPayWeek | null | undefined, row: DriverPayDriver | undefined) {
  if (!row) return false;
  return row.preparationState ? row.preparationState === "ready" : !report?.preparation || report.preparation.state === "ready";
}

export function centsAmount(cents: bigint) {
  const magnitude = cents < BigInt(0) ? -cents : cents;
  return `${cents < BigInt(0) ? "-" : ""}${magnitude / BigInt(100)}.${String(magnitude % BigInt(100)).padStart(2, "0")}`;
}

// A sourced row keeps its accounting direction, even when typing replaces its sign.
export function sourceAmountInput(value: string, source: string) {
  if (!value) return value;
  const magnitude = value.replace(/^[+-]/, "");
  return `${hundredths(source) <= BigInt(0) ? "-" : ""}${magnitude}`;
}

export function validSourceAmount(value: string, source: string) {
  if (!value.trim() || !validDecimal(value)) return false;
  const amount = hundredths(value), limit = hundredths(source);
  return limit < BigInt(0) ? amount >= limit && amount <= BigInt(0) : amount >= BigInt(0) && amount <= limit;
}

export function validExpenseAmount(amount: string, available: string) {
  return !!amount.trim() && validDecimal(amount) && hundredths(amount) >= BigInt(0) && hundredths(amount) <= hundredths(available);
}

export function isScaleTicket(row: { source: string }) {
  return row.source.startsWith("weighmytruck:");
}

export function paySourceError(edits: DriverPayEdits, driver?: DriverPayDriver): string | undefined {
  for (const row of edits.expenseDeductions ?? []) {
    const available = hundredths(row.available) - hundredths(row.escrowAmount ?? "0");
    if (!validExpenseAmount(row.amount, centsAmount(available))) return `${row.name}: enter an amount from zero to ${decimalDisplay(available, true)} with up to two decimal places.`;
  }
  for (const row of [...(driver?.autoCharges ?? []).filter(isScaleTicket), ...(driver?.deferredScaleCharges ?? [])]) {
    const amount = edits.scaleDeductions?.[row.source];
    const due = row.scaleDue ?? row.amount;
    if (amount !== undefined && !validSourceAmount(amount, due)) return `${row.name}: enter an amount within the unpaid balance of ${decimalDisplay(hundredths(due), true)}.`;
  }
  for (const row of edits.sourceCharges ?? []) {
    if (row.source.startsWith("investor_cost:") && driver?.investorId) continue;
    if (!validSourceAmount(row.amount, row.due)) return `${row.name}: enter an amount from zero to ${decimalDisplay(hundredths(row.due), true)}, including carry. Its charge or credit direction is fixed.`;
  }
  for (const row of edits.generatedCharges ?? []) {
    if (!validSourceAmount(row.amount, row.scheduledAmount)) return `${row.name}: enter an amount from zero to ${decimalDisplay(hundredths(row.scheduledAmount), true)}. Its charge or credit direction is fixed.`;
  }
  for (const { key, label } of costRows) {
    const value = edits[`${key}Override`];
    const due = edits.costs?.[`${key}Due`] ?? driver?.[`${key}Total`];
    if (value != null && due != null && !validSourceAmount(value, centsAmount(-hundredths(due)))) return `${label}: keep the source direction and enter no more than ${decimalDisplay(hundredths(due), true)} due, including carry.`;
  }
  return undefined;
}

export function carryBreakdown(current: bigint, carried: bigint): string | undefined {
  if (carried === BigInt(0)) return undefined;
  const money = (value: bigint) => decimalDisplay(value, true).replace(/\.00$/, "");
  return current === BigInt(0) ? `${money(carried)} carried` : `${money(current)} this week + ${money(carried)} carried`;
}

export function adjustmentTotals(items: DriverPayAdjustment[]) {
  return items.reduce((sum, item) => ({ ...sum, [item.kind]: sum[item.kind] + hundredths(item.amount) }), {
    reimbursement: BigInt(0), addition: BigInt(0), deduction: BigInt(0),
  });
}

export function driverTotals(driver: DriverPayDriver, edits: DriverPayEdits) {
  const values = driver.loads.reduce((sum, load) => ({
    original: sum.original + hundredths(load.originalRate), gross: sum.gross + hundredths(load.driverGross),
    totalMiles: sum.totalMiles + hundredths(load.totalMiles), loadedMiles: sum.loadedMiles + hundredths(load.loadedMiles),
    deadheadMiles: sum.deadheadMiles + hundredths(load.deadheadMiles), fee: sum.fee + hundredths(load.fee),
    missingFees: sum.missingFees + Number(load.fee === ""), review: sum.review + Number(load.issues.length > 0),
  }), { original: BigInt(0), gross: BigInt(0), totalMiles: BigInt(0), loadedMiles: BigInt(0), deadheadMiles: BigInt(0), fee: BigInt(0), missingFees: 0, review: 0 });
  const adjustments = adjustmentTotals(edits.adjustments);
  const costs = hundredths(costAmount(driver, edits, "fuel")) + hundredths(costAmount(driver, edits, "toll"));
  const autoCharges = [...(driver.autoCharges ?? []), ...(driver.deferredScaleCharges ?? [])].map(row => ({
    ...row,
    amount: edits.scaleDeductions?.[row.source] ?? edits.sourceCharges?.find(source => source.source === row.source)?.amount ?? (driver.settlement?.finalized ? row.amount : row.scaleDue ?? row.amount),
  }));
  const generated = [...(edits.generatedCharges ?? []), ...autoCharges, ...activeLayovers(driver, edits)].reduce((sum, row) => sum + hundredths(row.amount), BigInt(0));
  const expenses = (edits.expenseDeductions ?? []).reduce((sum, row) => sum + ((row.source === "reimburse" || row.source === "accessorial") ? -hundredths(row.amount) : hundredths(row.amount)), BigInt(0));
  const salaryCarry = hundredths(driver.salaryCarry ?? "0");
  const totals = { ...values, review: values.review + (driver.issues?.length ?? 0), ...adjustments, costs, generated, expenses, salaryCarry, payable: values.fee + adjustments.addition + adjustments.reimbursement - adjustments.deduction + costs + generated - expenses - salaryCarry };
  if (driver.summary) return { ...totals, fee: hundredths(driver.summary.fee), payable: hundredths(driver.summary.payable), missingFees: driver.summary.missingFees, review: driver.summary.review };
  return totals;
}

export function applyPayDetails(report: DriverPayWeek, expected: DriverPayDriver, detail: DriverPayDriver): DriverPayWeek {
  if (!report.drivers.includes(expected)) return report;
  const drivers = report.drivers.map(row => row === expected ? detail : row);
  if (!report.pagination) return { ...report, drivers };
  const before = driverTotals(expected, expected.edits), after = driverTotals(detail, detail.edits);
  return { ...report, drivers, pagination: { ...report.pagination,
    fee: centsAmount(hundredths(report.pagination.fee) + after.fee - before.fee),
    payable: centsAmount(hundredths(report.pagination.payable) + after.payable - before.payable),
    review: report.pagination.review + after.review - before.review,
    loads: report.pagination.loads + detail.loads.length - (expected.summary?.loads ?? expected.loads.length),
  } };
}

// Receipts acknowledge committed rows. The separate summary refresh replaces
// aggregate/order metadata; a receipt never gives old rows a newer input token.
export function applyPayReceipt(report: DriverPayWeek, receipt: DriverPayWeek): DriverPayWeek {
  if (report.weekStart !== receipt.weekStart) return report;
  let next = report;
  let finalizedDelta = 0;
  for (const row of receipt.drivers) {
    const previous = next.drivers.find(d => d.id === row.id);
    if (!previous) continue;
    if (!previous.truckInactive) finalizedDelta += Number(!!row.settlement?.finalized) - Number(!!previous.settlement?.finalized);
    next = applyPayDetails(next, previous, { ...row, preparationState: "ready", edits: { ...row.edits, inputRevision: receipt.revision } });
  }
  return next.pagination ? { ...next, pagination: { ...next.pagination, finalized: next.pagination.finalized + finalizedDelta } } : next;
}

export function activeLayovers(driver: DriverPayDriver, edits: DriverPayEdits) {
  return (driver.layovers ?? []).filter(row => !edits.layoverExcludedDates?.includes(row.date));
}

// Keep the all-filtered-rows summary current when one visible statement saves.
// Other pages' amounts remain included, and in-flight drafts stay in changes.
export function applyPaySave(report: DriverPayWeek, saved: DriverPayEdits): DriverPayWeek {
	if (saved.committedStatement) {
		const previous = report.drivers.find(row => row.id === saved.driverId);
		if (!previous) return report;
		const edits = { ...saved, expectedStatementRevision: saved.statementRevision };
		delete edits.committedStatement;
		delete edits.affectedStatements;
		delete edits.requestId;
		return applyPayDetails(report, previous, { ...saved.committedStatement, preparationState: "ready", statementRevision: saved.statementRevision, edits });
	}
  let delta = BigInt(0);
  const drivers = report.drivers.map(driver => {
    if (driver.id !== saved.driverId) return driver;
    delta += driverTotals(driver, saved).payable - driverTotals(driver, driver.edits).payable;
    return { ...driver, edits: saved };
  });
  if (!report.pagination) return { ...report, drivers };
  const value = hundredths(report.pagination.payable) + delta;
  const magnitude = value < BigInt(0) ? -value : value;
  const payable = `${value < BigInt(0) ? "-" : ""}${magnitude / BigInt(100)}.${String(magnitude % BigInt(100)).padStart(2, "0")}`;
  return { ...report, drivers, pagination: { ...report.pagination, payable } };
}

export const costRows = [{ key: "fuel", label: "Fuel" }, { key: "toll", label: "Toll" }] as const;
export type CostKey = typeof costRows[number]["key"];

export function costAmount(driver: DriverPayDriver, edits: DriverPayEdits, key: CostKey): string {
  if (driver.payType === "cpm" && hundredths(edits.costs?.[`${key}Carry`] ?? "0") === BigInt(0)) return "0.00";
  const override = edits[`${key}Override`];
  if (override != null) return override;
  if (!edits.costs && (!driver.isOwnerOperator || driver.payType !== "gross_percentage")) return "0.00";
  const cents = -hundredths(edits.costs?.[`${key}Due`] ?? driver[`${key}Total`] ?? "0");
  const magnitude = cents < BigInt(0) ? -cents : cents;
  return `${cents < BigInt(0) ? "-" : ""}${magnitude / BigInt(100)}.${String(magnitude % BigInt(100)).padStart(2, "0")}`;
}

// Preserve typing during saves while adopting the committed concurrency version.
export function reconcilePaySave(current: Record<string, DriverPayEdits>, snapshot: DriverPayEdits, saved: DriverPayEdits) {
  const result = { ...current };
  if (JSON.stringify(result[saved.driverId]) === JSON.stringify(snapshot)) delete result[saved.driverId];
  else if (result[saved.driverId]) result[saved.driverId] = { ...result[saved.driverId], version: saved.version, ...(saved.inputRevision && { inputRevision: saved.inputRevision }), ...(saved.statementRevision && { expectedStatementRevision: saved.statementRevision }),
    ...(saved.costs && { costs: saved.costs }),
    sourceCharges: result[saved.driverId].sourceCharges?.map(row => {
      const committed = saved.sourceCharges?.find(r => r.source === row.source && r.ownerId === row.ownerId);
      const submitted = snapshot.sourceCharges?.find(r => r.source === row.source && r.ownerId === row.ownerId);
      if (!committed) return row;
      return JSON.stringify(row) === JSON.stringify(submitted) ? committed : { ...committed, amount: row.amount };
    }),
    expenseDeductions: result[saved.driverId].expenseDeductions?.map(row => {
      const committed = saved.expenseDeductions?.find(r => r.expenseId === row.expenseId && r.source === row.source);
      const submitted = snapshot.expenseDeductions?.find(r => r.expenseId === row.expenseId && r.source === row.source);
      if (!committed) return row;
      return JSON.stringify(row) === JSON.stringify(submitted) ? committed : { ...row, version: committed.version, available: committed.available, openingBalance: committed.openingBalance };
    }),
    generatedCharges: result[saved.driverId].generatedCharges?.map(row => {
      const committed = saved.generatedCharges?.find(r => r.scheduleId === row.scheduleId);
      const submitted = snapshot.generatedCharges?.find(r => r.scheduleId === row.scheduleId);
      if (!committed) return row;
      return JSON.stringify(row) === JSON.stringify(submitted) ? committed : { ...row, version: committed.version, scheduleVersion: committed.scheduleVersion, typeVersion: committed.typeVersion, scheduledAmount: committed.scheduledAmount };
    }),
  };
  if (!current[saved.driverId]?.generatedCharges && result[saved.driverId]) delete result[saved.driverId].generatedCharges;
  if (!current[saved.driverId]?.expenseDeductions && result[saved.driverId]) delete result[saved.driverId].expenseDeductions;
  if (!current[saved.driverId]?.sourceCharges && result[saved.driverId]) delete result[saved.driverId].sourceCharges;
  if (result[saved.driverId] && JSON.stringify(result[saved.driverId].scaleDeductions) === JSON.stringify(snapshot.scaleDeductions)) {
    if (saved.scaleDeductions) result[saved.driverId].scaleDeductions = saved.scaleDeductions;
    else delete result[saved.driverId].scaleDeductions;
  }
  return result;
}

export function validAdjustments(edits: DriverPayEdits, driver?: DriverPayDriver) {
  return !paySourceError(edits, driver) && (edits.generatedCharges ?? []).every(row => row.name.trim() && row.amount.trim() && validDecimal(row.amount) && (row.kind !== "installment" || hundredths(row.amount) <= BigInt(0))) && costRows.every(({ key }) => {
    const value = edits[`${key}Override`];
    return value == null || (value.trim() !== "" && validDecimal(value));
  }) && normalizedPayEdits(edits).adjustments.every(item => item.name.trim() && validDecimal(item.amount, true) && hundredths(item.amount) > BigInt(0));
}

export function normalizedPayEdits(edits: DriverPayEdits): DriverPayEdits {
  const payload = { ...edits };
  delete payload.inputRevision;
  delete payload.statementRevision;
  delete payload.committedStatement;
  delete payload.affectedStatements;
  return { ...payload,
    // Ordinary expenses persist only explicit draft choices. Credits and escrow
    // retain their existing save behavior.
    ...(edits.expenseDeductions && { expenseDeductions: edits.expenseDeductions.map(row => !row.source || row.source === "accessorial" || row.saved ? row : { ...row, apply: true }) }),
    adjustments: edits.adjustments.filter(item => item.name.trim() !== "" || item.amount.trim() !== "" || item.kind === "deduction"),
  };
}
