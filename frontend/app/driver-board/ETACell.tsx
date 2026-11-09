"use client";

import { useId } from "react";
import { RefreshCw } from "lucide-react";
import type { BoardAutomation, BoardStop } from "@/app/lib/types";
import { parseETA } from "./board";
import { facilityETATitle, roundETAClock } from "./eta";

export function appointmentCellText(value: string, automation?: BoardAutomation) {
  const stop = automation?.stopType === "pickup" ? "PU" : automation?.stopType === "delivery" ? "DEL" : "";
  const schedule = automation?.appointment?.label || (automation?.arrived ? stop : "");
  const prefix = schedule && automation?.arrived ? "@" : "";
  const manual = parseETA(value);
  const travel = automation?.arrived ? "" : value && manual && automation?.manualETAValid !== false ?
    manual.miles ? `${manual.miles}mi` : `ETA ${roundETAClock(manual.time)}` : automation?.travelMode === "auto" ? automation.travelValue.replace(/^ETA (\d{2}:\d{2})$/, (_, clock) => `ETA ${roundETAClock(clock)}`) : automation?.manualETAValid === false ? "" : value;
  return { schedule: prefix + schedule, travel, full: [prefix + schedule, travel].filter(Boolean).join(" ") };
}

export function canRetryAutomaticETA(value: string, automation?: BoardAutomation) {
  return !!automation?.context && automation.routingEnabled && automation.travelMode === "auto" &&
    !automation.requiresConfirmation && !automation.arrived && !appointmentCellText(value, automation).travel;
}

export function ETACell({ name, value, automation, stop, disabled, expanded, onOpen, canRetry = false, retryDisabled = false, retrying = false, onRetry }: {
  canRetry?: boolean; retryDisabled?: boolean; retrying?: boolean; onRetry?: () => void;
  name: string; value: string; automation?: BoardAutomation; stop?: BoardStop; disabled: boolean; expanded: boolean;
  onOpen: (anchor: HTMLButtonElement) => void;
}) {
  const freshnessId = useId();
  const text = appointmentCellText(value, automation);
  const parts = text.schedule.split(" ");
  const title = facilityETATitle(value, automation, stop);
  const automaticArrival = !!text.schedule && automation?.arrived && automation.arrivalOverride === "auto";
  const automaticTravel = !!text.travel && (!value || automation?.manualETAValid === false) && automation?.travelMode === "auto";
  const outdated = automaticArrival ? automation.stale : automation?.travelStale;
  const freshnessColor = outdated ? "text-yellow-400" : "text-emerald-400";
  const freshness = automaticArrival || automaticTravel ? `${automaticArrival ? "Arrival observation" : "ETA"} ${outdated ? "outdated" : "up to date"}` : undefined;
  const showRetry = canRetry && !!onRetry && canRetryAutomaticETA(value, automation);
  const pending = retrying || !!automation?.travelRetryPending;
  return <div className="flex h-8 w-full min-w-0 items-center">
    <button type="button" data-grid-action aria-label={`${name} · ETA`} aria-describedby={freshness ? freshnessId : undefined} title={title} disabled={disabled}
      className="block h-8 min-w-0 flex-1 truncate whitespace-nowrap px-1.5 text-left text-[11px] text-zinc-200 hover:bg-zinc-800 focus:ring-1 focus:ring-inset focus:ring-blue-500"
      aria-haspopup="dialog" aria-expanded={expanded} onClick={event => onOpen(event.currentTarget)}>
      {text.schedule && <><span className={automaticArrival ? freshnessColor : undefined}>{parts[0]}</span>{" "}{parts.slice(1).join(" ")}</>}
      {text.schedule && text.travel && " "}<span className={automaticTravel ? freshnessColor : undefined}>{text.travel}</span>
      {!text.full && "—"}
    </button>
    {freshness && <span id={freshnessId} className="sr-only">{freshness}</span>}
    {showRetry && <button type="button" data-grid-action aria-label={`${name} · Retry automatic ETA`}
      title={pending ? "Recalculating automatic ETA now" : "Retry automatic ETA calculation"}
      disabled={retryDisabled || pending} aria-busy={pending}
      onClick={event => { event.stopPropagation(); onRetry?.(); }}
      className="mr-1 flex h-6 w-6 shrink-0 items-center justify-center rounded text-zinc-400 enabled:hover:bg-zinc-700 enabled:hover:text-zinc-100 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-blue-500 disabled:opacity-40">
      <RefreshCw aria-hidden="true" className={`h-3 w-3${pending ? " animate-spin" : ""}`} />
    </button>}
  </div>;
}
