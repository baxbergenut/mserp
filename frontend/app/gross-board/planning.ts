import type { GrossBoardPlanningEntry, GrossBoardPlanningKind } from "@/app/lib/types";

export type PlanningDrafts = Partial<Record<GrossBoardPlanningKind, GrossBoardPlanningEntry>>;
export const planningKey = (entry: Pick<GrossBoardPlanningEntry, "driverId" | "date" | "kind">) => [entry.driverId, entry.date, entry.kind].join(":");
export const planningLength = (text: string) => Array.from(text).length;

// Cleared rows remain versioned. Old snapshots and late acknowledgements cannot
// remove a newer note, destination or clear operation.
export function mergePlanningEntries(current: GrossBoardPlanningEntry[] = [], incoming: GrossBoardPlanningEntry[] = []) {
  const entries = new Map(current.map(entry => [planningKey(entry), entry]));
  for (const entry of incoming) {
    const previous = entries.get(planningKey(entry));
    if (!previous || entry.version >= previous.version) entries.set(planningKey(entry), entry);
  }
  return Array.from(entries.values());
}

export function acknowledgePlanningDraft(drafts: PlanningDrafts, sent: GrossBoardPlanningEntry, saved: GrossBoardPlanningEntry): PlanningDrafts {
  const next = { ...drafts }, current = next[sent.kind];
  if (!current) return next;
  if (current.text === sent.text) delete next[sent.kind];
  else next[sent.kind] = { ...current, version: saved.version };
  return next;
}

export function readPlanningDrafts(key: string, driverId: string, date: string): PlanningDrafts {
  try {
    const raw = JSON.parse(sessionStorage.getItem(key) ?? "null");
    if (!raw || !Number.isFinite(raw.savedAt) || Date.now() - raw.savedAt > 7 * 86400000) return {};
    const drafts: PlanningDrafts = {};
    for (const kind of ["note", "destination"] as const) {
      const entry = raw.drafts?.[kind];
      if (entry?.driverId === driverId && entry.date === date && entry.kind === kind && typeof entry.text === "string"
        && Number.isSafeInteger(entry.version) && entry.version >= 0) drafts[kind] = entry;
    }
    return drafts;
  } catch { return {}; }
}

export function writePlanningDrafts(key: string, drafts: PlanningDrafts) {
  try {
    if (Object.keys(drafts).length) sessionStorage.setItem(key, JSON.stringify({ savedAt: Date.now(), drafts }));
    else sessionStorage.removeItem(key);
    return true;
  } catch { return false; }
}
