"use client";

import { useState } from "react";
import { saveMaintenanceReading, saveMaintenanceSchedule, saveMaintenanceType, saveMaintenanceVendor } from "../lib/api";
import type { MaintenanceOptions, MaintenanceReading, MaintenanceSchedule, MaintenanceType, MaintenanceVendor } from "../lib/types";
import { controlClass, ErrorBanner, Field, FormSection, Modal } from "../components/management/ManagementUI";
import { today } from "./maintenance";

const nullableInt = (value: string) => value === "" ? null : Number(value);
export function MaintenanceScheduleEditor({ initial, options, onClose, onSaved, fleetOnly = false }: { fleetOnly?: boolean; initial: MaintenanceSchedule; options: MaintenanceOptions; onClose: () => void; onSaved: () => Promise<void> }) {
  const [value, setValue] = useState(initial); const [busy, setBusy] = useState(false); const [error, setError] = useState("");
  return <Modal title="Service interval" isSaving={busy} submitLabel="Save interval" onClose={onClose} onSubmit={e => { e.preventDefault(); e.stopPropagation(); setBusy(true); setError(""); void saveMaintenanceSchedule(value).then(onSaved).then(onClose).catch(e => setError(e.message)).finally(() => setBusy(false)); }}>
    {error && <ErrorBanner message={error} />}<FormSection title="Schedule">
      <Field label="Truck / fleet default"><select aria-label="Schedule truck" disabled={fleetOnly || !!value.id} className={controlClass} value={value.truckId ?? ""} onChange={e => setValue({ ...value, truckId: e.target.value || null })}>{options.administrator && <option value="">Fleet default</option>}{options.trucks.map(t => <option key={t.id} value={t.id}>{t.unitNumber}</option>)}</select></Field>
      <Field label="Tracked service"><span className="text-sm text-zinc-300">PM Service</span></Field>
      <Field label="Interval miles"><input aria-label="Interval miles" type="number" min="1" step="1" className={controlClass} value={value.intervalMiles ?? ""} onChange={e => setValue({ ...value, intervalMiles: nullableInt(e.target.value) })} /></Field>
      <Field label="Interval days"><input aria-label="Interval days" type="number" min="1" step="1" className={controlClass} value={value.intervalDays ?? ""} onChange={e => setValue({ ...value, intervalDays: nullableInt(e.target.value) })} /></Field>
      <Field label="Remind within miles"><input aria-label="Remind within miles" type="number" min="0" step="1" required className={controlClass} value={value.warningMiles} onChange={e => setValue({ ...value, warningMiles: Number(e.target.value) })} /></Field>
      <Field label="Remind within days"><input aria-label="Remind within days" type="number" min="0" step="1" required className={controlClass} value={value.warningDays} onChange={e => setValue({ ...value, warningDays: Number(e.target.value) })} /></Field>
    </FormSection><label className="mt-4 flex items-center gap-2 text-xs text-zinc-400"><input type="checkbox" checked={value.enabled} onChange={e => setValue({ ...value, enabled: e.target.checked })} />Enabled</label>
  </Modal>;
}
export function MaintenanceReadingEditor({ initial, options, onClose, onSaved }: { initial: MaintenanceReading; options: MaintenanceOptions; onClose: () => void; onSaved: () => Promise<void> }) {
  const [value, setValue] = useState(initial); const [busy, setBusy] = useState(false); const [error, setError] = useState("");
  return <Modal title={value.id ? "Correct odometer reading" : "Record odometer"} isSaving={busy} submitLabel="Save reading" onClose={onClose} onSubmit={e => { e.preventDefault(); e.stopPropagation(); setBusy(true); setError(""); void saveMaintenanceReading(value).then(onSaved).then(onClose).catch(e => setError(e.message)).finally(() => setBusy(false)); }}>
    {error && <ErrorBanner message={error} />}<FormSection title="Dated reading"><Field label="Truck"><select aria-label="Odometer truck" disabled={!!value.id} required className={controlClass} value={value.truckId} onChange={e => setValue({ ...value, truckId: e.target.value })}><option value="">Select truck</option>{options.trucks.map(t => <option key={t.id} value={t.id}>{t.unitNumber}</option>)}</select></Field><Field label="Reading date"><input aria-label="Odometer date" type="date" max={today()} required className={controlClass} value={value.date} onChange={e => setValue({ ...value, date: e.target.value })} /></Field><Field label="Odometer (miles)"><input aria-label="Odometer miles" type="number" min="0" max="2000000000" step="1" required className={controlClass} value={value.mileage} onChange={e => setValue({ ...value, mileage: Number(e.target.value) })} /></Field></FormSection>
  </Modal>;
}
export function MaintenanceVendorEditor({ initial, onClose, onSaved }: { initial: MaintenanceVendor; onClose: () => void; onSaved: (vendor: MaintenanceVendor) => Promise<void> }) {
  const [value, setValue] = useState(initial); const [busy, setBusy] = useState(false); const [error, setError] = useState("");
  return <Modal title={value.id ? "Edit maintenance vendor" : "Add maintenance vendor"} isSaving={busy} submitLabel="Save vendor" onClose={onClose} onSubmit={e => { e.preventDefault(); e.stopPropagation(); setBusy(true); setError(""); void saveMaintenanceVendor(value).then(onSaved).then(onClose).catch(e => setError(e.message)).finally(() => setBusy(false)); }}>
    {error && <ErrorBanner message={error} />}<FormSection title="Vendor details">{(["name", "address", "phone", "contact"] as const).map(key => <Field key={key} label={key[0].toUpperCase() + key.slice(1)}><input aria-label={`Vendor directory ${key}`} required={key === "name"} maxLength={key === "name" ? 200 : 500} className={controlClass} value={value[key]} onChange={e => setValue({ ...value, [key]: e.target.value })} /></Field>)}</FormSection><label className="mt-4 flex items-center gap-2 text-xs text-zinc-400"><input type="checkbox" checked={value.active} onChange={e => setValue({ ...value, active: e.target.checked })} />Active</label>
  </Modal>;
}
export function MaintenanceTypeEditor({ initial, onClose, onSaved }: { initial: MaintenanceType; onClose: () => void; onSaved: () => Promise<void> }) {
  const [value, setValue] = useState(() => ({...initial,code:initial.code || "type_"+crypto.randomUUID().replaceAll("-", "")})); const [busy, setBusy] = useState(false); const [error, setError] = useState("");
  return <Modal title={value.version ? "Edit service type" : "Add service type"} isSaving={busy} submitLabel="Save type" onClose={onClose} onSubmit={e => { e.preventDefault(); e.stopPropagation(); setBusy(true); setError(""); void saveMaintenanceType(value).then(onSaved).then(onClose).catch(e => setError(e.message)).finally(() => setBusy(false)); }}>
    {error && <ErrorBanner message={error} />}<FormSection title="Reporting type"><Field label="Name"><input aria-label="Service type name" required maxLength={100} className={controlClass} value={value.name} onChange={e => setValue({ ...value, name: e.target.value })} /></Field></FormSection><label className="mt-4 flex items-center gap-2 text-xs text-zinc-400"><input type="checkbox" checked={value.active} onChange={e => setValue({ ...value, active: e.target.checked })} />Active</label>
  </Modal>;
}
