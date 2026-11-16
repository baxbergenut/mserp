import { chromium, expect } from '@playwright/test';
import { createServer } from 'node:http';
import { readFile, mkdir } from 'node:fs/promises';
import { extname, join } from 'node:path';
import { tmpdir } from 'node:os';

const output = join(tmpdir(), 'clustie-dashboard-review');
await mkdir(output, { recursive: true });
const server = createServer(async (req, res) => {
  try {
    const path = new URL(req.url, 'http://localhost').pathname;
    const file = path === '/' ? 'index.html' : extname(path) ? path.slice(1) : `${path.slice(1)}.html`;
    const body = await readFile(new URL(`../out/${file}`, import.meta.url));
    res.writeHead(200, { 'Content-Type': { '.html': 'text/html', '.js': 'application/javascript', '.css': 'text/css', '.txt': 'text/plain' }[extname(file)] ?? 'application/octet-stream' }); res.end(body);
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const base = `http://127.0.0.1:${server.address().port}`;
const metrics = { gross: '143250.00', driverGross: '128000.00', miles: '51240.00', rpm: '2.80', loads: 64, pairedLoads: 61, missingGross: 0, missingDriverGross: 0, missingMiles: 3, provisional: 2, provisionalGross: '4500.00', conflicts: 2, conflictGross: '6000.00', repeats: 1, repeatedGross: '2500.00', rawGross: '156250.00' };
const dispatcher = '00000000-0000-0000-0000-000000000010';
const permissions = ['board.read', 'trucks.read', 'drivers.read', 'trailers.read', 'fuel.read', 'tolls.read', 'weighmytruck.read', 'expenses.read', 'tasks.read', 'driver_pay.read', 'investor_pay.read'];
let administrator = true;
let allowed = [...permissions], theme = 'default-light', revision = 1, fail = false, partial = false, empty = false, fleetMode = 'normal';
const requested = [];
const section = (kind, title, items, extra = {}) => ({ kind, title, items, updatedAt: '2026-10-26T20:00:00Z', ...extra });
function dashboard(query) {
  const dateFrom = query.get('dateFrom') || '2026-11-02', dateTo = new Date(Date.parse(dateFrom+'T12:00:00Z') + 6*86400000).toISOString().slice(0,10);
  const period = { preset: query.get('preset') || 'week', dateFrom, dateTo, compareFrom: '2026-10-12', compareTo: '2026-10-18', bucket: 'day' };
  const sections = {};
  if (empty) return { period, sections };
  if (allowed.includes('board.read')) sections.performance = section('performance', 'Gross Board performance', [], partial ? { error: 'This section could not be loaded' } : { performance: { current: metrics, previous: { ...metrics, gross: '128000.00' }, trend: ['19','20','21','22','23','24','25'].map((day, i) => ({ ...metrics, id: `2026-10-${day}`, name: '', gross: String(10000 + i * 3500) })), dispatchers: [ { ...metrics, id: dispatcher, name: 'Dispatch East', gross: '72000.00' }, { ...metrics, id: '00000000-0000-0000-0000-000000000011', name: 'Dispatch West', gross: '71250.00' } ] } });
  for (const kind of ['trucks', 'trailers']) if (allowed.includes(`${kind}.read`)) sections[kind] = section('fleet', kind === 'trucks' ? 'Trucks' : 'Trailers', ['Ready', 'In use', 'Shop', 'OOS', 'Terminated'].map((label, i) => ({ id: ['ready', 'in_use', 'shop', 'out_of_service', 'terminated'][i], label, count: [12, 136, 8, 2, 14][i], href: `${kind === 'trucks' ? '/trucks' : '/fleet/trailers'}?dashboard=1&status=${['ready', 'in_use', 'shop', 'out_of_service', 'terminated'][i]}` })));
  if (allowed.includes('drivers.read')) sections.drivers = section('fleet', 'Drivers', ['Active','Home','Vacation','Rest','Reset','Family issue','Health issue','New driver','Without truck'].map((label, i) => ({ id: ['active','home','vacation','rest','reset','family_issue','health_issue','new_driver','unassigned'][i], label, count: [126,8,4,2,3,0,1,2,6][i], href: '/drivers?dashboard=1&employment=active&availability=home' })));
  if (allowed.includes('tasks.read')) sections.tasks = section('attention', 'Your tasks', [{ id: 'open', label: 'Open tasks', count: 12, href: '/tasks?dashboard=1&status=open' }]);
  if (allowed.includes('trucks.read')) sections.gps = section('attention', 'GPS attention', [{ id: 'overdue', label: 'GPS update overdue', count: 3, href: '/trucks?dashboard=1&gpsAlert=overdue' }]);
  for (const [key,permission,label,path,amount] of [['diesel','fuel.read','Diesel','/fuel','25200.45'],['tolls','tolls.read','Tolls','/tolls','3100.25'],['scale','weighmytruck.read','Scale tickets','/weighmytruck','650.00'],['expenses','expenses.read','Maintenance','/expenses','8500.00']]) {
    if (allowed.includes(permission)) sections[key] = section('cost', key === 'expenses' ? 'Expenses by category' : label, [{ id: key, label, count: 32, amount, previousAmount: '1000.00', href: `${path}?dashboard=1&tab=transactions&dateFrom=${dateFrom}&dateTo=${dateTo}${key === 'diesel' ? '&category=fuel' : key === 'expenses' ? '&categoryId=maintenance' : ''}`, note: 'Recorded transactions; not company payment responsibility.' }]);
  }
  for (const kind of ['trucks','drivers','trailers']) if (sections[kind] && fleetMode !== 'normal') {
    sections[kind].items = sections[kind].items.map(item => ({...item, count: fleetMode === 'single' && ['active','in_use'].includes(item.id) ? 5 : 0}));
  }
  if (sections.expenses) {
    const [sample] = sections.expenses.items;
    for (let i = 1; i <= 15; i++) sections.expenses.items.push({
      ...sample, id: `expense-${i}`, label: `Expense category ${i}`,
      href: sample.href.replace('categoryId=maintenance', `categoryId=expense-${i}`),
    });
  }
  for (const [key,value] of Object.entries(sections)) if (query.get('view') === 'weekly' ? ['fleet','attention'].includes(value.kind) : query.get('view') === 'current' && !['fleet','attention'].includes(value.kind)) delete sections[key];
  if(query.has("sections")) for(const key of Object.keys(sections)) if(!query.get("sections").split(",").includes(key)) delete sections[key];
  return { period, sections };
}
let browser;
try {
  browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  await page.clock.setFixedTime(new Date('2026-11-03T14:00:00Z'));
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url()); const path = url.pathname.slice(4); requested.push({ path, query: Object.fromEntries(url.searchParams) });
    if (path === '/auth/session' || path === '/auth/login') return route.fulfill({ json: { user: { administrator, id: 'dashboard-fixture', username: 'Dashboard review', theme, accessRevision: revision, permissions: allowed, expenseCategoryAccess: [{ categoryId: 'maintenance', canView: true }] }, csrfToken: 'fixture' } });
    if (path === '/dashboard/summary') {return route.fulfill(fail ? { status: 500, json: { error: 'Synthetic connection failure' } } : { json: dashboard(url.searchParams) });}
    if (path === '/trucks') return route.fulfill({ json: { items: [], total: 0, page: 1, pageSize: 25, totalPages: 1, statusCounts: {ready:12,in_use:136,shop:8,out_of_service:2,terminated:14}, gpsAlertCounts: {overdue:3,missing:0} } });
    if (path === '/driver-directory') return route.fulfill({json:{items:[],total:0,page:1,pageSize:25,totalPages:1}});
    if (path === '/fuel-transactions') return route.fulfill({ json: { items: [], total: 0, page: 1, pageSize: 25, totalPages: 1, options: {drivers:[],states:[]}, summary: {spend:0,saved:0,gallons:0} } });
    if (path === '/tolls') return route.fulfill({ json: { items: [], total: 0, page: 1, pageSize: 25, totalPages: 1, options: {units:[],agencies:[]}, summary: {amount:0,truckCount:0} } });
    if (path === '/weighmytruck/transactions') return route.fulfill({ json: { items: [], total: 0, page: 1, pageSize: 25, totalPages: 1 } });
    if (path === '/tasks') return route.fulfill({ json: {items:[],total:0,page:1,pageSize:25,totalPages:1} });
    const data = { '/sidebar-counts': { fuel: 0, weighMyTruck: 0, tolls: 0, tollsFrom: '2026-09-07' }, '/tasks/count': { count: 12 }, '/maintenance/access': { canView: true }, '/maintenance/options': {}, '/gross-board': {weekStart:'2026-10-19',drivers:[],entries:[],balances:[]} };
    return route.fulfill({ json: data[path] ?? [] });
  });
  await page.goto(`${base}/dashboard`);
  const picker = page.getByRole('combobox', { name: 'Week of', exact: true });
  const weekNavigation = page.getByRole('group', { name: 'Week of navigation' });
  const grossCard = page.locator('dl > div').filter({has: page.getByText('Original Gross', {exact:true})});
  await expect(page.getByRole('heading',{name:'Weekly profit',exact:true})).toHaveCount(0);
  await expect(page.getByRole('img',{name:'Gross to profit waterfall'})).toHaveCount(0);
  await expect(grossCard).toContainText('$143,250.00');
  await expect(grossCard).toContainText('11.9% higher');
  await expect(picker.locator('option:checked')).toHaveText('Week 45 · 11/02 → 11/08');
  await expect(picker.locator('option')).toHaveCount(3);
  await expect(weekNavigation.getByRole('button', { name: 'Next week' })).toBeDisabled();
  expect((await weekNavigation.boundingBox()).x).toBe((await grossCard.boundingBox()).x);
  await expect(page.getByRole('heading', {name:/2026-10-19|Weekly overview/})).toHaveCount(0);
  await expect(page.getByText(/Compared with|Reconcile totals|provisional rows|conflicting rows|repeated rows|Incomplete \/ review/)).toHaveCount(0);
  await expect(grossCard.getByRole('button')).toHaveCount(0);
  await expect(page.getByRole('cell', {name:'Dispatch East'}).getByRole('button')).toHaveCount(0);
  const categories = page.getByRole('region', {name:'Expenses by category'});
  await expect(categories.getByRole('link')).toHaveCount(16);
  expect(await categories.evaluate(el => el.clientHeight <= 224 && el.scrollHeight > el.clientHeight)).toBe(true);
  expect((await categories.getByRole('link').nth(1).boundingBox()).y).toBe((await categories.getByRole('link').first().boundingBox()).y);
  await expect(page.getByRole('heading', { name: 'Recorded costs' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Needs attention' })).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Recorded costs' })).toBeVisible();
  await expect(page.getByRole('button', {name:/Driver Gross/})).toHaveCount(0);
  await expect(page.getByRole('heading', {name:'Trailers',exact:true})).toHaveCount(0);
  await page.getByRole('tab',{name:'Right now',exact:true}).click();
  await expect(page.getByRole('heading',{name:'Weekly profit',exact:true})).toHaveCount(0);
  await expect(page.getByRole('combobox', { name: 'Week of', exact: true })).toHaveCount(0);
  await expect(page.getByRole('heading',{name:'Needs attention'})).toBeVisible();
  const truckChart = page.getByRole('img',{name:'Trucks status distribution: 158 total',exact:true});
  const driverChart = page.getByRole('img',{name:'Drivers status distribution: 140 total',exact:true});
  await expect(truckChart).toBeVisible(); await expect(driverChart).toBeVisible();
  await expect(truckChart.locator('.recharts-pie-sector')).toHaveCount(4);
  await expect(driverChart.locator('.recharts-pie-sector')).toHaveCount(3);
  await expect(page.getByRole('heading',{name:'Trailers 158',exact:true})).toBeVisible();
  await expect(page.getByRole('img',{name:'Trailers status distribution: 158 total',exact:true}).locator('.recharts-pie-sector')).toHaveCount(4);
  await expect(page.getByLabel('Trucks status labels').getByRole('link',{name:'In use: 136, 86.1%',exact:true})).toBeVisible();
  await expect(page.getByLabel('Drivers status labels').getByRole('link')).toHaveCount(3);
  await expect(page.getByLabel('Drivers status labels').getByRole('link',{name:'Active: 128, 91.4%',exact:true})).toBeVisible();
  await expect(page.getByRole('link',{name:/Terminated|Without truck|Family issue|Health issue|New driver|Reset|Rest:/})).toHaveCount(0);
  await page.screenshot({path:join(output,'dashboard-current.png'),fullPage:true});
  await page.getByRole('tab',{name:'Weekly',exact:true}).click();
  await page.getByRole('group', {name:'Trend metric'}).getByRole('button', {name:'Miles',exact:true}).click();
  await expect(page.getByRole('heading', {name:'Recorded miles trend'})).toBeVisible();
  await page.getByRole('group', {name:'Trend metric'}).getByRole('button', {name:'RPM',exact:true}).click();
  await expect(page.getByRole('heading', {name:'RPM trend'})).toBeVisible();
  await page.getByRole('group', {name:'Trend metric'}).getByRole('button', {name:'Gross',exact:true}).click();
  await page.screenshot({ path: join(output, 'dashboard-light.png'), fullPage: true });
  await page.getByRole('tab',{name:'Right now',exact:true}).click();
  await page.getByLabel('Trailers status labels').getByRole('link',{name:/^Ready:/}).click();
  await expect(page).toHaveURL(/\/fleet\/trailers\?dashboard=1&status=ready/);
  await expect.poll(() => requested.some(r => r.path === '/trailers' && r.query.status === 'ready' && !r.query.search && !r.query.storage)).toBe(true);
  await page.goBack();
  await expect(page.getByLabel('Drivers status labels')).toBeVisible();
  await page.getByLabel('Drivers status labels').getByRole('link',{name:/^Active:/}).click();
  await expect.poll(() => requested.some(r => r.path === '/driver-directory' && r.query.availability === 'active_or_new' && r.query.employment === 'active')).toBe(true);
  await expect(page.getByLabel('Availability',{exact:true})).toHaveValue('active_or_new');
  await page.goBack();
  await expect(page.getByLabel('Drivers status labels')).toBeVisible();
  await page.getByRole('tab',{name:'Weekly',exact:true}).click();
  await picker.selectOption('2026-10-19');
  await expect(weekNavigation.getByRole('button', { name: 'Previous week' })).toBeDisabled();
  await weekNavigation.getByRole('button', { name: 'Next week' }).click();
  await expect.poll(() => requested.some(r => r.path === '/dashboard/summary' && r.query.dateFrom === '2026-10-26')).toBe(true);
  await picker.selectOption('2026-10-19');
  await expect.poll(() => requested.some(r => r.path === '/dashboard/summary' && r.query.dateFrom === '2026-10-19' && r.query.preset === 'week' && r.query.view === 'weekly')).toBe(true);
  for (const [path,api,from,to] of [['/fuel','/fuel-transactions','dateFrom','dateTo'],['/tolls','/tolls','postFrom','postTo'],['/weighmytruck','/weighmytruck/transactions','from','to']]) {
    await page.locator(`a[href^="${path}?dashboard=1"]`).first().click();
    await expect.poll(() => requested.some(r => r.path === api && r.query[from] === '2026-10-19' && r.query[to] === '2026-10-25')).toBe(true);
    await page.goBack(); await expect(picker).toHaveValue('2026-10-19');
  }
  await expect(categories.getByRole('link').last()).toHaveAttribute('href', /dateFrom=2026-10-19&dateTo=2026-10-25&categoryId=expense-15$/);
  await categories.getByRole('link').last().click();
  await expect(page).toHaveURL(/expenses\?.*categoryId=expense-15/);
  await page.goBack();
  await expect(picker).toHaveValue('2026-10-19');
  await page.evaluate(() => { sessionStorage.setItem('mserp-navigation-v1:dashboard-fixture:/trucks:view:page:search', JSON.stringify('HIDDEN SAVED FILTER')); });
  await page.getByRole('tab',{name:'Right now',exact:true}).click();
  await page.locator('section').filter({has: page.getByRole('heading',{name:/^Trucks \d/})}).getByRole('link').filter({hasText:'Shop'}).click();
  await expect.poll(() => requested.some(r => r.path === '/trucks' && r.query.status === 'shop' && !r.query.search)).toBe(true);
  await page.goBack();
  await expect(page.getByLabel('Drivers status labels')).toBeVisible();
  await page.getByRole('tab',{name:'Weekly',exact:true}).click();
  await expect(picker).toHaveValue('2026-10-19');

  await expect(grossCard).toContainText('$143,250.00');
  partial = true; await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(page.getByText(/Last result:/)).toBeVisible();
  await expect(grossCard).toContainText('$143,250.00');
  partial = false; fail = true; await page.getByRole('button',{name:'Retry unavailable sections'}).click();
  await expect(page.getByRole('alert').filter({hasText:'Synthetic connection failure'})).toBeVisible();
  fail = false; await page.getByRole('button',{name:'Retry unavailable sections'}).click();
  await expect(page.getByText('Synthetic connection failure', {exact:true})).toHaveCount(0);
  theme = 'default'; await page.reload(); await expect(page.locator('html')).toHaveAttribute('data-theme','default');
  await expect(page.getByRole('heading',{name:'Weekly profit',exact:true})).toHaveCount(0);
  await expect(grossCard).toBeVisible();
  await page.locator('main').dispatchEvent('pointerdown'); await page.locator('main').evaluate(el => el.scrollTo(0,0));
  await page.screenshot({ path: join(output, 'dashboard-dark.png'), fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 }); await expect(page.getByRole('button',{name:'Expand sidebar'})).toBeVisible(); await expect.poll(() => page.locator('main').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true); await page.screenshot({ path: join(output,'dashboard-mobile.png'), fullPage: true });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.getByRole('tab',{name:'Right now',exact:true}).click();
  await expect(page.getByLabel('Trucks status labels')).toBeVisible();
  await page.screenshot({ path: join(output,'dashboard-current-mobile.png'), fullPage: true });
  await page.getByRole('tab',{name:'Weekly',exact:true}).click();
  await page.getByRole('tab',{name:'Right now',exact:true}).click();
  fleetMode = 'single'; await page.reload();
  await expect(page.getByRole('img',{name:'Trucks status distribution: 5 total'}).locator('.recharts-pie-sector')).toHaveCount(1);
  await expect(page.getByLabel('Trucks status labels').getByRole('link',{name:'In use: 5, 100%',exact:true})).toBeVisible();
  fleetMode = 'zero'; await page.reload();
  await expect(page.getByText('No trucks in these statuses.',{exact:true})).toBeVisible();
  await expect(page.getByText('No drivers in these statuses.',{exact:true})).toBeVisible();
  await expect(page.getByText('No trailers in these statuses.',{exact:true})).toBeVisible();
  await expect(page.getByRole('img',{name:/status distribution/})).toHaveCount(0);
  fleetMode = 'normal';
  allowed = ['trucks.read']; revision++; await page.reload();
  await expect(page.getByRole('heading',{name:/^Trucks \d/})).toBeVisible();
  await expect(grossCard).toHaveCount(0); await expect(page.getByRole('heading',{name:'Recorded costs'})).toHaveCount(0);
  await expect(page.getByRole('heading',{name:/^Trailers \d/})).toHaveCount(0);
  allowed = []; revision++; await page.reload(); await expect(page.getByText(/No dashboard sections are available/)).toBeVisible();
  await page.goto(`${base}/`); await expect(page).toHaveURL(/\/dashboard$/);
  // Broad permissions do not grant dashboard access without Administrator status.
  allowed = [...permissions, 'access.manage']; administrator = false; revision++;
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(page.getByRole('heading',{name:'Access restricted'})).toBeVisible();
  await expect(page.getByRole('link',{name:'Dashboard',exact:true})).toHaveCount(0);
  const dashboardReads = requested.filter(r => r.path.startsWith('/dashboard/')).length;
  await page.reload(); await expect(page.getByRole('heading',{name:'Access restricted'})).toBeVisible();
  expect(requested.filter(r => r.path.startsWith('/dashboard/')).length).toBe(dashboardReads);
  await page.goto(`${base}/`); await expect(page).toHaveURL(/\/gross-board$/);
  await page.goto(`${base}/login?next=/dashboard`);
  await page.getByLabel('Email', {exact:true}).fill('review@example.test');
  await page.getByLabel('Password',{exact:true}).fill('synthetic-test-password');
  await page.getByRole('button',{name:'Sign in',exact:true}).click();
  await expect(page).toHaveURL(/\/gross-board$/);
  administrator = true; revision++; await page.goto(`${base}/`);
  await expect(page).toHaveURL(/\/dashboard$/);
  await expect(page.getByRole('link',{name:'Dashboard',exact:true})).toBeVisible();
  expect(requested.filter(r => r.query.sections?.split(',').includes('waterfall') || ['/dashboard/loads', '/dashboard/cost-review'].includes(r.path))).toEqual([]);
  await expect(page.getByRole('img',{name:'Gross to profit waterfall'})).toHaveCount(0);
  expect(errors).toEqual([]);
  console.log(`Dashboard browser checks passed. Screenshots: ${output}`);
} finally { await browser?.close(); await new Promise(resolve => server.close(resolve)); }
