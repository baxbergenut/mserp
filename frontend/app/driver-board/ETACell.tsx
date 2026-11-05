"use client";

import { Check, TriangleAlert } from "lucide-react";
import type { BoardAutomation, BoardStop } from "@/app/lib/types";
import { parseETA } from "./board";
import { facilityETATitle, roundETAClock } from "./eta";

export function appointmentCellText(value: string, automation?: BoardAutomation) {
  const stop = automation?.stopType === "pickup" ? "PU" : automation?.stopType === "delivery" ? "DEL" : "";
  const schedule = automation?.appointment?.label || (automation?.arrived ? stop : "");
  const prefix = schedule && automation?.arrived ? "@" : "";
  const manual = parseETA(value);
  const travel = automation?.arrived ? "" : value && manual && automation?.manualETAValid !== false ?
    manual.miles ? `${manual.miles}mi` : `ETA ${roundETAClock(manual.time)}` : automation?.travelMode === "auto" ? automation.travelValue.replace(/^ETA (\d{2}:\d{2})$/, (_, clock) => `ETA ${roundETAClock(clock)}`) : value;
  return { schedule: prefix + schedule, travel, full: [prefix + schedule, travel].filter(Boolean).join(" ") };
}

export function ETACell({ name, value, automation, stop, disabled, expanded, onOpen }: {
  name: string; value: string; automation?: BoardAutomation; stop?: BoardStop; disabled: boolean; expanded: boolean;
  onOpen: (anchor: HTMLButtonElement) => void;
}) {
  const text = appointmentCellText(value, automation);
  const parts = text.schedule.split(" ");
  const title = facilityETATitle(value, automation, stop);
  const automaticArrival = !!text.schedule && automation?.arrived && automation.arrivalOverride === "auto";
  const automaticTravel = !!text.travel && (!value || automation?.manualETAValid === false) && automation?.travelMode === "auto";
  const outdated = automaticArrival ? automation.stale : automation?.travelStale;
  const freshnessColor = outdated ? "text-red-400" : "text-emerald-400";
  const FreshnessIcon = outdated ? TriangleAlert : Check;
  const freshness = automaticArrival || automaticTravel ? `${automaticArrival ? "Arrival observation" : "ETA"} ${outdated ? "outdated" : "up to date"}` : undefined;
  return <button type="button" data-grid-action aria-label={`${name} · ETA`} aria-description={freshness} title={title} disabled={disabled}
    className="block h-8 w-full truncate whitespace-nowrap px-1.5 text-left text-[11px] text-zinc-200 hover:bg-zinc-800 focus:ring-1 focus:ring-inset focus:ring-blue-500"
    aria-haspopup="dialog" aria-expanded={expanded} onClick={event => onOpen(event.currentTarget)}>
    {text.schedule && <><span className={automaticArrival ? freshnessColor : undefined}>{parts[0]}</span>{" "}{parts.slice(1).join(" ")}</>}
    {text.schedule && text.travel && " "}<span className={automaticTravel ? freshnessColor : undefined}>{text.travel}</span>
    {freshness && <FreshnessIcon aria-hidden="true" className={`ml-1 inline-block size-3 align-text-bottom ${freshnessColor}`} />}
    {!text.full && "—"}
  </button>;
}
