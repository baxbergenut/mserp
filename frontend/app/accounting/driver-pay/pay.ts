import type { DriverPayAdjustment, DriverPayDriver, DriverPayEdits, DriverPayWeek } from "@/app/lib/types";
import { decimalDisplay, hundredths, validDecimal } from "@/app/gross-board/board";

export function carryBreakdown(current: bigint, carried: bigint): string | undefined {
  if (carried <= BigInt(0)) return undefined;
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
  const generated = [...(edits.generatedCharges ?? []), ...(driver.autoCharges ?? [])].reduce((sum, row) => sum + hundredths(row.amount), BigInt(0));
  const expenses = (edits.expenseDeductions ?? []).reduce((sum, row) => sum + hundredths(row.amount), BigInt(0));
  return { ...values, review: values.review + (driver.issues?.length ?? 0), ...adjustments, costs, generated, expenses, payable: values.fee + adjustments.addition + adjustments.reimbursement - adjustments.deduction + costs + generated - expenses };
}

// Keep the all-filtered-rows summary current when one visible statement saves.
// Other pages' amounts remain included, and in-flight drafts stay in changes.
export function applyPaySave(report: DriverPayWeek, saved: DriverPayEdits): DriverPayWeek {
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
  else if (result[saved.driverId]) result[saved.driverId] = { ...result[saved.driverId], version: saved.version,
    ...(saved.costs && { costs: saved.costs }),
    expenseDeductions: result[saved.driverId].expenseDeductions?.map(row => {
      const committed = saved.expenseDeductions?.find(r => r.expenseId === row.expenseId);
      const submitted = snapshot.expenseDeductions?.find(r => r.expenseId === row.expenseId);
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
  return result;
}

export function validAdjustments(edits: DriverPayEdits) {
  return (edits.expenseDeductions ?? []).every(row => row.amount.trim() && validDecimal(row.amount) && hundredths(row.amount) >= BigInt(0) && hundredths(row.amount) <= hundredths(row.available)) && (edits.generatedCharges ?? []).every(row => row.name.trim() && row.amount.trim() && validDecimal(row.amount) && (row.kind !== "installment" || hundredths(row.amount) <= BigInt(0))) && costRows.every(({ key }) => {
    const value = edits[`${key}Override`];
    return value == null || (value.trim() !== "" && validDecimal(value));
  }) && normalizedPayEdits(edits).adjustments.every(item => item.name.trim() && validDecimal(item.amount, true) && hundredths(item.amount) > BigInt(0));
}

export function normalizedPayEdits(edits: DriverPayEdits): DriverPayEdits {
  return { ...edits,
    // Include displayed expense deductions in a normal payroll save. Reading a
    // week remains read-only; saved rows only change when their amount is edited.
    ...(edits.expenseDeductions && { expenseDeductions: edits.expenseDeductions.map(row => row.saved ? row : { ...row, apply: true }) }),
    adjustments: edits.adjustments.filter(item => item.name.trim() !== "" || item.amount.trim() !== "" || item.kind === "deduction"),
  };
}
