"use client";
import { useCallback, useEffect, useState } from "react";
import { Truck } from "lucide-react";
import { fetchTruckDataTruck, retryTruckDataTruck } from "../lib/api";
import { usePermissions } from "../lib/access";
import type { DataTruckEnrollment } from "../lib/types";
import { ErrorBanner } from "../components/management/ManagementUI";

export function DataTruckSetup({truckId}:{truckId:string}) {
 const permissions=usePermissions();
 const [data,setData]=useState<DataTruckEnrollment|null>(null),[error,setError]=useState(""),[busy,setBusy]=useState(false);
 const refresh=useCallback(async()=>{try{setData(await fetchTruckDataTruck(truckId));setError("");}catch(e){setError(e instanceof Error?e.message:"Could not check DataTruck setup.");}},[truckId]);
 useEffect(()=>{const controller=new AbortController();fetchTruckDataTruck(truckId,controller.signal).then(setData).catch(e=>{if(!controller.signal.aborted)setError(e.message);});return()=>controller.abort();},[truckId]);
 useEffect(()=>{if(!data||!["queued","running"].includes(data.state))return;const timer=setInterval(()=>void refresh(),3000);return()=>clearInterval(timer);},[data,refresh]);
 if(!error&&(!data||data.state==="not_requested"))return null;
 const labels:Record<string,string>={queued:"Waiting to add",running:"Adding…",done:"Added",review:"Needs attention",unconfirmed:"Check result",canceled:"Setup canceled"};
 return <section aria-label="DataTruck setup" className="space-y-3 rounded-xl border border-zinc-800 bg-zinc-900/30 p-4">
  <div className="flex items-center justify-between gap-3"><a href="https://app.datatruck.io/fleet-management/main/trucks" target="_blank" rel="noreferrer" className="flex items-center gap-2 text-sm font-semibold text-zinc-200 hover:text-blue-400"><Truck className="h-4 w-4"/>DataTruck</a><span role="status" className={`text-xs ${data?.state==="done"?"text-emerald-400":"text-zinc-400"}`}>{data?labels[data.state]:""}</span></div>
  {error&&<ErrorBanner message={error}/>}
  {data?.message&&<p className="text-xs leading-5 text-zinc-400">{data.message}</p>}
  {data?.providerId&&<p className="text-xs text-zinc-500">DataTruck ID: {data.providerId}</p>}
  {data?.state==="review"&&permissions.includes("trucks.create")&&<button type="button" className="ui-button !text-xs" disabled={busy} onClick={async()=>{setBusy(true);try{await retryTruckDataTruck(truckId,data.version);await refresh();}catch(e){setError(e instanceof Error?e.message:"Could not retry enrollment.");}finally{setBusy(false);}}}>Retry enrollment</button>}
 </section>;
}
