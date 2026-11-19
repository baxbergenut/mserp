// Run against a disposable review-local server. Both boards accept the same tab
// UUID independently, and subscriptions must see only their board's committed rows.
import { chromium, expect } from '@playwright/test';
import { readFile } from 'node:fs/promises';
import { randomUUID } from 'node:crypto';
import assert from 'node:assert/strict';

const access = JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE, 'utf8'));
assert.ok(['127.0.0.1','localhost'].includes(new URL(access.base).hostname));
const browser = await chromium.launch();
try {
  const login = async () => {
    const context = await browser.newContext({ baseURL:access.base });
    const result = await context.request.post('/api/auth/login',{data:{email:access.email,password:access.password}});
    assert.equal(result.status(),200);
    return {context,headers:{Origin:access.base,'X-CSRF-Token':(await result.json()).csrfToken}};
  };
  const writer = await login(), observer = await login();
  const week = '2026-11-02', clientId = randomUUID();
  const board = await writer.context.request.get('/api/driver-board?weekStart='+week);
  assert.equal(board.status(),200);
  const driverId = (await board.json()).drivers.find(d=>!d.departed).id;
  const peers = async (boardPath, query='') => {
    const page = await observer.context.newPage();
    await page.goto('/'+boardPath);
    await page.evaluate(url=>{
      window.testPeers = null;
      const stream = new EventSource(url);
      stream.addEventListener('peers',event=>{window.testPeers=JSON.parse(event.data);});
    },'/api/'+boardPath+'/events'+query);
    await expect.poll(()=>page.evaluate(()=>Array.isArray(window.testPeers))).toBe(true);
    return () => page.evaluate(id=>window.testPeers.find(row=>row.clientId===id)?.column ?? null, clientId);
  };
  const status=await peers('driver-board'), gross=await peers('gross-board','?weekStart='+week);
  const put = async (viewer,path,cell) => viewer.context.request.put('/api/'+path+'/presence',{
    headers:viewer.headers,data:{driverId,clientId,...cell}});
  const clear = async (path) => writer.context.request.delete('/api/'+path+'/presence/'+clientId,{headers:writer.headers});
  assert.equal((await put(writer,'driver-board',{column:'notes'})).status(),204);
  assert.equal((await put(writer,'gross-board',{weekStart:week,date:'',slot:0,column:'balance'})).status(),204);
  await expect.poll(status).toBe('notes'); await expect.poll(gross).toBe('balance');
  assert.equal((await put(observer,'driver-board',{column:'eta'})).status(),409);
  assert.equal((await clear('driver-board')).status(),204);
  await expect.poll(status).toBe(null); await expect.poll(gross).toBe('balance');
  assert.equal((await clear('gross-board')).status(),204); await expect.poll(gross).toBe(null);
  console.log('Passed both board streams, same-client independence, session ownership and clear notifications.');
} finally { await browser.close(); }
