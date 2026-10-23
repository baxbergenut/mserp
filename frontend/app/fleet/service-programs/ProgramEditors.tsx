"use client";

import { useEffect, useState } from "react";
import { fetchProgramAssignees, saveMaintenance, saveProgramBaseline, saveProgramTrucks, saveServiceProgram } from "@/app/lib/api";
import type { MaintenanceOptions, ProgramTruck, ProgramTruckInput, ServiceProgram } from "@/app/lib/types";
import { controlClass, ErrorBanner, Field, FormSection, ManagementSearch, Modal } from "@/app/components/management/ManagementUI";
import { today } from "@/app/maintenance/maintenance";
import { ProgramServiceTypes } from "./ProgramServiceTypes";

const integer = (value: string) => value === "" ? null : Number(value);
type Intervals = Pick<ServiceProgram, "intervalMiles" | "intervalDays" | "intervalMonths">;
export function IntervalFields({ value, onChange }: { value: Intervals; onChange: (v: Intervals) => void }) {
  return <>
    <Field label="Repeat every (miles)"><input aria-label="Repeat every miles" className={controlClass} type="number" min="1" max="2000000000" step="1" value={value.intervalMiles ?? ""} onChange={e => onChange({ ...value, intervalMiles: integer(e.target.value) })} placeholder="Optional" /></Field>
    <Field label="Calendar interval"><div className="flex gap-2"><input aria-label="Repeat every time" className={controlClass} type="number" min="1" max={value.intervalMonths != null ? 1200 : 36500} step="1" value={value.intervalMonths ?? value.intervalDays ?? ""} onChange={e => onChange(value.intervalMonths != null ? { ...value, intervalMonths: integer(e.target.value), intervalDays: null } : { ...value, intervalDays: integer(e.target.value), intervalMonths: null })} placeholder="Optional" /><select aria-label="Calendar interval unit" className={`${controlClass} !w-32`} value={value.intervalMonths != null ? "months" : "days"} onChange={e => onChange({ ...value, intervalDays: e.target.value === "days" ? value.intervalMonths ?? value.intervalDays : null, intervalMonths: e.target.value === "months" ? value.intervalDays ?? value.intervalMonths ?? 1 : null })}><option value="days">Days</option><option value="months">Months</option></select></div></Field>
  </>;
}

export function ProgramEditor({ initial, options, onClose, onSaved }: { initial: ServiceProgram; options: MaintenanceOptions; onClose: () => void; onSaved: () => Promise<void> }) {
  const [value, setValue] = useState(initial), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const [choices, setChoices] = useState<{ people: { id: string; name: string }[]; roles: { id: string; name: string }[] } | null>(null);
  useEffect(() => { let active = true; fetchProgramAssignees().then(v => { if (active) setChoices(v); }).catch(e => { if (active) setError(e.message); }); return () => { active = false; }; }, []);
  return <Modal title={value.id ? "Edit service program" : "Add service program"} isSaving={busy} submitLabel="Save program" onClose={onClose} onSubmit={e => {
    e.preventDefault(); if (busy) return; setBusy(true); setError("");
    const serviceTypeCodes=value.serviceTypeCodes??[];
    const serviceTypeVersions={...Object.fromEntries(options.types.filter(t=>serviceTypeCodes.includes(t.code)).map(t=>[t.code,t.version])),...initial.serviceTypeVersions};
    void saveServiceProgram({...value,serviceTypeCodes,serviceTypeVersions}).then(onSaved).then(onClose).catch(e => setError(e.message)).finally(() => setBusy(false));
  }}>
    {error && <ErrorBanner message={error} />}
    <FormSection title="Program">
      <Field label="Name" wide><input aria-label="Program name" autoFocus required maxLength={100} className={controlClass} value={value.name} onChange={e => setValue({ ...value, name: e.target.value })} /></Field>
      <div className="space-y-1.5 sm:col-span-2"><span className="text-[12px] font-medium text-zinc-400">Expense types that reset this program</span><ProgramServiceTypes program={value} options={options} onChange={serviceTypeCodes=>setValue({...value,serviceTypeCodes})} /></div>
      <IntervalFields value={value} onChange={v => setValue({ ...value, ...v })} />
      <Field label="Remind before (miles)"><input aria-label="Program warning miles" className={controlClass} required type="number" min="0" max="2000000000" step="1" value={value.warningMiles} onChange={e => setValue({ ...value, warningMiles: Number(e.target.value) })} /></Field>
      <Field label="Remind before (days)"><input aria-label="Program warning days" className={controlClass} required type="number" min="0" max="36500" step="1" value={value.warningDays} onChange={e => setValue({ ...value, warningDays: Number(e.target.value) })} /></Field>
      <Field label="Service instructions" wide><textarea aria-label="Service instructions" className={controlClass} rows={3} maxLength={5000} value={value.instructions} onChange={e => setValue({ ...value, instructions: e.target.value })} /></Field>
    </FormSection>
    <p className="mt-2 text-xs text-zinc-500">Set mileage, calendar time, or both. The first limit reached makes service due. Interval changes recalculate assigned trucks; their service history stays intact.</p>
    <div className="my-4 flex flex-wrap gap-5 text-xs"><label className="flex items-center gap-2"><input type="checkbox" checked={value.active} onChange={e => setValue({ ...value, active: e.target.checked })} />Active</label><label className="flex items-center gap-2"><input type="checkbox" checked={value.useDefaultAssignees} onChange={e => setValue({ ...value, useDefaultAssignees: e.target.checked })} />Use default service reminder assignees</label></div>
    {!value.useDefaultAssignees && <FormSection title="Reminder assignees">
      <Field label="People"><div className="space-y-1">{choices?.people.map(p => <label key={p.id} className="flex min-h-8 items-center gap-2 text-xs"><input type="checkbox" checked={value.assigneeIds.includes(p.id)} onChange={e => setValue({ ...value, assigneeIds: e.target.checked ? [...value.assigneeIds, p.id] : value.assigneeIds.filter(id => id !== p.id) })} />{p.name}</label>)}</div></Field>
      <Field label="Roles"><div className="space-y-1">{choices?.roles.map(p => <label key={p.id} className="flex min-h-8 items-center gap-2 text-xs"><input type="checkbox" checked={value.assigneeRoleIds.includes(p.id)} onChange={e => setValue({ ...value, assigneeRoleIds: e.target.checked ? [...value.assigneeRoleIds, p.id] : value.assigneeRoleIds.filter(id => id !== p.id) })} />{p.name}</label>)}</div></Field>
    </FormSection>}
  </Modal>;
}

export function AddProgramTrucks({ program, members, options, truckId, onClose, onSaved }: { program: ServiceProgram; members: ProgramTruck[]; options: MaintenanceOptions; truckId?: string; onClose: () => void; onSaved: () => Promise<void> }) {
  const [selected, setSelected] = useState<string[]>([]), [search, setSearch] = useState(""), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const choices = options.trucks.filter(t => (!truckId || t.id === truckId) && !members.some(m => m.truckId === t.id && m.enrolled) && t.unitNumber.toLowerCase().includes(search.toLowerCase()));
  return <Modal title={`Add trucks · ${program.name}`} isSaving={busy} submitLabel={`Add ${selected.length || ""} trucks`} onClose={onClose} onSubmit={e => {
    e.preventDefault(); if (busy) return; if (!selected.length) { setError("Select at least one truck."); return; } setBusy(true); setError("");
    void saveProgramTrucks(program.id, selected.map(id => ({ truckId: id, version: members.find(m => m.truckId === id)?.memberVersion ?? 0, active: true, overrideInterval: false, intervalMiles: null, intervalDays: null, intervalMonths: null }))).then(onSaved).then(onClose).catch(e => setError(e.message)).finally(() => setBusy(false));
  }}>
    {error && <ErrorBanner message={error} />}<ManagementSearch value={search} onChange={setSearch} placeholder="Search trucks…" />
    <div className="mt-3 max-h-80 overflow-y-auto">{choices.map(t => <label key={t.id} className="flex min-h-8 items-center gap-2 border-b border-zinc-800 text-sm"><input type="checkbox" checked={selected.includes(t.id)} onChange={e => setSelected(e.target.checked ? [...selected, t.id] : selected.filter(id => id !== t.id))} />{t.unitNumber}{!t.active && <span className="text-xs text-zinc-500">Inactive</span>}</label>)}{!choices.length && <p className="py-4 text-sm text-zinc-500">No unassigned trucks match.</p>}</div>
    <p className="mt-3 text-xs text-zinc-500">Existing matching service records will be used. Trucks without service history need a last-service baseline.</p>
  </Modal>;
}

export function ProgramTruckEditor({ truck, onClose, onSaved }: { truck: ProgramTruck; onClose: () => void; onSaved: () => Promise<void> }) {
  const [value, setValue] = useState<ProgramTruckInput>({ truckId: truck.truckId, version: truck.memberVersion, active: truck.enrolled, overrideInterval: truck.overrideInterval, intervalMiles: truck.intervalMiles, intervalDays: truck.intervalDays, intervalMonths: truck.intervalMonths });
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  return <Modal title={`Program settings · ${truck.unitNumber}`} isSaving={busy} submitLabel="Save truck settings" onClose={onClose} onSubmit={e => { e.preventDefault(); if (busy) return; setBusy(true); setError(""); void saveProgramTrucks(truck.programId, [value]).then(onSaved).then(onClose).catch(e => setError(e.message)).finally(() => setBusy(false)); }}>
    {error && <ErrorBanner message={error} />}
    <div className="mb-4 space-y-3 text-sm"><label className="flex items-center gap-2"><input type="checkbox" checked={value.active} onChange={e => setValue({ ...value, active: e.target.checked })} />Assigned to {truck.programName}</label><label className="flex items-center gap-2"><input type="checkbox" checked={value.overrideInterval} onChange={e => setValue({ ...value, overrideInterval: e.target.checked })} />Use a different interval for this truck</label></div>
    {value.overrideInterval && <FormSection title="Truck interval"><IntervalFields value={value} onChange={v => setValue({ ...value, ...v })} /></FormSection>}
    <p className="mt-3 text-xs text-zinc-500">Removing this assignment stops reminders and retains service history.</p>
  </Modal>;
}

export function ProgramServiceEditor({ truck, program, options, baseline, onClose, onSaved }: { truck: ProgramTruck; program: ServiceProgram; options: MaintenanceOptions; baseline: boolean; onClose: () => void; onSaved: () => Promise<void> }) {
  const [id] = useState(() => crypto.randomUUID());
  const [date, setDate] = useState(baseline ? truck.lastDate ?? "" : today());
  const [miles, setMiles] = useState((baseline ? truck.lastMiles : truck.currentMiles)?.toString() ?? "");
  const [notes, setNotes] = useState("");
  const [programIds, setProgramIds] = useState([program.id]);
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const available = options.programs?.filter(p => p.active && options.programEnrollments?.some(m => m.programId === p.id && m.truckId === truck.truckId)) ?? [];
  return <Modal title={`${baseline ? "Update last service" : "Record completed service"} · ${truck.unitNumber}`} isSaving={busy} submitLabel={baseline ? "Save baseline" : "Record service"} onClose={onClose} onSubmit={e => {
    e.preventDefault(); if (busy) return; setBusy(true); setError("");
    const mileage = integer(miles), pm = options.programs?.some(p => p.legacyPM && programIds.includes(p.id));
    const save = baseline ? saveProgramBaseline(program.id, truck, date, mileage) : programIds.length ? saveMaintenance({ id, version: 0, programIds, truckId: truck.truckId, driverId: null, driverName: "", vendorId: null, vendorName: "", vendorAddress: "", vendorPhone: "", vendorContact: "", oilType: "", serviceDate: date, odometer: mileage, status: "completed", location: "internal", workOrder: "", notes: notes || `Completed ${options.programs?.filter(p => programIds.includes(p.id)).map(p => p.name).join(", ")}`, services: [{ typeCode: pm ? "preventive" : "other", name: pm ? "PM Service" : "Other maintenance", amount: null, trackingCode: pm ? "pm" : "" }], expense: null }) : Promise.reject(new Error("Select at least one completed program."));
    void save.then(onSaved).then(onClose).catch(e => setError(e.message)).finally(() => setBusy(false));
  }}>
    {error && <ErrorBanner message={error} />}
    {!baseline && <div className="mb-4 space-y-2">{available.map(p => <label className="flex items-center gap-2 text-sm" key={p.id}><input type="checkbox" checked={programIds.includes(p.id)} onChange={e => setProgramIds(e.target.checked ? [...programIds, p.id] : programIds.filter(id => id !== p.id))} />{p.name}</label>)}{program.instructions && <p className="whitespace-pre-wrap text-xs text-zinc-400">{program.instructions}</p>}</div>}
    <FormSection title={baseline ? "Last service" : "Completed service"}>
      <Field label="Service date"><input aria-label="Program service date" className={controlClass} required type="date" max={today()} value={date} onChange={e => setDate(e.target.value)} /></Field>
      <Field label="Service odometer"><input aria-label="Program service odometer" className={controlClass} type="number" min="0" max="2000000000" step="1" value={miles} onChange={e => setMiles(e.target.value)} placeholder="Unknown" /></Field>
      {!baseline && <Field label="Notes" wide><textarea aria-label="Program service notes" className={controlClass} rows={3} maxLength={5000} value={notes} onChange={e => setNotes(e.target.value)} /></Field>}
    </FormSection>
    <p className="mt-3 text-xs text-zinc-500">{baseline ? "Corrects the tracking baseline without editing an invoice. A newer or changed latest completed service supersedes it." : "Records completed service for program tracking. Select only programs actually completed during this service."}</p>
  </Modal>;
}
