"use client";

import { Check, ChevronRight, Circle } from "lucide-react";
import type { MaintenanceRecord } from "../lib/types";

export function WorkOrderProgress({record,step,onStep}:{record:MaintenanceRecord;step:number;onStep:(step:number)=>void}) {
  const status=record.workStatus ?? record.status;
  const workDone=status==="completed" || status==="closed";
  const finished=status==="closed";
  const steps=[
    {name:"Created",detail:"Work order saved",done:true,enabled:true},
    {name:"Work",detail:workDone ? "Work finished" : status==="in_progress" ? "In progress" : "Ready to start",done:workDone,enabled:true},
    {name:"Invoice & payment",detail:finished ? record.noCharge ? "No charge" : "Payment confirmed" : "Final step",done:finished,enabled:workDone},
  ];
  return <nav aria-label="Work order progress" className="grid grid-cols-3 overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900/30">
    {steps.map((item,index)=><button key={item.name} type="button" aria-current={step===index ? "step" : undefined} disabled={!item.enabled} onClick={()=>onStep(index)} className={`relative flex items-center gap-3 px-3 py-4 text-left transition sm:px-5 ${step===index ? "bg-blue-500/10" : "hover:bg-zinc-800/30"} ${!item.enabled ? "opacity-40" : ""}`}>
      <span className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-xs ${item.done ? "bg-emerald-500/15 text-emerald-400" : step===index ? "bg-blue-500/20 text-blue-400" : "bg-zinc-800 text-zinc-500"}`}>{item.done ? <Check className="h-4 w-4" /> : step===index ? index+1 : <Circle className="h-3 w-3" />}</span>
      <span className="min-w-0"><span className="block text-xs font-medium text-zinc-200 sm:text-[13px]">{item.name}</span><span className="mt-1 hidden text-xs text-zinc-500 sm:block">{item.detail}</span></span>
      {index<2 && <ChevronRight className="absolute right-0 h-4 w-4 text-zinc-700" />}
    </button>)}
  </nav>;
}
