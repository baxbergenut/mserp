type Sections = Record<string, unknown>;

function sections(value: unknown): value is Sections {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function applyBoardPatch(base: Sections, patch: Sections): Sections {
  const next = { ...base };
  for (const group of ["set", "rows", "fields"] as const) {
    const changes = patch[group];
    if (changes === undefined) continue;
    if (!sections(changes)) throw new Error("Invalid board update");
    for (const [key, value] of Object.entries(changes)) {
      if (!Object.hasOwn(base, key)) throw new Error("Unknown board section");
      if (group === "set") {
        next[key] = value;
        continue;
      }
      if (!sections(value)) throw new Error("Invalid board section");
      const previous = base[key];
      if (group === "rows" && Array.isArray(previous)) {
        const rows = previous.slice();
        for (const [index, row] of Object.entries(value)) {
          const i = Number(index);
          if (!Number.isInteger(i) || i < 0 || i >= rows.length || String(i) !== index) throw new Error("Invalid board row");
          rows[i] = row;
        }
        next[key] = rows;
      } else if (group === "fields" && sections(previous)) {
        if (Object.keys(value).some(name => !Object.hasOwn(previous, name))) throw new Error("Unknown board field");
        next[key] = { ...previous, ...value };
      } else throw new Error("Board section changed shape");
    }
  }
  return next;
}

// The wire baseline belongs to this subscription. HTTP reads and optimistic
// drafts never participate in reconstruction of an incoming board.
export function subscribeBoardStream<B, P>(
  url: (patches: boolean) => string, eventName: "board" | "grossBoard",
  ready: { open(): void; error(): void; close(): void },
  changed: (board?: B) => void, presence: (peers?: P[]) => void,
  connection: (connected: boolean) => void, unauthorized: () => void,
) {
  let stream: EventSource;
  function connect(patches: boolean) {
    const source = new EventSource(url(patches), { withCredentials: true });
    stream = source;
    let baseline: Sections | undefined;
    let sequence = 0;
    source.addEventListener("open", () => {
      baseline = undefined;
      sequence = 0;
      ready.open();
      connection(true);
    });
    source.addEventListener("error", () => { ready.error(); connection(false); });
    source.addEventListener("changed", () => changed());
    source.addEventListener("presence", () => presence());
    source.addEventListener("peers", event => { try { presence(JSON.parse(event.data)); } catch { presence(); } });
    source.addEventListener("unauthorized", () => { source.close(); connection(false); unauthorized(); });
    source.addEventListener(eventName, event => {
      try {
        const value: unknown = JSON.parse(event.data);
        if (!sections(value)) throw new Error("Invalid board snapshot");
        sequence = Number(event.lastEventId);
        baseline = patches && sequence > 0 ? value : undefined;
        changed(value as B);
      } catch { baseline = undefined; changed(); }
    });
    source.addEventListener("boardPatch", event => {
      if (source !== stream || !patches) return;
      try {
        const patch: unknown = JSON.parse(event.data);
        if (!baseline || !sections(patch) || patch.base !== sequence || Number(event.lastEventId) !== sequence + 1) throw new Error("Board update gap");
        baseline = applyBoardPatch(baseline, patch);
        sequence++;
        changed(baseline as B);
      } catch {
        // Recover with an authoritative read and ordinary full frames. An old
        // or malformed stream cannot repeatedly apply a patch to a stale base.
        baseline = undefined;
        source.close();
        ready.error();
        connection(false);
        connect(false);
        changed();
      }
    });
  }
  connect(true);
  return () => { ready.close(); stream.close(); };
}
