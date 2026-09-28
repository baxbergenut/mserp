import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import ts from "typescript";

const compile = source => `data:text/javascript;base64,${Buffer.from(ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2020 } }).outputText).toString("base64")}`;
const board = compile(readFileSync(new URL("../app/gross-board/board.ts", import.meta.url), "utf8"));
const pay = readFileSync(new URL("../app/accounting/driver-pay/pay.ts", import.meta.url), "utf8").replace('"@/app/gross-board/board"', JSON.stringify(board));
const { driverTotals, normalizedPayEdits, reconcilePaySave, validAdjustments } = await import(compile(pay));
const edits = { driverId: "driver", weekStart: "2026-09-28", version: 0, notes: "", comments: {}, adjustments: [
  { id: "a", kind: "reimbursement", amount: "0.10" }, { id: "b", kind: "addition", amount: "0.20" }, { id: "c", kind: "deduction", amount: "10.25" },
] };
const load = { originalRate: "1000.00", driverGross: "900.00", totalMiles: "847.34", loadedMiles: "725.78", deadheadMiles: "121.56", fee: "635.51", issues: [] };
const missing = { originalRate: "", driverGross: "800.00", totalMiles: "", loadedMiles: "", deadheadMiles: "", fee: "", issues: ["unmatched"] };
const totals = driverTotals({ loads: [load, missing] }, edits);
assert.equal(totals.payable, BigInt(62556));
assert.equal(totals.missingFees, 1);
assert.equal(totals.review, 1);
assert.equal(totals.totalMiles, BigInt(84734));
const saved = { ...edits, version: 1 };
assert.deepEqual(reconcilePaySave({ driver: edits }, edits, saved), {});
const later = { ...edits, notes: "Typed during save" };
assert.deepEqual(reconcilePaySave({ driver: later }, edits, saved).driver, { ...later, version: 1 });
console.log("Driver pay checks passed: exact adjustment totals, missing fees, and autosave reconciliation.");
const inlineEdits = { ...edits, adjustments: [{ id: "a", kind: "deduction", name: "Parking", amount: "25.50" }] };
assert.equal(validAdjustments(inlineEdits), true);
assert.equal(driverTotals({ loads: [load] }, inlineEdits).payable, BigInt(61001));
for (const adjustment of [{ name: "", amount: "25.50" }, { name: "Parking", amount: "" }, { name: "Parking", amount: "1.234" }, { name: "Parking", amount: "0" }]) {
  assert.equal(validAdjustments({ ...inlineEdits, adjustments: [{ ...inlineEdits.adjustments[0], ...adjustment }] }), false);
}
const cleared = { ...inlineEdits, adjustments: [{ id: "a", name: "", amount: "", kind: "reimbursement", note: "Old note" }] };
assert.equal(validAdjustments(cleared), true);
assert.deepEqual(normalizedPayEdits(cleared).adjustments, []);
assert.equal(validAdjustments({ ...cleared, adjustments: [{ ...cleared.adjustments[0], kind: "deduction" }] }), false);
