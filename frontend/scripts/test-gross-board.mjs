// Focused checks without adding a frontend test framework.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import ts from "typescript";

const source = readFileSync(new URL("../app/gross-board/board.ts", import.meta.url), "utf8");
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2020 } });
const { addDays, monday, emptyEntry, entryKey, hundredths, totals, decimalDisplay, rpmDisplay, reconcileAutosave } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString("base64")}`);

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
const confirmed = { ...committed, loadRecordId: 42, originalRate: "999.99", miles: "123.45" };
assert.deepEqual(reconcileAutosave({ [key]: later }, { [key]: draft }, [confirmed])[key], { ...later, version: 1, loadRecordId: 42, originalRate: "999.99", miles: "123.45" });
const differentLoad = { ...later, loadNumber: "HOME" };
assert.deepEqual(reconcileAutosave({ [key]: differentLoad }, { [key]: draft }, [confirmed])[key], { ...differentLoad, version: 1 });
console.log("Gross board checks passed: exact totals, week boundaries, and queued autosave reconciliation.");
