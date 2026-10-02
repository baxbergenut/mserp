import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import ts from "typescript";

const source = readFileSync(new URL("../app/lib/phone.ts", import.meta.url), "utf8");
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2020 } }).outputText;
const { normalizePhone, formatPhone, withPhone } = await import(`data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`);
for (const [input, want] of [["", ""], ["  ", ""], ["0123456789", "0123456789"], ["+1 (470) 334-4443", "4703344443"], ["14703344443", "4703344443"], [" 470.334.4443 ", "4703344443"]]) {
  assert.equal(normalizePhone(input), want);
  assert.equal(withPhone({ phone: input, fullName: "Driver" }).phone, want);
}
for (const input of ["123456789", "24703344443", "147033444430", "call 4703344443", "4703344443 x12", "4703344443/4703344444", "１２３４５６７８９０", "+", "470\n3344443"]) {
  assert.equal(normalizePhone(input), null);
  assert.throws(() => withPhone({ phone: input }), /10 digits/);
}
assert.equal(formatPhone("0123456789"), "+1 (012) 345-6789");
assert.equal(formatPhone("+14703344443"), "+1 (470) 334-4443");
for (const value of [null, undefined, "", "123"]) assert.equal(formatPhone(value), "");
console.log("Phone checks passed: canonical values, US display, optional blanks, and invalid inputs.");
