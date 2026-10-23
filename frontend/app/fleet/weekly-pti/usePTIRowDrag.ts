"use client";
import {useLayoutEffect,useRef,useState,type PointerEvent as ReactPointerEvent} from "react";
import type {PTIGroup,PTIMatrix} from "@/app/lib/types";
type Truck=PTIMatrix["trucks"][number];
type Drop={group:PTIGroup;target?:string;after:boolean};
export function usePTIRowDrag(trucks:Truck[],move:(id:string,drop:Drop,version:number)=>Promise<void>){
 const [frozen,setFrozen]=useState<Truck[]|null>(null),cleanup=useRef<(()=>void)|null>(null);
 useLayoutEffect(()=>()=>cleanup.current?.(),[]);
 function start(event:ReactPointerEvent<HTMLButtonElement>,truck:Truck,version:number){
  if(event.button!==0||!event.isPrimary||cleanup.current)return;
  const table=event.currentTarget.closest("table"),source=event.currentTarget.closest<HTMLTableRowElement>("tr");if(!table||!source)return;
  const scroller=table.parentElement!;event.preventDefault();event.stopPropagation();
  const original=source.getAttribute("style"),pointer=event.pointerId,startY=event.clientY,startScroll=scroller.scrollTop;
  let y=startY,frame=0,drop:Drop|null=null,indicator:HTMLElement|null=null,active=true,disposed=false;
  const clearIndicator=()=>{if(indicator)indicator.style.boxShadow="";indicator=null;};
  const update=()=>{
   source.style.transform=`translateY(${y-startY+scroller.scrollTop-startScroll}px)`;
   clearIndicator();drop=null;
   const bounds=scroller.getBoundingClientRect(),head=table.tHead?.getBoundingClientRect();
   if(y<Math.max(bounds.top,head?.bottom??0)||y>bounds.bottom)return;
   const candidates=Array.from(table.querySelectorAll<HTMLElement>("[data-pti-truck],[data-pti-group]"));
   let nearest:HTMLElement|null=null,distance=Infinity;
   for(const el of candidates){if(el===source)continue;const box=el.getBoundingClientRect(),d=y<box.top?box.top-y:y>box.bottom?y-box.bottom:0;if(d<distance){distance=d;nearest=el;}}
   if(!nearest)return;
   const group=(nearest.dataset.ptiGroup??nearest.dataset.ptiCategory) as PTIGroup;
   if(nearest.dataset.ptiGroup){const first=trucks.find(t=>t.group===group&&t.id!==truck.id);drop={group,target:first?.id,after:false};}
   else drop={group,target:nearest.dataset.ptiTruck,after:y>nearest.getBoundingClientRect().top+nearest.getBoundingClientRect().height/2};
   indicator=nearest;indicator.style.boxShadow=drop.after?"inset 0 -3px #60a5fa":"inset 0 3px #60a5fa";
  };
  let previous=performance.now();
  const tick=(now:number)=>{if(!active)return;const elapsed=Math.min(32,now-previous);previous=now;const box=scroller.getBoundingClientRect(),header=table.tHead?.getBoundingClientRect().height??0;const upper=box.top+header+30,lower=box.bottom-30;scroller.scrollTop+=(y<upper?-Math.min(1,(upper-y)/30):y>lower?Math.min(1,(y-lower)/30):0)*elapsed*0.6;update();frame=requestAnimationFrame(tick);};
  const dispose=()=>{
   active=false;cancelAnimationFrame(frame);clearIndicator();window.removeEventListener("pointermove",onMove);window.removeEventListener("pointerup",onUp);window.removeEventListener("pointercancel",onCancel);window.removeEventListener("keydown",onKey,true);window.removeEventListener("blur",onCancel);window.removeEventListener("resize",onCancel);table.removeEventListener("lostpointercapture",onCancel);
   if(table.hasPointerCapture(pointer))table.releasePointerCapture(pointer);
   if(original===null)source.removeAttribute("style");else source.setAttribute("style",original);delete source.dataset.rowDragging;
  };
  const finish=async(cancel:boolean)=>{if(!active)return;if(!cancel)update();const destination=drop;dispose();try{if(!cancel&&destination)await move(truck.id,destination,version);}finally{cleanup.current=null;if(!disposed)setFrozen(null);}};
  const onMove=(e:PointerEvent)=>{if(e.pointerId===pointer){y=e.clientY;e.preventDefault();update();}};
  const onUp=(e:PointerEvent)=>{if(e.pointerId===pointer){y=e.clientY;void finish(false);}};
  const onCancel=()=>{void finish(true);};const onKey=(e:KeyboardEvent)=>{if(e.key==="Escape"){e.preventDefault();e.stopPropagation();onCancel();}};
  cleanup.current=()=>{disposed=true;dispose();};source.style.position="relative";source.style.zIndex="15";source.style.opacity="0.85";source.style.outline="2px solid #60a5fa";source.dataset.rowDragging="true";
  table.setPointerCapture(pointer);window.addEventListener("pointermove",onMove,{passive:false});window.addEventListener("pointerup",onUp);window.addEventListener("pointercancel",onCancel);window.addEventListener("keydown",onKey,true);window.addEventListener("blur",onCancel);window.addEventListener("resize",onCancel);table.addEventListener("lostpointercapture",onCancel);setFrozen(trucks);frame=requestAnimationFrame(tick);
 }
 return {trucks:frozen??trucks,active:frozen!==null,start};
}
