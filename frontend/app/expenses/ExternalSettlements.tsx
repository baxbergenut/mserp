"use client";

import { useEffect, useState } from "react";
import { usePermissions } from "@/app/lib/access";
import { fetchExternalSettlements, recordExternalSettlement } from "@/app/lib/api";
import type { Expense, ExternalExpenseSettlement } from "@/app/lib/types";
import { controlClass } from "@/app/components/management/ManagementUI";

export function ExternalSettlements({expense,onSaved}:{expense:Expense;onSaved:()=>Promise<void>}){
 const canSettle=usePermissions().includes("expenses.settle");
 const [entries,setEntries]=useState<ExternalExpenseSettlement[]>([]),[open,setOpen]=useState(false),[amount,setAmount]=useState(""),[date,setDate]=useState(""),[reference,setReference]=useState(""),[error,setError]=useState(""),[busy,setBusy]=useState(false),[reverse,setReverse]=useState<ExternalExpenseSettlement|null>(null);
 const [request,setRequest]=useState<{fingerprint:string;id:string}|null>(null);
 useEffect(()=>{const c=new AbortController();fetchExternalSettlements(expense.id,c.signal).then(setEntries).catch(e=>{if(!c.signal.aborted)setError(e.message)});return()=>c.abort()},[expense.id]);
 if(expense.managedInSheets)return <p className="mt-4 rounded-lg border border-zinc-700 p-3 text-sm text-amber-200">Managed in Sheets · historical balances are excluded from Clustie payroll.</p>;
 async function save(){
  const values={amount:reverse?.amount??amount,settledOn:date,reference,reversesId:reverse?.id??"",version:expense.balanceVersion};const fingerprint=JSON.stringify(values),id=request?.fingerprint===fingerprint?request.id:crypto.randomUUID();setRequest({fingerprint,id});setBusy(true);setError("");
  try{setEntries(await recordExternalSettlement(expense.id,{...values,id}));await onSaved();setOpen(false);setReverse(null);setAmount("");setReference("");setRequest(null)}catch(e){setError(e instanceof Error?e.message:"Unable to save settlement")}finally{setBusy(false)}
 }
 const reversed=new Set(entries.map(e=>e.reversesId));
 return <section className="mt-4 space-y-3 border-t border-zinc-800 pt-4">
  {canSettle&&expense.remainingAmount!=null&&<button type="button" className="text-sm text-blue-400" onClick={()=>{setOpen(!open);setReverse(null)}}>Record outside Clustie</button>}
  {open&&<div className="space-y-3 rounded-lg border border-zinc-700 p-3"><p className="text-sm">{reverse?"Reverse external settlement":"Record a recovery or reimbursement already handled outside Clustie"}. This records history and does not send money.</p><div className="grid gap-2 sm:grid-cols-3"><label className="text-xs">Amount<input aria-label="External settlement amount" className={controlClass} inputMode="decimal" disabled={busy||!!reverse} value={reverse?.amount??amount} onChange={e=>setAmount(e.target.value)}/></label><label className="text-xs">Actual date<input aria-label="External settlement date" className={controlClass} type="date" disabled={busy} value={date} onChange={e=>setDate(e.target.value)}/></label><label className="text-xs">Reference / reversal reason<input aria-label="External settlement reference" className={controlClass} disabled={busy} value={reference} onChange={e=>setReference(e.target.value)}/></label></div><button type="button" disabled={busy||!date||!reference.trim()||(!reverse&&!amount)} onClick={()=>void save()} className="rounded border border-zinc-700 px-3 py-1 text-sm disabled:opacity-40">{busy?"Saving…":reverse?"Record reversal":"Record settlement"}</button></div>}
  {error&&<p role="alert" className="text-sm text-red-300">{error}</p>}
  {entries.length>0&&<ul className="space-y-2 text-xs text-zinc-400">{entries.map(e=><li key={e.id}>{e.settledOn} · {e.reversesId?"Reversed":"Recorded externally"} {e.amount} · {e.reference} · {e.actor}{canSettle&&!e.reversesId&&!reversed.has(e.id)&&<button type="button" className="ml-2 text-blue-400" onClick={()=>{setReverse(e);setOpen(true);setReference("")}}>Reverse</button>}</li>)}</ul>}
 </section>;
}
