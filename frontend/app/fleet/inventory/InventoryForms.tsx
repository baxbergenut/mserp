"use client";

import { useState } from "react";
import { ErrorBanner, Field, Modal, controlClass } from "@/app/components/management/ManagementUI";
import { moveInventoryStock, saveInventoryRecord } from "@/app/lib/api";
import type { InventoryCollection, InventoryData, InventoryFields, InventoryMovement, InventoryRecord, TruckOption } from "@/app/lib/types";

export const inventoryLabel = (record?: InventoryRecord) => record ? record.data.unitNumber || record.data.name || record.data.assetLabel || record.data.imei || record.data.serial || record.data.wirelessNumber || record.id : "Unrecorded";
export const readable = (value?: string) => value ? value.replaceAll("_", " ") : "Unknown";
export const newInventoryRecord = (data: Partial<InventoryFields> = {}): InventoryRecord => ({ id: crypto.randomUUID(), version: 0, data: { active: true, ...data }, updatedAt: "", updatedBy: "" });
export type InventoryContext = { data: InventoryData; trucks: TruckOption[] };
type Descriptor = { key: keyof InventoryFields; label: string; options?: string[]; date?: boolean; required?: boolean };
const conditions = ["unknown", "good", "damaged", "repair", "lost", "retired"];
const states = ["unknown", "reported_present", "verified_installed", "missing"];
const descriptors: Record<InventoryCollection, Descriptor[]> = {
  assets: [
    { key: "deviceType", label: "Device type", options: ["tablet", "samsara_gateway", "samsara_camera", "tt_camera", "pt30", "other"], required: true },
    { key: "imei", label: "IMEI" }, { key: "serial", label: "Serial" }, { key: "assetLabel", label: "Internal asset label" },
    { key: "make", label: "Make" }, { key: "model", label: "Model" }, { key: "condition", label: "Condition", options: conditions },
    { key: "status", label: "Device status" }, { key: "ownership", label: "Ownership", options: ["unknown", "owned", "rented", "contracted"] },
    { key: "equipmentState", label: "Installation / presence", options: states },
  ],
  lines: [ { key: "wirelessNumber", label: "Wireless number (10 digits)", required: true }, { key: "carrier", label: "Carrier" }, { key: "iccid", label: "SIM ICCID" },
    { key: "custodian", label: "Line user / custodian" }, { key: "status", label: "Carrier service status" }, { key: "plan", label: "Plan" },
    { key: "activationDate", label: "Activation date", date: true }, { key: "contractStart", label: "Contract start", date: true }, { key: "contractEnd", label: "Contract end", date: true }, { key: "sourceUpdated", label: "Source updated", date: true } ],
  locations: [{ key: "name", label: "Location name", required: true }],
  supplies: [{ key: "name", label: "Item name", required: true }, { key: "unit", label: "Count unit", options: ["units", "sets", "pairs"], required: true }, { key: "batchYear", label: "Batch year (annual stickers)" }],
  equipment: [{ key: "name", label: "Equipment name", required: true }, { key: "batchYear", label: "Sticker year" }, { key: "equipmentState", label: "Installation / presence", options: states, required: true }, { key: "sentDate", label: "Sent date (if known)", date: true }, { key: "attachedDate", label: "Attached date (if known)", date: true }],
  trailers: [ { key: "unitNumber", label: "Unit number", required: true }, { key: "vin", label: "VIN" }, { key: "year", label: "Year" }, { key: "make", label: "Make" },
    { key: "plate", label: "Plate" }, { key: "plateState", label: "Plate state" }, { key: "storageLocation", label: "Storage location" },
    { key: "condition", label: "Condition", options: conditions }, { key: "status", label: "Availability", options: ["stored_unused", "needs_review", "ready", "repair", "retired"] }, { key: "usage", label: "Usage notes" }, { key: "registrationUrl", label: "Registration HTTPS link" } ],
  "transponder-notes": [{ key: "providerEnvironment", label: "Provider environment", required: true }, { key: "providerAccount", label: "Provider account", required: true }, { key: "serial", label: "Exact transponder tag", required: true }, { key: "custodian", label: "Local custodian" }, { key: "storageLocation", label: "Local location" }, { key: "status", label: "Local custody status" }],
};

export function InventoryRecordFields({ collection, value, onChange, context }: { collection: InventoryCollection; value: InventoryFields; onChange: (value: InventoryFields) => void; context: InventoryContext }) {
  const set = (key: keyof InventoryFields, text: string | boolean) => onChange({ ...value, [key]: text });
  const [assignment, setAssignment] = useState(value.gatewayId ? "gatewayId" : value.truckId ? "truckId" : value.custodian ? "custodian" : value.locationId ? "locationId" : "");
  const fields = descriptors[collection].filter(field => collection !== "assets" || value.deviceType !== "samsara_gateway" || !["imei", "make", "model"].includes(field.key) || !!value[field.key]);
  const pick = (label: string, key: keyof InventoryFields, records: { id: string; label: string }[], extra?: (id: string) => Partial<InventoryFields>) => <Field label={label}><select aria-label={label} className={controlClass} value={String(value[key] || "")} onChange={event => onChange({ ...value, [key]: event.target.value, ...extra?.(event.target.value) })}><option value="">Unassigned / unknown</option>{records.map(record => <option key={record.id} value={record.id}>{record.label}</option>)}</select></Field>;
  return <div className="grid gap-4 sm:grid-cols-2">
    {fields.map(field => <Field key={field.key} label={field.label}>{field.options ? <select aria-label={field.label} className={controlClass} required={field.required} value={String(value[field.key] || "")} onChange={event => set(field.key, event.target.value)}><option value="">Select…</option>{field.options.map(option => <option key={option} value={option}>{readable(option)}</option>)}</select> : <input aria-label={field.label} className={controlClass} type={field.date ? "date" : "text"} required={field.required} maxLength={field.key === "registrationUrl" ? 2000 : 250} value={String(value[field.key] || "")} onChange={event => set(field.key, event.target.value)} />}</Field>)}
    {collection === "assets" && <>
      <Field label="Assign device to"><select className={controlClass} value={assignment} onChange={event => { setAssignment(event.target.value); onChange({ ...value, truckId: "", locationId: "", custodian: "", gatewayId: "", equipmentState: "unknown" }); }}><option value="">Unassigned</option><option value="truckId">Truck</option><option value="locationId">Storage location</option><option value="custodian">Named custodian</option>{value.deviceType === "samsara_camera" && <option value="gatewayId">Samsara gateway</option>}</select></Field>
      {assignment === "truckId" && pick("Truck", "truckId", context.trucks.map(truck => ({ id: truck.id, label: truck.unitNumber })))}
      {assignment === "locationId" && pick("Storage location", "locationId", context.data.locations.filter(item => item.data.active).map(item => ({ id: item.id, label: inventoryLabel(item) })))}
      {assignment === "gatewayId" && pick("Linked gateway", "gatewayId", context.data.assets.filter(item => item.data.active && item.data.deviceType === "samsara_gateway" && item.id !== value.assetId).map(item => ({ id: item.id, label: inventoryLabel(item) })))}
      {assignment === "custodian" && <Field label="Custodian"><input className={controlClass} value={value.custodian || ""} onChange={event => set("custodian", event.target.value)} /></Field>}
    </>}
    {collection === "lines" && pick("Linked tablet / phone", "assetId", context.data.assets.filter(item => item.data.active && ["tablet", "other"].includes(item.data.deviceType || "")).map(item => ({ id: item.id, label: `${inventoryLabel(item)} · ${item.data.make || ""} ${item.data.model || ""}` })))}
    {collection === "equipment" && <>
      {pick("Truck", "truckId", context.trucks.map(truck => ({ id: truck.id, label: truck.unitNumber })))}
      {pick("Supply batch (optional)", "supplyId", context.data.supplies.filter(item => item.data.active).map(item => ({ id: item.id, label: `${inventoryLabel(item)} ${item.data.batchYear || ""}` })), id => { const supply = context.data.supplies.find(item => item.id === id); return supply ? { name: supply.data.name, batchYear: supply.data.batchYear } : {}; })}
      {([ ["sent", "Issued / sent"], ["attached", "Attached"] ] as const).map(([key, label]) => <Field key={key} label={label}><input type="checkbox" checked={!!value[key]} onChange={event => onChange({ ...value, [key]: event.target.checked, ...(!event.target.checked ? { [key === "sent" ? "sentDate" : "attachedDate"]: "" } : {}) })} /></Field>)}
    </>}
    {collection === "supplies" && <Field label="Reusable (can be returned)"><input type="checkbox" checked={!!value.reusable} onChange={event => set("reusable", event.target.checked)} /></Field>}
    <Field label="Active"><input type="checkbox" checked={value.active} onChange={event => set("active", event.target.checked)} /></Field>
    <Field label="Notes" wide><textarea className={controlClass} rows={3} maxLength={5000} value={value.notes || ""} onChange={event => set("notes", event.target.value)} /></Field>
    {value.verifiedAt && <p className="text-xs text-emerald-400 sm:col-span-2">Verified {new Date(value.verifiedAt).toLocaleString()} by {value.verifiedBy}</p>}
    {value.source && <p className="break-all text-xs text-zinc-500 sm:col-span-2">Source: {value.source}</p>}
  </div>;
}

export function InventoryEditor({ collection, record, context, onClose, onSaved }: { collection: InventoryCollection; record: InventoryRecord; context: InventoryContext; onClose: () => void; onSaved: () => void }) {
  const [draft, setDraft] = useState(record.data), [requestId] = useState(() => crypto.randomUUID());
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  return <Modal title={`${record.version ? "Edit" : "Add"} ${collection === "assets" ? readable(draft.deviceType || "device") : collection === "equipment" ? "truck equipment" : collection === "trailers" ? "trailer" : collection === "lines" ? "service line" : collection === "supplies" ? "supply item" : collection === "locations" ? "location" : "transponder notes"}`} description={record.version ? inventoryLabel(record) : undefined} isSaving={busy} submitLabel="Save" onClose={onClose} onSubmit={event => { event.preventDefault(); if (busy) return; setBusy(true); setError(""); void saveInventoryRecord(collection, { ...record, data: draft }, requestId).then(onSaved).catch(reason => setError(reason.message)).finally(() => setBusy(false)); }}>
    {error && <ErrorBanner message={error} />}<InventoryRecordFields collection={collection} value={draft} onChange={setDraft} context={context} />
  </Modal>;
}

export function StockMovementDialog({ context, initial, onClose, onSaved }: { context: InventoryContext; initial?: InventoryMovement; onClose: () => void; onSaved: () => void }) {
  const [draft, setDraft] = useState<InventoryMovement>(() => initial || ({ id: crypto.randomUUID(), supplyId: context.data.supplies.find(item => item.data.active)?.id || "", kind: "opening", from: "external", to: `location:${context.data.locations.find(item => item.data.active)?.id || ""}`, quantity: 1, reason: "", physicalCount: true, actor: "", createdAt: "" }));
  const [error, setError] = useState(""), [busy, setBusy] = useState(false);
  const holders = [...context.data.locations.filter(item => item.data.active).map(item => ({ id: `location:${item.id}`, label: `Storage · ${inventoryLabel(item)}` })), ...context.trucks.map(truck => ({ id: `truck:${truck.id}`, label: `Truck · ${truck.unitNumber}` }))];
  const holderField = (key: "from" | "to", label: string) => <Field label={label}><select aria-label={label} className={controlClass} value={draft[key].startsWith("custodian:") ? "custodian:" : draft[key]} onChange={event => setDraft({ ...draft, [key]: event.target.value })}><option value="">Select…</option>{holders.map(holder => <option key={holder.id} value={holder.id}>{holder.label}</option>)}<option value="custodian:">Named custodian…</option></select>{draft[key].startsWith("custodian:") && <input aria-label={`${label} custodian`} className={`${controlClass} mt-2`} placeholder="Custodian name" value={draft[key].slice(10)} onChange={event => setDraft({ ...draft, [key]: `custodian:${event.target.value}` })} />}</Field>;
  const kind = draft.kind;
  return <Modal title={kind === "reversal" ? "Reverse stock movement" : "Stock movement"} isSaving={busy} submitLabel="Record movement" onClose={onClose} onSubmit={event => { event.preventDefault(); if (busy) return; setBusy(true); setError(""); void moveInventoryStock(draft).then(onSaved).catch(reason => setError(reason.message)).finally(() => setBusy(false)); }}>
    {error && <ErrorBanner message={error} />}
    <div className="grid gap-4 sm:grid-cols-2">
      <Field label="Supply item / year"><select aria-label="Supply item / year" className={controlClass} disabled={kind === "reversal"} value={draft.supplyId} onChange={event => setDraft({ ...draft, supplyId: event.target.value })}>{context.data.supplies.filter(item => item.data.active).map(item => <option key={item.id} value={item.id}>{inventoryLabel(item)} {item.data.batchYear || ""} · {item.data.unit}</option>)}</select></Field>
      <Field label="Movement"><select aria-label="Movement" className={controlClass} disabled={kind === "reversal"} value={kind} onChange={event => { const next = event.target.value; const location = holders.find(item => item.id.startsWith("location:"))?.id || ""; setDraft({ ...draft, kind: next, from: ["opening", "receipt"].includes(next) ? "external" : location, to: next === "use" ? "consumed" : ["lost", "damaged"].includes(next) ? next : next === "issue" ? "" : location, physicalCount: next === "opening" }); }}>{["opening", "receipt", "issue", "return", "transfer", "use", "lost", "damaged", "adjustment", ...(kind === "reversal" ? ["reversal"] : [])].map(value => <option key={value} value={value}>{readable(value)}</option>)}</select></Field>
      {kind === "reversal" ? <p className="text-sm text-zinc-400 sm:col-span-2">Reverse exactly {draft.quantity} from {draft.from} to {draft.to}. The original entry remains in history.</p> : <>
        {!["opening", "receipt"].includes(kind) && draft.from !== "external" && holderField("from", "From")}
        {!["use", "lost", "damaged"].includes(kind) && draft.to !== "external" && holderField("to", "To")}
        {kind === "adjustment" && <Field label="Adjustment direction"><select className={controlClass} value={draft.from === "external" ? "add" : "remove"} onChange={event => { const location = holders.find(item => item.id.startsWith("location:"))?.id || ""; setDraft({ ...draft, from: event.target.value === "add" ? "external" : location, to: event.target.value === "add" ? location : "external" }); }}><option value="add">Add counted stock</option><option value="remove">Remove counted stock</option></select></Field>}
      </>}
      <Field label="Quantity"><input className={controlClass} type="number" min={kind === "opening" || kind === "adjustment" ? 0 : 1} max={1000000} step={1} required disabled={kind === "reversal"} value={draft.quantity} onChange={event => setDraft({ ...draft, quantity: Number(event.target.value) })} /></Field>
      {["opening", "adjustment"].includes(kind) && <Field label="Physically counted and verified"><input type="checkbox" checked={draft.physicalCount} onChange={event => setDraft({ ...draft, physicalCount: event.target.checked })} /></Field>}
      <Field label="Reason / count evidence" wide><textarea className={controlClass} required maxLength={1000} value={draft.reason} onChange={event => setDraft({ ...draft, reason: event.target.value })} /></Field>
    </div>
  </Modal>;
}
