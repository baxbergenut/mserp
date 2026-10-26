"use client";

import { useState, type ReactNode } from "react";
import { LoaderCircle, Sparkles } from "lucide-react";
import { extractMaintenance } from "../lib/api";
import type { MaintenanceInput, MaintenanceOptions } from "../lib/types";
import { controlClass, Field, FormSection } from "../components/management/ManagementUI";
import { ExpensePayerSelect, ExpensePaymentMethodSelect } from "../expenses/ExpenseFields";
import { ExpenseFeePreview } from "../expenses/ExpenseFeePreview";
import { assignMaintenanceTruck, maintenanceDraft, normalizeMaintenance, serviceWork, today, maintenanceInvoice, waitingLabels } from "./maintenance";

export function MaintenanceAI({ options, onDrafts, onError, onBusyChange }: { options: MaintenanceOptions; onDrafts: (drafts: MaintenanceInput[], file: File | null) => void; onError: (message: string) => void; onBusyChange: (busy: boolean) => void }) {
  const [text, setText] = useState(""); const [file, setFile] = useState<File | null>(null); const [busy, setBusy] = useState(false);
  function attach(next: File) { if (!next.size || next.size > 20 * 1024 * 1024 || !/\.(pdf|txt|png|jpe?g|webp)$/i.test(next.name)) { onError("Choose a nonempty PDF, TXT, PNG, JPEG or WEBP up to 20 MB."); return; } setFile(next); onError(""); }
  async function analyze() {
    if (busy) return;
    if (!text.trim() && !file) { onError("Provide maintenance details or a receipt."); return; }
    setBusy(true); onBusyChange(true); onError("");
    try { const result = await extractMaintenance(text, file); if (!result.drafts.length) throw new Error("No maintenance visits were found."); onDrafts(result.drafts.map(d => maintenanceDraft(d, options)), file); }
    catch (e) { onError(e instanceof Error ? e.message : "Unable to analyze maintenance"); }
    finally { setBusy(false); onBusyChange(false); }
  }
  return <section className="mb-6 space-y-3" onPaste={event => { if (busy) return; const image = Array.from(event.clipboardData.items).find(i => i.type.startsWith("image/"))?.getAsFile(); if (image) attach(image); }}>
    <h3 className="flex items-center gap-2 text-[13px] font-semibold text-zinc-200"><Sparkles className="h-4 w-4 text-blue-400" />Fill with AI</h3>
    <textarea aria-label="Maintenance details for AI" disabled={busy} rows={3} className={controlClass} value={text} onChange={e => setText(e.target.value)} placeholder="Paste service details or drop an invoice…" onDragOver={e => e.preventDefault()} onDrop={e => { e.preventDefault(); if (busy) return; if (e.dataTransfer.files.length === 1) attach(e.dataTransfer.files[0]); else onError("Attach one document at a time."); }} />
    <div className="flex flex-wrap items-center justify-end gap-2">
      {file && <button type="button" disabled={busy} aria-label="Remove maintenance attachment" className="mr-auto max-w-full truncate text-xs text-zinc-400" onClick={() => setFile(null)}>{file.name} ×</button>}
      <label className="ui-button cursor-pointer">Browse invoice<input aria-label="Maintenance AI attachment" disabled={busy} type="file" className="sr-only" accept=".pdf,.txt,.png,.jpg,.jpeg,.webp" onChange={e => { if (e.target.files?.[0]) attach(e.target.files[0]); e.target.value = ""; }} /></label>
      <button type="button" className="ui-button ui-button-primary" disabled={busy} onClick={() => void analyze()}>{busy && <LoaderCircle className="h-4 w-4 animate-spin" />}{busy ? "Analyzing…" : "Analyze"}</button>
    </div>
  </section>;
}

const oilGrades = ["10W-30", "15W-40", "5W-40", "5W-30", "10W-40", "0W-40"];
function OptionalServiceSection({title,collapse,children}:{title:string;collapse:boolean;children:ReactNode}) {
  if(!collapse)return <FormSection title={title}>{children}</FormSection>;
  return <details className="rounded-lg border border-zinc-800 p-3"><summary className="cursor-pointer text-xs font-medium text-zinc-400">{title}</summary><div className="pt-4"><FormSection title={title} hideTitle>{children}</FormSection></div></details>;
}
export function MaintenanceForm({ value, options, onChange, odometerMessage, onRelease, section="all" }: { value: MaintenanceInput; options: MaintenanceOptions; onChange: (value: MaintenanceInput) => void; odometerMessage?: string; onRelease?:()=>void; section?:"all"|"create"|"work"|"payment" }) {
  const [customOil, setCustomOil] = useState(!!value.oilType && !oilGrades.includes(value.oilType));
  const change = (next: MaintenanceInput) => onChange(normalizeMaintenance({ ...next, oilType: !value.services.some(s => s.typeCode === "preventive") && next.services.some(s => s.typeCode === "preventive") && !next.oilType ? "10W-30" : next.oilType }, options));
  const set = <K extends keyof MaintenanceInput>(key: K, next: MaintenanceInput[K]) => change({ ...value, [key]: next });
  const expense = value.expense;
  const creation=section==="create";
  const work=section==="all" || section==="work";
  const payment=section==="all" || section==="payment";
  const truck = options.trucks.find(t => t.id === value.truckId);
  const completedPrograms=[...new Set([...(value.programIds??[]),...value.services.map(s=>options.types.find(t=>t.code===s.typeCode)?.programId).filter((id):id is string=>!!id)])];
  const responsibility = ["Company", ...(value.driverId ? ["Driver"] : []), ...(!truck?.isCompanyOwned && truck?.ownerId && truck.ownerDriverId !== value.driverId ? ["Truck Owner"] : [])];
  const finance = <K extends keyof NonNullable<MaintenanceInput["expense"]>>(key: K, next: NonNullable<MaintenanceInput["expense"]>[K]) => { if (expense) set("expense", { ...expense, [key]: next }); };
  const serviceTypeField=(<Field label="Service type"><select autoFocus={section==="all"} aria-label="Service type" required className={controlClass} value={value.services.length === 1 ? value.services[0].typeCode : value.version ? "__saved" : ""} onChange={e => change({...value,services:[serviceWork(e.target.value, options)],programIds:undefined})}>
        <option value="">Select service type</option>
        {value.services.length > 1 && value.version > 0 && <option value="__saved">Saved types: {Array.from(new Set(value.services.map(s => s.name))).join(", ")}</option>}
        {options.types.filter(t => t.active || value.services.some(s => s.typeCode === t.code)).map(t => <option key={t.code} value={t.code}>{t.name}{t.active ? "" : " (archived)"}</option>)}
      </select></Field>);
  const serviceDateField=(<Field label="Service date"><input aria-label="Maintenance service date" type="date" max={today()} required={value.status === "completed"} className={controlClass} value={value.serviceDate} onChange={e => set("serviceDate", e.target.value)} /></Field>);
  const scheduledField=(<Field label="Scheduled for"><input aria-label="Work order scheduled for" type="date" className={controlClass} value={value.scheduledFor ?? ""} onChange={e=>set("scheduledFor",e.target.value || null)} /></Field>);
  const readyField=(<Field label="Expected ready"><input aria-label="Work order expected ready" type="date" className={controlClass} value={value.expectedReady ?? ""} onChange={e=>set("expectedReady",e.target.value || null)} /></Field>);
  const descriptionField=(<Field label={value.dispatchStatus?"Description / status reason":"Description"} wide={section!=="work"}><textarea required={!!value.dispatchStatus} aria-label="Work order description" maxLength={5000} rows={section==="work"?5:3} className={controlClass} value={value.notes || expense?.description || ""} onChange={e=>change({...value,notes:e.target.value,expense:expense?{...expense,description:e.target.value}:null})} placeholder="Describe the problem or work needed" /></Field>);
  return <div className="space-y-6">
    {section!=="payment" && <FormSection title={creation ? "Work order" : "Work"} hideTitle={section==="work"}>
      <Field label="Truck"><select aria-label="Maintenance truck" required={value.status === "completed"} className={controlClass} value={value.truckId ?? ""} onChange={e => change({...assignMaintenanceTruck(value, e.target.value, options),programIds:undefined})}><option value="">Select truck</option>{options.trucks.map(t => <option key={t.id} value={t.id}>{t.unitNumber}{t.active ? "" : " (inactive)"}</option>)}</select></Field>
      <Field label="Truck status"><select aria-label="Work order truck status" className={controlClass} value={value.dispatchStatus ?? ""} onChange={e=>set("dispatchStatus",e.target.value as MaintenanceInput["dispatchStatus"])}><option value="">Does not block dispatch</option><option value="shop">Shop</option><option value="out_of_service">OOS</option></select>{value.dispatchStatus && onRelease && ["completed","canceled"].includes(value.workStatus ?? value.status) && <button type="button" className="ui-button mt-2" onClick={onRelease}>Release truck</button>}</Field>
      {section==="work" ? <div className="grid gap-4 sm:col-span-2 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">{descriptionField}<div className="grid grid-cols-1 gap-3 sm:grid-cols-2">{serviceTypeField}{serviceDateField}{scheduledField}{readyField}</div></div> : descriptionField}
      {!creation && <Field label="Waiting on"><select aria-label="Work order waiting on" className={controlClass} value={value.waitingOn ?? ""} onChange={e=>set("waitingOn",e.target.value)}><option value="">Nothing</option>{Object.entries(waitingLabels).map(([code,name])=><option key={code} value={code}>{name}</option>)}</select></Field>}
      {section==="all" && <>{scheduledField}{readyField}</>}
      {!creation && <Field label="Priority"><select aria-label="Work order priority" className={controlClass} value={value.priority ?? "normal"} onChange={e=>set("priority",e.target.value as MaintenanceInput["priority"])}><option value="normal">Normal</option><option value="high">High</option><option value="urgent">Urgent</option></select></Field>}
      {value.workStatus==="canceled" && <Field label="Cancellation reason" wide><textarea aria-label="Work order cancellation reason" required minLength={3} maxLength={1000} className={controlClass} value={value.cancelReason ?? ""} onChange={e=>set("cancelReason",e.target.value)} /></Field>}
    </FormSection>}
    {work && (section!=="work" || value.services.some(s=>s.typeCode==="preventive")) && <FormSection title="Service details">
      {section==="all" && value.workStatus==="needs_review" && <Field label="Service confirmation"><select aria-label="Service confirmation" className={controlClass} value={value.workStatus} onChange={e=>set("workStatus",e.target.value as MaintenanceInput["workStatus"])}><option value="needs_review">Needs review</option><option value="completed">Work finished</option></select></Field>}
      {section!=="work" && serviceTypeField}
      {value.services.some(s => s.typeCode === "preventive") && <Field label="Oil type"><select aria-label="Oil type" className={controlClass} value={customOil || value.oilType && !oilGrades.includes(value.oilType) ? "custom" : value.oilType} onChange={e => { setCustomOil(e.target.value === "custom"); set("oilType", e.target.value === "custom" ? "" : e.target.value); }}><option value="">Unknown</option>{oilGrades.map(grade => <option key={grade} value={grade}>{grade}</option>)}<option value="custom">Other / custom oil type</option></select>{(customOil || !!value.oilType && !oilGrades.includes(value.oilType)) && <input aria-label="Custom oil type" maxLength={100} className={`${controlClass} mt-2`} value={value.oilType} onChange={e => set("oilType", e.target.value.toUpperCase())} placeholder="Enter oil grade" />}</Field>}
      {section!=="work" && serviceDateField}
      {payment && expense && <Field label="Amount"><input aria-label="Maintenance amount" type="number" step="0.01" min="0" required className={controlClass} value={expense.amount} onChange={e => finance("amount", e.target.value)} />{value.legacyPaymentTotal === expense.amount && expense.paymentType.trim().toLowerCase() === "efs" ? <p className="text-xs text-zinc-500">Saved total includes the existing payment fee.</p> : <ExpenseFeePreview amount={expense.amount} paymentType={expense.paymentType} />}</Field>}
    </FormSection>}
    {work && <OptionalServiceSection title="Additional service details" collapse={section==="work"}>
      {completedPrograms.length>0 && <Field label="Service programs" wide><p className="text-xs text-zinc-400">Updates {completedPrograms.map(id=>options.programs?.find(p=>p.id===id)?.name??"saved service program").join(", ")} automatically when completed.</p></Field>}
      <Field label="Driver"><select aria-label="Maintenance driver" className={controlClass} value={value.driverId ?? ""} onChange={e => { const driver = options.drivers.find(d => d.id === e.target.value); change({ ...value, driverId: driver?.id ?? null, driverName: driver?.name ?? "" }); }}><option value="">{!value.driverId && value.driverName ? `${value.driverName} (unlinked)` : "No assigned driver"}</option>{value.driverId && !options.drivers.some(d => d.id === value.driverId) && <option value={value.driverId}>{value.driverName || "Saved driver"}</option>}{options.drivers.filter(d => d.active || d.id === value.driverId).map(d => <option key={d.id} value={d.id}>{d.name}{d.active ? "" : " (inactive)"}</option>)}</select></Field>
      <Field label="Odometer (miles)"><input aria-label="Maintenance odometer" title={odometerMessage || undefined} type="number" min="0" max="2000000000" step="1" className={controlClass} value={value.odometer ?? ""} onChange={e => set("odometer", e.target.value === "" ? null : Number(e.target.value))} placeholder="Enter odometer" /></Field>
      <Field label="Service location"><select aria-label="Service location" className={controlClass} value={value.location} onChange={e => set("location", e.target.value as MaintenanceInput["location"])}>{["shop", "roadside", "internal", "unknown"].map(v => <option key={v} value={v}>{v[0].toUpperCase() + v.slice(1)}</option>)}</select></Field>
      <Field label="Work-order number"><input aria-label="Work-order number" maxLength={500} className={controlClass} value={value.workOrder} onChange={e => set("workOrder", e.target.value)} /></Field>
    </OptionalServiceSection>}
    {work && <OptionalServiceSection title="Shop details" collapse={section==="work"}>
      <Field label="Shop"><div className="flex gap-2"><select aria-label="Maintenance vendor" className={`${controlClass} min-w-0 flex-1`} value={value.vendorId ?? ""} onChange={e => { const v = options.vendors.find(v => v.id === e.target.value); change({ ...value, vendorId: v?.id ?? null, vendorName: v?.name ?? "", vendorAddress: v?.address ?? "", vendorPhone: v?.phone ?? "", vendorContact: v?.contact ?? "" }); }}><option value="">Other / custom shop</option>{options.vendors.filter(v => v.active || v.id === value.vendorId).map(v => <option key={v.id} value={v.id}>{v.name}{v.active ? "" : " (archived)"}</option>)}</select></div></Field>
      {!value.vendorId && <Field label="Shop name"><input aria-label="Vendor name" maxLength={200} className={controlClass} value={value.vendorName} onChange={e => set("vendorName", e.target.value)} /></Field>}
      <Field label="Shop address"><input aria-label="Vendor address" maxLength={500} className={controlClass} value={value.vendorAddress} onChange={e => set("vendorAddress", e.target.value)} /></Field>
      <Field label="Shop phone"><input aria-label="Vendor phone" maxLength={500} className={controlClass} value={value.vendorPhone} onChange={e => set("vendorPhone", e.target.value)} /></Field>
      <Field label="Contact"><input aria-label="Vendor contact" maxLength={500} className={controlClass} value={value.vendorContact} onChange={e => set("vendorContact", e.target.value)} /></Field>
    </OptionalServiceSection>}
    {payment && <FormSection title="Invoice">
      <Field label="Invoice"><select aria-label="Work order invoice" className={controlClass} value={expense ? "invoice" : value.noCharge ? "no_charge" : "unknown"} onChange={e=>change({...value,noCharge:e.target.value==="no_charge",services:e.target.value==="invoice" ? value.services : value.services.map(s=>({...s,amount:null})),expense:e.target.value==="invoice" ? maintenanceInvoice(value,options) : null})}><option value="unknown" disabled={value.version>0 && !!expense}>No invoice yet</option><option value="invoice">Invoice</option><option value="no_charge" disabled={value.version>0 && !!expense}>No charge</option></select></Field>
      {section==="payment" && expense && <Field label="Amount"><input aria-label="Maintenance amount" type="number" step="0.01" min="0" required className={controlClass} value={expense.amount} onChange={e=>finance("amount",e.target.value)} /><ExpenseFeePreview amount={expense.amount} paymentType={expense.paymentType} /></Field>}
    </FormSection>}
    {payment && expense && <FormSection title="Payment">
      <Field label="Invoice / reference number"><input aria-label="Maintenance reference number" className={controlClass} value={expense.referenceNumber} onChange={e => finance("referenceNumber", e.target.value)} /></Field>
      <Field label="Payment method"><ExpensePaymentMethodSelect label="Maintenance payment method" value={expense.paymentType} methods={options.paymentMethods} onChange={next => finance("paymentType", next)} saved={value.version > 0} className={controlClass} /></Field>
      <Field label="Paid by"><ExpensePayerSelect value={expense.paidBy} users={options.users} label="Maintenance paid by" onChange={next => finance("paidBy", next)} className={controlClass} /></Field>
      <Field label="Who will cover"><select aria-label="Maintenance responsibility" className={controlClass} value={expense.coveredBy} onChange={e => set("expense", { ...expense, coveredBy: e.target.value, ownerId: e.target.value === "Truck Owner" ? truck?.ownerId ?? null : null })}>{!responsibility.includes(expense.coveredBy) && <option value={expense.coveredBy}>{expense.coveredBy} (saved)</option>}{responsibility.map(v => <option key={v} value={v}>{v}{v === "Truck Owner" ? ` · ${expense.ownerId && expense.ownerId !== truck?.ownerId ? value.savedOwnerName || "Saved owner" : truck?.ownerName || "Saved owner"}` : ""}</option>)}</select></Field>
    </FormSection>}
  </div>;
}
