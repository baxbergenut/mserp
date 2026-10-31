// Runs inside the disposable benchmark browser. Observe actual input dispatch
// and painted UI state; Playwright's locator/actionability waits are recorded
// separately and must not be mistaken for application response time.
export function installPayrollBrowserTiming() {
  const timing = window.__payrollTiming = { active: null, navigationVisible: null };
  const visible = element => !!element && element.getClientRects().length > 0 && getComputedStyle(element).visibility !== 'hidden';
  const regions = () => [...document.querySelectorAll('[role="region"][aria-label$=" weekly loads"]')].filter(visible).length;
  const status = () => [...document.querySelectorAll('[role="status"]')].map(e => e.textContent.trim());
  let navigationPending = false;
  const check = () => {
    if (timing.navigationVisible === null && !navigationPending && document.querySelector('tr button[aria-expanded]')) {
      navigationPending = true;
      requestAnimationFrame(() => requestAnimationFrame(() => {
        if (document.querySelector('tr button[aria-expanded]')) timing.navigationVisible = performance.now();
        navigationPending = false;
      }));
    }
    const a = timing.active;
    if (!a || a.started === null || a.visible !== null || a.pending) return;
    const ready = () => {
      if (['firstExpansion', 'cachedExpansion', 'multipleOpen'].includes(a.action)) return regions() > a.regions;
      if (['reopenDialog', 'reopenWeekPreview'].includes(a.action)) return visible(document.querySelector('[role="dialog"] textarea'));
      if (!a.committedResponse) return false;
      if (['save', 'fiveUsersWithSync'].includes(a.action)) return status().includes('Saved');
      if (a.action.startsWith('finalize')) return status().includes('Finalized');
      if (a.action.startsWith('reopen')) return !document.querySelector('[role="dialog"]') && status().includes('Reopened');
      return false;
    };
    if (!ready()) return;
    a.pending = true;
    requestAnimationFrame(() => requestAnimationFrame(() => {
      if (timing.active === a && ready()) a.visible = performance.now();
      a.pending = false;
    }));
  };
  const dispatch = () => {
    const a = timing.active;
    if (a && a.started === null) { a.started = performance.now(); requestAnimationFrame(check); }
  };
  document.addEventListener('click', dispatch, true);
  document.addEventListener('contextmenu', dispatch, true);
  new MutationObserver(check).observe(document, { childList: true, subtree: true, attributes: true, characterData: true });
  const originalFetch = window.fetch;
  window.fetch = async (...args) => {
    const a = timing.active;
    const response = await originalFetch(...args);
    if (a && timing.active === a && a.started !== null) {
      const url = new URL(args[0] instanceof Request ? args[0].url : String(args[0]), location.href);
      const method = (args[1]?.method || 'GET').toUpperCase();
      const mutation = /^\/api\/(driver|investor)-pay(?:\/(finalize|reopen))?$/.test(url.pathname) && ['PUT', 'POST'].includes(method);
      if (mutation && response.ok) { a.committedResponse = true; requestAnimationFrame(check); }
    }
    return response;
  };
  timing.begin = action => {
    timing.active = { action, started: null, visible: null, pending: false, committedResponse: false, regions: regions() };
  };
}
