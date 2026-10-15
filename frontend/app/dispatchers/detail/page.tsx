"use client";

import { useEffect, useState } from "react";
import { useSearchParams } from "next/navigation";
import Link from "next/link";
import { Headset, Pencil } from "lucide-react";
import { fetchDispatcher, fetchDrivers, fetchUpdaters, updateDispatcher } from "@/app/lib/api";
import type { Dispatcher, DispatcherInput, Driver, Updater } from "@/app/lib/types";
import { PageHeader } from "@/app/components/PageHeader";
import { EmptyState, ErrorBanner, Modal, StatusBadge, TableShell } from "@/app/components/management/ManagementUI";
import { usePermissions } from "@/app/lib/access";
import { formatPhone } from "@/app/lib/phone";
import { DispatcherForm, dispatcherToInput } from "../DispatcherForm";

export default function DispatcherDetailPage() {
  const id = useSearchParams().get("id") ?? "";
  const canWrite = usePermissions().includes("fleet.write");
  const [dispatcher, setDispatcher] = useState<Dispatcher | null>(null);
  const [drivers, setDrivers] = useState<Driver[]>([]);
  const [updaters, setUpdaters] = useState<Updater[]>([]);
  const [form, setForm] = useState<DispatcherInput | null>(null);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => { let cancelled = false; Promise.all([fetchDispatcher(id), fetchDrivers(), fetchUpdaters()]).then(([d, drivers, updaters]) => { if (!cancelled) { setDispatcher(d); setDrivers(drivers); setUpdaters(updaters); setError(""); } }).catch(e => { if (!cancelled) setError(e.message); }); return () => { cancelled = true; }; }, [id, attempt]);
  const assigned = drivers.filter(d => d.dispatcherId === id);
  const updater = (uid: string | null) => { const u = updaters.find(u => u.id === uid); return u ? `${u.fullName}${u.extension !== null ? ` · Ext. ${u.extension}` : ""}` : "Unassigned"; };
  return <div className="space-y-6">
    {error && <div className="space-y-2"><ErrorBanner message={error} /><button className="ui-button" onClick={() => setAttempt(v => v + 1)}>Retry dispatcher</button></div>}
    {!dispatcher && !error && <p className="text-zinc-400">Loading dispatcher…</p>}
    {dispatcher && <><PageHeader><div className="flex items-center gap-3"><Headset /><h1>{dispatcher.fullName}</h1><StatusBadge active={dispatcher.active} /></div></PageHeader>
      {canWrite && <div className="flex justify-end"><button className="ui-button ui-button-primary" onClick={() => setForm(dispatcherToInput(dispatcher, drivers))}><Pencil />Edit dispatcher</button></div>}
      <section className="ui-card"><dl className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">{[["Phone", formatPhone(dispatcher.phone)], ["Email", dispatcher.email], ["Extension", dispatcher.extension], ["Commission", dispatcher.payPercentage === null ? null : `${dispatcher.payPercentage}%`], ["Main updater", updater(dispatcher.mainUpdaterId)], ["After-hours updater", updater(dispatcher.afterHoursUpdaterId)]].map(([label,value]) => <div key={label}><dt className="text-xs text-zinc-400">{label}</dt><dd className="mt-1 break-words">{value ?? "Not recorded"}</dd></div>)}</dl></section>
      <section className="space-y-3"><h2>Assigned drivers <span className="text-zinc-400">{assigned.length}</span></h2><TableShell>{!assigned.length ? <EmptyState message="No drivers assigned." /> : <table className="[&_th]:px-3 [&_td]:px-3 [&_th]:py-2 [&_td]:py-2 w-full min-w-[500px] text-left"><thead><tr><th>Driver</th><th>Truck</th><th>Phone</th><th>Status</th></tr></thead><tbody>{assigned.map(d => <tr key={d.id}><td><Link href={`/drivers/detail?id=${d.id}`}>{d.fullName}</Link></td><td>{d.truckId ? <Link href={`/trucks/detail?id=${d.truckId}`}>{d.truckUnit}</Link> : "Unassigned"}</td><td>{formatPhone(d.phone) || "—"}</td><td><StatusBadge active={d.active} /></td></tr>)}</tbody></table>}</TableShell></section>
      {dispatcher.notes && <section className="ui-card"><h2>Internal notes</h2><p className="mt-3 whitespace-pre-wrap text-zinc-300">{dispatcher.notes}</p></section>}
    </>}
    {form && <Modal title="Edit dispatcher" isSaving={saving} submitLabel="Save dispatcher" onClose={() => setForm(null)} onSubmit={e => { e.preventDefault(); setSaving(true); setError(""); void updateDispatcher(id, form).then(() => { setForm(null); setAttempt(v => v + 1); }).catch(e => setError(e.message)).finally(() => setSaving(false)); }}>{error && <ErrorBanner message={error} />}<DispatcherForm value={form} onChange={setForm} drivers={drivers} updaters={updaters} dispatcherId={id} /></Modal>}
  </div>;
}
