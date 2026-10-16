"use client";
import { useLayoutEffect, useState } from "react";
import { createPortal } from "react-dom";
import type { GrossBoardPresence } from "@/app/lib/types";

const colors = ["#2563eb", "#b45309", "#9333ea", "#047857", "#be185d", "#0e7490"];
type Cursor = { key: string; names: string; label: string; color: string; left: number; top: number; width: number; height: number };
export function GrossPresence({ peers }: { peers: GrossBoardPresence[] }) {
  const [cursors,setCursors] = useState<Cursor[]>([]);
  useLayoutEffect(() => {
    let frame=0;
    const update=()=>{
      frame=0;
      const groups = new Map<string,{ cell: HTMLElement; people: GrossBoardPresence[] }>();
      const roots = Array.from(document.querySelectorAll<HTMLElement>('[data-gross-driver]'));
      const dialog = document.querySelector('[data-gross-editor-day]');
      for(const p of peers){
        const field=p.column==="note"?"loadNumber":p.column;
        const matches=roots.filter(e=>e.dataset.grossDriver===p.driverId&&(e.dataset.grossDate??"")===p.date&&(e.dataset.grossField===field||e.querySelector(`[data-gross-field="${field}"]`)));
        const inDialog=matches.filter(e=>e.closest('[data-gross-editor-day]')&&Number(e.dataset.grossSlot)===p.slot);
        const root=dialog ? inDialog[0] : matches.find(e=>Number(e.dataset.grossSlot??0)===p.slot) ?? matches.find(e=>e.dataset.grossSlot==="-1");
        if(!root)continue;
        const cell=root.dataset.grossField===field ? root : root.querySelector<HTMLElement>(`[data-gross-field="${field}"]`);
        if(!cell)continue;
        const key=`${p.driverId}:${p.date}:${root.dataset.grossSlot??0}:${field}`;
        const group=groups.get(key)??{cell,people:[]};group.people.push(p);groups.set(key,group);
      }
      const result: Cursor[]=[];
      for(const [key,{cell,people}] of groups){
        const modal=cell.closest<HTMLElement>('[data-gross-editor-day]');
        const table=cell.closest('table');
        const area=modal??cell.closest<HTMLElement>('[data-scroll-key="gross-board"]');
        if(!area||!table)continue;
        const bounds=area.getBoundingClientRect(),rect=cell.getBoundingClientRect();
        const row=cell.closest('tr');
        const root=cell.closest('[data-gross-driver]');
        const frozen=!modal&&root instanceof HTMLTableCellElement&&root.cellIndex>1 ? row?.cells[1]?.getBoundingClientRect().right??bounds.left : bounds.left;
        const left=Math.max(rect.left,bounds.left+1,frozen),right=Math.min(rect.right,bounds.right-1,innerWidth);
        const top=Math.max(rect.top,bounds.top,modal?0:table.tHead?.getBoundingClientRect().bottom??0),bottom=Math.min(rect.bottom,bounds.bottom,innerHeight);
        if(right-left<12||bottom-top<12)continue;
        const p=people[0];
        const hash=Array.from(p.userId).reduce((s,ch)=>s+ch.charCodeAt(0),0);
        const names=Array.from(new Set(people.map(p=>p.userName))).join(", ");
        const driver=document.querySelector(`[data-gross-board-grid] [data-gross-driver="${p.driverId}"][data-gross-field="driver"]`)?.textContent??"Driver";
        result.push({key,names,label:`${names} is on ${driver} · ${p.date||p.weekStart} · ${p.column}${p.slot>=0&&p.date?` · Load ${p.slot+1}`:""}`,color:colors[hash%colors.length],left,top,width:right-left,height:bottom-top});
      }
      setCursors(result);
    };
    const schedule=()=>{if(!frame)frame=requestAnimationFrame(update)};
    const observer=new MutationObserver(records=>{if(records.some(r=>!(r.target instanceof Element&&r.target.closest('[data-gross-cursor-layer]'))))schedule()});
    observer.observe(document.body,{childList:true,subtree:true});
    document.addEventListener('scroll',schedule,true);window.addEventListener('resize',schedule);schedule();
    return()=>{cancelAnimationFrame(frame);observer.disconnect();document.removeEventListener('scroll',schedule,true);window.removeEventListener('resize',schedule)};
  },[peers]);
  if(!cursors.length)return null;
  return createPortal(<div data-gross-cursor-layer className="pointer-events-none fixed inset-0 z-[110]" aria-live="polite">{cursors.map(c=><div key={c.key} role="status" data-gross-presence={c.names} aria-label={c.label} className="fixed rounded-sm border-2" style={{left:c.left,top:c.top,width:c.width,height:c.height,borderColor:c.color}}><span className="absolute -top-4 right-0 max-w-40 truncate rounded-t px-1 text-[10px] leading-[14px] text-white" style={{backgroundColor:c.color}}>{c.names}</span></div>)}</div>,document.body);
}
