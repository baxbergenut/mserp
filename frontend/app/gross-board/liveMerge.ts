import type { GrossBoard } from "@/app/lib/types";
import { entryKey } from "./board";

export function mergeGrossLive(current: GrossBoard | null, incoming: GrossBoard, protectedKeys: Set<string>, protectedDays: Set<string>) {
  if (!current || current.weekStart !== incoming.weekStart) return incoming;
  const previous = new Map(current.entries.map(e => [entryKey(e.driverId,e.date,e.slot), e]));
  const next = new Map(incoming.entries.map(e => [entryKey(e.driverId,e.date,e.slot), e]));
  for (const [key, e] of next) if (protectedDays.has(`${e.driverId}:${e.date}`)) protectedKeys.add(key);
  for (const [key, e] of previous) {
    if (protectedDays.has(`${e.driverId}:${e.date}`) || (next.get(key)?.version ?? -1) < e.version) protectedKeys.add(key);
  }
  for (const key of protectedKeys) { const e = previous.get(key); if (e) next.set(key,e); else next.delete(key); }
  return { ...incoming, entries: Array.from(next.values()) };
}
