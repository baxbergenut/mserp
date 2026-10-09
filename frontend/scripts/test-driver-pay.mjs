import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import ts from "typescript";

const compile = source => `data:text/javascript;base64,${Buffer.from(ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2020 } }).outputText).toString("base64")}`;
const board = compile(readFileSync(new URL("../app/gross-board/board.ts", import.meta.url), "utf8"));
const pay = readFileSync(new URL("../app/accounting/driver-pay/pay.ts", import.meta.url), "utf8").replace('"@/app/gross-board/board"', JSON.stringify(board));
const { applyPaySave, costAmount, driverTotals, normalizedPayEdits, reconcilePaySave, validAdjustments, sourceAmountInput, validSourceAmount } = await import(compile(pay));
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
const pageReport = { drivers: [{ id: 'driver', loads: [load], edits }], pagination: { payable: '5000.00' } };
const changed = { ...edits, adjustments: [...edits.adjustments, { id: 'new', kind: 'deduction', name: 'Fee', amount: '50.01' }] };
const savedPage = applyPaySave(pageReport, changed);
assert.equal(savedPage.pagination.payable, '4949.99');
assert.equal(applyPaySave(savedPage, changed).pagination.payable, '4949.99');
assert.equal(pageReport.pagination.payable, '5000.00');
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

const owner = { loads: [load], isOwnerOperator: true, payType: "gross_percentage", fuelTotal: "100.10", tollTotal: "12.25" };
const auto = { ...edits, adjustments: [], fuelOverride: null, tollOverride: null };
assert.equal(costAmount(owner, auto, "fuel"), "-100.10");
assert.equal(driverTotals(owner, auto).payable, 52316n);
assert.equal(costAmount({ ...owner, tollTotal: "-3.25" }, auto, "toll"), "3.25");
assert.equal(costAmount({ ...owner, tollTotal: "0.00" }, auto, "toll"), "0.00");
assert.equal(driverTotals({ ...owner, isOwnerOperator: false }, auto).payable, 63551n);
assert.equal(driverTotals({ ...owner, payType: "cpm" }, { ...auto, fuelOverride: "-99" }).payable, 63551n);
const manual = { ...auto, fuelOverride: "-80.15", tollOverride: "0.00" };
assert.equal(validAdjustments(manual), true);
assert.equal(driverTotals({ ...owner, fuelTotal: "200" }, manual).payable, 55536n);
assert.equal(driverTotals(owner, { ...manual, fuelOverride: null }).payable, 53541n);
for (const value of ["", "-", "1.234", "NaN", "10000000000", "1/2"]) {
  assert.equal(validAdjustments({ ...auto, fuelOverride: value }), false);
}
for (const value of ["0", "-0.00", "-25.50", "10.25"]) {
  assert.equal(validAdjustments({ ...auto, fuelOverride: value }), true);
}
const costSaved = { ...manual, version: 4 };
const laterCost = { ...manual, tollOverride: "-10" };
assert.deepEqual(reconcilePaySave({ driver: laterCost }, manual, costSaved).driver, { ...laterCost, version: 4 });
console.log("Driver pay cost checks passed: eligibility, signed credits, zero weeks, manual overrides, reset, validation and save reconciliation.");

const generatedRow = { scheduleId: "plan", weekStart: edits.weekStart, kind: "installment", name: "Advance", amount: "-100.00", scheduledAmount: "-100.00", overridden: false, confirmedAt: null, confirmedBy: null, version: 0, scheduleVersion: 1 };
const generatedEdits = { ...auto, generatedCharges: [generatedRow] };
assert.equal(driverTotals({ ...owner, loads: [], payType: "cpm" }, generatedEdits).payable, -10000n);
assert.equal(validAdjustments(generatedEdits), true);
assert.equal(validAdjustments({ ...generatedEdits, generatedCharges: [{ ...generatedRow, amount: "0" }] }), true);
assert.equal(validAdjustments({ ...generatedEdits, generatedCharges: [{ ...generatedRow, amount: "50" }] }), false);
assert.equal(validAdjustments({ ...generatedEdits, generatedCharges: [{ ...generatedRow, name: "" }] }), false);
const chargeSave = { ...generatedEdits, version: 3, generatedCharges: [{ ...generatedRow, amount: "-60.00", version: 1, scheduleVersion: 2, typeVersion: 3 }] };
const submittedCharge = { ...generatedEdits, generatedCharges: [{ ...generatedRow, amount: "-60" }] };
const laterCharge = { ...submittedCharge, generatedCharges: [{ ...generatedRow, amount: "-70" }] };
const rebased = reconcilePaySave({ driver: laterCharge }, submittedCharge, chargeSave).driver;
assert.equal(rebased.generatedCharges[0].amount, "-70");
assert.equal(rebased.generatedCharges[0].scheduleVersion, 2);
assert.equal(rebased.generatedCharges[0].version, 1);
assert.equal(rebased.generatedCharges[0].typeVersion, 3);
assert.equal(rebased.version, 3);
console.log("Generated charge checks passed: charge-only totals, skips, validation, and typing during save.");

const expenseRow = { expenseId: "expense", name: "Penalty", total: "100.25", available: "100.25", amount: "30.10", version: 1, saved: false, apply: true };
const expenseEdits = { ...auto, expenseDeductions: [expenseRow] };
const suggestedExpense = { ...expenseRow, saved: false, apply: false };
const savedExpense = { ...expenseRow, saved: true, apply: false };
const normalSave = normalizedPayEdits({ ...auto, notes: "Regular payroll edit", expenseDeductions: [suggestedExpense, savedExpense] });
assert.equal(normalSave.expenseDeductions[0].apply, true);
assert.equal(normalSave.expenseDeductions[1].apply, false);
assert.equal(suggestedExpense.apply, false, "normalizing a save must not mutate the fetched report");
assert.equal(driverTotals({ loads: [] }, expenseEdits).payable, -3010n);
assert.equal(validAdjustments(expenseEdits), true);
for (const amount of ["", "-1", "100.26", "1.001"]) assert.equal(validAdjustments({ ...expenseEdits, expenseDeductions: [{ ...expenseRow, amount }] }), false);
assert.equal(validAdjustments({ ...expenseEdits, expenseDeductions: [{ ...expenseRow, amount: "0" }] }), true);
const committedExpense = { ...expenseEdits, version: 2, expenseDeductions: [{ ...expenseRow, version: 2, saved: true, apply: false }] };
const typedExpense = { ...expenseEdits, expenseDeductions: [{ ...expenseRow, amount: "20.25" }] };
const reconciledExpense = reconcilePaySave({ driver: typedExpense }, expenseEdits, committedExpense).driver;
assert.equal(reconciledExpense.expenseDeductions[0].amount, "20.25");
assert.equal(reconciledExpense.expenseDeductions[0].version, 2);
assert.equal(reconciledExpense.expenseDeductions[0].apply, true);
console.log("Expense deduction checks passed: exact totals, bounds, zero deferrals and edits during save.");

const investorTruck = { ...owner, investorId: "investor", fuelTotal: "1500.00", tollTotal: "200.00", loads: [{ ...load, fee: "8800.00", driverFee: "2500.00" }], autoCharges: [{ name: "Driver earnings", amount: "-2500.00", source: "driver:hired" }, { name: "Admin", amount: "-100.00", source: "truck_charge:truck:admin" }] };
const investorEdits = { ...auto, expenseDeductions: [{ ...expenseRow, amount: "300.00", available: "300.00" }] };
assert.equal(driverTotals(investorTruck, investorEdits).payable, 420000n);
assert.equal(driverTotals({ ...investorTruck, issues: ["Ownership requires review"] }, investorEdits).review, 1);
assert.equal(driverTotals({ ...owner, autoCharges: [{ name: "Admin", amount: "-100.00", source: "truck_charge:truck:admin" }] }, auto).payable, 42316n);
console.log("Investor pay checks passed: earnings charged once, truck costs and expenses, owner-only fees, review indicators.");

const carryEdits = { ...auto, costs: { revision: 'v1', fuelCarry: '40.30', tollCarry: '7.25', fuelDue: '140.40', tollDue: '19.50' } };
assert.equal(costAmount(owner, carryEdits, 'fuel'), '-140.40');
assert.equal(costAmount(owner, { ...carryEdits, fuelOverride: '0.00' }, 'fuel'), '0.00');
assert.equal(costAmount({ ...owner, payType: 'cpm' }, { ...carryEdits, costs: { ...carryEdits.costs, fuelDue: '40.30' } }, 'fuel'), '-40.30');
const carrySaved = { ...carryEdits, version: 9, costs: { ...carryEdits.costs, revision: 'v2' } };
assert.equal(reconcilePaySave({ driver: { ...carryEdits, notes: 'typing' } }, carryEdits, carrySaved).driver.costs.revision, 'v2');
console.log('Remainder checks passed: automatic carry, zero deferral, tariff changes and cross-week revision reconciliation.');

assert.equal(driverTotals({ ...owner, autoCharges: [{ name: "Escrow release", amount: "100.25", source: "escrow-release:release" }] }, auto).payable, driverTotals(owner, auto).payable + 10025n);
console.log("Escrow releases add the exact fixed credit to payroll totals.");

const { restorePayEdit } = await import(compile(readFileSync(new URL('../app/accounting/driver-pay/payUndo.ts', import.meta.url), 'utf8')));
const undoBefore = { ...auto, notes: 'before', generatedCharges: [{ scheduleId: 'fee', amount: '-40', name: 'Fee', overridden: false, version: 1 }], expenseDeductions: [{ expenseId: 'expense', amount: '20', version: 1 }] };
const undoAfter = { ...undoBefore, notes: 'after', generatedCharges: [{ ...undoBefore.generatedCharges[0], amount: '-10', overridden: true }], expenseDeductions: [{ ...undoBefore.expenseDeductions[0], amount: '5' }] };
const undoCurrent = { ...undoAfter, version: 9, costs: { revision: 'latest' }, generatedCharges: [{ ...undoAfter.generatedCharges[0], version: 8, scheduleVersion: 4 }], expenseDeductions: [{ ...undoAfter.expenseDeductions[0], version: 7, available: '80' }] };
const undone = restorePayEdit(undoCurrent, undoBefore, undoAfter);
assert.equal(undone.notes, 'before'); assert.equal(undone.version, 9); assert.equal(undone.costs.revision, 'latest');
assert.equal(undone.generatedCharges[0].amount, '-40'); assert.equal(undone.generatedCharges[0].version, 8); assert.equal(undone.generatedCharges[0].scheduleVersion, 4); assert.equal(undone.generatedCharges[0].reset, true);
assert.equal(undone.expenseDeductions[0].amount, '20'); assert.equal(undone.expenseDeductions[0].version, 7); assert.equal(undone.expenseDeductions[0].available, '80'); assert.equal(undone.expenseDeductions[0].apply, true);
console.log('Payroll undo preserves current concurrency and payment metadata.');

assert.equal(sourceAmountInput('250', '-400'), '-250');
assert.equal(sourceAmountInput('+250', '-400'), '-250');
assert.equal(sourceAmountInput('-2.50', '3.25'), '2.50');
for (const value of ['0', '-250', '-400']) assert.equal(validSourceAmount(value, '-400'), true);
for (const value of ['250', '-400.01', '-']) assert.equal(validSourceAmount(value, '-400'), false);
assert.equal(validSourceAmount('3.25', '3.25'), true);
assert.equal(validSourceAmount('-3.25', '3.25'), false);
assert.equal(validSourceAmount('3.26', '3.25'), false);
assert.equal(validSourceAmount('-0.01', '0'), false);
assert.equal(validAdjustments({ ...auto, fuelOverride: '-100.11' }, owner), false);
assert.equal(validAdjustments({ ...auto, fuelOverride: '10' }, owner), false);
console.log('Source-backed charges keep their direction and cannot exceed the source due.');
