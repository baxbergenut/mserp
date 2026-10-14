"use client";

import { useState } from "react";
import { LoaderCircle, Plus, Sparkles, Trash2 } from "lucide-react";
import { extractMaintenance } from "../lib/api";
import type { MaintenanceInput, MaintenanceOptions, MaintenanceWork } from "../lib/types";
import { controlClass, Field, FormSection, Toggle } from "../components/management/ManagementUI";
import { ExpenseNameSelect } from "../expenses/ExpenseCostBreakdown";
import { ExpensePayerSelect } from "../expenses/ExpenseFields";
import { ExpenseFeePreview } from "../expenses/ExpenseFeePreview";
import { emptyExpenseInput } from "../expenses/ExpenseForm";
import { assignMaintenanceTruck, maintenanceDraft, maintenanceError, serviceTypeForName, today } from "./maintenance";

export function MaintenanceAI({ options, onDrafts, onError }: { options: MaintenanceOptions; onDrafts: (drafts: MaintenanceInput[], file: File | null) => void; onError: (message: string) => void }) {
  const [text, setText] = useState(""); const [file, setFile] = useState<File | null>(null); const [busy, setBusy] = useState(false);
  function attach(next: File) { if (!next.size || next.size > 20 * 1024 * 1024 || !/\.(pdf|txt|png|jpe?g|webp)$/i.test(next.name)) { onError("Choose a nonempty PDF, TXT, PNG, JPEG or WEBP up to 20 MB."); return; } setFile(next); onError(""); }
  async function analyze() { if (!text.trim() && !file) { onError("Provide maintenance details or a receipt."); return; } setBusy(true); onError(""); try { const result = await extractMaintenance(text, file); if (!result.drafts.length) throw new Error("No maintenance visits were found."); onDrafts(result.drafts.map(d => maintenanceDraft(d, options)), file); } catch (e) { onError(e instanceof Error ? e.message : "Unable to analyze maintenance"); } finally { setBusy(false); } }
  return <section className="mb-6 space-y-3" onPaste={event => { const image = Array.from(event.clipboardData.items).find(i => i.type.startsWith("image/"))?.getAsFile(); if (image) attach(image); }}>
    <h3 className="flex items-center gap-2 text-[13px] font-semibold text-zinc-200"><Sparkles className="h-4 w-4 text-blue-400" />Fill with AI</h3>
    <textarea aria-label="Maintenance details for AI" rows={3} className={controlClass} value={text} onChange={e => setText(e.target.value)} placeholder="Paste service details or drop an invoice…" onDragOver={e => e.preventDefault()} onDrop={e => { e.preventDefault(); if (e.dataTransfer.files.length === 1) attach(e.dataTransfer.files[0]); else onError("Attach one document at a time."); }} />
    <div className="flex flex-wrap items-center justify-end gap-2">{file && <button type="button" aria-label="Remove maintenance attachment" className="mr-auto max-w-full truncate text-xs text-zinc-400" onClick={() => setFile(null)}>{file.name} ×</button>}<label className="ui-button cursor-pointer">Browse<input aria-label="Maintenance AI attachment" type="file" className="sr-only" accept=".pdf,.txt,.png,.jpg,.jpeg,.webp" onChange={e => { if (e.target.files?.[0]) attach(e.target.files[0]); e.target.value = ""; }} /></label><button type="button" className="ui-button-primary inline-flex items-center gap-2" disabled={busy} onClick={() => void analyze()}>{busy && <LoaderCircle className="h-4 w-4 animate-spin" />}{busy ? "Analyzing…" : "Analyze"}</button></div>
  </section>;
}

export function MaintenanceForm({ value, options, onChange, billed = false }: { value: MaintenanceInput; options: MaintenanceOptions; onChange: (value: MaintenanceInput) => void; billed?: boolean }) {
  const set = <K extends keyof MaintenanceInput>(key: K, next: MaintenanceInput[K]) => onChange({ ...value, [key]: next });
  const priced = value.services.some(s => s.amount !== null);
  const expense = value.expense;
  const truck = options.trucks.find(t => t.id === value.truckId);
  const updateWork = (index: number, patch: Partial<MaintenanceWork>) => set("services", value.services.map((s, i) => i === index ? { ...s, ...patch } : s));
  const error = maintenanceError(value);
  const responsibility = ["Company", ...(expense?.driverId ? ["Driver"] : []), ...(!truck?.isCompanyOwned && truck?.ownerId && truck.ownerDriverId !== truck.driverId ? ["Truck Owner"] : [])];
  const finance = <K extends keyof NonNullable<MaintenanceInput["expense"]>>(key: K, next: NonNullable<MaintenanceInput["expense"]>[K]) => { if (expense) set("expense", { ...expense, [key]: next }); };
  return <div className="space-y-6">
    <FormSection title="Service visit">
      <Field label="Truck"><select aria-label="Maintenance truck" required={value.status === "completed"} className={controlClass} value={value.truckId ?? ""} onChange={e => onChange(assignMaintenanceTruck(value, e.target.value, options))}><option value="">Needs truck link</option>{options.trucks.map(t => <option key={t.id} value={t.id}>{t.unitNumber}{t.active ? "" : " (inactive)"}</option>)}</select></Field>
      <Field label="Status"><select aria-label="Maintenance status" className={controlClass} value={value.status} onChange={e => set("status", e.target.value as MaintenanceInput["status"])}><option value="completed">Completed work</option><option value="needs_review">Needs review</option></select></Field>
      <Field label="Service date"><input aria-label="Maintenance service date" type="date" max={today()} required={value.status === "completed"} className={controlClass} value={value.serviceDate} onChange={e => set("serviceDate", e.target.value)} /></Field>
      <Field label="Odometer (miles)"><input aria-label="Maintenance odometer" type="number" min="0" max="2000000000" step="1" className={controlClass} value={value.odometer ?? ""} onChange={e => set("odometer", e.target.value === "" ? null : Number(e.target.value))} placeholder="Unknown" /></Field>
      <Field label="Service location"><select aria-label="Service location" className={controlClass} value={value.location} onChange={e => set("location", e.target.value as MaintenanceInput["location"])}>{["shop", "roadside", "internal", "unknown"].map(v => <option key={v} value={v}>{v[0].toUpperCase() + v.slice(1)}</option>)}</select></Field>
      <Field label="Work-order number"><input aria-label="Work-order number" maxLength={500} className={controlClass} value={value.workOrder} onChange={e => set("workOrder", e.target.value)} /></Field>
    </FormSection>
    <FormSection title="Vendor">
      <Field label="Vendor"><select aria-label="Maintenance vendor" className={controlClass} value={value.vendorId ?? ""} onChange={e => { const v = options.vendors.find(v => v.id === e.target.value); onChange({ ...value, vendorId: v?.id ?? null, vendorName: v?.name ?? "", vendorAddress: v?.address ?? "", vendorPhone: v?.phone ?? "", vendorContact: v?.contact ?? "" }); }}><option value="">One-off / unknown vendor</option>{options.vendors.filter(v => v.active || v.id === value.vendorId).map(v => <option key={v.id} value={v.id}>{v.name}{v.active ? "" : " (archived)"}</option>)}</select></Field>
      <Field label="Vendor name"><input aria-label="Vendor name" maxLength={200} className={controlClass} value={value.vendorName} onChange={e => set("vendorName", e.target.value)} /></Field>
      <Field label="Vendor address"><input aria-label="Vendor address" maxLength={500} className={controlClass} value={value.vendorAddress} onChange={e => set("vendorAddress", e.target.value)} /></Field>
      <Field label="Vendor phone"><input aria-label="Vendor phone" maxLength={500} className={controlClass} value={value.vendorPhone} onChange={e => set("vendorPhone", e.target.value)} /></Field>
      <Field label="Vendor contact" wide><input aria-label="Vendor contact" maxLength={500} className={controlClass} value={value.vendorContact} onChange={e => set("vendorContact", e.target.value)} /></Field>
    </FormSection>
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="text-[13px] font-semibold text-zinc-200">Work performed</h3><button type="button" className="ui-button inline-flex items-center gap-1" disabled={value.services.length >= 100} onClick={() => set("services", [...value.services, { typeCode: "other", name: "", amount: priced ? "" : null, trackingCode: "" }])}><Plus className="h-4 w-4" />Add work</button></div>
      {expense && <Toggle label="Itemized service costs" description="Leave off when the invoice does not give a reliable cost allocation." checked={priced} onChange={next => set("services", value.services.map(s => ({ ...s, amount: next ? "" : null })))} />}
      {value.services.map((work, index) => <div key={index} className="grid gap-3 rounded-lg border border-zinc-800 p-3 sm:grid-cols-2">
        <Field label="Service type"><select aria-label={`Work ${index + 1} service type`} required className={controlClass} value={work.typeCode} onChange={e => updateWork(index, { typeCode: e.target.value, trackingCode: "", name: "" })}>{options.types.filter(t => t.active || t.code === work.typeCode).map(t => <option key={t.code} value={t.code}>{t.name}{t.active ? "" : " (archived)"}</option>)}</select></Field>
        <Field label="Work name"><ExpenseNameSelect value={work.name} names={options.names.filter(n => serviceTypeForName(n) === work.typeCode)} label={`Work ${index + 1} name`} onChange={name => updateWork(index, { name })} /></Field>
        {priced && <Field label="Cost"><input aria-label={`Work ${index + 1} cost`} type="number" step="0.01" required className={controlClass} value={work.amount ?? ""} onChange={e => updateWork(index, { amount: e.target.value })} /></Field>}
        {work.typeCode === "preventive" && <Field label="Confirmed service completion"><select aria-label={`Work ${index + 1} completion`} className={controlClass} value={work.trackingCode} onChange={e => updateWork(index, { trackingCode: e.target.value as MaintenanceWork["trackingCode"] })}><option value="">Does not reset a schedule</option><option value="pm">PM service completed</option><option value="oil">Oil change completed</option></select></Field>}
        <button type="button" className="justify-self-end rounded-md p-1 text-zinc-500 hover:text-red-400 sm:col-span-2" aria-label={`Remove work ${index + 1}`} disabled={value.services.length === 1} onClick={() => set("services", value.services.filter((_, i) => i !== index))}><Trash2 className="h-4 w-4" /></button>
      </div>)}
      {error && <p role="status" className="text-xs text-red-400">{error}</p>}
    </section>
    <Toggle label="Record an invoice / charge" checked={!!expense} onChange={next => { if (!billed) onChange({ ...value, services: value.services.map(s => ({ ...s, amount: null })), expense: next ? { ...emptyExpenseInput, categoryId: options.categoryId, truckId: value.truckId, paidBy: options.currentUser, expenseDate: value.serviceDate || today() } : null }); }} description={billed ? "This visit retains its existing financial record." : undefined} />
    {expense && <FormSection title="Invoice & payment">
      <Field label="Invoice name"><ExpenseNameSelect value={expense.expenseType} names={options.names} label="Maintenance invoice name" onChange={next => finance("expenseType", next)} /></Field>
      <Field label="Invoice date"><input aria-label="Maintenance invoice date" type="date" required className={controlClass} value={expense.expenseDate} onChange={e => finance("expenseDate", e.target.value)} /></Field>
      <Field label="Amount before payment fee"><input aria-label="Maintenance invoice amount" type="number" step="0.01" min="0" required className={controlClass} value={expense.amount} onChange={e => finance("amount", e.target.value)} />{value.legacyPaymentTotal === expense.amount && expense.paymentType.trim().toLowerCase() === "efs" ? <p className="text-xs text-zinc-500">Saved historical total; the existing payment fee is preserved.</p> : <ExpenseFeePreview amount={expense.amount} paymentType={expense.paymentType} />}</Field>
      <Field label="Invoice / reference number"><input aria-label="Maintenance reference number" className={controlClass} value={expense.referenceNumber} onChange={e => finance("referenceNumber", e.target.value)} /></Field>
      <Field label="Payment method"><input aria-label="Maintenance payment method" list="maintenance-payment-methods" className={controlClass} value={expense.paymentType} onChange={e => finance("paymentType", e.target.value)} /><datalist id="maintenance-payment-methods">{options.paymentMethods.map(m => <option key={m} value={m} />)}</datalist></Field>
      <Field label="Paid by"><ExpensePayerSelect value={expense.paidBy} users={options.users} label="Maintenance paid by" onChange={next => finance("paidBy", next)} className={controlClass} /></Field>
      <Field label="Who will cover"><select aria-label="Maintenance responsibility" className={controlClass} value={expense.coveredBy} onChange={e => set("expense", { ...expense, coveredBy: e.target.value, ownerId: e.target.value === "Truck Owner" ? truck?.ownerId ?? null : null })}>{!responsibility.includes(expense.coveredBy) && <option value={expense.coveredBy}>{expense.coveredBy} (saved)</option>}{responsibility.map(v => <option key={v} value={v}>{v}{v === "Truck Owner" ? ` · ${expense.ownerId && expense.ownerId !== truck?.ownerId ? value.savedOwnerName || "Saved owner" : truck?.ownerName || "Saved owner"}` : ""}</option>)}</select></Field>
      <Field label="Charged driver"><select aria-label="Maintenance driver" className={controlClass} value={expense.driverId ?? ""} onChange={e => finance("driverId", e.target.value || null)}><option value="">No linked driver</option>{expense.driverId && !options.trucks.some(t => t.driverId === expense.driverId) && <option value={expense.driverId}>{expense.driverName || "Saved driver"}</option>}{Array.from(new Map(options.trucks.filter(t => t.driverId).map(t => [t.driverId!, t.driverName])).entries()).map(([id, name]) => <option key={id} value={id}>{name}</option>)}</select></Field>
    </FormSection>}
    <Field label="Service notes"><textarea aria-label="Maintenance notes" maxLength={5000} rows={3} className={controlClass} value={value.notes} onChange={e => set("notes", e.target.value)} /></Field>
  </div>;
}
