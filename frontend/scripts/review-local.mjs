// Synthetic, isolated local review environment. Build the frontend with /api first.
// MSERP_REVIEW_TEST_DATABASE_URL must point to a local disposable _test database.
import { execFileSync, spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { createServer } from 'node:http';
import { mkdtemp, readFile, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, extname, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const frontend = resolve(fileURLToPath(new URL('..', import.meta.url)));
const backend = resolve(frontend, '../backend');
const dsn = process.env.MSERP_REVIEW_TEST_DATABASE_URL;
if (!dsn) throw new Error('Set MSERP_REVIEW_TEST_DATABASE_URL to a local disposable _test database');
const database = new URL(dsn);
if (!['localhost', '127.0.0.1'].includes(database.hostname) || !database.pathname.endsWith('_test')) throw new Error('Only local _test databases are permitted');
const schema = `review_${randomBytes(8).toString('hex')}`;
const temp = await mkdtemp(join(tmpdir(), 'mserp-review-'));
const password = randomBytes(18).toString('base64url');
const email = 'review@example.test';
const apiPort = 18570, port = 13570;
const base = `http://127.0.0.1:${port}`;
const sql = input => execFileSync(process.env.PSQL_PATH || 'psql', ['-X', '-q', '-v', 'ON_ERROR_STOP=1', '-d', dsn], { input, encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'], windowsHide: true });
let api, server, stopping = false;
async function stop() {
  if (stopping) return;
  stopping = true;
  if (server) await new Promise(done => server.close(done));
  if (api && api.exitCode === null) { const exited = new Promise(done => api.once('exit', done)); api.kill(); await exited; }
  sql(`DROP SCHEMA IF EXISTS ${schema} CASCADE`);
}
process.once('SIGINT', () => { void stop(); });
process.once('SIGTERM', () => { void stop(); });
try {
  const init = await readFile(join(backend, 'sql/init.sql'), 'utf8');
  sql(`CREATE SCHEMA ${schema}; SET search_path TO ${schema},public;\n${init}\n
    GRANT USAGE ON SCHEMA ${schema} TO mserp_app;
    GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA ${schema} TO mserp_app;
    INSERT INTO app_users(username,password_hash,email,role_id) VALUES('Local Review',crypt('${password}',gen_salt('bf')),'${email}',(SELECT id FROM app_roles WHERE system_role));
    INSERT INTO drivers(full_name,normalized_name,pay_type,pay_rate,hire_date,active) VALUES
      ('Alex Morgan','alex morgan','cpm',0.75,'2026-09-28',true),
      ('Jordan Lee','jordan lee','cpm',0.70,'2026-09-28',true),
      ('Sam Rivera','sam rivera','cpm',0.72,'2026-09-28',true),
      ('Taylor Brooks','taylor brooks','cpm',0.75,'2026-09-28',true),
      ('Casey Park','casey park','cpm',0.68,'2026-09-28',true),
      ('Riley Chen','riley chen','cpm',0.70,'2026-09-28',true),
      ('Inactive Demo Driver','inactive demo driver','cpm',0.70,'2026-09-28',false);
    INSERT INTO driver_escrows(driver_id,driver_name,start_date,amount)
      SELECT id,full_name,'2026-09-28',2500 FROM drivers;
    INSERT INTO driver_escrow_payments(escrow_id,week_start,amount)
      SELECT id,'2026-09-28',CASE driver_name WHEN 'Alex Morgan' THEN 2000 WHEN 'Jordan Lee' THEN 500.25 WHEN 'Sam Rivera' THEN 2500 ELSE 1000 END
      FROM driver_escrows WHERE driver_name IN ('Alex Morgan','Jordan Lee','Sam Rivera','Inactive Demo Driver');
    INSERT INTO driver_escrow_payments(escrow_id,week_start,amount)
      SELECT id,'2026-10-05',CASE driver_name WHEN 'Alex Morgan' THEN 500 ELSE 250.50 END FROM driver_escrows WHERE driver_name IN ('Alex Morgan','Jordan Lee');
    INSERT INTO expenses(driver_id,charge_driver_id,driver_name,company,category,category_id,expense_date,amount,covered_by,description,expense_type)
      SELECT id,id,full_name,'MS Express Inc.','Other',(SELECT id FROM expense_settings WHERE kind='category' AND name='Other'),'2026-09-28',450,'Driver','Equipment replacement','Equipment' FROM drivers WHERE full_name='Jordan Lee';
    INSERT INTO expense_payments(expense_id,week_start,amount) SELECT id,'2026-09-28',125.25 FROM expenses;
    INSERT INTO expense_payments(expense_id,week_start,amount) SELECT id,'2026-10-05',100 FROM expenses;
  `);
  const binary = join(temp, process.platform === 'win32' ? 'api.exe' : 'api');
  execFileSync('go', ['build', '-o', binary, './cmd/server'], { cwd: backend, windowsHide: true });
  database.searchParams.set('search_path', `${schema},public`);
  database.searchParams.set('role', 'mserp_app');
  api = spawn(binary, [], { cwd: temp, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'], env: {
    ...process.env, DATABASE_URL: database.toString(), PORT: String(apiPort), BIND_ADDRESS: '127.0.0.1',
    DATATRUCK_API_KEY: 'test-disabled', DATATRUCK_COMPANY_NAME: 'test', SCHEDULED_SYNCS_ENABLED: 'false',
    FIVE_ELD_API_KEY: '', FIVE_ELD_PROVIDER_TOKEN: '', FIVE_ELD_USDOT: '',
    RELAY_API_KEY: 'test-disabled', PREPASS_CLIENT_ID: 'test-disabled', PREPASS_CLIENT_SECRET: 'test-disabled',
    RELAY_PRODUCTION_API_KEY: 'test-disabled', RELAY_STAGING_API_KEY: '', PREPASS_PRODUCTION_CLIENT_ID: '', PREPASS_PRODUCTION_CLIENT_SECRET: '', PREPASS_NONPRODUCTION_CLIENT_ID: '', PREPASS_NONPRODUCTION_CLIENT_SECRET: '',
    FLEETSCOPE_COMPANY_ID: '', FLEETSCOPE_WEBHOOK_SECRET: '', GROQ_API_KEY: '', GEMINI_API_KEY: '',
    AUTH_COOKIE_SECURE: 'false', FRONTEND_ORIGIN: base,
  }});
  let log = ''; api.stdout.on('data', chunk => { log += chunk; }); api.stderr.on('data', chunk => { log += chunk; });
  for (let attempt = 0; ; attempt++) {
    if (api.exitCode !== null) throw new Error(`Preview API exited: ${log}`);
    try { if ((await fetch(`http://127.0.0.1:${apiPort}/readyz`)).ok) break; } catch {}
    if (attempt === 100) throw new Error('Preview API did not become ready');
    await new Promise(done => setTimeout(done, 200));
  }
  const output = resolve(frontend, 'out');
  server = createServer(async (req, res) => {
    try {
      const url = new URL(req.url, base);
      if (url.pathname.startsWith('/api/')) {
        const chunks = []; for await (const chunk of req) chunks.push(chunk);
        const headers = {};
        for (const key of ['cookie', 'content-type', 'x-csrf-token', 'origin']) if (req.headers[key]) headers[key] = req.headers[key];
        const response = await fetch(`http://127.0.0.1:${apiPort}${url.pathname.slice(4)}${url.search}`, { method: req.method, headers, ...(chunks.length ? { body: Buffer.concat(chunks) } : {}) });
        res.writeHead(response.status, Object.fromEntries(response.headers)); res.end(Buffer.from(await response.arrayBuffer())); return;
      }
      const path = url.pathname === '/' ? '/index.html' : extname(url.pathname) ? url.pathname : `${url.pathname}.html`;
      const file = resolve(output, `.${decodeURIComponent(path)}`);
      if (!file.startsWith(output + sep)) { res.writeHead(403); res.end(); return; }
      const content = await readFile(file);
      const type = { '.txt': 'text/plain', '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.woff2': 'font/woff2' }[extname(file)] ?? 'application/octet-stream';
      res.writeHead(200, { 'Content-Type': type, 'Cache-Control': 'no-store' }); res.end(content);
    } catch { res.writeHead(404); res.end(); }
  });
  await new Promise((done, reject) => { server.once('error', reject); server.listen(port, '127.0.0.1', done); });
  const access = join(temp, 'review-access.json');
  await writeFile(access, JSON.stringify({ base, email, password, schema, pid: process.pid, apiPid: api.pid }, null, 2));
  console.log(`Local review ready: ${base}/accounting/escrow\nLocal-only access details: ${access}\nSynthetic data; external syncs disabled. Ctrl+C stops both servers and removes the test schema.`);
  api.once('exit', () => { if (!stopping) { console.error('Preview API exited'); void stop(); } });
} catch (error) { console.error(error.message); await stop(); process.exitCode = 1; }
