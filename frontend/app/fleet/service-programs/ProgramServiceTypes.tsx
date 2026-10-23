"use client";

import { useId, useRef, useState } from "react";
import { ChevronDown } from "lucide-react";
import type { MaintenanceOptions, ServiceProgram } from "@/app/lib/types";
import { controlClass } from "@/app/components/management/ManagementUI";

export function ProgramServiceTypes({program,options,onChange}:{program:ServiceProgram;options:MaintenanceOptions;onChange:(codes:string[])=>void}) {
  const id=useId(), panel=useRef<HTMLDivElement>(null);
  const [open,setOpen]=useState(false);
  const selected=program.serviceTypeCodes??[];
  const types=options.types.filter(t=>t.active||selected.includes(t.code));
  const names=selected.map(code=>options.types.find(t=>t.code===code)?.name??code);
  return <>
    <button type="button" aria-label="Resetting expense types" aria-expanded={open} popoverTarget={id} className={`${controlClass} flex items-center justify-between gap-2 text-left`} onClick={e=>{
      const rect=e.currentTarget.getBoundingClientRect();
      if(panel.current){panel.current.style.left=`${rect.left}px`;panel.current.style.top=`${rect.bottom+4}px`;panel.current.style.width=`${rect.width}px`;panel.current.style.maxHeight=`${Math.max(100,innerHeight-rect.bottom-16)}px`;}
    }}><span className="truncate">{names.join(", ")||"Select expense types"}</span><ChevronDown className="h-4 w-4 shrink-0" /></button>
    <div id={id} ref={panel} popover="auto" role="group" aria-label="Resetting expense type choices" onToggle={e=>setOpen(e.newState==="open")} className="fixed m-0 max-w-[calc(100vw-16px)] overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-900 p-2 text-sm text-zinc-200 shadow-xl">
      {types.map(t=>{const elsewhere=!!t.programId&&t.programId!==program.id;return <label key={t.code} className={`flex min-h-9 items-center gap-2 rounded px-2 py-1 ${elsewhere?"text-zinc-500":"cursor-pointer hover:bg-zinc-800"}`}>
        <input type="checkbox" aria-label={`Reset with ${t.name}`} disabled={elsewhere} checked={selected.includes(t.code)} onChange={e=>onChange(e.target.checked?[...selected,t.code]:selected.filter(code=>code!==t.code))} />
        <span>{t.name}{!t.active&&" (archived)"}{elsewhere&&<span className="block text-xs">Resets {options.programs?.find(p=>p.id===t.programId)?.name??"another program"}</span>}</span>
      </label>;})}
      {!types.length&&<p className="p-2 text-zinc-400">No expense types available.</p>}
    </div>
    <p className="mt-1.5 text-xs text-zinc-500">Recording a Maintenance expense with a selected type resets this program.</p>
  </>;
}
