/* Carrier 360 grid selectors inspected in the signed-in site on 2026-10-13.
   Read DOM only; refresh and pagination use the site's own controls. */
globalThis.DispatchWatchAdapter = (() => {
  const rowSelector = '[data-cy="lb-table-row"]';
  const visible = e => !!e && e.getClientRects().length > 0 && getComputedStyle(e).visibility !== "hidden";
  function rows() {
    return [...document.querySelectorAll(rowSelector)].map(row => ({ row,
      id: row.querySelector('[id="jobIdentifier"]')?.textContent.trim().match(/^[A-Z0-9]{5,20}\b/)?.[0] || "" }));
  }
  function loading() { return [...document.querySelectorAll('[role="progressbar"]')].some(visible); }
  function refresh() { return document.querySelector('[data-cy="lb-grid-refresh-button"]'); }
  function next() { const e = document.querySelector('[aria-label="Go to next page"]'); return e && !e.disabled ? e : null; }
  function first() { const e = document.querySelector('[aria-label="Go to first page"]'); return e && !e.disabled ? e : null; }
  function blocked() {
    return [...document.querySelectorAll('[role="dialog"], .p-dialog, .p-toast-message-error')].some(visible);
  }
  return { rows, loading, refresh, next, first, blocked, visible, rowSelector };
})();
