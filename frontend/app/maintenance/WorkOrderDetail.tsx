"use client";

import { statusClass } from "@/app/lib/statusColors";

import { useEffect, useRef, useState } from "react";
import { useSearchParams } from "next/navigation";
import { CheckCircle2, FileText, History, LoaderCircle, Wrench } from "lucide-react";
import Link from "../components/AccessLink";
import { fetchMaintenance, fetchMaintenanceHistory, fetchMaintenanceOptions, maintenanceDocumentUrl, saveMaintenance, uploadMaintenanceDocument } from "../lib/api";
import type { MaintenanceChange, MaintenanceInput, MaintenanceOptions, MaintenanceRecord, WorkOrderStatus } from "../lib/types";
import { useExpenseCategoryAccess } from "../lib/access";
import { controlClass, ErrorBanner, ManagementHeader, Modal } from "../components/management/ManagementUI";
import { FleetStatusBadge } from "../components/FleetStatusBadge";
import { ReceiptUpload } from "../components/ReceiptUpload";
import { MaintenanceForm } from "./MaintenanceForm";
import { MaintenanceWorkspace } from "./MaintenanceWorkspace";
import { WorkOrderProgress } from "./WorkOrderProgress";
import { maintenanceError, maintenanceToInput, normalizeMaintenance, today, workStatusLabels } from "./maintenance";

const dateTime=(value:string)=>new Date(value).toLocaleString("en-US",{month:"short",day:"numeric",year:"numeric",hour:"numeric",minute:"2-digit",timeZone:"America/New_York"});

export function WorkOrderDetail() {
  const id=useSearchParams().get("id") ?? "";
  const [record,setRecord]=useState<MaintenanceRecord|null>(null);const [draft,setDraft]=useState<MaintenanceInput|null>(null);
  const [options,setOptions]=useState<MaintenanceOptions|null>(null);const [step,setStep]=useState(1);
  const [error,setError]=useState("");const [notice,setNotice]=useState("");const [busy,setBusy]=useState(false);
  const [attachment,setAttachment]=useState<File|null>(null);const [changes,setChanges]=useState<MaintenanceChange[]|null>(null);
  const [editing,setEditing]=useState(false);
  const [cancelReason,setCancelReason]=useState<string|null>(null);const lock=useRef(false);
  const access=useExpenseCategoryAccess();const canEdit=record?.canEdit ?? !!access.find(a=>a.categoryId===options?.categoryId)?.canEdit;
  useEffect(()=>{let active=true;Promise.all([fetchMaintenance(id),fetchMaintenanceOptions()]).then(([r,o])=>{if(active){setRecord(r);setDraft(maintenanceToInput(r));setOptions(o);setStep(["completed","closed"].includes(r.workStatus ?? r.status)?2:1);setError("");}}).catch(e=>{if(active)setError(e.message);});return()=>{active=false;};},[id]);
  const dirty=!!record && !!draft && JSON.stringify(draft)!==JSON.stringify(maintenanceToInput(record));
  useEffect(()=>{if(!dirty)return;const before=(e:BeforeUnloadEvent)=>{e.preventDefault();e.returnValue="";};window.addEventListener("beforeunload",before);return()=>window.removeEventListener("beforeunload",before);},[dirty]);
  useEffect(()=>{if(!notice)return;const timer=setTimeout(()=>setNotice(""),5000);return()=>clearTimeout(timer);},[notice]);

  async function save(stage?:WorkOrderStatus,confirm=false,release=false) {
    if(!draft || !record || !options || lock.current || !canEdit)return;
    const next=normalizeMaintenance({...draft,...(stage?{workStatus:stage}:{}),...(release?{dispatchStatus:"" as const}:{}),confirmPayment:confirm,serviceDate:stage==="completed" && !draft.serviceDate ? today() : draft.serviceDate},options);
    const invalid=maintenanceError(next);if(invalid){setError(invalid);return;}
    lock.current=true;setBusy(true);setError("");setNotice("");
    try {const saved=await saveMaintenance(next);setRecord(saved);setDraft(maintenanceToInput(saved));setCancelReason(null);setNotice(release?"This work order’s truck hold was released.":stage==="closed"?"Work order finished.":"Work order saved.");if(stage==="completed" || stage==="closed")setStep(2);}
    catch(e){setError(e instanceof Error?e.message:"Unable to save work order");}
    finally{lock.current=false;setBusy(false);}
  }
  async function attach(file:File){if(!record || lock.current)return;lock.current=true;setBusy(true);setError("");try{const document=await uploadMaintenanceDocument(record.id,file);setRecord(r=>r?{...r,documents:[...r.documents.filter(d=>d.id!==document.id),document]}:r);setAttachment(null);setNotice("Receipt attached.");}catch(e){setError(e instanceof Error?e.message:"Unable to attach receipt");}finally{lock.current=false;setBusy(false);}}
  if(!record || !draft || !options)return <div className="space-y-4">{error?<ErrorBanner message={error}/>:<p className="py-12 text-center text-xs text-zinc-500">Loading work order…</p>}</div>;
  const status=record.workStatus ?? record.status;const workDone=["completed","closed"].includes(status);const finished=status==="closed";const terminal=["canceled","voided"].includes(status);
  const title=record.workOrder || `WO-${record.id.slice(0,8).toUpperCase()}`;
  const body=<MaintenanceForm value={draft} options={options} onChange={setDraft} section={step===2?"payment":"work"} />;
  return <div className="space-y-5 pb-8">
    <ManagementHeader icon={Wrench} title={title} />
    {editing && <MaintenanceWorkspace modalOnly recordId={record.id} onRecordClose={()=>setEditing(false)} onComplete={()=>setEditing(false)} onRecordSaved={()=>{void fetchMaintenance(record.id).then(saved=>{setRecord(saved);setDraft(maintenanceToInput(saved));setNotice("Work order updated.");});}}/>}
    <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-zinc-500">
      {record.truckId?<Link href={`/trucks/detail?id=${record.truckId}`} className="text-blue-400">Truck {record.unitNumber}</Link>:<span>Truck not linked</span>}
      <span>Added by <span className="text-zinc-300">{record.createdByName || "Unknown"}</span> · {dateTime(record.createdAt)}</span>
      <FleetStatusBadge status={record.dispatchStatus} />
      <span className={`ml-auto rounded-full px-2.5 py-1 ${statusClass(status)}`}>{workStatusLabels[status] ?? status}</span>
    </div>
    {!terminal && <WorkOrderProgress record={record} step={step} onStep={setStep} />}
    {error && <ErrorBanner message={error} />}
    {notice && <p role="status" className="text-xs text-emerald-400">{notice}</p>}
    <div className="grid items-start gap-5 xl:grid-cols-[minmax(0,1fr)_280px]">
      <section className="min-w-0 overflow-hidden rounded-xl border border-zinc-800 bg-zinc-900/20">
        <header className="flex flex-wrap items-center justify-between gap-3 border-b border-zinc-800 px-5 py-4"><div><h2 className="text-sm font-semibold text-zinc-200">{step===0?"Work order created":step===2?finished?"Work order finished":"Invoice & payment":"Work"}</h2><p className="mt-1 text-xs text-zinc-500">{step===0?"The request that started this work order.":step===2?finished?record.historicalPaid?"Historical work marked paid at the workflow cutover.":"Work and payment steps are complete.":"Use the invoice and payment fields below to finish this order.":workDone?"The repair work has been finished.":"Keep the repair details here, then move to payment when the work is finished."}</p></div>{record.dispatchStatus && (workDone || terminal) && canEdit && <button type="button" className="ui-button" disabled={busy} onClick={()=>void save(undefined,false,true)}>Release truck</button>}</header>
        <div className="p-5 sm:p-6">
          {step===0 ? <div className="space-y-4"><p className="whitespace-pre-wrap text-sm leading-6 text-zinc-300">{record.notes}</p><p className="text-xs text-zinc-500">Created {dateTime(record.createdAt)} by {record.createdByName || "Unknown"}</p></div> : finished ? <div className="space-y-5"><div className="flex items-center gap-3 text-emerald-400"><CheckCircle2 className="h-6 w-6"/><span className="text-sm font-medium">{record.historicalPaid?"Historical work · paid":record.noCharge?"Finished · no charge":"Payment confirmed"}</span></div><p className="whitespace-pre-wrap text-sm text-zinc-300">{record.notes}</p>{record.expense && <dl className="grid grid-cols-2 gap-5 text-xs">{[["Invoice amount",`$${record.expense.amount}`],["Payment method",record.expense.paymentType],["Paid by",record.expense.paidBy],["Reference",record.expense.referenceNumber || "—"]].map(([label,value])=><div key={label}><dt className="text-zinc-500">{label}</dt><dd className="mt-1 text-zinc-200">{value}</dd></div>)}</dl>}{record.paymentConfirmation && <p className="text-xs text-zinc-500">Confirmed by {record.paymentConfirmation.confirmedBy} · {dateTime(record.paymentConfirmation.confirmedAt)}</p>}</div> : <form onSubmit={e=>{e.preventDefault();void save();}}><fieldset disabled={busy || !canEdit || terminal}>{body}</fieldset>{canEdit && !terminal && <div className="mt-6 flex flex-wrap items-center justify-end gap-2 border-t border-zinc-800 pt-4"><button type="submit" className="ui-button" disabled={busy}>{step===2?"Save invoice":"Save changes"}</button>{step===1 && !workDone && <button type="button" disabled={busy} className="ui-button ui-button-primary" onClick={()=>void save(status==="in_progress"?"completed":"in_progress")}>{busy?<LoaderCircle className="h-4 w-4 animate-spin"/>:null}{status==="in_progress"?"Finish work":"Start work"}</button>}{step===1 && workDone && <button type="button" className="ui-button ui-button-primary" onClick={()=>setStep(2)}>Continue to invoice & payment</button>}{step===2 && <button type="button" disabled={busy || !draft.expense && !draft.noCharge} className="ui-button ui-button-primary" onClick={()=>void save("closed",!!draft.expense)}>{busy?<LoaderCircle className="h-4 w-4 animate-spin"/>:null}{draft.noCharge?"Finish work order":"Confirm paid & finish"}</button>}</div>}</form>}
        </div>
      </section>
      <aside className="space-y-4">
        {canEdit && finished && <button type="button" className="ui-button" disabled={busy} onClick={()=>setEditing(true)}>Edit work order</button>}
        <section className="rounded-xl border border-zinc-800 p-4"><h3 className="text-xs font-semibold text-zinc-300">Truck availability</h3><div className="mt-3">{record.dispatchStatus?<FleetStatusBadge status={record.dispatchStatus}/>:<span className="text-xs text-zinc-400">No hold from this order</span>}</div><p className="mt-3 text-xs leading-5 text-zinc-500">{record.dispatchStatus?"This order blocks new dispatch assignments until Fleet releases its hold.":"This order does not block dispatch. Other truck holds still apply."}</p></section>
        <section className="rounded-xl border border-zinc-800 p-4"><h3 className="mb-3 text-xs font-semibold text-zinc-300">Attachments</h3><div className="space-y-3">{record.documents.map(d=><a key={d.id} className="flex items-center gap-2 break-all text-xs text-blue-400" href={maintenanceDocumentUrl(d.id)} target="_blank" rel="noreferrer"><FileText className="h-3.5 w-3.5 shrink-0"/>{d.fileName}</a>)}{canEdit && !terminal && <><ReceiptUpload file={attachment} disabled={busy} onChange={setAttachment} onError={setError}/>{attachment && <button type="button" className="ui-button w-full" disabled={busy} onClick={()=>void attach(attachment)}>Attach receipt</button>}</>}</div></section>
        <details className="rounded-xl border border-zinc-800 p-4" onToggle={e=>{if(e.currentTarget.open && changes===null)void fetchMaintenanceHistory(record.id).then(setChanges).catch(e=>setError(e.message));}}><summary className="flex cursor-pointer items-center gap-2 text-xs font-semibold text-zinc-300"><History className="h-3.5 w-3.5"/>Activity</summary><div className="mt-4 space-y-4">{changes?.map((change,index)=><div key={index} className="border-l border-zinc-800 pl-3 text-xs"><p className="text-zinc-300">{change.actorName}</p><p className="mt-1 text-zinc-500">{workStatusLabels[change.after.workStatus ?? change.after.status] ?? "Updated details"}</p><p className="mt-1 text-[11px] text-zinc-600">{dateTime(change.happenedAt)}</p></div>)}</div></details>
        {canEdit && !workDone && !terminal && <button type="button" className="text-xs text-zinc-600 hover:text-red-400" onClick={()=>setCancelReason("")}>Cancel work order</button>}
      </aside>
    </div>
    {cancelReason!==null && <Modal title="Cancel work order" isSaving={busy} submitLabel="Cancel work order" onClose={()=>setCancelReason(null)} onSubmit={e=>{e.preventDefault();void save("canceled");}}><label className="text-xs text-zinc-400">Reason<textarea aria-label="Cancellation reason" className={`${controlClass} mt-2`} required minLength={3} maxLength={1000} value={cancelReason} onChange={e=>{setCancelReason(e.target.value);setDraft({...draft,cancelReason:e.target.value});}}/></label></Modal>}
  </div>;
}
