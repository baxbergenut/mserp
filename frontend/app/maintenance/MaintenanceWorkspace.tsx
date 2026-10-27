"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { TableFilters } from "@/app/components/management/TableFilters";
import { WorkOrderTable } from "./WorkOrderTable";
import Link from "@/app/components/AccessLink";
import { FileText, History, Wrench } from "lucide-react";
import { fetchMaintenance, fetchMaintenanceAccess, fetchMaintenanceHistory, fetchMaintenanceOptions, fetchMaintenancePage, maintenanceDocumentUrl, saveMaintenance, uploadMaintenanceDocument, verifyExpense, unverifyExpense, voidMaintenance } from "../lib/api";
import type { ExpenseInput, AIExpenseDraft, MaintenanceChange, MaintenanceInput, MaintenanceOptions, MaintenanceRecord, MaintenanceType, MaintenanceVendor } from "../lib/types";
import { useExpenseCategoryAccess } from "../lib/access";
import { useQuickCreate } from "../lib/topNavigation";
import { useViewState } from "../lib/viewMemory";
import { controlClass, ErrorBanner, ManagementHeader, ManagementSearch, Modal, TablePagination, TableShell } from "../components/management/ManagementUI";
import { MaintenanceAI, MaintenanceForm } from "./MaintenanceForm";
import { MaintenanceTypeEditor, MaintenanceVendorEditor } from "./MaintenanceSettings";
import { expenseToMaintenance, maintenanceDraft, emptyMaintenance, maintenanceError, maintenanceToInput, normalizeMaintenance, today, workStatusLabels } from "./maintenance";
import { fetchMaintenanceOdometer } from "../lib/api";
import { centsLabel } from "../lib/expenseCosts";
import { ReceiptUpload } from "../components/ReceiptUpload";

const money = (value: string | null | undefined) => {if(value==null)return "—";const match=/^(-?)(\d+)(?:\.(\d{1,2}))?$/.exec(value);return match ? centsLabel((BigInt(match[2])*BigInt(100)+BigInt((match[3]??"").padEnd(2,"0")))*(match[1]?BigInt(-1):BigInt(1))) : "—";};
const label = (value: string) => value.replaceAll("_", " ").replace(/^./, s => s.toUpperCase());

export function MaintenanceWorkspace({ truckId, embedded = false, modalOnly = false, recordId, onRecordClose, onRecordSaved, initialExpense, initialAIDraft, initialAttachment, onComplete, onUseExpense, reviewLabel, reviewHasNext }: { truckId?: string; embedded?: boolean; modalOnly?: boolean; recordId?: string; onRecordClose?: () => void; onRecordSaved?: () => void; initialExpense?: ExpenseInput; initialAIDraft?: AIExpenseDraft; initialAttachment?: File | null; onComplete?: () => void; onUseExpense?: (value:ExpenseInput,file:File|null)=>void; reviewLabel?: string; reviewHasNext?: boolean }) {
  const access = useExpenseCategoryAccess();
  const router=useRouter();
  const [options, setOptions] = useState<MaintenanceOptions | null>(null); const [records, setRecords] = useState<MaintenanceRecord[]>([]);
  const [loadedStatus,setLoadedStatus]=useState("");
  const [error, setError] = useState(""); const [notice, setNotice] = useState(""); const [loading, setLoading] = useState(true);
  const [savedTab, setTab] = useViewState<string>(`maintenance:${truckId ?? "all"}:work-tabs`, "In progress");
  const tab = ["In progress","Completed - pending payment","Completed - paid","Canceled"].includes(savedTab) || savedTab==="Settings" && !embedded ? savedTab : "In progress";
  const [settingsSection,setSettingsSection]=useState("Service types");
  const [vendorSearch,setVendorSearch]=useState("");
  const [vendorPage,setVendorPage]=useState(1);
  const [vendorPageSize,setVendorPageSize]=useState(25);
  const [selectedTruck, setSelectedTruck] = useViewState("maintenance:truck", ""); const [vendor, setVendor] = useViewState("maintenance:vendor", ""); const [type, setType] = useViewState("maintenance:type", ""); const [status, setStatus] = useViewState("maintenance:ongoing-status", ""); const [search, setSearch] = useState("");
  const listStatus=tab==="Completed - pending payment" ? "completed" : tab==="Completed - paid" ? "closed" : tab==="Canceled" ? "archived" : status || "active";
  const [from, setFrom] = useViewState("maintenance:from", ""); const [to, setTo] = useViewState("maintenance:to", ""); const [page, setPage] = useState(1); const [pageSize, setPageSize] = useState(25); const [total, setTotal] = useState(0); const [totalPages, setTotalPages] = useState(1);
  const [editing, setEditing] = useState<MaintenanceInput | null>(null); const [original, setOriginal] = useState<MaintenanceRecord | null>(null); const [busy, setBusy] = useState(false); const [formError, setFormError] = useState(""); const [drafts, setDrafts] = useState<MaintenanceInput[]>([]); const [draftIndex, setDraftIndex] = useState(0); const [attachment, setAttachment] = useState<File | null>(null); const [uploadError, setUploadError] = useState("");
  const [vendorEdit, setVendorEdit] = useState<MaintenanceVendor | null>(null); const [typeEdit, setTypeEdit] = useState<MaintenanceType | null>(null); const [changes, setChanges] = useState<MaintenanceChange[] | null>(null); const [voidReason, setVoidReason] = useState<string | null>(null);
  const version = useRef(0); const deepOpened = useRef(false);
  const [aiBusy,setAIBusy]=useState(false);
  const [odometerMessage,setOdometerMessage]=useState("");
  const autoOdometer=useRef<{truckId:string; mileage:number}|null>(null);
  const saveLock=useRef(false);
  const visitId=editing?.id,visitTruck=editing?.truckId,visitDate=editing?.serviceDate,visitVersion=editing?.version;
  useEffect(()=>{
    if(!visitId||!visitTruck||visitVersion!==0||visitDate!==today()) return;
    const controller=new AbortController();
    fetchMaintenanceOdometer(visitTruck,controller.signal).then(result=>{
      if(controller.signal.aborted)return;
      setOdometerMessage(result.reading ? `${result.reading.source==="samsara"?"Samsara":result.reading.source==="manual"?"Manual":"Five ELD"} · ${new Date(result.reading.reportedAt).toLocaleString()}` : result.message);
      if(result.reading){autoOdometer.current={truckId:visitTruck,mileage:result.reading.mileage};setEditing(current=>current?.id===visitId&&current.truckId===visitTruck&&current.serviceDate===today()&&current.odometer===null ? {...current,odometer:result.reading!.mileage}:current);}
    }).catch(()=>{if(!controller.signal.aborted)setOdometerMessage("Enter the service odometer manually; telematics is unavailable.");});
    return ()=>controller.abort();
  },[visitId,visitTruck,visitDate,visitVersion]);
  const invalidate = useCallback(() => {version.current++;}, []);
  const [queryTruck,setQueryTruck] = useState(() => typeof window === "undefined" ? "" : new URLSearchParams(window.location.search).get("truckId") ?? "");
  const currentTruck = truckId ?? (queryTruck || selectedTruck);
  const ready = options !== null;
  const operationalView=tab==="In progress";
  const paidView=tab==="Completed - paid";
  const rights = access.find(a => a.categoryId === options?.categoryId);
  const canEditRecord = original?.canEdit ?? !!rights?.canEdit;
  const canView=!!rights?.canView; const canEdit = !!rights?.canEdit; const canCreate = !!rights?.canCreate; const canDelete = !!rights?.canDelete;
  const load = useCallback(async () => {
    const run = ++version.current; setLoading(true);
    try {
      if(modalOnly) { const opts=await fetchMaintenanceOptions(); if(run===version.current){setOptions(opts);setError("");} return; }
      const roleAccess=await fetchMaintenanceAccess();
      if(!roleAccess.canView&&roleAccess.canCreate) {const opts=await fetchMaintenanceOptions();if(run!==version.current)return;setOptions(opts);setRecords([]);setTotal(0);setError("");return;}
      const [opts, listing] = await Promise.all([fetchMaintenanceOptions(), fetchMaintenancePage({ truckId: currentTruck || undefined, vendorId: vendor || undefined, typeCode: type || undefined, status: listStatus, search, dateFrom: from || undefined, dateTo: to || undefined, page, pageSize })]);
      if (run !== version.current) return; setOptions(opts); setRecords(listing.items);setLoadedStatus(listStatus); setTotal(listing.total); setTotalPages(listing.totalPages); setError("");
    } catch (e) { if (run === version.current) setError(e instanceof Error ? e.message : "Unable to load maintenance"); } finally { if (run === version.current) setLoading(false); }
  }, [currentTruck, vendor, type, listStatus, search, from, to, page, pageSize, modalOnly]);
  useEffect(() => { const timer = setTimeout(() => void load(), 200); return () => { clearTimeout(timer); invalidate(); }; }, [load, invalidate]);
  useEffect(() => { const refresh = () => { if (!document.hidden) void load(); }; window.addEventListener("focus", refresh); const timer = setInterval(refresh, 30000); return () => { window.removeEventListener("focus", refresh); clearInterval(timer); }; }, [load]);
  useEffect(() => { if (!notice) return; const timer = setTimeout(() => setNotice(""), 5000); return () => clearTimeout(timer); }, [notice]);
  useEffect(() => {
    if (!ready || deepOpened.current || (embedded && !modalOnly)) return;
    if(options && (initialExpense || initialAIDraft)) {const timer=setTimeout(()=>{if(deepOpened.current)return;deepOpened.current=true;const value=initialAIDraft ? maintenanceDraft({...initialAIDraft,maintenance:initialAIDraft.maintenance ?? null},options) : expenseToMaintenance(initialExpense!,options);setOriginal(null);setEditing(value);setAttachment(initialAttachment ?? null);if(initialAIDraft)setDrafts([value]);},0);return()=>clearTimeout(timer);}
    const id = recordId ?? new URLSearchParams(window.location.search).get("recordId"); if (!id) return; deepOpened.current = true; let cancelled = false;
    fetchMaintenance(id).then(record => { if (!cancelled) { setOriginal(record); setEditing(maintenanceToInput(record)); } }).catch(e => { if (!cancelled) setError(e.message); }); return () => { cancelled = true; };
  }, [ready, embedded, modalOnly, recordId, initialExpense, initialAIDraft, initialAttachment, options]);
  function openNew() { if (!options) return; setOriginal(null); setEditing(emptyMaintenance(options, currentTruck || undefined)); setFormError(""); setUploadError(""); setDrafts([]); setAttachment(null);setOdometerMessage("");autoOdometer.current=null; }
  useQuickCreate(openNew, !embedded && !modalOnly && !!options && canCreate);
  function openRecord(record: MaintenanceRecord) { if(!modalOnly){router.push(`/maintenance/detail?id=${record.id}`);return;} setOriginal(record); setEditing(maintenanceToInput(record)); setFormError(""); setDrafts([]); setAttachment(null); setUploadError(""); }
  function close(completed=false) { setEditing(null); setOriginal(null); setDrafts([]); setAttachment(null); setVoidReason(null); if(completed && onComplete)onComplete();else onRecordClose?.(); }
  async function submit(override?:MaintenanceInput) {
    if (!editing || !options || saveLock.current || aiBusy) return; const input=normalizeMaintenance(override ?? editing,options);const invalid = maintenanceError(input); if (invalid) { setFormError(invalid); return; } saveLock.current=true;setBusy(true); setFormError("");
    try {
      const createdPlain=!original && !initialExpense && !initialAIDraft && !drafts.length && !modalOnly && canView;
      const record = await saveMaintenance(input); onRecordSaved?.(); setOriginal(record); setEditing(maintenanceToInput(record));
      if (attachment) { try { await uploadMaintenanceDocument(record.id, attachment); setAttachment(null); } catch (e) { setUploadError(`Visit saved. Attachment upload failed: ${e instanceof Error ? e.message : "retry the upload"}`); await load(); return; } }
      if (drafts.length && draftIndex < drafts.length - 1) { const next = draftIndex + 1; setDraftIndex(next); setOriginal(null); setEditing(drafts[next]); setAttachment(attachment); setNotice("Visit saved. Review the next AI draft."); } else { close(true); setNotice("Work order saved."); }
      if(createdPlain){router.push(`/maintenance/detail?id=${record.id}`);return;}
      await load();
    } catch (e) { setFormError(e instanceof Error ? e.message : "Unable to save maintenance"); } finally { setBusy(false);saveLock.current=false; }
  }
  async function attachSaved(file: File) { if (!original) return; setBusy(true); setFormError(""); try { const doc=await uploadMaintenanceDocument(original.id,file);setOriginal(current=>current ? {...current,documents:current.documents.some(d=>d.id===doc.id) ? current.documents:[...current.documents,doc]} : current);setNotice("Attachment saved.");return true; } catch (e) { setFormError(e instanceof Error ? e.message : "Unable to upload attachment");return false; } finally { setBusy(false); } }
  const filteredVendors=options?.vendors.filter(v=>`${v.name} ${v.address}`.toLowerCase().includes(vendorSearch.toLowerCase())) ?? [];
  const visibleVendors=filteredVendors.slice((vendorPage-1)*vendorPageSize,vendorPage*vendorPageSize);
  const tabs = embedded ? ["In progress","Completed - pending payment","Completed - paid","Canceled"] : ["In progress","Completed - pending payment","Completed - paid","Canceled","Settings"];
  return <div className="space-y-5">
    {!modalOnly && <>
    {!embedded && <ManagementHeader icon={Wrench} title="Work orders" count={total} />}
    {error && <ErrorBanner message={error} />}

    <div className="flex flex-wrap items-center gap-2">
      <ManagementSearch value={search} onChange={v => { setSearch(v); setPage(1); }} placeholder="Search work orders…" />
      <TableFilters>{!truckId && <><select aria-label="Filter maintenance truck" className={controlClass} value={currentTruck} onChange={e=>{setQueryTruck("");setSelectedTruck(e.target.value);setPage(1);}}><option value="">All trucks</option>{options?.trucks.map(t=><option key={t.id} value={t.id}>{t.unitNumber}</option>)}</select></>}<select aria-label="Filter service type" className={controlClass} value={type} onChange={e=>{setType(e.target.value);setPage(1);}}><option value="">All service types</option>{options?.types.map(t=><option key={t.code} value={t.code}>{t.name}</option>)}</select><select aria-label="Filter maintenance vendor" className={controlClass} value={vendor} onChange={e=>{setVendor(e.target.value);setPage(1);}}><option value="">All shops</option>{options?.vendors.map(v=><option key={v.id} value={v.id}>{v.name}</option>)}</select>{operationalView && <><select aria-label="Filter maintenance status" className={controlClass} value={status} onChange={e=>{setStatus(e.target.value);setPage(1);}}><option value="">All ongoing work</option>{["open","scheduled","in_progress","needs_review"].map(code=><option key={code} value={code}>{workStatusLabels[code]}</option>)}</select></>}</TableFilters>
      <input aria-label="Maintenance period from" className={`${controlClass} !w-auto`} type="date" value={from} onChange={e => { setFrom(e.target.value); setPage(1); }} /><span aria-hidden="true">→</span><input aria-label="Maintenance period to" className={`${controlClass} !w-auto`} type="date" value={to} onChange={e => { setTo(e.target.value); setPage(1); }} />
      {canCreate && <button type="button" className="ui-button ui-button-primary" onClick={openNew}>Add work order</button>}

      <span role="status" className="text-xs text-emerald-400">{notice}</span>
    </div>
    <nav aria-label="Maintenance sections" className="flex flex-wrap gap-1 border-b border-zinc-800">{tabs.map(name => <button type="button" key={name} role="tab" aria-selected={tab === name} className={`border-b-2 px-3 py-2 text-xs ${tab === name ? "border-blue-400 text-blue-400" : "border-transparent text-zinc-500"}`} onClick={() => {setTab(name);setStatus("");setPage(1);}}>{name}</button>)}</nav>
    {tab!=="Settings" && canView && <><WorkOrderTable records={loadedStatus===listStatus?records:[]} options={options} stage={operationalView?"work":paidView?"paid":tab==="Canceled"?"archived":"payment"} loading={loading || loadedStatus!==listStatus} embeddedTruck={!!truckId} onOpen={openRecord} /><TablePagination page={page} pageSize={pageSize} totalItems={total} totalPages={totalPages} onPageChange={setPage} onPageSizeChange={size=>{setPageSize(size);setPage(1);}} /></>}
    {tab === "Settings" && <div className="space-y-5"><nav aria-label="Work order settings" className="flex gap-2">{["Service types","Vendors"].map(name=><button type="button" key={name} role="tab" aria-selected={settingsSection===name} className={`ui-button ${settingsSection===name ? "ui-button-primary" : ""}`} onClick={()=>setSettingsSection(name)}>{name}</button>)}</nav>{settingsSection==="Service types" && <>{options && <section className="space-y-3"><div className="flex items-center justify-between"><h3 className="text-[13px] font-semibold">Service reporting types</h3>{options.administrator && <button type="button" className="ui-button" onClick={() => setTypeEdit({ code: "", name: "", active: true, version: 0 })}>Add service type</button>}</div>{options.types.map(t => <div key={t.code} className="flex items-center gap-3 border-b border-zinc-800 py-3 text-xs"><span className="flex-1">{t.name}{t.active ? "" : " · Archived"}</span>{options.administrator && <button type="button" className="ui-button" onClick={() => setTypeEdit(t)}>Edit type</button>}</div>)}</section>}</>}{settingsSection==="Vendors" && <><section className="space-y-3"><h3 className="text-[13px] font-semibold">Vendors</h3><ManagementSearch value={vendorSearch} onChange={v=>{setVendorSearch(v);setVendorPage(1);}} placeholder="Search vendors…" />{canEdit && <button type="button" className="ui-button ui-button-primary" onClick={() => setVendorEdit({ id: "", name: "", address: "", phone: "", contact: "", active: true, version: 0 })}>Add vendor</button>}<TableShell><table className="w-full text-left text-[13px]"><thead><tr className="border-b border-zinc-800 text-xs text-zinc-500">{["Vendor", "Address", "Phone / contact", "Status", ""].map((v, i) => <th key={i} className="p-3 font-medium">{v}</th>)}</tr></thead><tbody>{visibleVendors.map(v => <tr key={v.id} className="border-b border-zinc-800/60"><td className="p-3">{v.name}</td><td className="p-3">{v.address}</td><td className="p-3">{v.phone}<span className="block text-xs text-zinc-500">{v.contact}</span></td><td className="p-3">{v.active ? "Active" : "Archived"}</td><td className="p-3">{canEdit && <button type="button" className="ui-button" onClick={() => setVendorEdit(v)}>Edit vendor</button>}</td></tr>)}</tbody></table></TableShell><TablePagination page={vendorPage} pageSize={vendorPageSize} totalItems={filteredVendors.length} totalPages={Math.max(1,Math.ceil(filteredVendors.length/vendorPageSize))} onPageChange={setVendorPage} onPageSizeChange={size=>{setVendorPageSize(size);setVendorPage(1);}} /></section></>}</div>}
    </>}
    {modalOnly && error && <Modal title="Work order" submitLabel="Close" hideSubmit isSaving={false} onClose={()=>onRecordClose?.()} onSubmit={e=>e.preventDefault()}><ErrorBanner message={error} /></Modal>}
    {editing && options && <Modal title={original ? "Work order details" : "Add work order"} isSaving={busy || aiBusy} submitLabel={reviewHasNext || drafts.length && draftIndex < drafts.length - 1 ? "Save & review next" : "Save work order"} hideSubmit={original ? !canEditRecord || original.status === "voided" : !canCreate} onClose={()=>close()} onSubmit={e => { e.preventDefault(); void submit(); }} footerStart={original && canDelete && original.status !== "voided" ? <button type="button" className="ui-button text-red-400" onClick={() => setVoidReason("")}>Void record</button> : undefined}>
      {onUseExpense && !original && editing.expense && <button type="button" className="ui-button mb-4" disabled={busy || aiBusy} onClick={()=>onUseExpense({...editing.expense!,description:editing.notes},attachment)}>Change expense category</button>}{reviewLabel && <p className="mb-3 text-xs text-zinc-500">{reviewLabel}</p>}
      {formError && <ErrorBanner message={formError} />}{uploadError && <ErrorBanner message={uploadError} />}
      {!original && !drafts.length && <details className="mb-4"><summary className="cursor-pointer text-xs text-blue-400">Fill from invoice or notes</summary><div className="mt-4"><MaintenanceAI options={options} onBusyChange={setAIBusy} onError={setFormError} onDrafts={(values, file) => { setDrafts(values); setDraftIndex(0); setEditing(values[0]); setAttachment(file ?? attachment); }} /></div></details>}
      {drafts.length > 0 && !reviewLabel && <p className="mb-4 text-xs text-zinc-500">Review AI visit {draftIndex + 1} of {drafts.length}. Confirm the completed work before saving.</p>}
      <fieldset disabled={busy || aiBusy || (original ? !canEditRecord || original.status === "voided" : !canCreate)}><MaintenanceForm value={editing} options={options} section={!original && !initialExpense && !initialAIDraft && !drafts.length ? "create" : "all"} onRelease={original ? ()=>void submit({...editing,dispatchStatus:""}) : undefined} onChange={next=>{if(next.truckId!==editing.truckId){setOdometerMessage(next.serviceDate===today() ? "Reading current odometer…" : "Enter the odometer at the time of service.");autoOdometer.current=null;}if(next.serviceDate!==editing.serviceDate && autoOdometer.current?.truckId===next.truckId && next.odometer===autoOdometer.current?.mileage){next={...next,odometer:null};setOdometerMessage(next.serviceDate===today() ? "Reading current odometer…" : "Enter the odometer at the time of service.");autoOdometer.current=null;}setEditing(next);}} odometerMessage={odometerMessage} /></fieldset>
      <ReceiptUpload file={attachment} disabled={busy || aiBusy || (original ? !canEditRecord || original.status === "voided" : !canCreate)} onChange={setAttachment} onError={setFormError} />
      {(original?.documents.length || attachment && original) ? <section className="mt-4 space-y-3">{original?.documents.map(d=><a key={d.id} className="flex items-center gap-2 text-xs text-blue-400" href={maintenanceDocumentUrl(d.id)} target="_blank" rel="noreferrer"><FileText className="h-4 w-4" />{d.fileName}</a>)}{attachment && original && <button type="button" disabled={busy || aiBusy} className="ui-button" onClick={()=>void attachSaved(attachment).then(ok=>{if(ok){setAttachment(null);setUploadError("");}})}>Upload receipt</button>}</section> : null}
      {original && <div className="mt-6 space-y-3"><p className="text-xs text-zinc-500">Added by {original.createdByName || "Unknown"} · {new Date(original.createdAt).toLocaleString()}</p><Link className="text-xs text-blue-400" href={`/maintenance/detail?id=${original.id}`}>Open work order page</Link><button type="button" className="ui-button inline-flex items-center gap-2" onClick={() => void fetchMaintenanceHistory(original.id).then(setChanges).catch(e => setFormError(e.message))}><History className="h-4 w-4" />View changes</button>{original.voidReason && <p className="text-xs text-zinc-400">Voided: {original.voidReason}</p>}{original.expense && <section className="space-y-2 text-xs"><div>Verifications: {original.expense.verifications?.length ? original.expense.verifications.map(v => <span key={v.id} className="mr-3 inline-flex items-center gap-2">{label(v.role)} · {v.userName}{(options.administrator || v.userId === options.currentUserId && options.verificationRole === "accountant") && <button type="button" aria-label={`Remove ${v.role} verification`} className="text-blue-400" onClick={() => {setBusy(true);void unverifyExpense(original.expense!.id,v.id).then(()=>fetchMaintenance(original.id)).then(record=>{setOriginal(record);setEditing(maintenanceToInput(record));}).catch(e=>setFormError(e.message)).finally(()=>setBusy(false));}}>Remove</button>}</span>) : "None"}</div>{options.verificationRole && !(original.expense.accountingVerified) && <button type="button" className="ui-button" onClick={() => { setBusy(true); void verifyExpense(original.expense!.id, original.expense!.updatedAt).then(() => fetchMaintenance(original.id)).then(record => { setOriginal(record); setEditing(maintenanceToInput(record)); }).catch(e => setFormError(e.message)).finally(() => setBusy(false)); }}>Verify invoice</button>}{original.expense.payments?.map(p => <p key={p.weekStart}>Week {p.weekStart}: {money(p.amount)}</p>)}</section>}</div>}
    </Modal>}
    {voidReason !== null && original && <Modal title="Void work order" isSaving={busy} submitLabel="Void record" onClose={() => setVoidReason(null)} onSubmit={e => { e.preventDefault(); setBusy(true); void voidMaintenance(original.id, original.version, voidReason).then(() => { onRecordSaved?.(); close(); setNotice("Record voided."); return load(); }).catch(e => setFormError(e.message)).finally(() => setBusy(false)); }}>{formError && <ErrorBanner message={formError} />}<textarea aria-label="Void reason" required minLength={3} maxLength={1000} rows={3} className={controlClass} value={voidReason} onChange={e => setVoidReason(e.target.value)} /></Modal>}
    {changes && <Modal title="Work order changes" isSaving={false} submitLabel="Done" hideSubmit cancelLabel="Done" onClose={() => setChanges(null)} onSubmit={e => e.preventDefault()}>{changes.length ? changes.map((change, index) => <section key={index} className="mb-4 border-b border-zinc-800 pb-3 text-xs"><p className="font-medium">{change.actorName} · {new Date(change.happenedAt).toLocaleString()}</p><p className="mt-2 text-zinc-400">{workStatusLabels[change.after.workStatus ?? change.after.status] ?? label(change.after.status)} · {change.after.serviceDate ?? "Date unknown"} · {change.after.odometer?.toLocaleString() ?? "Unknown"} mi · {change.after.vendorName || "Unknown vendor"}</p><p className="mt-1 text-zinc-400">{change.after.services?.map(s => `${s.name}${s.amount !== null ? ` ${money(s.amount)}` : ""}`).join(", ")}</p>{change.before && <p className="mt-1 text-zinc-500">Previous: {change.before.serviceDate ?? "Date unknown"} · {change.before.odometer?.toLocaleString() ?? "Unknown"} mi · {change.before.services?.map(s => s.name).join(", ")}</p>}</section>) : <p className="text-xs text-zinc-500">Imported historical record; no application edits recorded yet.</p>}</Modal>}
    {vendorEdit && <MaintenanceVendorEditor initial={vendorEdit} onClose={() => setVendorEdit(null)} onSaved={load} />}
    {typeEdit && <MaintenanceTypeEditor initial={typeEdit} options={options!} onClose={() => setTypeEdit(null)} onSaved={load} />}
  </div>;
}
