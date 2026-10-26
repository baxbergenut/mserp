import { TriangleAlert } from "lucide-react";
import type { Truck } from "@/app/lib/types";

export const gpsAlertLabels = { overdue: "GPS update overdue", missing: "No GPS data" } as const;

export function TruckGPSHealth({ truck, detail = false }: { truck: Truck; detail?: boolean }) {
  const label = truck.gpsAlert ? gpsAlertLabels[truck.gpsAlert] : null;
  const reported = truck.gpsReportedAt ? new Intl.DateTimeFormat("en-US", {
    dateStyle: "medium", timeStyle: "short", timeZone: "America/New_York",
  }).format(new Date(truck.gpsReportedAt)) + " ET" : null;
  const source = truck.gpsSource === "samsara" ? "Samsara" : truck.gpsSource === "five_eld" ? "Five ELD" : null;
  const lastReport = reported ? `Last GPS report: ${reported} · ${source}` : "No matched GPS report available.";
  if (!detail && !label) return null;
  return <div className={detail ? "space-y-1 text-xs" : "mt-1.5"}>
    {label && <span title={`${lastReport} ${truck.gpsAlert === "overdue" ? "No GPS report in over seven days." : ""} This does not confirm whether the truck moved.`}
      className="inline-flex items-center gap-1 rounded-md bg-amber-500/10 px-2 py-1 text-[11px] font-medium text-amber-400">
      <TriangleAlert aria-hidden="true" className="h-3 w-3 shrink-0" />{label}
    </span>}
    {detail && <>
      <p className="text-zinc-400">{lastReport}</p>
      {label && <p className="text-zinc-500">{truck.gpsAlert === "overdue" ? "No GPS report in over seven days. " : ""}This does not confirm whether the truck moved.</p>}
    </>}
  </div>;
}
