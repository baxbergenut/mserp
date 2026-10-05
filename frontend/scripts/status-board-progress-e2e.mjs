import { expect } from '@playwright/test';

export async function runStatusBoardProgressE2E({ page, base, sql, schema, week }) {
  const id = 'e4500000-0000-0000-0000-000000000091';
  const other = 'e4500000-0000-0000-0000-000000000092';
  const today = new Intl.DateTimeFormat('en-CA', { timeZone: 'America/New_York', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date());
  sql(`SET search_path TO ${schema},public;
    INSERT INTO drivers(id,full_name,normalized_name,pay_type,pay_rate) VALUES
    ('${id}','Progress Browser','progress browser','cpm',0.6),('${other}','Progress Other','progress other','cpm',0.6);
    INSERT INTO loads(id,load_id,status,load_pay,total_pay,total_miles,raw_payload) VALUES
    (88131,'BROWSER-A','Dispatched',100,100,100,'{"stops":[{"ordering":1,"stop_type":"pickup","location":{"city":"Atlanta","state":"GA"}},{"ordering":2,"stop_type":"delivery","location":{"city":"Dallas","state":"TX"}}]}'),
    (88132,'BROWSER-B','Dispatched',200,200,200,'{"stops":[{"ordering":1,"stop_type":"pickup","location":{"city":"Boston","state":"MA"}},{"ordering":2,"stop_type":"delivery","location":{"city":"Miami","state":"FL"}}]}');
    INSERT INTO gross_board_entries(driver_id,service_date,load_number,load_record_id) VALUES('${id}','${week}','BROWSER-A',88131);
    INSERT INTO gross_board_extra_entries(driver_id,service_date,slot,load_number,load_record_id) VALUES('${id}','${today}',1,'BROWSER-B',88132);
  `);
  await page.goto(`${base}/driver-board`);
  await page.getByRole('button', { name: 'All drivers', exact: true }).click();
  await page.getByLabel('Dispatcher filter').selectOption('all');
  await page.getByLabel('Search Status Board').fill('Progress Browser');
  const field = name => page.getByLabel(`Progress Browser · ${name}`, { exact: true });
  const advance = field('Advance load');
  const location = field('Destination from load');
  const saved = () => expect(page.getByText('All changes saved', { exact: true })).toBeVisible({ timeout: 15000 });
  const undo = async () => { await page.getByRole('heading', { name: 'Status Board', exact: true }).click(); await page.keyboard.press('Control+z'); await saved(); };
  await field('Current load').fill('BROWSER-A');
  await expect(field('Status')).toHaveValue('DISPATCHED');
  await expect(location).toHaveText('Atlanta, GAPU');
  await field('Notes').click(); await saved();
  await undo();
  await expect(field('Current load')).toHaveValue('');
  await expect(field('Status')).toHaveValue('');
  await field('Current load').fill('BROWSER-A'); await field('Notes').click(); await saved();
  await advance.click(); await saved();
  await expect(field('Status')).toHaveValue('RESERVED');
  await expect(location).toHaveText('Dallas, TXDEL');
  // A more recent action by another actor must never become this page's undo target.
  sql(`SET search_path TO ${schema},public; BEGIN; SELECT set_config('mserp.board_actor','other-updater',true),set_config('mserp.board_source','board',true); INSERT INTO driver_board(driver_id,notes) VALUES('${other}','Keep other user action'); COMMIT;`);
  await undo();
  await expect(field('Status')).toHaveValue('DISPATCHED');
  await expect(location).toHaveText('Atlanta, GAPU');
  const board = await (await page.request.get(`${base}/api/driver-board?weekStart=${week}`)).json();
  expect(board.entries.find(e => e.driverId === other).notes).toBe('Keep other user action');
  await advance.click(); await saved();
  await advance.click(); await saved();
  await expect(field('Current load')).toHaveValue('BROWSER-B');
  await expect(field('Status')).toHaveValue('DISPATCHED');
  await expect(location).toHaveText('Boston, MAPU');
  await expect(field('Next loads')).toHaveText('—');
  await advance.dblclick(); await saved();
  await expect(field('Current load')).toHaveValue('BROWSER-B');
  await expect(field('Status')).toHaveValue('ENROUTE');
  await expect(location).toHaveText('Miami, FLDEL');
  await advance.click(); await saved();
  await expect(field('Current load')).toHaveValue('');
  await expect(field('Destination')).toHaveValue('');
  await expect(field('Status')).toHaveValue('');
  await undo(); await expect(field('Current load')).toHaveValue('BROWSER-B'); await expect(field('Status')).toHaveValue('ENROUTE');
  await undo(); await expect(field('Status')).toHaveValue('DISPATCHED');
  await undo(); await expect(field('Current load')).toHaveValue('BROWSER-A'); await expect(field('Status')).toHaveValue('RESERVED');
  await expect(location).toHaveText('Dallas, TXDEL');
  await field('Notes').fill('Unsaved draft');
  await undo(); await expect(field('Notes')).toHaveValue('');
  await field('Status').selectOption('SHOP'); await saved();
  await expect(advance).toBeDisabled();
  expect(await field('Status').locator('option').evaluateAll(options => new Set(options.map(o => o.className)).size)).toBe(1);
  await undo(); await expect(field('Status')).toHaveValue('RESERVED');
  await page.reload(); await expect(field('Current load')).toHaveValue('BROWSER-A');
  console.log('Status Board progress passed: pickup, reserved/enroute, promotion, completion, double-click protection, personal sequential Ctrl+Z, draft undo and neutral dropdown.');
}
