// Subscribe before the opening GET so no commit can fall between the snapshot
// and the live subscription. The stream opens without calculating another board.
export class BoardReadiness {
  private connections = new Map<string, { ready: Promise<void>; resolve: () => void; connected: boolean; recover: boolean }>();

  subscribe(key: string, recover: () => void) {
    const gate = () => {
      let resolve!: () => void;
      const ready = new Promise<void>(done => { resolve = done; });
      return { ready, resolve };
    };
    const entry = { ...gate(), connected: false, recover: false };
    this.connections.set(key, entry);
    let active = true;
    return {
      open: () => {
        if (!active) return;
        entry.connected = true;
        entry.resolve();
        if (entry.recover) recover();
        entry.recover = true; // A reconnect must recover the disconnected interval.
      },
      error: () => {
        if (entry.connected) Object.assign(entry, gate());
        entry.connected = false;
      },
      close: () => {
        active = false;
        entry.resolve();
        if (this.connections.get(key) === entry) this.connections.delete(key);
      },
    };
  }

  async wait(key: string, fallbackMS = 150) {
    const entry = this.connections.get(key);
    if (!entry || entry.connected) return;
    await new Promise<void>(resolve => {
      const timer = setTimeout(() => {
        // Keep opening available when SSE is blocked. A later connection must
        // then reconcile any commit missed by that fallback HTTP snapshot.
        entry.recover = true;
        resolve();
      }, fallbackMS);
      void entry.ready.then(() => { clearTimeout(timer); resolve(); });
    });
  }
}
