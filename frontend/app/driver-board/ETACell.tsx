"use client";

import type { BoardAutomation } from "@/app/lib/types";
import { parseETA } from "./board";

export function appointmentCellText(value: string, automation?: BoardAutomation) {
  const stop = automation?.stopType === "pickup" ? "PU" : automation?.stopType === "delivery" ? "DEL" : "";
  const schedule = automation?.appointment?.label || (automation?.arrived ? stop : "");
  const prefix = schedule && automation?.arrived ? "@" : "";
  const manual = parseETA(value);
  const travel = automation?.travelMode === "auto" ? automation.travelValue :
    automation?.manualETAValid === false ? "" : manual?.miles ? `${manual.miles}mi` : manual?.time ? `ETA ${manual.time}` : value;
  return { schedule: prefix + schedule, travel, full: [prefix + schedule, travel].filter(Boolean).join(" ") };
}

export function ETACell({ name, value, automation, disabled, expanded, onOpen }: {
  name: string; value: string; automation?: BoardAutomation; disabled: boolean; expanded: boolean;
  onOpen: (anchor: HTMLButtonElement) => void;
}) {
  const text = appointmentCellText(value, automation);
  const parts = text.schedule.split(" ");
  const observed = automation?.observedAt && !automation.observedAt.startsWith("0001") ? new Date(automation.observedAt).toLocaleString("en-US", { timeZone: "America/New_York" }) + " NY" : "No arrival observation";
  const arrival = automation?.arrivalOverride === "auto" ? `Within 1 mile of selected stop (straight-line GPS) · ${observed}${automation.stale ? " · last known, GPS stale" : ""}` : "Marked at stop manually";
  const title = [automation?.appointment?.detail, "ETA uses New York time", automation?.arrived ? arrival : "", automation?.reason].filter(Boolean).join("\n");
  return <button type="button" data-grid-action aria-label={`${name} · ETA`} title={title} disabled={disabled}
    className="block h-8 w-full truncate whitespace-nowrap px-1.5 text-left text-[11px] text-zinc-200 hover:bg-zinc-800 focus:ring-1 focus:ring-inset focus:ring-blue-500"
    aria-haspopup="dialog" aria-expanded={expanded} onClick={event => onOpen(event.currentTarget)}>
    {text.schedule && <><span className={automation?.arrived ? "text-red-500" : ""}>{parts[0]}</span>{" "}{parts.slice(1).join(" ")}</>}
    {text.schedule && text.travel && " "}<span className="text-red-500">{text.travel}</span>
    {!text.full && "—"}
  </button>;
}
