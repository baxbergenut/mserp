import type { DriverPayDriver, DriverPayEdits } from "@/app/lib/types";
import { activeLayovers, centsAmount, costAmount, driverTotals } from "./pay";
import { hundredths } from "@/app/gross-board/board";

// Text cells cannot execute spreadsheet formulas. Money cells are generated
// from validated integer cents, and every legacy adjustment retains its reference.
export function statementCSV(driver: DriverPayDriver, edits: DriverPayEdits) {
  const text = (value: string) => `"${(/^[=+\-@\t\r]/.test(value) ? "'" + value : value).replaceAll('"', '""')}"`;
  const rows: string[][] = [["Statement", driver.fullName, edits.weekStart, driver.settlement?.finalized ? "Finalized" : "Draft"], ["Type", "Description", "Amount", "Reference"]];
  const add = (type: string, name: string, amount: bigint, ref = "") => rows.push([type, name, centsAmount(amount), ref]);
  for (const l of driver.loads) add("Earnings", l.loadNumber, hundredths(l.fee), l.date);
  for (const key of ["fuel", "toll"] as const) add("Operating cost", key, hundredths(costAmount(driver, edits, key)));
  for (const row of edits.expenseDeductions ?? []) add(row.source === "reimburse" ? "Reimbursement" : "Collection", row.name, hundredths(row.amount) * (row.source === "reimburse" ? BigInt(1) : BigInt(-1)), row.expenseId);
  for (const row of edits.generatedCharges ?? []) add("Scheduled", row.name, hundredths(row.amount));
  for (const row of driver.autoCharges ?? []) add("Automatic", row.name, hundredths(edits.sourceCharges?.find(s => s.source === row.source)?.amount ?? row.amount), row.source);
  for (const row of activeLayovers(driver, edits)) add("Layover", row.date, hundredths(row.amount));
  for (const row of edits.adjustments) add(row.legacySheets ? "Sheets balance adjustment" : "Adjustment", row.name, hundredths(row.amount) * (row.kind === "deduction" ? BigInt(-1) : BigInt(1)), row.note);
  add("Total", "Final payable", driverTotals(driver, edits).payable);
  rows.push(["Notes", edits.notes, "", ""]);
  return "\uFEFF" + rows.map((row, index) => row.map((cell, col) => index > 1 && col === 2 && /^-?\d+\.\d{2}$/.test(cell) ? cell : text(cell)).join(",")).join("\r\n");
}

export function downloadStatement(driver: DriverPayDriver, edits: DriverPayEdits) {
  const url = URL.createObjectURL(new Blob([statementCSV(driver, edits)], { type: "text/csv;charset=utf-8" }));
  const link = document.createElement("a"); link.href = url;
  link.download = `statement-${edits.weekStart}-${driver.id}.csv`; link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
