import type { BoardLoads, DriverBoardEntry } from "@/app/lib/types";

// Preview a draft immediately; autosave resolves the same match transactionally.
export function previewCurrentLoad(loads: BoardLoads | undefined, entry: DriverBoardEntry, saved?: DriverBoardEntry): BoardLoads | undefined {
  if (!loads) return loads;
  const number = entry.currentLoad.trim().toLowerCase();
  const aligned = !loads.current || loads.current.number.trim().toLowerCase() === number;
  if (aligned && !entry.resolveCurrentLoad && entry.currentLoad === saved?.currentLoad) return loads;
  const matches = loads.week.filter(p => p.number.trim().toLowerCase() === number);
  const chosen = matches[0];
  const unique = chosen && (matches.length === 1 || (chosen.loadId !== null && matches.every(p => p.loadId === chosen.loadId)));
  const current = unique ? chosen : null;
  const stop = current?.stops.findLast(s => s.type.toLowerCase() === "delivery" && s.location.trim())
    ?? current?.stops.find(s => s.type.toLowerCase() === "pickup" && s.location.trim());
  return { ...loads, current, destinationSource: !!stop && !entry.destination, sourceDestination: stop?.location ?? "", stopKey: stop?.key ?? "" };
}

// ETA is a New York wall-clock date with optional minute precision. Keep legacy
// text untouched; never guess a date from values such as "Friday afternoon".
export function parseETA(value: string): { date: string; time: string } | null {
  if (!value) return { date: "", time: "" };
  const match = /^(\d{4}-\d{2}-\d{2})(?:T([01]\d|2[0-3]):([0-5]\d))?$/.exec(value);
  if (!match) return null;
  const date = new Date(`${match[1]}T12:00:00Z`);
  if (!Number.isFinite(date.getTime()) || date.toISOString().slice(0, 10) !== match[1]) return null;
  return { date: match[1], time: match[2] ? `${match[2]}:${match[3]}` : "" };
}

export function formatETA(value: string, full = false) {
  const eta = parseETA(value);
  if (!eta) return value;
  if (!eta.date) return "—";
  const [, month, day] = eta.date.split("-");
  const date = full ? eta.date : `${Number(month)}/${Number(day)}`;
  if (!eta.time) return `${date}${full ? " · Time not set" : ""}`;
  const [hours, minutes] = eta.time.split(":");
  const hour = Number(hours);
  return `${date} · ${hour % 12 || 12}:${minutes}${hour >= 12 ? "p" : "a"}${full ? "m · New York time" : ""}`;
}

export const statuses = ["ENROUTE", "DISPATCHED", "RESERVED", "HOME", "VACATION", "SHOP", "RESET", "NO LOAD", "STUCK", "LATE DEL", "TRUCK ISSUE", "LEFT", "NEW DRIVER", "DEADHEAD", "LOAD CANCELLED", "REJECTED"];
export function statusColor(status: string) {
  // Exact TODAY sheet fills (Driver Board, gid 1851742271, column J).
  // Use dark labels for readable contrast on the sheet's bright backgrounds.
  switch (status) {
    case "ENROUTE": return "bg-[#3d85c6] text-black";
    case "DISPATCHED": return "bg-[#fbbc04] text-black";
    case "RESERVED": return "bg-[#b7e1cd] text-black";
    case "HOME": return "bg-[#ff0000] text-black";
    case "VACATION": return "bg-[#e06666] text-black";
    case "SHOP": return "bg-[#ff9900] text-black";
    case "LEFT": return "bg-white text-black";
  }
  // Retain the existing palette for statuses without an equivalent sheet rule.
  if (["ENROUTE", "DEADHEAD"].includes(status)) return "bg-blue-500/15 text-blue-300";
  if (["DISPATCHED", "SHOP", "LATE DEL"].includes(status)) return "bg-amber-500/15 text-amber-300";
  if (["RESERVED", "NEW DRIVER"].includes(status)) return "bg-emerald-500/15 text-emerald-300";
  if (["HOME", "VACATION", "RESET"].includes(status)) return "bg-violet-500/15 text-violet-300";
  if (["STUCK", "TRUCK ISSUE", "REJECTED", "LOAD CANCELLED"].includes(status)) return "bg-red-500/15 text-red-300";
  return "text-zinc-400";
}

// An in-flight save may finish after more typing. Keep the new text, while
// advancing both concurrency tokens to the committed response.
export function reconcileDriverBoard(current: Record<string, DriverBoardEntry>, snapshot: Record<string, DriverBoardEntry>, committed: DriverBoardEntry[]) {
  const remaining = { ...current };
  for (const entry of committed) {
    const draft = current[entry.driverId];
    if (!draft || JSON.stringify(draft) === JSON.stringify(snapshot[entry.driverId])) delete remaining[entry.driverId];
    else remaining[entry.driverId] = { ...draft, version: entry.version, homeVersion: entry.homeVersion };
  }
  return remaining;
}
