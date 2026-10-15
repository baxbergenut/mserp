"use client";

import { useState } from "react";
import { LoaderCircle, Plus, Sparkles, Trash2 } from "lucide-react";
import { extractMaintenance } from "../lib/api";
import type { MaintenanceInput, MaintenanceOptions, MaintenanceWork } from "../lib/types";
import { controlClass, Field, FormSection, Toggle } from "../components/management/ManagementUI";
import { ExpensePayerSelect } from "../expenses/ExpenseFields";
import { ExpenseFeePreview } from "../expenses/ExpenseFeePreview";
import { assignMaintenanceTruck, maintenanceDraft, maintenanceError, normalizeMaintenance, serviceWork, today } from "./maintenance";

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

export function MaintenanceForm({ value, options, onChange, odometerMessage }: { value: MaintenanceInput; options: MaintenanceOptions; onChange: (value: MaintenanceInput) => void; odometerMessage?: string }) {
  const change = (next: MaintenanceInput) => onChange(normalizeMaintenance(next, options));
  const set = <K extends keyof MaintenanceInput>(key: K, next: MaintenanceInput[K]) => change({ ...value, [key]: next });
  const priced = value.services.some(s => s.amount !== null);
  const expense = value.expense;
  const truck = options.trucks.find(t => t.id === value.truckId);
  const updateWork = (index: number, patch: Partial<MaintenanceWork>) => set("services", value.services.map((s, i) => i === index ? { ...s, ...patch } : s));
  const error = maintenanceError(value);
  const responsibility = ["Company", ...(value.driverId ? ["Driver"] : []), ...(!truck?.isCompanyOwned && truck?.ownerId && truck.ownerDriverId !== value.driverId ? ["Truck Owner"] : [])];
  const finance = <K extends keyof NonNullable<MaintenanceInput["expense"]>>(key: K, next: NonNullable<MaintenanceInput["expense"]>[K]) => { if (expense) set("expense", { ...expense, [key]: next }); };
  return <div className="space-y-6">
    <FormSection title="Service visit">
      <Field label="Truck"><select aria-label="Maintenance truck" required={value.status === "completed"} className={controlClass} value={value.truckId ?? ""} onChange={e => change(assignMaintenanceTruck(value, e.target.value, options))}><option value="">Select truck</option>{options.trucks.map(t => <option key={t.id} value={t.id}>{t.unitNumber}{t.active ? "" : " (inactive)"}</option>)}</select></Field>
      <Field label="Driver"><select aria-label="Maintenance driver" className={controlClass} value={value.driverId ?? ""} onChange={e => { const driver = options.drivers.find(d => d.id === e.target.value); change({ ...value, driverId: driver?.id ?? null, driverName: driver?.name ?? "" }); }}><option value="">No assigned driver</option>{value.driverId && !options.drivers.some(d => d.id === value.driverId) && <option value={value.driverId}>{value.driverName || "Saved driver"}</option>}{options.drivers.filter(d => d.active || d.id === value.driverId).map(d => <option key={d.id} value={d.id}>{d.name}{d.active ? "" : " (inactive)"}</option>)}</select></Field>
      <Field label="Service date"><input aria-label="Maintenance service date" type="date" max={today()} required={value.status === "completed"} className={controlClass} value={value.serviceDate} onChange={e => set("serviceDate", e.target.value)} /></Field>
      <Field label="Odometer (miles)"><input aria-label="Maintenance odometer" type="number" min="0" max="2000000000" step="1" className={controlClass} value={value.odometer ?? ""} onChange={e => set("odometer", e.target.value === "" ? null : Number(e.target.value))} placeholder="Enter odometer" />{odometerMessage && <p className="mt-1 text-xs text-zinc-500" role="status">{odometerMessage}</p>}</Field>
      {expense && <Field label="Amount"><input aria-label="Maintenance amount" type="number" step="0.01" min="0" required className={controlClass} value={expense.amount} onChange={e => finance("amount", e.target.value)} />{value.legacyPaymentTotal === expense.amount && expense.paymentType.trim().toLowerCase() === "efs" ? <p className="text-xs text-zinc-500">Saved total includes the existing payment fee.</p> : <ExpenseFeePreview amount={expense.amount} paymentType={expense.paymentType} />}</Field>}
      <Field label="Status"><select aria-label="Maintenance status" className={controlClass} value={value.status} onChange={e => set("status", e.target.value as MaintenanceInput["status"])}><option value="completed">Completed work</option><option value="needs_review">Needs review</option></select></Field>
      <Field label="Service location"><select aria-label="Service location" className={controlClass} value={value.location} onChange={e => set("location", e.target.value as MaintenanceInput["location"])}>{["shop", "roadside", "internal", "unknown"].map(v => <option key={v} value={v}>{v[0].toUpperCase() + v.slice(1)}</option>)}</select></Field>
      <Field label="Work-order number"><input aria-label="Work-order number" maxLength={500} className={controlClass} value={value.workOrder} onChange={e => set("workOrder", e.target.value)} /></Field>
    </FormSection>
    <FormSection title="Vendor">
      <Field label="Shop"><div className="flex gap-2"><select aria-label="Maintenance vendor" className={`${controlClass} min-w-0 flex-1`} value={value.vendorId ?? ""} onChange={e => { const v = options.vendors.find(v => v.id === e.target.value); change({ ...value, vendorId: v?.id ?? null, vendorName: v?.name ?? "", vendorAddress: v?.address ?? "", vendorPhone: v?.phone ?? "", vendorContact: v?.contact ?? "" }); }}><option value="">Other / custom shop</option>{options.vendors.filter(v => v.active || v.id === value.vendorId).map(v => <option key={v.id} value={v.id}>{v.name}{v.active ? "" : " (archived)"}</option>)}</select></div></Field>
      {!value.vendorId && <Field label="Shop name"><input aria-label="Vendor name" maxLength={200} className={controlClass} value={value.vendorName} onChange={e => set("vendorName", e.target.value)} /></Field>}
      <Field label="Shop address"><input aria-label="Vendor address" maxLength={500} className={controlClass} value={value.vendorAddress} onChange={e => set("vendorAddress", e.target.value)} /></Field>
      <Field label="Shop phone"><input aria-label="Vendor phone" maxLength={500} className={controlClass} value={value.vendorPhone} onChange={e => set("vendorPhone", e.target.value)} /></Field>
      <Field label="Contact"><input aria-label="Vendor contact" maxLength={500} className={controlClass} value={value.vendorContact} onChange={e => set("vendorContact", e.target.value)} /></Field>
    </FormSection>
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="text-[13px] font-semibold text-zinc-200">Services</h3><button type="button" className="ui-button" disabled={value.services.length >= 100} onClick={() => set("services", [...value.services, serviceWork(options.types.find(t => t.active && t.code === "other")?.code ?? options.types.find(t => t.active)!.code, options, priced ? "" : null)])}><Plus className="h-4 w-4" />Add service</button></div>
      {expense && value.services.length > 1 && <Toggle label="Split cost by service" description="Use when the invoice lists individual service costs." checked={priced} onChange={next => set("services", value.services.map(s => ({ ...s, amount: next ? "" : null })))} />}
      {value.services.map((work, index) => <div key={index} className="flex flex-wrap items-end gap-3 rounded-lg border border-zinc-800 p-3">
        <div className="min-w-0 flex-1"><Field label="Service type"><select aria-label={`Service ${index + 1} type`} required className={controlClass} value={work.typeCode} onChange={e => updateWork(index, serviceWork(e.target.value, options, work.amount))}>{options.types.filter(t => t.active || t.code === work.typeCode).map(t => <option key={t.code} value={t.code}>{t.name}{t.active ? "" : " (archived)"}</option>)}</select></Field></div>
        {priced && value.services.length > 1 && <div className="w-36"><Field label="Cost"><input aria-label={`Service ${index + 1} cost`} type="number" step="0.01" required className={controlClass} value={work.amount ?? ""} onChange={e => updateWork(index, { amount: e.target.value })} /></Field></div>}
        <button type="button" className="ui-button mb-0.5 text-zinc-500 hover:text-red-400" aria-label={`Remove service ${index + 1}`} disabled={value.services.length === 1} onClick={() => set("services", value.services.filter((_, i) => i !== index))}><Trash2 className="h-4 w-4" /></button>
      </div>)}
      {error && <p role="status" className="text-xs text-red-400">{error}</p>}
    </section>
    {expense && <FormSection title="Payment">
      <Field label="Invoice / reference number"><input aria-label="Maintenance reference number" className={controlClass} value={expense.referenceNumber} onChange={e => finance("referenceNumber", e.target.value)} /></Field>
      <Field label="Payment method"><input aria-label="Maintenance payment method" list="maintenance-payment-methods" className={controlClass} value={expense.paymentType} onChange={e => finance("paymentType", e.target.value)} /><datalist id="maintenance-payment-methods">{options.paymentMethods.map(m => <option key={m} value={m} />)}</datalist></Field>
      <Field label="Paid by"><ExpensePayerSelect value={expense.paidBy} users={options.users} label="Maintenance paid by" onChange={next => finance("paidBy", next)} className={controlClass} /></Field>
      <Field label="Who will cover"><select aria-label="Maintenance responsibility" className={controlClass} value={expense.coveredBy} onChange={e => set("expense", { ...expense, coveredBy: e.target.value, ownerId: e.target.value === "Truck Owner" ? truck?.ownerId ?? null : null })}>{!responsibility.includes(expense.coveredBy) && <option value={expense.coveredBy}>{expense.coveredBy} (saved)</option>}{responsibility.map(v => <option key={v} value={v}>{v}{v === "Truck Owner" ? ` · ${expense.ownerId && expense.ownerId !== truck?.ownerId ? value.savedOwnerName || "Saved owner" : truck?.ownerName || "Saved owner"}` : ""}</option>)}</select></Field>
    </FormSection>}
    <Field label="Service notes"><textarea aria-label="Maintenance notes" maxLength={5000} rows={3} className={controlClass} value={value.notes} onChange={e => set("notes", e.target.value)} /></Field>
  </div>;
}
