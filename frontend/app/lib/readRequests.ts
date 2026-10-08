// Share only pending reads. Responses are never retained, and each consumer
// receives its own object so editors cannot mutate another component's data.
export class ReadRequests {
  private pending = new Map<string, { controller: AbortController; promise: Promise<unknown>; users: number }>();

  clear(abort = false) {
    if (abort) for (const entry of this.pending.values()) entry.controller.abort();
    this.pending.clear();
  }

  read<T>(key: string, run: (signal: AbortSignal) => Promise<T>, signal?: AbortSignal | null): Promise<T> {
    if (signal?.aborted) return Promise.reject(new DOMException("Aborted", "AbortError"));
    let entry = this.pending.get(key);
    if (!entry) {
      const controller = new AbortController();
      entry = { controller, promise: Promise.resolve().then(() => run(controller.signal)), users: 0 };
      this.pending.set(key, entry);
      const current = entry;
      // Both handlers resolve: no unhandled rejection from a detached finally.
      const finish = () => { if (this.pending.get(key) === current) this.pending.delete(key); };
      void entry.promise.then(finish, finish);
    }
    const current = entry;
    current.users++;
    return new Promise<T>((resolve, reject) => {
      let done = false;
      const release = () => {
        if (done) return false;
        done = true; signal?.removeEventListener("abort", abort);
        if (--current.users === 0) {
          if (this.pending.get(key) === current) this.pending.delete(key);
          current.controller.abort();
        }
        return true;
      };
      const abort = () => { if (release()) reject(new DOMException("Aborted", "AbortError")); };
      signal?.addEventListener("abort", abort, { once: true });
      current.promise.then(value => { if (release()) resolve(structuredClone(value) as T); }, error => { if (release()) reject(error); });
    });
  }
}
