// Real API/database and browser checks against review-local.mjs only.
import { readFile } from 'node:fs/promises';
import { randomBytes, randomUUID } from 'node:crypto';
import { chromium, expect } from '@playwright/test';

const access = JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE, 'utf8'));
if (!['localhost', '127.0.0.1'].includes(new URL(access.base).hostname) || !/^review_[a-f0-9]+$/.test(access.schema)) throw Error('Isolated local review required');
const browser = await chromium.launch();
const contexts = [];
const suffix = randomBytes(5).toString('hex');
async function login(email, password) {
  const context = await browser.newContext(); contexts.push(context);
  const response = await context.request.post(access.base + '/api/auth/login', { data: { email, password } });
  expect(response.status(), await response.text()).toBe(200);
  const session = await response.json();
  return { context, headers: { 'X-CSRF-Token': session.csrfToken, Origin: access.base } };
}
async function api(user, method, path, data, status = 200) {
  const response = await user.context.request.fetch(access.base + '/api' + path, { method, headers: user.headers, ...(data ? { data } : {}) });
  expect(response.status(), `${method} ${path}: ${await response.text()}`).toBe(status);
  return response.status() === 204 ? null : response.json();
}
try {
  const admin = await login(access.email, access.password);
  const options = await api(admin, 'GET', '/maintenance/options');
  const expensePage = await api(admin, 'GET', '/expenses?pageSize=1');
  const category = expensePage.options.settings.find(s => s.kind === 'category' && s.name === 'Other').id;
  const grants = (add, edit = false) => [category, options.categoryId].map(categoryId => ({ categoryId, canView: true, canCreate: add, canEdit: edit, canDelete: false }));
  async function role(name, rights) {
    await api(admin, 'POST', '/settings/roles', { name: name + suffix, permissions: [], expenseCategoryAccess: rights }, 204);
    return (await api(admin, 'GET', '/settings/access')).roles.find(r => r.name === name + suffix);
  }
  async function user(name, role) {
    const email = `${name}-${suffix}@example.test`, password = randomBytes(18).toString('hex');
    await api(admin, 'POST', '/settings/users', { username: name + suffix, email, password, roleId: role.id, active: true }, 200);
    return { ...await login(email, password), name: name + suffix };
  }
  const shared = await role('Shared', grants(true));
  const otherRole = await role('Other', grants(true));
  const viewerRole = await role('Viewer', grants(false));
  const editorRole = await role('Editor', grants(false, true));
  const alice = await user('Alice', shared), bob = await user('Bob', shared);
  const other = await user('Other', otherRole), viewer = await user('Viewer', viewerRole), editor = await user('Editor', editorRole);
  const input = { categoryId: category, expenseDate: '2026-10-22', amount: '12.34', expenseType: 'Equipment', description: 'Same-role expense ' + suffix, coveredBy: 'Company', paymentType: '', paidBy: '' };
  let expense = await api(alice, 'POST', '/expenses', input, 201);
  expect(expense.canEdit).toBe(true);
  expect((await api(bob, 'GET', `/expenses/${expense.id}`)).canEdit).toBe(true);
  expect((await api(bob, 'GET', '/expenses?pageSize=100')).items.find(e => e.id === expense.id).canEdit).toBe(true);
  const stale = expense.updatedAt;
  expense = await api(bob, 'PUT', `/expenses/${expense.id}`, { ...input, description: 'Bob edited ' + suffix, updatedAt: stale });
  expect(expense.createdByName).toBe(alice.name);
  await api(alice, 'PUT', `/expenses/${expense.id}`, { ...input, updatedAt: stale }, 409);
  for (const denied of [other, viewer]) {
    expect((await api(denied, 'GET', `/expenses/${expense.id}`)).canEdit).toBe(false);
    await api(denied, 'PUT', `/expenses/${expense.id}`, input, 403);
  }
  const foreignExpense = await api(other, 'POST', '/expenses', { ...input, description: 'Foreign expense ' + suffix }, 201);
  await api(bob, 'PUT', `/expenses/${foreignExpense.id}`, input, 403);
  await api(editor, 'PUT', `/expenses/${expense.id}`, input);
  await api(bob, 'DELETE', `/expenses/${expense.id}`, undefined, 403);
  const file = { name: 'same-role.txt', mimeType: 'text/plain', buffer: Buffer.from('Synthetic same-role receipt') };
  for (const [u, status] of [[bob, 200], [other, 403], [viewer, 403]]) {
    const response = await u.context.request.post(`${access.base}/api/expenses/${expense.id}/documents`, { headers: u.headers, multipart: { file } });
    expect(response.status(), await response.text()).toBe(status);
  }
  const workInput = { id: randomUUID(), version: 0, status: 'needs_review', workStatus: 'open', location: 'unknown', notes: 'Same-role work ' + suffix, services: [], truckId: options.trucks[0].id };
  let work = await api(alice, 'POST', '/maintenance', workInput);
  expect(work.canEdit).toBe(true);
  expect((await api(bob, 'GET', '/maintenance?pageSize=100')).items.find(m => m.id === work.id).canEdit).toBe(true);
  work = await api(bob, 'PUT', `/maintenance/${work.id}`, { ...workInput, version: work.version, notes: 'Bob work edit ' + suffix });
  expect(work.createdByName).toBe(alice.name); expect(work.updatedByName).toBe(bob.name);
  await api(bob, 'PUT', `/maintenance/${work.id}`, { ...workInput, version: 1 }, 409);
  for (const denied of [other, viewer]) {
    expect((await api(denied, 'GET', `/maintenance/${work.id}`)).canEdit).toBe(false);
    await api(denied, 'PUT', `/maintenance/${work.id}`, { ...workInput, version: work.version }, 403);
  }
  for (const [u, status] of [[bob, 200], [other, 403], [viewer, 403]]) {
    const response = await u.context.request.post(`${access.base}/api/maintenance/${work.id}/documents`, { headers: u.headers, multipart: { file } });
    expect(response.status(), await response.text()).toBe(status);
  }
  await api(bob, 'POST', `/maintenance/${work.id}/void`, { version: work.version, reason: 'Unauthorized' }, 403);
  await api(bob, 'POST', '/maintenance/vendors', { name: 'Unauthorized' }, 403);
  // Exercise both actual forms, rather than just trusting API capability flags.
  const page = await bob.context.newPage();
  await page.goto(access.base + '/expenses');
  await page.getByRole('row', { name: 'Open Same-role expense ' + suffix, exact: true }).click();
  await expect(page.getByLabel('Description', { exact: true })).toBeEnabled();
  await page.getByLabel('Description', { exact: true }).fill('Browser expense edit ' + suffix);
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect((await api(bob, 'GET', `/expenses/${expense.id}`)).description).toBe('Browser expense edit ' + suffix);
  await page.goto(`${access.base}/maintenance/detail?id=${work.id}`);
  await expect(page.getByLabel('Work order description', { exact: true })).toBeEnabled();
  await page.getByLabel('Service type', { exact: true }).selectOption(options.types.find(t => t.active && !t.programId).code);
  await page.getByLabel('Work order description', { exact: true }).fill('Browser work edit ' + suffix);
  await page.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.getByText('Work order saved.', { exact: true })).toBeVisible();
  expect((await api(bob, 'GET', `/maintenance/${work.id}`)).notes).toBe('Browser work edit ' + suffix);
  const readOnlyPage = await viewer.context.newPage();
  await readOnlyPage.goto(`${access.base}/maintenance/detail?id=${work.id}`);
  await expect(readOnlyPage.getByLabel('Work order description', { exact: true })).toBeDisabled();
  // Current role IDs are authoritative even after the creator changes roles.
  const creator = (await api(admin, 'GET', '/settings/access')).users.find(u => u.username === alice.name);
  await api(admin, 'PUT', `/settings/users/${creator.id}`, { ...creator, roleId: otherRole.id });
  expect((await api(bob, 'GET', `/expenses/${expense.id}`)).canEdit).toBe(false);
  expect((await api(bob, 'GET', `/maintenance/${work.id}`)).canEdit).toBe(false);
  await api(bob, 'PUT', `/maintenance/${work.id}`, { ...workInput, version: work.version }, 403);
  expect((await api(other, 'GET', `/maintenance/${work.id}`)).canEdit).toBe(true);
  console.log('Same-role editing passed: expense/work-order forms, uploads, creator preservation, stale saves, role changes, cross-role/view-only denial, category-wide editors and separate Delete/settings permissions.');
} finally {
  await Promise.all(contexts.map(c => c.close()));
  await browser.close();
}
