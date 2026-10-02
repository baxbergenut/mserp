// Focused checks without adding a frontend test framework.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import ts from "typescript";

const source = readFileSync(new URL("../app/gross-board/board.ts", import.meta.url), "utf8");
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2020 } });
const { indexBoardEntries, addDays, additionalLoadEntries, monday, emptyEntry, entryKey, hundredths, totals, decimalDisplay, rpmDisplay, reconcileAutosave, rateBalance, rateChange, incompleteRates, matchLoad, mismatch, dayStatuses, exactDayStatus, suggestedDayStatuses, setDayStatus } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`);

assert.equal(monday(new Date(2026, 8, 28)), "2026-09-28");
assert.equal(monday(new Date(2026, 9, 4)), "2026-09-28");
assert.equal(monday(new Date(2027, 0, 1)), "2026-12-28");
assert.equal(addDays("2026-12-28", 7), "2027-01-04");
assert.equal(addDays("2026-03-02", 7), "2026-03-09");
assert.equal(hundredths("0.10") + hundredths("0.20"), BigInt(30));
assert.equal(decimalDisplay(hundredths("-1234.50"), true), "-$1,234.50");
assert.equal(rpmDisplay(hundredths("1350.35"), hundredths("525.60")), "$2.57");
assert.equal(rpmDisplay(BigInt(1), BigInt(0)), "—");
const blank = emptyEntry("driver", "2026-09-28");
const key = entryKey(blank.driverId, blank.date);
const draft = { ...blank, loadNumber: "PLAN", originalRate: "0.10", driverRate: "0.05" };
assert.equal(totals([draft, { ...draft, originalRate: "0.20" }]).original, BigInt(30));
const committed = { ...draft, version: 1 };
assert.deepEqual(reconcileAutosave({ [key]: draft }, { [key]: draft }, [committed]), {});
// Typing during a request must remain pending with the new version.
const later = { ...draft, driverRate: "123.45" };
assert.deepEqual(reconcileAutosave({ [key]: later }, { [key]: draft }, [committed])[key], { ...later, version: 1 });
// Reverting to the old blank value while saving is still an edit to persist.
assert.deepEqual(reconcileAutosave({ [key]: blank }, { [key]: draft }, [committed])[key], { ...blank, version: 1 });
// Server confirmation locks source fields but preserves the newest driver rate.
const confirmed = { ...committed, loadRecordId: 42, originalRate: "999.99", miles: "123.45", systemOriginalRate: "1000", systemMiles: "124" };
assert.deepEqual(reconcileAutosave({ [key]: later }, { [key]: draft }, [confirmed])[key], { ...later, version: 1, loadRecordId: 42, originalRate: "999.99", miles: "123.45", systemOriginalRate: "1000", systemMiles: "124", acceptSystemValues: false });
const differentLoad = { ...later, loadNumber: "HOME" };
assert.deepEqual(reconcileAutosave({ [key]: differentLoad }, { [key]: draft }, [confirmed])[key], { ...differentLoad, version: 1 });
console.log("Gross board checks passed: exact totals, week boundaries, and queued autosave reconciliation.");

// Carry includes entered plans, excludes missing values, and respects explicit zero.
const plan = { ...blank, loadNumber: "PLAN", originalRate: "1000.10", driverRate: "1200.20" };
assert.equal(rateBalance("300.10", [plan]), 10000n);
assert.equal(rateChange({ ...plan, driverRate: "" }), null);
assert.equal(rateChange({ ...plan, driverRate: "0" }), 100010n);
assert.equal(rateChange({ ...plan, loadNumber: "" }), null);
assert.equal(rateChange({ ...plan, duplicate: true }), null);
assert.equal(incompleteRates([{ ...plan, driverRate: "" }, blank]), 1);
const match = matchLoad({ ...plan, miles: "500" }, { id: 4, loadNumber: "PLAN", originalRate: "950.25", miles: "480.50" });
assert.equal(match.enteredOriginalRate, "1000.10");
assert.equal(match.enteredMiles, "500");
assert.equal(match.driverRate, "1200.20");
assert.equal(match.originalRate, "1000.10");
assert.equal(match.miles, "500");
assert.equal(mismatch(match.enteredOriginalRate, match.systemOriginalRate), true);
assert.equal(mismatch("100", "100.00"), false);
assert.equal(mismatch("", "100"), false);
assert.equal(mismatch("100", ""), true);
// An edit made while confirmation is saving retains its comparison value.
const editedMiles = { ...plan, miles: "700", enteredMiles: "700" };
const rebased = reconcileAutosave({ [key]: editedMiles }, { [key]: plan }, [{ ...match, version: 2 }])[key];
assert.equal(rebased.enteredMiles, "700");
assert.equal(rebased.miles, "700");
assert.equal(rebased.systemMiles, "480.50");
assert.equal(rebased.version, 2);
console.log("Rate balance checks passed: carry, missing/zero rates, duplicates, comparison values, and confirmation races.");

assert.equal(dayStatuses.length, 13);
assert.equal(exactDayStatus("load cancelled").value, "LOAD CANCELLED");
assert.equal(exactDayStatus("canceled").value, "LOAD CANCELLED");
assert.equal(suggestedDayStatuses("cancel")[0].value, "LOAD CANCELLED");
const cancelled = setDayStatus(confirmed, "LOAD CANCELLED");
assert.equal(cancelled.loadNumber, "");
assert.equal(cancelled.loadRecordId, null);
assert.equal(rateBalance("200", [cancelled]), 20000n);
assert.equal(incompleteRates([cancelled]), 0);
assert.equal(totals([cancelled]).original, 0n);
assert.equal(exactDayStatus("  in-transit ").value, "IN TRANSIT");
assert.equal(exactDayStatus("late delivery").value, "LATE DEL");
assert.equal(exactDayStatus("sh"), undefined);
assert.equal(exactDayStatus("SHOP123"), undefined);
assert.equal(exactDayStatus("LATE10I"), undefined);
assert.equal(suggestedDayStatuses("shop")[0].value, "SHOP");
assert.equal(suggestedDayStatuses("truck")[0].value, "TRUCK ISSUE");
assert.equal(suggestedDayStatuses("").length, 13);
const home = setDayStatus(confirmed, "HOME");
assert.equal(home.version, confirmed.version);
assert.equal(home.loadNumber, "");
assert.equal(home.loadRecordId, null);
assert.equal(home.originalRate, "");
assert.equal(home.driverRate, "");
assert.equal(home.miles, "");
assert.equal(home.enteredOriginalRate, "");
assert.equal(rateBalance("200", [home]), 20000n);
assert.equal(incompleteRates([home]), 0);
assert.equal(totals([{...home, originalRate: "900"}]).original, 0n);
assert.equal(matchLoad(home, {id:1, loadNumber:"L1", originalRate:"100", miles:"1"}).dayStatus, "");
assert.deepEqual(reconcileAutosave({[key]:home}, {[key]:draft}, [confirmed])[key], home);
console.log("Day status checks passed: aliases, partial suggestions, totals exclusions, replacement, and autosave races.");

const extra = { ...emptyEntry("driver", "2026-09-28", 1), loadNumber: "SECOND", originalRate: "125.50", driverRate: "100.00" };
const extraKey = entryKey(extra.driverId, extra.date, extra.slot);
assert.notEqual(extraKey, key);
assert.equal(totals([draft, extra, { ...extra, slot: 2, deleted: true }]).original, BigInt(12560));
const edits = { [key]: later, [extraKey]: { ...extra, driverRate: "110.25" } };
const extraRebased = reconcileAutosave(edits, { [extraKey]: extra }, [{ ...extra, version: 3 }]);
assert.deepEqual(extraRebased[key], later);
assert.equal(extraRebased[extraKey].driverRate, "110.25");
assert.equal(extraRebased[extraKey].version, 3);
const removed = { ...extra, deleted: true, loadNumber: "", originalRate: "", driverRate: "", miles: "" };
assert.equal(reconcileAutosave({ [extraKey]: removed }, { [extraKey]: extra }, [{ ...extra, version: 2 }])[extraKey].deleted, true);
console.log("Multiple-load checks passed: separate slots, removed-load totals, and independent save versions.");
assert.deepEqual(additionalLoadEntries([], blank.driverId, blank.date).map(entry => entry.slot), [0, 1]);
assert.deepEqual(additionalLoadEntries([draft, extra], blank.driverId, blank.date).map(entry => entry.slot), [2]);
const reused = additionalLoadEntries([draft, { ...removed, version: 4 }], blank.driverId, blank.date);
assert.equal(reused[0].slot, 1);
assert.equal(reused[0].version, 4);
assert.equal(reused[0].deleted, false);

const indexedEntries = [
  { ...blank, slot: 2, deleted: true },
  { ...blank, driverId: "other", slot: 0 },
  { ...blank, slot: 0 },
  { ...blank, date: "2026-09-29", slot: 1 },
];
const indexed = indexBoardEntries(indexedEntries);
assert.deepEqual(indexed.byDay.get("driver:2026-09-28").map(e => e.slot), [0, 2]);
assert.equal(indexed.byDay.get("driver:2026-09-28")[1].deleted, true);
assert.equal(indexed.byDriver.get("driver").length, 3);
assert.equal(indexed.byDriver.get("other").length, 1);
assert.equal(indexedEntries[0].slot, 2); // Source ordering is unchanged.
console.log("Board indexing checks passed: driver/day isolation, slots and tombstones.");

const { editAmount } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`);
const autoGross = editAmount(blank, "originalRate", "1200");
assert.equal(autoGross.driverRate, "1200");
assert.equal(editAmount(autoGross, "originalRate", "1500").driverRate, "1500");
assert.equal(editAmount({ ...autoGross, driverRate: "1100" }, "originalRate", "1500").driverRate, "1100");
assert.equal(editAmount({ ...autoGross, driverRate: "0" }, "originalRate", "1500").driverRate, "0");
assert.equal(editAmount(match, "miles", "0").enteredMiles, "0");
assert.equal(matchLoad(blank, { id: 1, loadNumber: "L1", originalRate: "900", miles: "500" }).driverRate, "900");
assert.equal(matchLoad({ ...blank, originalRate: "0", miles: "0" }, { id: 1, loadNumber: "L1", originalRate: "900", miles: "500" }).originalRate, "0");
const newerGross = editAmount(match, "originalRate", "1400");
assert.equal(reconcileAutosave({[key]:newerGross}, {[key]:match}, [{...match,version:3}])[key].originalRate, "1400");
console.log("Entered values and original-to-driver gross defaults passed.");
