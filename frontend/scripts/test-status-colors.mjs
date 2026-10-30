import assert from 'node:assert/strict';
import fs from 'node:fs';
import ts from 'typescript';
const source=fs.readFileSync(new URL('../app/lib/statusColors.ts',import.meta.url),'utf8');
const url='data:text/javascript;base64,'+Buffer.from(ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2020}}).outputText).toString('base64');
const {statusColorKey,defaultStatusColors,statusSwatch,contrast,displayStatus,statusDefinitions}=await import(url);
assert.equal(defaultStatusColors[statusColorKey('READY','driver')],'#f59e0b');
assert.equal(defaultStatusColors[statusColorKey('READY','truck')],'#10b981');
assert.equal(defaultStatusColors[statusColorKey('DEADHEAD')],'#ef4444');
assert.equal(statusColorKey('constructor'),'discarded');
assert.equal(statusColorKey('IN TRANSIT'),statusColorKey('ENROUTE'));
assert.equal(statusColorKey('HOME (YARD)'),statusColorKey('home'));
assert.notEqual(statusColorKey('unpaid'),statusColorKey('paid'));
assert.equal(displayStatus('in_transit'),'Enroute');
assert.equal(displayStatus('At DEL'),'Enroute');
assert.equal(displayStatus('At PU'),'Dispatched');
assert.equal(displayStatus('HOME (YARD)'),'Home');
assert.equal(displayStatus('34 RESET'),'Rest');
for(const surface of ['#ffffff','#111113','#fdf6e3','#073642','#272822','#1e1e1e','#202020']) {
 for(const color of [...Object.values(defaultStatusColors),'#000000','#ffffff','#888888','#fefefe','#010101']) {
  const badge=statusSwatch(color,surface);
  assert.ok(contrast(badge.color,badge.background)>=4.5,`${color} on ${surface} failed AA`);
 }
}
const backend=fs.readFileSync(new URL('../../backend/internal/repository/status_colors.go',import.meta.url),'utf8');
const keys=backend.match(/strings.Fields\(`([^`]+)`\)/)[1].trim().split(/\s+/).sort();
assert.deepEqual(keys,statusDefinitions.map(([key])=>key).sort());
console.log('Status colors passed: domain-specific readiness, aliases, Deadhead, exact paid matching, catalog parity and AA contrast across themes/custom hues.');
