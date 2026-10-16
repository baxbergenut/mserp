import { expect } from '@playwright/test';

// Runs against the isolated real API/database used by --board-only.
export async function runStatusBoardEditingE2E({ page, base, id, week }) {
  const field = name => page.getByLabel(`Board Cpm · ${name}`, { exact: true });
  const saved = () => expect(page.getByText('All changes saved', { exact: true })).toBeVisible();
  const read = async () => (await (await page.request.get(`${base}/api/driver-board?weekStart=${week}`)).json()).entries.find(e => e.driverId === id);
  const columns = await page.getByRole('columnheader').allTextContents();
  expect(columns.indexOf('Notes')).toBe(columns.indexOf('ETA') - 1);
  const before = (await read()).notes;
  await field('Notes').fill('Instant ');
  await field('Notes').pressSequentially('typed note', { delay: 80 });
  await saved();
  expect((await read()).notes).toBe('Instant typed note');
  await page.getByRole('heading', { name: 'Status Board', exact: true }).click();
  await page.keyboard.press('Control+z');
  await expect(field('Notes')).toHaveValue(before);
  await saved();
  expect((await read()).notes).toBe(before);

  await field('Trailer').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Add note', exact: true }).click();
  const note = page.getByLabel('Note for trailer TA 102', { exact: true });
  await note.fill('Seal 42\nCheck doors');
  await saved();
  expect((await read()).trailerNote).toBe('Seal 42\nCheck doors');
  await page.keyboard.press('Escape');
  await expect(page.getByRole('img', { name: 'Has trailer note', exact: true })).toHaveCount(1);
  await page.reload();
  await field('Trailer').hover();
  await expect(note).toHaveValue('Seal 42\nCheck doors');
  await page.keyboard.press('Escape');

  await field('Notes').click({ button: 'right' });
  const history = page.getByRole('dialog', { name: 'Board Cpm · Cell history', exact: true });
  await expect(history.getByLabel('History column')).toHaveValue('notes');
  await expect(history.locator('article').first()).toContainText('investor-e2e');
  await history.getByLabel('History column').selectOption('trailerNote');
  await expect(history.getByText('Seal 42\nCheck doors', { exact: true })).toBeVisible();
  await history.getByRole('button', { name: 'Close history' }).click();
  await page.getByRole('button', { name: 'History', exact: true }).click();
  const wholeHistory = page.getByRole('dialog', { name: 'Status Board history', exact: true });
  await expect(wholeHistory.getByLabel('History column')).toHaveValue('');
  await expect(wholeHistory.locator('article').first()).toContainText('Board Cpm');
  await wholeHistory.getByRole('button', { name: 'Close history' }).click();
  await field('Trailer').click({ button: 'right' });
  await page.getByRole('menuitem', { name: 'Delete note', exact: true }).click();
  await saved();
  expect((await read()).trailerNote).toBe('');
  await page.getByRole('heading', { name: 'Status Board', exact: true }).click();
  await page.keyboard.press('Control+z');
  await saved();
  await expect(page.getByRole('img', { name: 'Has trailer note', exact: true })).toHaveCount(1);

  await field('ETA').click();
  await page.getByLabel('Arrival time', { exact: true }).fill('13:00');
  await page.getByLabel('Miles left', { exact: true }).fill('300');
  await expect(page.getByLabel('Arrival time', { exact: true })).toHaveValue('');
  await page.getByRole('button', { name: 'Apply ETA' }).click();
  await saved();
  await expect(field('ETA')).toHaveText('300 mi');
  expect((await read()).eta).toBe('300 mi');
  await field('ETA').click();
  await page.getByLabel('Arrival time', { exact: true }).fill('13:00');
  await expect(page.getByLabel('Miles left', { exact: true })).toHaveValue('');
  await page.getByRole('button', { name: 'Apply ETA' }).click();
  await saved();
  await expect(field('ETA')).toHaveText('13:00');
  expect((await read()).eta).toBe('13:00');
  await page.getByRole('heading', { name: 'Status Board', exact: true }).click();
  await page.keyboard.press('Control+z');
  await saved();
  await expect(field('ETA')).toHaveText('300 mi');
  await page.getByRole('heading', { name: 'Status Board', exact: true }).click();
  await page.keyboard.press('Control+z');
  await saved();
  await expect(field('ETA')).toHaveText('17:00');
  const homeTime = (await read()).homeTime;
  for (let index = 0; index < 12; index++) {
    await field('Notes').fill(`Rapid note ${index} `);
    await field('Home time').fill(`Rapid home ${index}`);
  }
  await saved();
  const rapid = await read();
  expect(rapid.notes).toBe('Rapid note 11 ');
  expect(rapid.homeTime).toBe('Rapid home 11');
  await field('Notes').fill(before);
  await field('Home time').fill(homeTime);
  await saved();
  console.log('Status Board editing passed: immediate typing, spaces, grouped personal undo, trailer note persistence/deletion/undo, per-cell history and ETA time/miles.');
}
