// The database lease lasts 30 seconds. Renew from the start of the last
// successful PUT so a slow response cannot extend the assumed lease.
const renewalMS = 18_000;
const retryMS = 5_000;
const movementIntervalMS = 75;

export function createBoardCursorPublisher<S>(
  ready: () => boolean, write: (cell: S) => Promise<void>, clear: () => Promise<void>,
) {
  let selection: { cell: S; key: string } | undefined;
  let lastKey: string | undefined;
  let renewAt = 0, nextMoveAt = 0;
  let paused = false, published = false;
  let pending: Promise<void> | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;

  function announce() {
    clearTimeout(timer);
    const target = selection;
    if (!target || paused || pending || !ready()) return;
    const start = performance.now();
    const due = target.key === lastKey ? Math.max(nextMoveAt, renewAt) : nextMoveAt;
    if (due > start) {
      timer = setTimeout(announce, Math.ceil(due - start));
      return;
    }
    nextMoveAt = start + movementIntervalMS;
    published = true;
    const request = write(target.cell);
    pending = request;
    const settled = (success: boolean) => {
      if (pending !== request) return;
      pending = undefined;
      lastKey = target.key;
      renewAt = success ? start + renewalMS : performance.now() + retryMS;
      announce(); // A move during the request publishes only the latest cell.
    };
    void request.then(() => settled(true), () => settled(false));
  }

  const remove = () => clear().catch(() => {});
  return {
    select(cell: S) {
      const key = JSON.stringify(cell);
      if (selection?.key === key) return;
      selection = { cell, key };
      announce();
    },
    resume() {
      paused = false;
      announce();
    },
    leave() {
      paused = true;
      clearTimeout(timer);
      if (!published) return;
      published = false;
      const writing = pending;
      // Clear immediately on pagehide. If a PUT was on the wire, clear once
      // more after it settles. A returning tab publishes after both deletes.
      const cleanup = Promise.all([remove(), writing?.catch(() => {})]).then(async () => {
        if (writing) await remove();
      });
      pending = cleanup;
      void cleanup.then(() => {
        pending = undefined;
        lastKey = undefined;
        nextMoveAt = 0;
        announce();
      });
    },
  };
}
