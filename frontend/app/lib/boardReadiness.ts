// An opening snapshot must follow registration of its live subscription.
export class BoardReadiness {
  parallel = false;
  private connections = new Map<string, { ready: Promise<void>; resolve: () => void; connected: boolean; recover: boolean; opening?: string; attempted: boolean }>();

  subscribe(key: string, recover: () => void) {
    const gate = () => {
      let resolve!: () => void;
      const ready = new Promise<void>(done => { resolve = done; });
      return { ready, resolve };
    };
    const opening = this.parallel ? globalThis.crypto?.randomUUID?.() : undefined;
    const entry = { ...gate(), connected: false, recover: false, opening, attempted: false };
    this.connections.set(key, entry);
    let active = true;
    return {
      opening,
      open: () => {
        if (!active) return;
        entry.connected = true;
        entry.resolve();
        if (entry.recover) recover();
        entry.recover = true; // A reconnect must recover the disconnected interval.
      },
      error: () => {
        // An authorized server registration can precede the first browser open.
        if (opening) entry.recover = true;
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

  async read<T>(key: string, read: () => Promise<T>, paired: (opening: string) => Promise<{ opening: string; board: T } | undefined>) {
    const entry = this.connections.get(key);
    if (entry?.opening && !entry.connected && !entry.attempted) {
      const result = await paired(entry.opening).finally(() => { entry.attempted = true; });
      if (result?.opening === entry.opening) return result.board;
      // The stream was blocked, routed elsewhere, or served by an older API.
      // The server has already waited, so arm late-connection recovery now.
      await this.wait(key, 0);
    } else {
      await this.wait(key);
    }
    return read();
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
