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
  return { ...values, ...adjustments, payable: values.fee + adjustments.addition + adjustments.reimbursement - adjustments.deduction };
}

// Preserve typing during saves while adopting the committed concurrency version.
export function reconcilePaySave(current: Record<string, DriverPayEdits>, snapshot: DriverPayEdits, saved: DriverPayEdits) {
  const result = { ...current };
  if (JSON.stringify(result[saved.driverId]) === JSON.stringify(snapshot)) delete result[saved.driverId];
  else if (result[saved.driverId]) result[saved.driverId] = { ...result[saved.driverId], version: saved.version };
  return result;
}

export function validAdjustments(edits: DriverPayEdits) {
  return normalizedPayEdits(edits).adjustments.every(item => item.name.trim() && validDecimal(item.amount, true) && hundredths(item.amount) > BigInt(0));
}

export function normalizedPayEdits(edits: DriverPayEdits): DriverPayEdits {
  return { ...edits, adjustments: edits.adjustments.filter(item => item.name.trim() !== "" || item.amount.trim() !== "" || item.kind === "deduction") };
}
