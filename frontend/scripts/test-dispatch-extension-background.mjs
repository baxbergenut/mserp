// Isolated service-worker regression tests; no network or real credentials.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';

const local = {token: 'synthetic-token', dispatcherName: 'Test Dispatcher', settings: {interval: 5, sound: 'bell', highlight: 'blue'}};
let listener, fetchImpl = async () => ({ok: true, json: async () => ({})});
const sandbox = vm.createContext({AbortSignal, importScripts() {},
  fetch: (...args) => fetchImpl(...args),
  chrome: {
    storage: {local: {
      setAccessLevel: async () => {},
      get: async keys => Object.fromEntries((Array.isArray(keys) ? keys : [keys]).map(k => [k, local[k]])),
      set: async values => Object.assign(local, values),
      remove: async keys => { for (const k of Array.isArray(keys) ? keys : [keys]) delete local[k]; },
    }},
    runtime: {getURL: path => `chrome-extension://test/${path}`, onMessage: {addListener: fn => { listener = fn; }}},
  },
});
vm.runInContext(await readFile('../extensions/dispatcher/core.js', 'utf8'), sandbox);
vm.runInContext(await readFile('../extensions/dispatcher/background.js', 'utf8'), sandbox);
const board = id => ({tab: {id}, url: 'https://scm.jbhunt.com/carrier/ngx/load-board/grid'});
const settings = {url: 'chrome-extension://test/settings.html'};
const send = (message, sender) => new Promise((resolve, reject) => {
  const timeout = setTimeout(() => reject(new Error(`Timed out waiting for ${message.type}`)), 3000);
  listener(message, sender, result => { clearTimeout(timeout); resolve(result); });
});

// No arbitrary running-tab cap: each tab retains the same shared preferences.
const starts = await Promise.all(Array.from({length: 100}, (_,id) => send({type: 'start'}, board(id))));
assert.ok(starts.every(r => r.ok && r.settings.interval === 5 && r.settings.sound === 'bell' && r.settings.highlight === 'blue'));
await send({type: 'stop'}, board(0));
assert.equal((await send({type: 'poll'}, board(1))).ok, true);

let release, entered;
const waiting = new Promise(resolve => { entered = resolve; });
fetchImpl = () => new Promise(resolve => { release = () => resolve({ok: true, json: async () => ({})}); entered(); });
const slowCheck = send({type: 'poll'}, board(0));
await waiting;
fetchImpl = async () => ({ok: true, json: async () => ({})});
assert.equal((await send({type: 'poll'}, board(1))).ok, true, 'one pending check must not block other tabs');
assert.equal((await send({type: 'forget'}, settings)).ok, true);
release();
assert.match((await slowCheck).error, /activation changed/, 'an old check cannot authorize after Disconnect');
assert.equal((await send({type: 'poll'}, board(1))).ok, false);

local.token = 'synthetic-replacement';
await send({type: 'save', settings: {interval: 8, sound: 'radar', highlight: 'green', volume: 40}}, settings);
for (const id of [0, 1, 99]) {
  const r = await send({type: 'poll'}, board(id));
  assert.equal(r.settings.interval, 8); assert.equal(r.settings.sound, 'radar'); assert.equal(r.settings.highlight, 'green');
}
fetchImpl = async () => ({ok: false, json: async () => ({error: 'Access revoked'})});
const revoked = await Promise.all([0, 1, 99].map(id => send({type: 'poll'}, board(id))));
assert.ok(revoked.every(r => !r.ok && r.error === 'Access revoked'));
assert.equal((await send({type: 'start'}, {url: 'https://example.com'})).ok, false);
console.log('PASS: 100 concurrent tab starts, shared settings, independent access checks, per-tab stop, revocation and Disconnect race.');
