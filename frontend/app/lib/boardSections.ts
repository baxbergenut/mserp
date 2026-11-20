const sectionNames = ["drivers", "entries", "grossEntries", "plans", "loads", "balances", "planningEntries", "nextDayDestinations"] as const;
type Section = { digest: string; json: string };
type SavedBoard = { scope: string; sections: Map<string, Section> };

// Keep one bounded snapshot per board in this tab. Only the server can confirm
// which sections still match the current week, permissions and committed rows.
export class BoardSections {
  private saved = new Map<string, SavedBoard>();
  private generation = 0;

  clear() {
    this.generation++;
    this.saved.clear();
  }

  async read<T>(board: string, scope: string, fetchBoard: (offer?: string) => Promise<T>): Promise<T> {
    const generation = this.generation;
    const cached = this.saved.get(board);
    const previous = cached?.scope === scope ? cached.sections : undefined;
    const offer = ["v1", ...Array.from(previous ?? [], ([name, part]) => name + "=" + part.digest)].join(",");
    const result = await fetchBoard(offer);
    if (!result || typeof result !== "object") return result;
    const payload = ("opening" in result && "board" in result ? result.board : result) as Record<string, unknown>;
    if (!payload || !("_sections" in payload)) return result; // Older server or full response.
    if (!payload._sections || typeof payload._sections !== "object" || Array.isArray(payload._sections)) return fetchBoard();
    const tags = payload._sections as Record<string, unknown>;
    delete payload._sections;
    const next = new Map<string, Section>();
    let bytes = 0;
    for (const name of sectionNames) {
      const digest = tags[name];
      if (typeof digest !== "string") continue; // Absent or inaccessible in this snapshot.
      let section = previous?.get(name);
      if (Object.hasOwn(payload, name)) {
        section = { digest, json: JSON.stringify(payload[name]) };
      } else if (section?.digest === digest) {
        payload[name] = JSON.parse(section.json);
      } else {
        // Recover with one ordinary full read if this response cannot be assembled.
        return fetchBoard();
      }
      const size = section.json.length * 2;
      if (bytes + size <= 4 * 1024 * 1024) {
        next.set(name, section);
        bytes += size;
      }
    }
    if (this.generation === generation) this.saved.set(board, { scope, sections: next });
    return result;
  }
}
