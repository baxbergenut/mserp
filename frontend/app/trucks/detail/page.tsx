"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { ArrowLeft, FileBadge, Pencil, Truck as TruckIcon } from "lucide-react";
import { PageHeader } from "@/app/components/PageHeader";
import { fetchTruck, fileDownloadUrl } from "@/app/lib/api";
import type { Truck } from "@/app/lib/types";
import { useViewState } from "@/app/lib/viewMemory";
import { useExpenseCategoryAccess, usePermissions } from "@/app/lib/access";
import { ErrorBanner } from "@/app/components/management/ManagementUI";
import { RelatedExpenses } from "@/app/components/expenses/RelatedExpenses";
import { TruckLocationPanel } from "@/app/components/fleet/TruckLocationPanel";
import { TruckEditor } from "./TruckEditor";

const tabs = ["Overview", "Expenses"] as const;
const showDate = (value: string | null) => value ? value.slice(0, 10) : "Not recorded";

function Details({ values }: { values: [string, string | number | null][] }) {
  return <dl className="grid gap-5 text-xs sm:grid-cols-2">{values.map(([label, value]) => <div key={label}><dt className="text-zinc-500">{label}</dt><dd className="mt-1 break-words text-zinc-300">{value === null || value === "" ? "Not recorded" : value}</dd></div>)}</dl>;
}

export default function TruckDetailPage() {
  const [truck, setTruck] = useState<Truck | null>(null);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [editing, setEditing] = useState(false);
  const [tab, setTab] = useViewState<typeof tabs[number]>("page:tab", "Overview");
  const permissions = usePermissions();
  const expenseCategoryAccess = useExpenseCategoryAccess();
  const expenseTabsVisible = expenseCategoryAccess.length > 0;
  const visibleTabs: readonly (typeof tabs)[number][] = expenseTabsVisible ? tabs : tabs.filter(name => name !== "Expenses");
  useEffect(() => {
    if (!expenseTabsVisible && tab === "Expenses") setTab("Overview");
  }, [expenseTabsVisible, setTab, tab]);
  useEffect(() => {
    let cancelled = false;
    const id = new URLSearchParams(window.location.search).get("id") ?? "";
    fetchTruck(id).then(value => { if (!cancelled) { setTruck(value); setError(""); } })
      .catch(e => { if (!cancelled) setError(id ? e.message : "No truck was selected."); });
    return () => { cancelled = true; };
  }, [attempt]);
  return <div className="space-y-5 animate-fade-in">
    <Link href="/trucks" className="inline-flex items-center gap-1.5 text-xs text-zinc-500 hover:text-zinc-300"><ArrowLeft className="h-3.5 w-3.5" />Back to trucks</Link>
    {error && <><ErrorBanner message={error} /><button className="text-sm text-blue-400" onClick={() => setAttempt(v => v + 1)}>Retry truck</button></>}
    {!truck && !error && <p className="py-12 text-center text-zinc-500">Loading truck…</p>}
    {truck && <>
      <header className="overflow-hidden rounded-2xl border border-zinc-800 bg-zinc-900/30">
        <div className="flex flex-wrap items-start justify-between gap-4 p-5 sm:p-6">
          <div className="min-w-0"><PageHeader><div><div className="flex flex-wrap items-center gap-3"><TruckIcon className="h-5 w-5 shrink-0 text-zinc-500" /><h1 className="text-xl font-semibold text-zinc-100">Truck {truck.unitNumber}</h1><span className={`rounded-full px-2 py-0.5 text-xs ${truck.active ? "bg-emerald-500/10 text-emerald-400" : "bg-zinc-800 text-zinc-400"}`}>{truck.active ? "Active" : "Inactive"}</span></div><p className="mt-1 text-sm text-zinc-500">{[truck.year, truck.make, truck.model].filter(Boolean).join(" ") || "No vehicle details"}</p></div></PageHeader><p className="break-all font-mono text-xs text-zinc-400">VIN · {truck.vin || "Not recorded"}</p></div>
          {permissions.includes("fleet.write") && <button className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-500" onClick={() => setEditing(true)}><Pencil className="h-3.5 w-3.5" />Edit truck</button>}
        </div>
        <dl className="grid grid-cols-1 gap-px border-t border-zinc-800 bg-zinc-800 sm:grid-cols-2 md:grid-cols-4">
          <div className="min-w-0 break-words bg-zinc-950/90 px-5 py-4"><dt className="text-xs text-zinc-500">Current driver</dt><dd className="mt-1 text-sm font-medium text-zinc-200">{truck.driverId ? <Link href={`/drivers/detail?id=${truck.driverId}`} className="hover:text-blue-300">{truck.driverName}</Link> : "Unassigned"}</dd></div>
          <div className="min-w-0 break-words bg-zinc-950/90 px-5 py-4"><dt className="text-xs text-zinc-500">Truck owner</dt><dd className="mt-1 text-sm font-medium text-zinc-200"><Link href="/investors" className="hover:text-blue-300">{truck.ownerName}</Link></dd></div>
          {[["Status", truck.status.replaceAll("_", " ")], ["Mileage", truck.mileage === null ? "Not recorded" : `${truck.mileage.toLocaleString()} mi`]].map(([label, value]) => <div key={label} className="min-w-0 break-words bg-zinc-950/90 px-5 py-4"><dt className="text-xs text-zinc-500">{label}</dt><dd className="mt-1 text-sm font-medium capitalize text-zinc-200">{value}</dd></div>)}
        </dl>
      </header>
      <nav className="flex flex-wrap gap-1 border-b border-zinc-800" aria-label="Truck sections">{visibleTabs.map(name => <button key={name} role="tab" aria-selected={tab === name} onClick={() => setTab(name)} className={`border-b-2 px-3 py-3 text-xs font-medium ${tab === name ? "border-blue-500 text-blue-400" : "border-transparent text-zinc-500 hover:text-zinc-300"}`}>{name}</button>)}</nav>
      {tab === "Overview" && <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_320px]">
        <div className="min-w-0 space-y-5">
          <section className="rounded-xl border border-zinc-800 p-5"><h2 className="mb-4 text-sm font-semibold text-zinc-100">Vehicle details</h2><Details values={[["Unit number", truck.unitNumber], ["VIN", truck.vin], ["Year", truck.year], ["Make", truck.make], ["Model", truck.model], ["License plate", [truck.licensePlate, truck.licenseState].filter(Boolean).join(" · ")]]} /></section>
          <section className="rounded-xl border border-zinc-800 p-5"><h2 className="mb-4 flex items-center gap-2 text-sm font-semibold text-zinc-100"><FileBadge className="h-4 w-4 text-zinc-500" />Registration & documents</h2><Details values={[["Registration expires", showDate(truck.registrationExpires)], ["Insurance expires", showDate(truck.insuranceExpires)]]} />{truck.irpFileId ? <a className="mt-5 inline-block text-xs text-blue-400" href={fileDownloadUrl(truck.irpFileId)} target="_blank" rel="noreferrer">Open cab card · {truck.irpFileName}</a> : <p className="mt-5 text-xs text-zinc-500">No cab card uploaded.</p>}</section>
          <section className="rounded-xl border border-zinc-800 p-5"><h2 className="mb-4 text-sm font-semibold text-zinc-100">Maintenance</h2><Details values={[["Last service", showDate(truck.lastServiceDate)], ["Next service", truck.nextServiceMiles === null ? "Not recorded" : `${truck.nextServiceMiles.toLocaleString()} mi`], ["Last updated", showDate(truck.updatedAt)]]} /></section>
          <section className="rounded-xl border border-zinc-800 p-5"><h2 className="text-sm font-semibold text-zinc-100">Internal notes</h2><p className="mt-3 whitespace-pre-wrap text-sm text-zinc-400">{truck.notes || "No notes recorded."}</p></section>
        </div>
        <aside className="min-w-0"><TruckLocationPanel key={`${truck.id}:${truck.updatedAt}`} truckId={truck.id} /></aside>
      </div>}
      {expenseCategoryAccess.length > 0 && tab === "Expenses" && <RelatedExpenses truckId={truck.id} />}
      {editing && <TruckEditor truck={truck} onClose={() => setEditing(false)} onSaved={value => { setTruck(value); setEditing(false); }} />}
    </>}
  </div>;
}
