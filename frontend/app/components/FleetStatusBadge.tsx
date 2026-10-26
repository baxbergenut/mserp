import type { TruckOperatingStatus, TruckStatus } from "../lib/types";

export function operatingStatus(status:TruckStatus):TruckOperatingStatus {
  return status==="maintenance" ? "shop" : status==="out_of_service" ? "out_of_service" : status==="terminated" ? "terminated" : status==="assigned" ? "in_use" : "ready";
}

export function FleetStatusBadge({status,showReady=false}:{status?:TruckOperatingStatus|"hold"|"";showReady?:boolean}) {
  if (!status || (status==="ready" || status==="in_use") && !showReady) return null;
  const labels={ready:"Ready",shop:"Shop",out_of_service:"OOS",in_use:"In use",terminated:"Terminated",hold:"Shop"};
  const colors={ready:"bg-emerald-500/10 text-emerald-400",shop:"bg-amber-500/10 text-amber-400",out_of_service:"bg-red-500/10 text-red-400",hold:"bg-amber-500/10 text-amber-400",in_use:"bg-blue-500/10 text-blue-400",terminated:"bg-zinc-500/10 text-zinc-400"};
  return <span className={`inline-flex rounded px-1.5 py-0.5 font-sans text-[10px] font-medium ${colors[status]}`}>{labels[status]}</span>;
}
