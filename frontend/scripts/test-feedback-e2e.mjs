// Real API/database/browser coverage. Requires the isolated review-local runner.
import assert from 'node:assert/strict';
import { readFile, mkdir } from 'node:fs/promises';
import { execFileSync } from 'node:child_process';
import { randomBytes, createHash, randomUUID } from 'node:crypto';
import { dirname, join } from 'node:path';
import { chromium, expect } from '@playwright/test';

const access = JSON.parse(await readFile(process.env.MSERP_REVIEW_ACCESS_FILE, 'utf8'));
const database = new URL(process.env.MSERP_REVIEW_TEST_DATABASE_URL);
if (!['127.0.0.1', 'localhost'].includes(new URL(access.base).hostname) || !['127.0.0.1', 'localhost'].includes(database.hostname) || !database.pathname.endsWith('_test') || !/^review_[a-f0-9]+$/.test(access.schema)) throw Error('Only disposable local review is allowed');
const sql = input => execFileSync(process.env.PSQL_PATH || 'psql', ['-X', '-q', '-v', 'ON_ERROR_STOP=1', '-At', '-d', database.toString()], { input: `SET search_path TO ${access.schema},public;\n${input}`, encoding: 'utf8', windowsHide: true }).trim();
const role = randomUUID(), user = randomUUID(), token = randomBytes(32).toString('base64url'), csrf = randomBytes(32).toString('base64url');
const initialTitle = `Make screenshot sharing easier ${user.slice(0, 8)}`, editedTitle = `Make screenshot sharing much easier ${user.slice(0, 8)}`;
sql(`INSERT INTO app_roles(id,name,permissions) VALUES('${role}','Feedback member ${role}','{}');
INSERT INTO app_users(id,username,email,password_hash,role_id) VALUES('${user}','Feedback ${user}','${user}@example.test','$2test','${role}');
INSERT INTO auth_sessions(user_id,token_hash,csrf_token,expires_at) VALUES('${user}','${createHash('sha256').update(token).digest('hex')}','${csrf}',now()+interval '1 hour');`);
const artifacts = join(dirname(process.env.MSERP_REVIEW_ACCESS_FILE), 'feedback');
await mkdir(artifacts, { recursive: true });
const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aY9sAAAAASUVORK5CYII=', 'base64');
const browser = await chromium.launch();
let debugPage;
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  await context.addCookies([{ name: 'mserp_session', value: token, url: access.base, httpOnly: true, sameSite: 'Strict' }]);
  const page = await context.newPage(), errors = [];
  const operationalRequests = [];
  page.on('request', request => { if (/\/api\/tasks(?:\/|\?|$)/.test(request.url())) operationalRequests.push(request.url()); });
  debugPage = page; page.setDefaultTimeout(10000);
  page.on('pageerror', e => errors.push(e.message));
  await page.goto(access.base + '/feedback');
  await expect(page.getByRole('tab', { name: 'Feed', exact: true })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('link', { name: 'Feedback', exact: true })).toBeVisible();
  const nav = await page.locator('nav a').allTextContents();
  assert.deepEqual(nav.slice(-2), ['Feedback', 'Settings']);
  const shareBox = await page.getByRole('button', { name: 'Share feedback', exact: true }).boundingBox();
  const feedBox = await page.getByRole('tab', { name: 'Feed', exact: true }).boundingBox();
  const sortBox = await page.getByLabel('Sort feedback').boundingBox();
  assert.ok(Math.abs(shareBox.y + shareBox.height / 2 - feedBox.y - feedBox.height / 2) < 8 && shareBox.x > sortBox.x, 'Share feedback should sit on the right of the same toolbar row');
  await page.getByRole('button', { name: 'Share feedback', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('More detail helps');
  await dialog.getByLabel('Title', { exact: true }).fill(initialTitle);
  await dialog.getByLabel('Details', { exact: true }).fill('On Feedback, paste a screenshot with Ctrl+V. Expected: a preview appears, and everyone can see it after posting. This helps the dispatch team report bugs quickly.');
  await dialog.getByRole('button', { name: 'Bug', exact: true }).click();
  await dialog.getByRole('button', { name: 'UI', exact: true }).click();
  await dialog.getByLabel('Custom tag').fill('dispatch');
  await dialog.getByRole('button', { name: 'Add tag', exact: true }).click();
  // Dispatch a real browser ClipboardEvent containing an image File.
  await dialog.getByLabel('Details', { exact: true }).evaluate((element, bytes) => {
    const clipboard = new DataTransfer();
    clipboard.items.add(new File([new Uint8Array(bytes)], 'pasted-screenshot.png', { type: 'image/png' }));
    element.dispatchEvent(new ClipboardEvent('paste', { clipboardData: clipboard, bubbles: true, cancelable: true }));
  }, [...png]);
  await expect(dialog.getByRole('img', { name: 'pasted-screenshot.png', exact: true })).toBeVisible();
  await dialog.getByLabel('Feedback attachments').setInputFiles({ name: 'steps.txt', mimeType: 'text/plain', buffer: Buffer.from('Reproduction steps attached.') });
  // A failed save retains both text and files, then one retry creates one post.
  let fail = true;
  await page.route('**/api/feedback', async route => {
    if (route.request().method() === 'POST' && fail) { fail = false; await route.fulfill({ status: 503, json: { error: 'Temporary test failure' } }); }
    else await route.continue();
  });
  await dialog.getByRole('button', { name: 'Post feedback', exact: true }).click();
  await expect(dialog).toContainText('Temporary test failure');
  await expect(dialog.getByLabel('Title', { exact: true })).toHaveValue(initialTitle);
  await expect(dialog.getByRole('button', { name: 'Remove pasted-screenshot.png' })).toBeVisible();
  await dialog.getByRole('button', { name: 'Post feedback', exact: true }).click();
  await expect(page.getByRole('dialog', { name: initialTitle })).toBeVisible();
  await expect(dialog.getByRole('img', { name: 'pasted-screenshot.png', exact: true })).toBeVisible();
  await expect(dialog.getByLabel('Request status')).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Upvote', exact: false }).click();
  await expect(dialog.getByRole('button', { name: /Upvoted · 1/ })).toBeVisible();
  await dialog.getByLabel('Add details or join the discussion').fill('It also happens on a smaller screen.');
  await dialog.getByRole('button', { name: 'Add comment', exact: true }).click();
  await expect(dialog.getByRole('heading', { name: 'Comments (1)' })).toBeVisible();
  await dialog.getByRole('button', { name: 'Edit', exact: true }).click();
  await page.getByRole('dialog', { name: 'Edit feedback' }).getByLabel('Title', { exact: true }).fill(editedTitle);
  await page.getByRole('dialog', { name: 'Edit feedback' }).getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('dialog', { name: editedTitle })).toBeVisible();
  await dialog.getByRole('button', { name: 'Close', exact: true }).last().click();
  await page.getByLabel('Filter by tag').selectOption('dispatch');
  await expect(page.getByRole('button', { name: editedTitle, exact: true })).toBeVisible();
  await page.getByLabel('Sort feedback').selectOption('top');
  await page.reload();
  await expect(page.getByRole('button', { name: editedTitle, exact: true })).toBeVisible();
  const session = await (await context.request.get(access.base + '/api/auth/session')).json();
  assert.deepEqual(session.user.permissions, []);
  const feed = await (await context.request.get(access.base + '/api/feedback')).json();
  const post = feed.items.find(p => p.title === editedTitle);
  assert.ok(post); assert.equal(post.votes, 1); assert.equal(post.attachments.length, 2);
  const article = page.locator('article').filter({ has: page.getByRole('button', { name: editedTitle, exact: true }) });
  await expect(article.getByText('pasted-screenshot.png', { exact: true })).toHaveCount(0);
  const media = await article.getByRole('img', { name: 'pasted-screenshot.png', exact: true }).boundingBox();
  const caption = await article.locator('p').boundingBox();
  assert.ok(media.width > 300 && caption.y >= media.y + media.height, 'post images should be prominent media above the caption');
  const attachment = await context.request.get(access.base + '/api/feedback/attachments/' + post.attachments[0].id);
  assert.equal(attachment.status(), 200);
  const before = (await attachment.body()).length; assert.ok(before > 0);
  await page.screenshot({ path: join(artifacts, 'feed.png'), fullPage: true });
  await page.getByRole('tab', { name: 'Kanban', exact: true }).click();
  for (const name of ['Open', 'In progress', 'Completed', 'Discarded']) await expect(page.getByRole('region', { name, exact: true })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Open', exact: true }).getByRole('button', { name: post.title, exact: true })).toBeVisible();
  assert.equal(await page.locator('article[draggable="true"]').count(), 0);
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator('aside')).toHaveCSS('width', '64px');
  await page.screenshot({ path: join(artifacts, 'kanban-mobile.png'), fullPage: true });
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), 'document must not overflow on mobile');
  await page.getByRole('tab', { name: 'Feed', exact: true }).click();
  await expect(page.getByRole('button', { name: post.title, exact: true })).toBeVisible();
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1));

  // A second signed-in user can read, discuss and vote, with no inherited permissions.
  const secondToken = randomBytes(32).toString('base64url'), secondUser = randomUUID();
  sql(`INSERT INTO app_users(id,username,email,password_hash,role_id) VALUES('${secondUser}','Second ${secondUser}','${secondUser}@example.test','$2test','${role}'); INSERT INTO auth_sessions(user_id,token_hash,csrf_token,expires_at) VALUES('${secondUser}','${createHash('sha256').update(secondToken).digest('hex')}','${csrf}',now()+interval '1 hour');`);
  const second = await browser.newContext();
  await second.addCookies([{ name: 'mserp_session', value: secondToken, url: access.base, httpOnly: true, sameSite: 'Strict' }]);
  const other = await second.newPage(); await other.goto(access.base + '/feedback');
  await other.getByRole('button', { name: post.title, exact: true }).click();
  await expect(other.getByRole('dialog').getByRole('button', { name: 'Edit', exact: true })).toHaveCount(0);
  await other.getByRole('dialog').getByRole('button', { name: /Upvote · 1/ }).click();
  await expect(other.getByRole('dialog').getByRole('button', { name: /Upvoted · 2/ })).toBeVisible();

  const admin = await browser.newContext({ viewport: { width: 1440, height: 1000 } }), adminPage = await admin.newPage();
  await adminPage.goto(access.base + '/login');
  await adminPage.getByLabel('Email or existing username').fill(access.email);
  await adminPage.getByLabel('Password', { exact: true }).fill(access.password);
  await adminPage.getByRole('button', { name: 'Sign in', exact: true }).click();
  await adminPage.waitForURL(u => u.pathname !== '/login');
  await adminPage.goto(access.base + '/feedback');
  await adminPage.getByRole('button', { name: post.title, exact: true }).click();
  for (const value of ['in_progress', 'completed', 'discarded']) {
    await adminPage.getByLabel('Request status').selectOption(value);
    await expect(adminPage.getByLabel('Request status')).toBeEnabled();
    await expect.poll(async () => (await (await admin.request.get(access.base + '/api/feedback/' + post.id)).json()).status).toBe(value);
  }
  // Discarded feedback remains in Kanban, while the feed excludes it.
  await page.goto(access.base + '/feedback');
  await expect(page.getByRole('button', { name: post.title, exact: true })).toHaveCount(0);
  await expect(page.getByLabel('Filter by status').locator('option[value="discarded"]')).toHaveCount(0);
  const messageResult = await (await context.request.get(access.base + '/api/feedback/messages')).json();
  assert.equal(messageResult.total, 1); assert.equal(messageResult.items[0].postId, post.id);
  assert.equal((await (await second.request.get(access.base + '/api/feedback/messages')).json()).total, 0);
  assert.equal((await (await admin.request.get(access.base + '/api/feedback/messages')).json()).total, 0);
  assert.equal((await context.request.get(access.base + '/api/tasks')).status(), 403);
  await page.locator('nav a[href="/tasks"]').click();
  const updates = page.getByRole('region', { name: 'Feedback updates' });
  await expect(updates).toContainText(post.title);
  await expect(updates).toContainText('has been completed');
  await expect(page.getByRole('button', { name: 'Add task', exact: true })).toHaveCount(0);
  await updates.getByRole('link', { name: 'View feedback', exact: true }).click();
  await expect(page.getByRole('dialog', { name: post.title })).toBeVisible();
  await page.getByRole('dialog').getByRole('button', { name: 'Close', exact: true }).last().click();
  await page.locator('nav a[href="/tasks"]').click();
  await updates.getByRole('button', { name: 'Mark as read', exact: true }).click();
  await expect(updates.getByRole('button', { name: 'Mark as read', exact: true })).toHaveCount(0);
  await expect(updates).toContainText(' · Read');
  await expect.poll(async () => (await (await context.request.get(access.base + '/api/feedback/messages/count')).json()).count).toBe(0);
  assert.deepEqual(operationalRequests, [], 'personal messages must not fetch operational tasks');
  await adminPage.getByRole('dialog').getByRole('button', { name: 'Close', exact: true }).last().click();
  await adminPage.getByRole('tab', { name: 'Kanban', exact: true }).click();
  const discarded = adminPage.getByRole('region', { name: 'Discarded', exact: true });
  await expect(discarded.getByRole('button', { name: post.title, exact: true })).toBeVisible();
  await discarded.locator('article').dragTo(adminPage.getByRole('region', { name: 'In progress', exact: true }));
  await expect(adminPage.getByRole('region', { name: 'In progress', exact: true }).getByRole('button', { name: post.title, exact: true })).toBeVisible();
  await adminPage.screenshot({ path: join(artifacts, 'kanban.png'), fullPage: true });
  await adminPage.getByRole('region', { name: 'In progress', exact: true }).getByRole('button', { name: post.title, exact: true }).click();
  await adminPage.getByText('Status history', { exact: true }).click();
  await expect(adminPage.getByRole('dialog')).toContainText('Discarded → In progress');
  const anonymous = await browser.newContext();
  assert.equal((await anonymous.request.get(access.base + '/api/feedback')).status(), 401);
  assert.equal((await anonymous.request.get(access.base + '/api/feedback/attachments/' + post.attachments[0].id)).status(), 401);
  assert.deepEqual(errors, []);
  console.log('Feedback real API/browser checks passed: shared feed/media, toolbar, uploads, retry, comments/votes, discarded filtering, private completion messages in Tasks, read state/deep links, admin status/drag, mobile and access boundaries.');
  console.log('Screenshots: ' + artifacts);
} catch (error) {
  if (debugPage) { console.log('Failure URL:', debugPage.url()); console.log((await debugPage.locator('body').innerText()).slice(-5000)); await debugPage.screenshot({ path: join(artifacts, 'failure.png'), fullPage: true }); }
  throw error;
} finally { await browser.close(); }
