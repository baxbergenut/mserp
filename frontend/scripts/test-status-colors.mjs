import assert from 'node:assert/strict';
import fs from 'node:fs';
import ts from 'typescript';
const source=fs.readFileSync(new URL('../app/lib/statusColors.ts',import.meta.url),'utf8');
const url='data:text/javascript;base64,'+Buffer.from(ts.transpileModule(source,{compilerOptions:{module:ts.ModuleKind.ESNext,target:ts.ScriptTarget.ES2022}}).outputText).toString('base64');
const {statusClass,statusColorKey,statusPair,contrast,displayStatus,statusDefinitions,legacyStatusColorKeys}=await import(url);
for(const scope of ['general','driver','truck']) for(const mode of ['tone','text','dot']) {
 assert.equal(statusClass('',scope,mode),'');
 assert.equal(statusClass(' \t\n ',scope,mode),'');
}
assert.equal(statusClass('NO LOAD','driver'),'status-tone status-no_load');
const originalBoard={
 READY:['#ffffff','#000000'], REST:['#000000','#d9d9d9'], COVERED:['#000000','#ffff00'],
 ENROUTE:['#000000','#3d85c6'], DISPATCHED:['#ffffff','#fbbc04'], RESERVED:['#000000','#b7e1cd'],
 HOME:['#000000','#ff0000'], VACATION:['#000000','#e06666'], SHOP:['#ffffff','#ff9900'],
 STOP:['#000000','#990000'], DEADHEAD:['#000000','#ffffff'], 'NEW DRIVER':['#000000','#ffffff'],
 'Family issue':['#000000','#ffffff'], 'Health issue':['#000000','#ffffff'], 'In the YARD':['#000000','#ffffff'],
 'Hold for Load':['#000000','#ffffff'], 'Issue with PU/Del':['#000000','#ffffff'], ACCIDENT:['#000000','#ffffff'], Left:['#000000','#ffffff'],
};
for(const [status,[foreground,background]] of Object.entries(originalBoard)) assert.deepEqual(statusPair(statusColorKey(status,'driver')),{foreground,background},status);
assert.equal(statusColorKey('READY','driver'),statusColorKey('READY','truck'));
assert.equal(statusColorKey('truck_ready'),statusColorKey('READY'));
assert.equal(statusColorKey('constructor'),'discarded');
assert.equal(statusColorKey('IN TRANSIT'),statusColorKey('ENROUTE'));
assert.equal(statusColorKey('HOME (YARD)'),statusColorKey('home'));
assert.notEqual(statusColorKey('unpaid'),statusColorKey('paid'));
assert.deepEqual(statusPair('home',{'home.fg':'#abcdef','home.bg':'#123456'}),{foreground:'#abcdef',background:'#123456'});
assert.deepEqual(statusPair('home',{'home.fg':'#ffffff','home.bg':'#ffffff'}),{foreground:'#ffffff',background:'#ffffff'});
assert.equal(contrast('#ffffff','#ffffff'),1);
assert.equal(statusPair('driver_ready',{truck_ready:'#445566'}).background,'#445566');
assert.equal(statusPair('home',{home:'#cc44aa'}).background,'#cc44aa');
assert.equal(displayStatus('in_transit'),'Enroute');
assert.equal(displayStatus('At DEL'),'Enroute');
assert.equal(displayStatus('At PU'),'Dispatched');
assert.equal(displayStatus('HOME (YARD)'),'Home');
assert.equal(displayStatus('34 RESET'),'Rest');
const backend=fs.readFileSync(new URL('../../backend/internal/repository/status_colors.go',import.meta.url),'utf8');
const keys=backend.match(/strings.Fields\(`([^`]+)`\)/)[1].trim().split(/\s+/).sort();
assert.deepEqual(keys,[...statusDefinitions.map(([key])=>key),...legacyStatusColorKeys].sort());
console.log('Status colors passed: exact original Status Board pairs, one shared Ready, independent FG/BG, legacy compatibility, aliases and catalog parity.');
