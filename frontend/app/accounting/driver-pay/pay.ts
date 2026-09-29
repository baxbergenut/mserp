import type { DriverPayAdjustment, DriverPayDriver, DriverPayEdits } from "@/app/lib/types";
import { hundredths, validDecimal } from "@/app/gross-board/board";

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
  const generated = (edits.generatedCharges ?? []).reduce((sum, row) => sum + hundredths(row.amount), BigInt(0));
  const expenses = (edits.expenseDeductions ?? []).reduce((sum, row) => sum + hundredths(row.amount), BigInt(0));
  return { ...values, ...adjustments, costs, generated, expenses, payable: values.fee + adjustments.addition + adjustments.reimbursement - adjustments.deduction + costs + generated - expenses };
}

export const costRows = [{ key: "fuel", label: "Fuel" }, { key: "toll", label: "Toll" }] as const;
export type CostKey = typeof costRows[number]["key"];

export function costAmount(driver: DriverPayDriver, edits: DriverPayEdits, key: CostKey): string {
  if (driver.payType === "cpm") return "0.00";
  const override = edits[`${key}Override`];
  if (override != null) return override;
  if (!driver.isOwnerOperator || driver.payType !== "gross_percentage") return "0.00";
  const cents = -hundredths(driver[`${key}Total`] ?? "0");
  const magnitude = cents < BigInt(0) ? -cents : cents;
  return `${cents < BigInt(0) ? "-" : ""}${magnitude / BigInt(100)}.${String(magnitude % BigInt(100)).padStart(2, "0")}`;
}

// Preserve typing during saves while adopting the committed concurrency version.
export function reconcilePaySave(current: Record<string, DriverPayEdits>, snapshot: DriverPayEdits, saved: DriverPayEdits) {
  const result = { ...current };
  if (JSON.stringify(result[saved.driverId]) === JSON.stringify(snapshot)) delete result[saved.driverId];
  else if (result[saved.driverId]) result[saved.driverId] = { ...result[saved.driverId], version: saved.version,
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
  return { ...edits, adjustments: edits.adjustments.filter(item => item.name.trim() !== "" || item.amount.trim() !== "" || item.kind === "deduction") };
}
