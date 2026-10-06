"use client";
import { TruckCharges } from "./TruckCharges";

import { RememberedDetails } from "@/app/components/RememberedDetails";

import { useViewState } from "@/app/lib/viewMemory";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { Receipt } from "lucide-react";
import { bulkDriverCharges, createDriverCharges, deleteChargeType, fetchChargeHistory, fetchDriverCharges, fetchDrivers, previewDriverCharge, previewNewCharges, saveChargeType } from "@/app/lib/api";
import type { ChargeBulk, ChargeCreate, ChargeData, ChargeEvent, ChargeOccurrence, ChargeSchedule, ChargeType, Driver } from "@/app/lib/types";
import { controlClass, ErrorBanner, Field, ManagementHeader, ManagementSearch, Modal } from "@/app/components/management/ManagementUI";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";
import { payButtonClass } from "../driver-pay/DriverCard";
import RecurringMatrix from "./RecurringMatrix";
import ChargeNotice, { type ChargeNotification } from "./ChargeNotice";
import { useRecurringChargeSaves } from "./useRecurringChargeSaves";
import ChargeTypeFields from "./ChargeTypeFields";
import { eligibilityLabel, currentChargeWeek } from "./charges";

const money = (s: string) => decimalDisplay(hundredths(s), true);
function historyDescription(event: ChargeEvent) {
  const detail = event.details as { weekStart?: string; startWeek?: string; amount?: string; reason?: string; before?: { weekStart?: string; amount?: string }; after?: { weekStart?: string; amount?: string } };
  const week = detail.weekStart ?? detail.after?.weekStart ?? detail.startWeek;
  const amount = detail.after?.amount ?? detail.amount;
  return [week && `Week of ${week}`, detail.before?.amount && `Previous amount ${money(detail.before.amount)}`, amount && `Amount ${money(amount)}`, detail.reason].filter(Boolean).join(" · ") || "Change recorded";
}
const td = "border-b border-zinc-800 px-3 py-3 text-left text-xs";
const newType = (): ChargeType => ({ id: "", name: "", direction: "charge", amount: "", amounts: [""], eligibility: "calendar", rules: [], archived: false, version: 0 });
const newCharge = (kind: "recurring" | "installment", driverId: string, week: string): ChargeCreate => ({ kind, driverIds: driverId ? [driverId] : [], typeId: "", name: "", amount: "", total: "", installments: 0, startWeek: week, endWeek: null, eligibility: "calendar" });
function ScheduleTable({ rows }: { rows: ChargeOccurrence[] }) {
  const [limit, setLimit] = useState(52);
  return <><div className="max-h-80 overflow-auto rounded-lg border border-zinc-800"><table className="w-full text-xs"><thead><tr>{["Week of", "Description", "Deduction / credit", "Status"].map(v => <th key={v} className={td}>{v}</th>)}</tr></thead><tbody>{rows.slice(0, limit).map(row => <tr key={`${row.scheduleId}:${row.weekStart}`}><td className={td}><Link className="text-blue-400" href={`/accounting/driver-pay?weekStart=${row.weekStart}`}>{row.weekStart}</Link></td><td className={td}>{row.name}</td><td className={`${td} font-mono`}>{money(row.amount)}</td><td className={td}>{row.confirmedAt ? "Confirmed" : row.amount === "0.00" ? "Skipped" : row.overridden ? "Edited · unconfirmed" : "Scheduled"}</td></tr>)}</tbody></table>{!rows.length && <p className="p-4 text-sm text-zinc-500">No scheduled deductions. Check the effective weeks and pause status.</p>}</div>{rows.length > limit && <button type="button" className={`${payButtonClass} mt-2`} onClick={() => setLimit(limit + 52)}>Show more weeks ({rows.length} total)</button>}</>;
}

export default function DriverChargesPage() {
  const [data, setData] = useState<ChargeData | null>(null);
  const [drivers, setDrivers] = useState<Driver[]>([]);
  const [tab, setTab] = useViewState<"recurring" | "installment" | "types" | "trucks">("page:tab", "recurring");
  const [search, setSearch] = useViewState("page:search", "");
  const [driverFilter, setDriverFilter] = useViewState("page:driverFilter", "");
  const [typeFilter, setTypeFilter] = useViewState("page:typeFilter", "");
  const [showArchivedTypes, setShowArchivedTypes] = useViewState("RecurringMatrix:archived", false);
  const [status, setStatus] = useViewState("page:status", "all");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [typeForm, setTypeForm] = useState<ChargeType | null>(null);
  const [deletingType, setDeletingType] = useState<ChargeType | null>(null);
  const [create, setCreate] = useState<ChargeCreate | null>(null);
  const [createPreview, setCreatePreview] = useState<ChargeOccurrence[] | null>(null);
  const [countMode, setCountMode] = useState(false);
  const [bulk, setBulk] = useState<ChargeBulk | null>(null);
  const [bulkPreview, setBulkPreview] = useState(false);
  const [detail, setDetail] = useState<{ schedule: ChargeSchedule; rows: ChargeOccurrence[]; events: ChargeEvent[] } | null>(null);
  const [notice, setNotice] = useState<ChargeNotification | null>(null);
  const dismissNotice = useCallback(() => setNotice(null), []);
  const setMessage = useCallback((message: string, error = false) => setNotice({ message, error }), []);
  const recurringSaves = useRecurringChargeSaves(setData, setMessage);
  const week = data?.currentWeek ?? currentChargeWeek();
  useEffect(() => {
    let cancelled = false;
    Promise.all([fetchDriverCharges(), fetchDrivers()]).then(([charges, drivers]) => {
      if (cancelled) return;
      const activeDrivers = drivers.filter(d => d.active);
      setData(charges); setDrivers(activeDrivers);
      const q = new URLSearchParams(window.location.search); const driver = activeDrivers.find(d => d.id === q.get("driverId"))?.id ?? ""; if (q.has("driverId")) setDriverFilter(driver);
      if (q.get("tab") === "trucks") setTab("trucks");
      if (q.get("tab") === "installment") setTab("installment");
      const source = charges.schedules.find(s => s.id === q.get("scheduleId") && activeDrivers.some(d => d.id === s.driverId));
      if (source) { setTab(source.kind); void Promise.all([previewDriverCharge(source.id), fetchChargeHistory(source.id)]).then(([rows, events]) => { if (!cancelled) setDetail({ schedule: source, rows, events }); }).catch(err => { if (!cancelled) setError(err.message); }); }
      const action = q.get("action");
      if (action === "recurring" || action === "installment") { setTab(action); if(action === "installment") setCreate(newCharge(action, driver, charges.currentWeek)); }
    }).catch(err => { if (!cancelled) setError(err.message); });
    return () => { cancelled = true; };
  }, [setDriverFilter, setTab]);
  async function run(action: () => Promise<void>) {
    setError(""); setBusy(true);
    try { await recurringSaves.waitForSaves(); await action(); } catch (err) { setError(err instanceof Error ? err.message : "Unable to save charges"); } finally { setBusy(false); }
  }
  async function reload() {
    const [charges, drivers] = await Promise.all([fetchDriverCharges(), fetchDrivers()]);
    setData(charges); setDrivers(drivers.filter(d => d.active)); setSelected(new Set());
  }
  const activeDriverIds = new Set(drivers.map(d => d.id));
  // A remembered filter may point to a type that is now archived or absent.
  // Use the same visible catalog for the filter and matrix on initial entry.
  const matrixTypes = (data?.types ?? []).filter(t => !t.archived || showArchivedTypes);
  const matrixTypeFilter = matrixTypes.some(t => t.id === typeFilter) ? typeFilter : "";
  const visible = (data?.schedules ?? []).filter(s => activeDriverIds.has(s.driverId) && s.kind === tab && (!driverFilter || s.driverId === driverFilter) && (!typeFilter || s.typeId === typeFilter) && (status === "all" || s.status === status) && `${s.driverName} ${s.name}`.toLowerCase().includes(search.toLowerCase()));
  const types = (data?.types ?? []).filter(t => t.name.toLowerCase().includes(search.toLowerCase()) && (status === "all" || (status === "archived") === t.archived));
  const selectedSchedules = (data?.schedules ?? []).filter(s => selected.has(s.id));
  const toggle = (id: string) => setSelected(current => { const next = new Set(current); if (next.has(id)) next.delete(id); else next.add(id); return next; });
  const setCreateField = <K extends keyof ChargeCreate>(key: K, value: ChargeCreate[K]) => setCreate(c => c && ({ ...c, [key]: value }));
  const chooseTab = (value: typeof tab) => { setTab(value); setSelected(new Set()); setStatus("all"); setTypeFilter(""); };
  return <div className="space-y-5 animate-fade-in">
    <ManagementHeader icon={Receipt} title="Charges" description="Assign weekly fees and reimbursements, and track installment recovery." count={tab === "types" ? types.length : tab === "recurring" ? drivers.length : visible.length} actionLabel={tab === "installment" ? "New installment plan" : "New charge type"} onAction={() => { setError(""); setCreatePreview(null); setCountMode(false); if (tab !== "installment") setTypeForm(newType()); else setCreate(newCharge("installment", driverFilter, week)); }} />
    {error && !typeForm && !create && !bulk && <ErrorBanner message={error} />}
    <ChargeNotice notice={notice} dismiss={dismissNotice} />
    <div className="flex flex-wrap gap-2" role="tablist" aria-label="Charges views">{([["trucks", "Truck charges"], ["recurring", "Driver charges"], ["installment", "Installment plans"], ["types", "Charge types"]] as const).map(([key, label]) => <button key={key} role="tab" aria-selected={tab === key} onClick={() => chooseTab(key)} className={`${payButtonClass} ${tab === key ? "bg-blue-500/10 text-blue-300" : ""}`}>{label}</button>)}</div>
    {tab === "recurring" && <p className="text-xs text-zinc-500">Recurring fees stop on the driver from the week they join an investor truck. Truck fees remain in Truck charges; personal installment plans stay with the driver.</p>}
    {tab !== "trucks" && <div className="flex flex-wrap items-center gap-3"><ManagementSearch value={search} onChange={setSearch} placeholder={tab === "types" ? "Search charge types…" : tab === "recurring" ? "Search drivers…" : "Search driver or plan…"} />{tab !== "types" && <><select aria-label="Filter driver" className={`${controlClass} !w-52`} value={driverFilter} onChange={e => { setDriverFilter(e.target.value); setSelected(new Set()); }}><option value="">All active drivers</option>{drivers.map(d => <option key={d.id} value={d.id}>{d.fullName}</option>)}</select>{tab === "recurring" && <select aria-label="Filter charge type" className={`${controlClass} !w-44`} value={matrixTypeFilter} onChange={e => { setTypeFilter(e.target.value); setSelected(new Set()); }}><option value="">All charge types</option>{matrixTypes.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}</select>}</>}{tab !== "recurring" && <select aria-label="Filter status" className={`${controlClass} !w-40`} value={status} onChange={e => { setStatus(e.target.value); setSelected(new Set()); }}><option value="all">All statuses</option>{(tab === "types" ? ["active", "archived"] : ["active", "scheduled", "paused", "ended", "completed"]).map(v => <option key={v} value={v}>{v}</option>)}</select>}<button className={payButtonClass} disabled={busy} onClick={() => void run(reload)}>Reload</button></div>}
    {tab === "installment" && !!selectedSchedules.length && <div className="flex flex-wrap items-center gap-2 rounded-lg border border-blue-500/30 p-3 text-xs"><span>{selectedSchedules.length} selected</span>{(["amount", "pause", "resume"] as ChargeBulk["action"][]).map(action => <button key={action} className={payButtonClass} onClick={() => { setBulk({ targets: selectedSchedules.map(s => ({ id: s.id, version: s.version })), action, weekStart: week, amount: "" }); setBulkPreview(false); setError(""); }}>{action === "amount" ? "Change amount" : action === "end" ? "End assignments" : action === "pause" ? "Pause" : "Resume"}</button>)}</div>}
    {!data ? <p role="status" className="p-8 text-sm text-zinc-500">{error ? "Unable to load charges. Use Reload to retry." : "Loading driver charges…"}</p> : tab === "trucks" ? <TruckCharges charges={data} onChanged={reload} /> : tab === "recurring" ? <RecurringMatrix data={data} drivers={drivers} search={search} driverFilter={driverFilter} typeFilter={matrixTypeFilter} archived={showArchivedTypes} onArchivedChange={setShowArchivedTypes} busy={busy} pending={recurringSaves.pending} onSave={recurringSaves.save} onSaveAll={(targets, typeName) => { void run(() => recurringSaves.saveAll(targets, typeName)); }} showHistory={s => { void run(async () => { const [rows, events] = await Promise.all([previewDriverCharge(s.id), fetchChargeHistory(s.id)]); setDetail({schedule:s, rows, events}); }); }} /> : <div className="overflow-x-auto rounded-lg border border-zinc-800"><table className="w-full min-w-[850px] text-zinc-300"><thead className="bg-zinc-900"><tr>{tab !== "types" && <th className={td}><input aria-label="Select visible assignments" type="checkbox" checked={visible.length > 0 && visible.every(s => selected.has(s.id))} onChange={e => setSelected(e.target.checked ? new Set(visible.map(s => s.id)) : new Set())} /></th>}{(tab === "types" ? ["Name", "Direction", "Available amounts", "Eligibility", "Status", ""] : ["Driver", "Plan", "Original", "Confirmed", "Remaining", "Scheduled / end", "Status", ""]).map((v, i) => <th className={td} key={i}>{v}</th>)}</tr></thead><tbody>
      {tab === "types" ? types.map(t => <tr key={t.id}><td className={td}>{t.name}</td><td className={td}>{t.direction}</td><td className={td}>{t.amounts.map(money).join(" / ")}</td><td className={td}>{eligibilityLabel(t.eligibility)}</td><td className={td}>{t.archived ? "Archived" : "Active"}</td><td className={td}><button className={payButtonClass} onClick={() => { setTypeForm({ ...t }); setError(""); }}>Edit type</button><button className={`${payButtonClass} ml-2 text-red-300`} disabled={busy} aria-label={`Delete ${t.name}`} onClick={() => { setDeletingType(t); setError(""); }}>Delete</button></td></tr>) : visible.map(s => { return <tr key={s.id}><td className={td}><input type="checkbox" aria-label={`Select ${s.driverName}, ${s.name}`} checked={selected.has(s.id)} onChange={() => toggle(s.id)} /></td><td className={td}><Link className="text-blue-400" href={`/drivers/detail?id=${s.driverId}`}>{s.driverName}</Link></td><td className={td}>{s.name}</td><><td className={td}>{money(s.total ?? "0")}</td><td className={td}>{money(s.confirmed)}</td><td className={`${td} font-medium`}>{money(s.remaining)}</td><td className={td}>{money(s.scheduled)}<br /><span className="text-zinc-500">{s.completionWeek || "No completion scheduled"}{s.eligibility === "loads" && " · provisional"}</span></td></><td className={td}>{s.status}</td><td className={td}><button className={payButtonClass} disabled={busy} onClick={() => void run(async () => { const [rows, events] = await Promise.all([previewDriverCharge(s.id), fetchChargeHistory(s.id)]); setDetail({ schedule: s, rows, events }); })}>Schedule / history</button></td></tr>; })}
    </tbody></table>{(tab === "types" ? types : visible).length === 0 && <p className="p-10 text-center text-sm text-zinc-500">No {tab === "types" ? "charge types" : "assignments"} match this view.</p>}</div>}
    <p className="text-xs text-zinc-500">Truck fees follow the truck into its owner settlement. Driver fees stay on the person’s paycheck. Charge types are shared.</p>

    {deletingType && <Modal title="Delete charge type" description={`Delete “${deletingType.name}”? Types with driver or truck assignments must be archived to retain accounting history.`} isSaving={busy} submitLabel="Delete type" onClose={() => { setDeletingType(null); setError(""); }} onSubmit={e => { e.preventDefault(); void run(async () => { await deleteChargeType(deletingType); await reload(); setDeletingType(null); setMessage("Charge type deleted"); }); }}>{error && <ErrorBanner message={error} />}</Modal>}
    {typeForm && <Modal title={typeForm.id ? "Edit charge type" : "New charge type"} description="Set the available amounts and qualifying weeks for this charge type." isSaving={busy} submitLabel="Save type" onClose={() => { setTypeForm(null); setError(""); }} onSubmit={e => { e.preventDefault(); void run(async () => { await saveChargeType(typeForm); await reload(); setTypeForm(null); setMessage("Charge type saved"); }); }}><div className="space-y-4">{error && <ErrorBanner message={error} />}<ChargeTypeFields value={typeForm} onChange={setTypeForm} /></div></Modal>}

    {create && <Modal title="New installment plan" description={createPreview ? "Review the driver and schedule before saving." : "Deductions begin on the selected Monday. Weekly amounts remain editable in Driver Pay."} isSaving={busy} submitLabel={createPreview ? "Create assignments" : "Preview schedule"} onClose={() => { setCreate(null); setCreatePreview(null); setError(""); }} onSubmit={e => { e.preventDefault(); void run(async () => { if (!createPreview) { setCreatePreview(await previewNewCharges(create)); return; } await createDriverCharges(create); await reload(); setCreate(null); setCreatePreview(null); setMessage("Installment plan created"); }); }}>
      {error && <ErrorBanner message={error} />}
      {createPreview ? <div className="space-y-4"><p className="text-sm text-zinc-300">{drivers.find(d => d.id === create.driverIds[0])?.fullName}</p><p className="text-xs text-zinc-500">{create.eligibility === "loads" ? "Provisional: assumes a load each future week; empty weeks postpone installments." : "Every calendar week, including weeks with no loads."}</p><ScheduleTable rows={createPreview} /><button type="button" className={payButtonClass} onClick={() => setCreatePreview(null)}>Back to edit</button></div> : <div className="space-y-4 mt-3">
        <Field label="Driver"><select aria-label="Driver" required className={controlClass} value={create.driverIds[0] ?? ""} onChange={e => setCreateField("driverIds", [e.target.value])}><option value="">Select a driver</option>{drivers.filter(d => d.active).map(d => <option key={d.id} value={d.id}>{d.fullName}</option>)}</select></Field>
        <Field label="Description"><input required maxLength={200} className={controlClass} value={create.name} onChange={e => setCreateField("name", e.target.value)} /></Field>
        <Field label="Total charge"><input required inputMode="decimal" className={controlClass} value={create.total} onChange={e => setCreateField("total", e.target.value)} /></Field>
        <label className="flex gap-2 text-sm text-zinc-400"><input type="checkbox" checked={countMode} onChange={e => { setCountMode(e.target.checked); setCreateField("installments", e.target.checked ? 6 : 0); }} />Calculate from number of installments</label>
        {countMode ? <Field label="Number of installments"><input required type="number" min={1} max={5200} className={controlClass} value={create.installments} onChange={e => setCreateField("installments", Number(e.target.value))} /></Field> : <Field label="Weekly amount"><input required inputMode="decimal" className={controlClass} value={create.amount} onChange={e => setCreateField("amount", e.target.value)} /></Field>}
        <Field label="Start week (Monday)"><input required type="date" min={week} className={controlClass} value={create.startWeek} onChange={e => setCreateField("startWeek", e.target.value)} /></Field>
        <Field label="Installment eligibility"><select aria-label="Installment eligibility" className={controlClass} value={create.eligibility} onChange={e => setCreateField("eligibility", e.target.value as ChargeCreate["eligibility"])}><option value="calendar">Every calendar week</option><option value="loads">Only weeks with Gross Board loads</option></select></Field>
      </div>}
    </Modal>}

    {bulk && <Modal title={`${bulk.action === "amount" ? "Change amount" : bulk.action === "end" ? "End" : bulk.action === "pause" ? "Pause" : "Resume"} assignments`} description="Changes apply from the effective week and replace later scheduled changes. Saved overrides and confirmations are protected." isSaving={busy} submitLabel={bulkPreview ? "Apply changes" : "Preview changes"} onClose={() => { setBulk(null); setError(""); }} onSubmit={e => { e.preventDefault(); if (!bulkPreview) { setBulkPreview(true); return; } void run(async () => { await bulkDriverCharges(bulk); await reload(); setBulk(null); setMessage("Assignments updated"); }); }}><div className="space-y-4">{error && <ErrorBanner message={error} />}{bulkPreview ? <><p className="text-sm text-zinc-300">{bulk.action} from {bulk.weekStart}{bulk.action === "amount" && `: ${money(bulk.amount)} per week`}. {bulk.action === "end" && "The effective week will have no charge."}</p><ul className="max-h-64 overflow-auto text-sm text-zinc-400">{selectedSchedules.map(s => <li key={s.id} className="py-1">{s.driverName} · {s.name}</li>)}</ul><button type="button" className={payButtonClass} onClick={() => setBulkPreview(false)}>Back to edit</button></> : <><Field label="Effective week (Monday)"><input required type="date" min={week} className={controlClass} value={bulk.weekStart} onChange={e => setBulk({ ...bulk, weekStart: e.target.value })} /></Field>{bulk.action === "amount" && <Field label="New weekly amount"><input required inputMode="decimal" className={controlClass} value={bulk.amount} onChange={e => setBulk({ ...bulk, amount: e.target.value })} /></Field>}</>}</div></Modal>}
    {detail && <Modal title={`${detail.schedule.driverName} · ${detail.schedule.name}`} description={detail.schedule.kind === "recurring" ? "Future charges depend on this charge type’s eligibility and Gross Board entries. Saved weekly decisions remain in place." : detail.schedule.eligibility === "loads" ? "Future dates are provisional and assume weekly loads." : "Schedule and recorded changes"} isSaving={false} submitLabel="Close" onSubmit={e => { e.preventDefault(); setDetail(null); }} onClose={() => setDetail(null)}><div className="space-y-4"><ScheduleTable rows={detail.rows} /><h3 className="text-sm font-medium text-zinc-200">History</h3>{detail.events.map(event => <RememberedDetails memoryKey={`charge-event:${event.id}`} key={event.id} className="rounded border border-zinc-800 p-2 text-xs text-zinc-400"><summary>{event.action.replaceAll("_", " ")} · {event.actor} · {new Date(event.createdAt).toLocaleString()}</summary><p className="mt-2">{historyDescription(event)}</p></RememberedDetails>)}</div></Modal>}
  </div>;
}
