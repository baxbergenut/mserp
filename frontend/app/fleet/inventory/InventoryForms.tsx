"use client";

import { useState } from "react";
import { ErrorBanner, Field, Modal, controlClass } from "@/app/components/management/ManagementUI";
import { saveInventoryRecord } from "@/app/lib/api";
import { formatPhone } from "@/app/lib/phone";
import type { InventoryCollection, InventoryData, InventoryFields, InventoryRecord, TruckOption } from "@/app/lib/types";
import { carrierStates, conditionStates, deviceStates, presenceStates, readable } from "./InventoryStatus";

export { readable } from "./InventoryStatus";
export const inventoryPhone = (value?: string) => formatPhone(value) || value || "";
export const inventoryLabel = (record?: InventoryRecord) => record ? record.data.unitNumber || record.data.name || record.data.imei || record.data.serial || record.data.assetLabel || inventoryPhone(record.data.wirelessNumber) || record.id : "Unrecorded";
export const newInventoryRecord = (data: Partial<InventoryFields> = {}): InventoryRecord => ({ id: crypto.randomUUID(), version: 0, data: { active: true, ...data }, updatedAt: "", updatedBy: "" });
export type InventoryContext = { data: InventoryData; trucks: TruckOption[] };
type Descriptor = { key: keyof InventoryFields; label: string; options?: string[]; date?: boolean; required?: boolean };
const descriptors: Record<InventoryCollection, Descriptor[]> = {
  assets: [
    { key: "deviceType", label: "Device Type", options: ["tablet", "phone", "dashcam", "samsara_gateway"], required: true },
    { key: "brand", label: "Brand", options: ["Samsara", "FiveELD"], required: true },
    { key: "imei", label: "IMEI" }, { key: "serial", label: "Serial" }, { key: "assetLabel", label: "Internal Asset Label" },
    { key: "make", label: "Make" }, { key: "model", label: "Model" },
    { key: "condition", label: "Condition", options: conditionStates },
    { key: "status", label: "Operational Status", options: deviceStates },
    { key: "ownership", label: "Ownership", options: ["unknown", "owned", "rented", "contracted"] },
    { key: "equipmentState", label: "Installation Status", options: presenceStates },
  ],
  lines: [
    { key: "wirelessNumber", label: "Wireless Number (10 Digits)", required: true }, { key: "carrier", label: "Carrier" }, { key: "iccid", label: "SIM ICCID" },
    { key: "custodian", label: "Line User / Custodian" }, { key: "status", label: "Carrier Status", options: carrierStates }, { key: "plan", label: "Plan" },
    { key: "activationDate", label: "Activation Date", date: true }, { key: "contractStart", label: "Contract Start", date: true },
    { key: "contractEnd", label: "Contract End", date: true }, { key: "sourceUpdated", label: "Source Updated", date: true },
  ],
  locations: [{ key: "name", label: "Location Name", required: true }],
  supplies: [{ key: "name", label: "Supply Name", required: true }, { key: "batchYear", label: "Year (Annual Stickers)" }],
  equipment: [{ key: "name", label: "Equipment Name", required: true }, { key: "batchYear", label: "Sticker Year" }, { key: "equipmentState", label: "Presence", options: presenceStates, required: true }],
  trailers: [
    { key: "unitNumber", label: "Unit Number", required: true }, { key: "vin", label: "VIN" }, { key: "year", label: "Year" }, { key: "make", label: "Make" },
    { key: "plate", label: "Plate" }, { key: "plateState", label: "Plate State" }, { key: "storageLocation", label: "Storage Location" },
    { key: "condition", label: "Condition", options: conditionStates }, { key: "status", label: "Availability", options: ["stored_unused", "needs_review", "ready", "repair", "retired"] },
    { key: "usage", label: "Usage Notes" }, { key: "registrationUrl", label: "Registration HTTPS Link" },
  ],
  "transponder-notes": [
    { key: "providerEnvironment", label: "Provider Environment", required: true }, { key: "providerAccount", label: "Provider Account", required: true },
    { key: "serial", label: "Exact Transponder Tag", required: true }, { key: "custodian", label: "Local Custodian" }, { key: "storageLocation", label: "Local Location" },
    { key: "status", label: "Local Status", options: ["unknown", "active", "inactive", "lost", "retired"] },
  ],
};

export function InventoryRecordFields({ collection, value, onChange, context }: { collection: InventoryCollection; value: InventoryFields; onChange: (value: InventoryFields) => void; context: InventoryContext }) {
  const set = (key: keyof InventoryFields, text: string | boolean) => {
    const next = { ...value, [key]: text };
    if (key === "deviceType") { next.brand = text === "dashcam" ? "Samsara" : ""; next.gatewayId = ""; next.equipmentState = "unknown"; }
    if (key === "brand" && text !== "Samsara") { next.gatewayId = ""; next.equipmentState = "unknown"; }
    onChange(next);
  };
  const assignment = value.gatewayId ? "gatewayId" : value.truckId ? "truckId" : value.custodian ? "custodian" : value.locationId ? "locationId" : "";
  const [holder, setHolder] = useState(assignment);
  const gateway = context.data.assets.find(asset => asset.id === value.gatewayId);
  const canInstall = !!(value.truckId || gateway?.data.truckId);
  const gatewayType = value.deviceType === "samsara_gateway";
  const samsaraCamera = value.deviceType === "dashcam" && value.brand === "Samsara";
  const fields = descriptors[collection].filter(field => {
    if (collection !== "assets") return true;
    if (field.key === "brand") return value.deviceType === "dashcam";
    if (field.key === "imei") return !gatewayType && !samsaraCamera;
    if (field.key === "serial") return gatewayType || samsaraCamera;
    if (field.key === "assetLabel") return ["tablet", "phone"].includes(value.deviceType || "");
    if (field.key === "make") return !gatewayType && value.deviceType !== "dashcam";
    if (field.key === "model") return !gatewayType;
    return true;
  });
  const pick = (label: string, key: keyof InventoryFields, records: { id: string; label: string }[]) => <Field label={label}>
    <select aria-label={label} className={controlClass} value={String(value[key] || "")} onChange={event => onChange({ ...value, [key]: event.target.value, ...(collection === "assets" ? { equipmentState: "unknown" } : {}) })}>
      <option value="">Unassigned / Unknown</option>{records.map(record => <option key={record.id} value={record.id}>{record.label}</option>)}
    </select>
  </Field>;
  return <div className="grid gap-4 sm:grid-cols-2">
    {fields.map(field => <Field key={field.key} label={field.label}>
      {field.options ? <select aria-label={field.label} className={controlClass} required={field.required} value={String(value[field.key] || (field.options.includes("unknown") ? "unknown" : ""))} onChange={event => set(field.key, event.target.value)}>
        {!field.options.includes("unknown") && <option value="">Select…</option>}
        {field.options.map(option => <option key={option} value={option} disabled={collection === "assets" && field.key === "equipmentState" && !canInstall && ["reported_present", "verified_installed"].includes(option)}>{readable(option)}</option>)}
      </select> : <input aria-label={field.label} className={controlClass} type={field.date ? "date" : "text"} required={field.required || collection === "assets" && field.key === "serial"} maxLength={field.key === "registrationUrl" ? 2000 : 250} value={String(value[field.key] || "")} onChange={event => set(field.key, event.target.value)} />}
      {collection === "assets" && field.key === "equipmentState" && !canInstall && <p className="mt-1 text-xs text-zinc-500">Assign a truck, or link a gateway assigned to a truck, before recording installation.</p>}
    </Field>)}
    {collection === "assets" && <>
      <Field label="Assign Device To"><select aria-label="Assign Device To" className={controlClass} value={holder === "gatewayId" && !samsaraCamera ? "" : holder} onChange={event => { setHolder(event.target.value); onChange({ ...value, truckId: "", locationId: "", custodian: "", gatewayId: "", equipmentState: "unknown" }); }}>
        <option value="">Unassigned</option><option value="truckId">Truck</option><option value="locationId">Storage Location</option><option value="custodian">Named Custodian</option>{samsaraCamera && <option value="gatewayId">Samsara Gateway</option>}
      </select></Field>
      {holder === "truckId" && pick("Truck", "truckId", context.trucks.map(truck => ({ id: truck.id, label: truck.unitNumber })))}
      {holder === "locationId" && pick("Storage Location", "locationId", context.data.locations.filter(item => item.data.active).map(item => ({ id: item.id, label: inventoryLabel(item) })))}
      {holder === "gatewayId" && samsaraCamera && pick("Linked Gateway", "gatewayId", context.data.assets.filter(item => item.data.active && item.data.deviceType === "samsara_gateway" && !["lost", "retired"].includes(item.data.condition || "")).map(item => ({ id: item.id, label: inventoryLabel(item) })))}
      {holder === "custodian" && <Field label="Custodian"><input aria-label="Custodian" className={controlClass} value={value.custodian || ""} onChange={event => set("custodian", event.target.value)} /></Field>}
    </>}
    {collection === "lines" && pick("Linked Tablet / Phone", "assetId", context.data.assets.filter(item => item.data.active && ["tablet", "phone"].includes(item.data.deviceType || "")).map(item => ({ id: item.id, label: [inventoryLabel(item), item.data.make, item.data.model].filter(Boolean).join(" · ") })))}
    {collection === "equipment" && pick("Truck", "truckId", context.trucks.map(truck => ({ id: truck.id, label: truck.unitNumber })))}
    <Field label="Active"><input type="checkbox" checked={value.active} onChange={event => set("active", event.target.checked)} /></Field>
    <Field label="Notes" wide><textarea className={controlClass} rows={3} maxLength={5000} value={value.notes || ""} onChange={event => set("notes", event.target.value)} /></Field>
    {value.verifiedAt && <p className="text-xs text-emerald-400 sm:col-span-2">Verified {new Date(value.verifiedAt).toLocaleString()} by {value.verifiedBy}</p>}
    {value.source && <p className="break-all text-xs text-zinc-500 sm:col-span-2">Source: {value.source}</p>}
  </div>;
}

export function InventoryEditor({ collection, record, context, onClose, onSaved }: { collection: InventoryCollection; record: InventoryRecord; context: InventoryContext; onClose: () => void; onSaved: () => void }) {
  const [draft, setDraft] = useState(record.data), [requestId] = useState(() => crypto.randomUUID());
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const label = collection === "assets" ? readable(draft.deviceType) : collection === "equipment" ? "Truck Equipment" : collection === "trailers" ? "Trailer" : collection === "lines" ? "Service Line" : collection === "supplies" ? "Supply" : collection === "locations" ? "Location" : "Transponder Notes";
  return <Modal title={(record.version ? "Edit " : "Add ") + label} description={record.version ? inventoryLabel(record) : undefined} isSaving={busy} submitLabel="Save" onClose={onClose} onSubmit={event => {
    event.preventDefault(); if (busy) return;
    setBusy(true); setError("");
    const data = collection === "supplies" ? { ...draft, unit: draft.name === "IFTA Sticker" ? "pairs" : draft.unit || "units", ...(draft.name === "IFTA Sticker" ? { reusable: false } : {}) } : draft;
    void saveInventoryRecord(collection, { ...record, data }, requestId).then(onSaved).catch(reason => setError(reason.message)).finally(() => setBusy(false));
  }}>
    {error && <ErrorBanner message={error} />}<InventoryRecordFields collection={collection} value={draft} onChange={setDraft} context={context} />
  </Modal>;
}
