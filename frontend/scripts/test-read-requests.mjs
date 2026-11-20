import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import ts from "typescript";
const compile = name => `data:text/javascript;base64,${Buffer.from(ts.transpileModule(readFileSync(new URL(name, import.meta.url), "utf8"), { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText).toString("base64")}`;
const { ReadRequests } = await import(compile("../app/lib/readRequests.ts"));
const reads = new ReadRequests();
let calls = 0, finish, upstream;
const run = signal => { calls++; upstream = signal; return new Promise(resolve => { finish = resolve; }); };
const a = new AbortController(), b = new AbortController();
const first = reads.read("same", run, a.signal), second = reads.read("same", run, b.signal);
await Promise.resolve();
a.abort(); await assert.rejects(first, { name: "AbortError" });
assert.equal(calls, 1); assert.equal(upstream.aborted, false);
finish({ items: ["original"] }); assert.deepEqual(await second, { items: ["original"] });
const c = new AbortController(); const last = reads.read("cancel", run, c.signal); await Promise.resolve(); c.abort(); await assert.rejects(last, { name: "AbortError" }); assert.equal(upstream.aborted, true);
const left = reads.read("clone", async () => ({ nested: { value: 1 } })), right = reads.read("clone", async () => { throw Error("should deduplicate"); });
const l = await left, r = await right; l.nested.value = 2; assert.equal(r.nested.value, 1);
await reads.read("clone", async () => { calls++; return {}; }); assert.equal(calls, 3);
const { expandBoard } = await import(compile("../app/lib/compactBoard.ts"));
const plan = { planId: "one", stops: [{ location: "live" }] };
const value = expandBoard({ plans: [plan], loads: { driver: { current: 0, week: [0], next: [0], earlier: [], hidden: [], unavailable: [], revision: "r" } } });
assert.deepEqual(value.loads.driver.current, plan); value.loads.driver.current.stops[0].location = "edit"; assert.equal(value.loads.driver.week[0].stops[0].location, "live");
console.log("Read cancellation, shared consumers, cloning, no retained responses, and compact board checks passed.");
const { BoardReadiness } = await import(compile("../app/lib/boardReadiness.ts"));
const readiness = new BoardReadiness();
let recoveries = 0, opened = false;
const connection = readiness.subscribe("status", () => recoveries++);
const opening = readiness.wait("status").then(() => { opened = true; });
await Promise.resolve(); assert.equal(opened, false);
connection.open(); await opening; assert.equal(opened, true); assert.equal(recoveries, 0);
connection.error();
const reconnecting = readiness.wait("status"); connection.open(); await reconnecting;
assert.equal(recoveries, 1);
connection.close(); await readiness.wait("status");
const delayed = readiness.subscribe("status", () => recoveries++);
await readiness.wait("status", 1); delayed.open(); assert.equal(recoveries, 2);
delayed.close(); delayed.open(); assert.equal(recoveries, 2);
const old = readiness.subscribe("gross:old", () => { throw Error("closed scope recovered"); });
const current = readiness.subscribe("gross:new", () => recoveries++);
old.close(); await readiness.wait("gross:old");
let currentReady = false;
const next = readiness.wait("gross:new").then(() => { currentReady = true; });
await Promise.resolve(); assert.equal(currentReady, false);
current.open(); await next; current.close();
console.log("Board subscription ordering, fallback, reconnect recovery and scope cleanup passed.");

readiness.parallel = true;
let fallbacks = 0, probes = 0;
const parallel = readiness.subscribe("status", () => recoveries++);
assert.ok(parallel.opening);
const normal = async () => { fallbacks++; return "normal"; };
const pair = async id => { probes++; return { opening: id, board: "paired" }; };
assert.equal(await readiness.read("status", normal, pair), "paired");
assert.equal(fallbacks, 0);
parallel.open();
assert.equal(await readiness.read("status", normal, pair), "normal");
assert.equal(probes, 1);
parallel.close();
const slow = readiness.subscribe("status", () => recoveries++);
const before = recoveries;
assert.equal(await readiness.read("status", normal, async () => undefined), "normal");
slow.open();
assert.equal(recoveries, before + 1);
slow.close();
const disconnected = readiness.subscribe("status", () => recoveries++);
await readiness.read("status", normal, pair);
disconnected.error(); disconnected.open();
assert.equal(recoveries, before + 2);
disconnected.close();
console.log("Parallel reads require a matching registration and recover after a missed stream.");


const { BoardSections } = await import(compile("../app/lib/boardSections.ts"));
const sections = new BoardSections();
const initial = await sections.read("status", "week", async offer => {
  assert.equal(offer, "v1");
  return { drivers: [{name:"Original"}], planningEntries:["Private"], _sections:{drivers:"a",planningEntries:"p"} };
});
initial.drivers[0].name = "Unsaved client edit";
const refreshedBoard = await sections.read("status", "week", async offer => {
  assert.ok(offer.includes("drivers=a"));
  return { opening:"tab", board:{entries:["New edit"], _sections:{drivers:"a",entries:"b"}} };
});
assert.deepEqual(refreshedBoard.board, {entries:["New edit"], drivers:[{name:"Original"}]});
const offers = [];
const recovered = await sections.read("status", "week", async offer => {
  offers.push(offer);
  return offer ? {_sections:{drivers:"unknown"}} : {drivers:["Recovered"]};
});
assert.deepEqual(recovered.drivers, ["Recovered"]);
assert.equal(offers.length, 2);
assert.equal(offers[1], undefined);
for (const scope of ["other week", "week"]) {
  if (scope === "week") sections.clear();
  await sections.read("status",scope,async offer => {assert.equal(offer,"v1");return {drivers:[]};});
}
console.log("Board section reuse verified for independent edits, removed permissions, recovery, week and access changes.");

const { subscribeBoardStream } = await import(compile("../app/lib/boardStream.ts"));
const sockets = [];
class TestBoardStream extends EventTarget {
  constructor(url) { super(); this.url = url; sockets.push(this); }
  close() { this.closed = true; }
  emit(name, id, value) { this.dispatchEvent(new MessageEvent(name, { lastEventId: String(id), data: JSON.stringify(value) })); }
}
const previousEventSource = globalThis.EventSource;
globalThis.EventSource = TestBoardStream;
try {
  const boards = [];
  const ready = { open() {}, error() {}, close() {} };
  const stop = subscribeBoardStream(enabled => enabled ? 'rows' : 'full', 'board', ready, board => boards.push(board), () => {}, () => {}, () => {});
  const stream = sockets[0];
  stream.emit('board', 1, { entries: [{id:'a'}, {id:'b'}], loads: {a:1,b:2}, week: 'one' });
  stream.emit('boardPatch', 2, { base:1, rows:{entries:{1:{id:'b2'}}}, fields:{loads:{b:3}}, set:{week:'two'} });
  assert.deepEqual(boards[1], {entries:[{id:'a'},{id:'b2'}],loads:{a:1,b:3},week:'two'});
  assert.deepEqual(boards[0], {entries:[{id:'a'},{id:'b'}],loads:{a:1,b:2},week:'one'});
  stream.emit('boardPatch', 4, {base:3, set:{week:'wrong'}});
  assert.equal(stream.closed,true);
  assert.equal(sockets[1].url,'full');
  assert.equal(boards.at(-1),undefined);
  sockets[1].emit('board', '', {entries:[{id:'restored'}]});
  assert.deepEqual(boards.at(-1),{entries:[{id:'restored'}]});
  stop();
  assert.equal(sockets[1].closed,true);
} finally { globalThis.EventSource = previousEventSource; }
