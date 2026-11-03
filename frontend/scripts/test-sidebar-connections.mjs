// Compare actual browser navigation with six SSE streams under HTTP/1.1 and HTTP/2.
import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { createSecureServer } from 'node:http2';
import { execFileSync } from 'node:child_process';
import { readFile, mkdtemp, rm } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';
const temporary = await mkdtemp(join(tmpdir(), 'sidebar-http2-'));
const key = join(temporary, 'key.pem'), cert = join(temporary, 'cert.pem');
execFileSync(process.env.OPENSSL_PATH || 'openssl', ['req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-keyout', key, '-out', cert, '-days', '1', '-subj', '/CN=localhost'], { stdio: 'ignore', windowsHide: true });
const browser = await chromium.launch();
try {
for (const http2 of [false, true]) {
const streams=new Set();let probes=0;
const handler=async(req,res)=>{
 const path=new URL(req.url,'http://localhost').pathname;
 if(path==='/probe'){probes++;res.end('ok');return;}
 if(path.startsWith('/api/')&&path.includes('/events')){res.writeHead(200,{'content-type':'text/event-stream','cache-control':'no-cache'});res.write(': ready\n\n');streams.add(res);res.on('close',()=>streams.delete(res));return;}
 if(path.startsWith('/api/')){const fixtures={
  '/api/auth/session':{user:{id:'navigation-diagnostic',username:'Navigation diagnostic',permissions:['driver_board.read','tasks.read','expenses.read']},csrfToken:'fixture'},
  '/api/driver-board':{weekStart:'2026-11-02',drivers:[],entries:[],grossEntries:[],loads:{},eld:{configured:false}},
  '/api/tasks/count':{count:0},'/api/feedback/messages/count':{count:0},'/api/expenses':{items:[],total:0,page:1,pageSize:50}
 };res.setHeader('Content-Type','application/json');res.end(JSON.stringify(fixtures[path]??[]));return;}
 try{const file=extname(path)?path.slice(1):path.slice(1)+'.html';const body=await readFile(new URL('../out/'+file,import.meta.url));res.writeHead(200,{'Content-Type':{'.html':'text/html','.js':'application/javascript','.css':'text/css','.txt':'text/plain'}[extname(file)]??'application/octet-stream'});res.end(body);}catch{res.writeHead(404);res.end();}
};
const server = http2 ? createSecureServer({ key: await readFile(key), cert: await readFile(cert), allowHTTP1: true }, handler) : createServer(handler);
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const base=`${http2 ? 'https' : 'http'}://127.0.0.1:${server.address().port}`;
const context=await browser.newContext({ ignoreHTTPSErrors: true });
try{const pages=[];
 for(let i=0;i<3;i++){const page=await context.newPage();pages.push(page);await page.goto(base+'/driver-board');await page.getByRole('heading',{name:'Status Board',exact:true}).waitFor();await page.waitForTimeout(300);}
 await expect.poll(() => streams.size).toBe(6);
 const first=pages[0];const probe=await first.evaluate(async()=>{const start=performance.now();try{await fetch('/probe',{signal:AbortSignal.timeout(2500)});return {ok:true,ms:performance.now()-start};}catch{return {ok:false,ms:performance.now()-start};}});console.log('Request with three board tabs:',JSON.stringify(probe),'server received:',probes);
 expect(probe.ok).toBe(http2);
 expect(probes).toBe(http2 ? 1 : 0);
 if (http2) expect(await first.evaluate(() => performance.getEntriesByType('navigation')[0].nextHopProtocol)).toBe('h2');
 await first.getByRole('link',{name:'Expenses',exact:true}).click();
 if (http2) await expect(first).toHaveURL(base+'/expenses');
 else { await first.waitForTimeout(1000); expect(first.url()).toBe(base+'/driver-board'); }
 await pages[2].close();await pages[1].close();await first.waitForTimeout(1000);
 const recovered=await first.evaluate(async()=>{const start=performance.now();await fetch('/probe');return performance.now()-start;});console.log('After closing two tabs:',JSON.stringify({url:await first.url(),probeMs:recovered,streams:streams.size}));
 await expect(first).toHaveURL(base+'/expenses');
 console.log(`${http2 ? 'HTTP/2' : 'HTTP/1.1'} navigation checks passed`);
}finally{await context.close();for(const s of streams)s.end();await new Promise(r=>server.close(r));}
}
} finally { await browser.close(); await rm(temporary, { recursive: true, force: true }); }
