import tzLookup from "tz-lookup";
import type { BoardAutomation, BoardStop } from "@/app/lib/types";
import { parseETA } from "./board";

export function roundETAClock(clock: string) {
  const match = /^([01]\d|2[0-3]):([0-5]\d)$/.exec(clock);
  if (!match) return clock;
  const minutes = Math.round((Number(match[1]) * 60 + Number(match[2])) / 15) * 15 % 1440;
  return `${String(Math.floor(minutes / 60)).padStart(2, "0")}:${String(minutes % 60).padStart(2, "0")}`;
}

// Manual times retain their existing New York input convention. Resolve the
// calendar date before rounding so midnight and DST conversions stay correct.
function manualArrival(value: string, clock: string, now: Date) {
  const date = /^\d{4}-\d{2}-\d{2}/.exec(value)?.[0] ?? new Intl.DateTimeFormat("en-CA", {
    timeZone: "America/New_York", year: "numeric", month: "2-digit", day: "2-digit",
  }).format(now);
  const wall = Date.parse(`${date}T${clock}:00Z`);
  let instant = wall;
  const format = new Intl.DateTimeFormat("en-CA", { timeZone: "America/New_York", year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" });
  for (let i = 0; i < 3; i++) {
    const p = Object.fromEntries(format.formatToParts(new Date(instant)).map(part => [part.type, part.value]));
    instant += wall - Date.parse(`${p.year}-${p.month}-${p.day}T${p.hour}:${p.minute}:${p.second}Z`);
  }
  return new Date(instant);
}

export function facilityETATitle(value: string, automation?: BoardAutomation, stop?: BoardStop, now = new Date()) {
  if (automation?.arrived || stop?.latitude == null || stop.longitude == null) return "";
  const manual = value && parseETA(value);
  let arrival: Date;
  if (manual && !manual.miles && manual.time && automation?.manualETAValid !== false) {
    arrival = manualArrival(value, manual.time, now);
  } else if ((!value || automation?.manualETAValid === false) && !automation?.travelStale && automation?.travelArrivalAt) {
    arrival = new Date(automation.travelArrivalAt);
  } else return "";
  if (!Number.isFinite(arrival.getTime())) return "";
  try {
    const timeZone = tzLookup(stop.latitude, stop.longitude);
    const rounded = new Date(Math.round(arrival.getTime() / 900000) * 900000);
    return `ETA ${rounded.toLocaleString("en-US", { timeZone, month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", hourCycle: "h23", timeZoneName: "short" })}`;
  } catch { return ""; }
}
