"use client";

import { useViewState } from "@/app/lib/viewMemory";

import { useEffect, useState } from "react";
import Link from "next/link";
import { ArrowLeft, Pencil, UserRound, Truck as TruckIcon, Mail, Phone, FileBadge } from "lucide-react";
import { fetchDriver, fetchTruck, fileDownloadUrl } from "@/app/lib/api";
import type { Driver, Truck } from "@/app/lib/types";
import { ErrorBanner } from "@/app/components/management/ManagementUI";
import { RelatedExpenses } from "@/app/components/expenses/RelatedExpenses";
import { DriverChargeSummary } from "./DriverChargeSummary";
import { AssignmentHistory } from "./AssignmentHistory";
import { DriverEditor } from "./DriverEditor";
import { PayHistory } from "./PayHistory";

const tabs = ["Overview", "Pay history", "Personal charges", "Company & other expenses", "Assignments"] as const;
const showDate = (value: string | null) => value ? value.slice(0, 10) : "Not recorded";
export default function DriverDetailPage() {
  const [driver, setDriver] = useState<Driver | null>(null);
  const [truck, setTruck] = useState<Truck | null>(null);
  const [error, setError] = useState("");
  const [tab, setTab] = useViewState<typeof tabs[number]>("page:tab", "Overview");
  const [editing, setEditing] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let cancelled = false;
    const id = new URLSearchParams(window.location.search).get("id") ?? "";
    fetchDriver(id).then(value => { if (!cancelled) { setDriver(value); setError(""); } }).catch(e => { if (!cancelled) setError(id ? e.message : "No driver was selected."); });
    return () => { cancelled = true; };
  }, [attempt]);
  useEffect(() => {
    let cancelled = false;
    if (driver?.truckId) fetchTruck(driver.truckId).then(value => { if (!cancelled) setTruck(value); }).catch(() => { if (!cancelled) setTruck(null); });
    return () => { cancelled = true; };
  }, [driver?.truckId, driver?.updatedAt]);
  const currentTruck = truck?.id === driver?.truckId ? truck : null;
  const rate = driver ? driver.payType === "cpm" ? `$${driver.payRate.toFixed(2)} / mile` : `${driver.payRate}% of driver gross` : "";
  return <div className="space-y-5 animate-fade-in">
    <Link href="/drivers" className="inline-flex items-center gap-1.5 text-xs text-zinc-500 hover:text-zinc-300"><ArrowLeft className="h-3.5 w-3.5" />Back to drivers</Link>
    {error && <><ErrorBanner message={error} /><button className="text-sm text-blue-400" onClick={() => setAttempt(v => v + 1)}>Retry driver</button></>}
    {!driver && !error && <p className="py-12 text-center text-zinc-500">Loading driver…</p>}
    {driver && <>
      <header className="overflow-hidden rounded-2xl border border-zinc-800 bg-zinc-900/30">
        <div className="flex flex-wrap items-start justify-between gap-4 p-5 sm:p-6"><div className="flex min-w-0 gap-4"><div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-blue-500/10 text-blue-400"><UserRound className="h-6 w-6" /></div><div className="min-w-0 break-words"><div className="flex flex-wrap items-center gap-3"><h1 className="text-xl font-semibold text-zinc-100">{driver.fullName}</h1><span className={`rounded-full px-2 py-0.5 text-xs ${driver.active ? "bg-emerald-500/10 text-emerald-400" : "bg-zinc-800 text-zinc-400"}`}>{driver.active ? "Active" : "Inactive"}</span></div><p className="mt-1 text-sm text-zinc-500">{driver.isOwnerOperator ? "Owner-operator" : "Company driver"} · Joined {showDate(driver.hireDate)}</p><div className="mt-3 flex flex-wrap gap-4 text-xs text-zinc-400"><span className="flex items-center gap-1.5"><Phone className="h-3 w-3" />{driver.phone || "No phone"}</span><span className="flex items-center gap-1.5"><Mail className="h-3 w-3" />{driver.email || "No email"}</span></div></div></div><button className="inline-flex items-center gap-2 rounded-lg bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-500" onClick={() => setEditing(true)}><Pencil className="h-3.5 w-3.5" />Edit driver</button></div>
        <dl className="grid grid-cols-1 gap-px sm:grid-cols-2 border-t border-zinc-800 bg-zinc-800 md:grid-cols-4">{[["Compensation", rate], ["Current truck", driver.truckUnit || "Unassigned"], ["Dispatcher", driver.dispatcherName || "Unassigned"], ["Truck owner", currentTruck?.ownerName || "No truck assigned"]].map(([label, value]) => <div key={label} className="min-w-0 break-words bg-zinc-950/90 px-5 py-4"><dt className="text-xs text-zinc-500">{label}</dt><dd className="mt-1 text-sm font-medium text-zinc-200">{value}</dd></div>)}</dl>
      </header>
      <nav className="flex flex-wrap gap-1 border-b border-zinc-800" aria-label="Driver sections">{tabs.map(name => <button key={name} role="tab" aria-selected={tab === name} onClick={() => setTab(name)} className={`border-b-2 px-3 py-3 text-xs font-medium ${tab === name ? "border-blue-500 text-blue-400" : "border-transparent text-zinc-500 hover:text-zinc-300"}`}>{name}</button>)}</nav>
      {tab === "Overview" && <div className="space-y-5">
        <div className="grid gap-5 lg:grid-cols-2"><section className="rounded-xl border border-zinc-800 p-5"><h2 className="mb-4 text-sm font-semibold text-zinc-100">Contact & personal details</h2><dl className="grid grid-cols-1 gap-5 text-xs sm:grid-cols-2">{[["Phone", driver.phone], ["Email", driver.email], ["Address", [driver.address, driver.city, driver.state, driver.postalCode].filter(Boolean).join(", ")], ["Emergency contact", driver.emergencyContact], ["Hire date", showDate(driver.hireDate)], ["Last updated", showDate(driver.updatedAt)]].map(([label, value]) => <div key={label}><dt className="text-zinc-500">{label}</dt><dd className="mt-1 break-words text-zinc-300">{value || "Not recorded"}</dd></div>)}</dl></section>
          <section className="rounded-xl border border-zinc-800 p-5"><div className="mb-4 flex items-center gap-2"><FileBadge className="h-4 w-4 text-zinc-500" /><h2 className="text-sm font-semibold text-zinc-100">License & documents</h2></div><dl className="grid grid-cols-1 gap-5 text-xs sm:grid-cols-2">{[["License number", driver.licenseNumber], ["License state", driver.licenseState], ["Expires", showDate(driver.licenseExpires)]].map(([label,value]) => <div key={label}><dt className="text-zinc-500">{label}</dt><dd className="mt-1 text-zinc-300">{value || "Not recorded"}</dd></div>)}</dl>{driver.cdlFileId && <a className="mt-5 inline-block text-xs text-blue-400" href={fileDownloadUrl(driver.cdlFileId)} target="_blank" rel="noreferrer">Open CDL · {driver.cdlFileName}</a>}</section></div>
        <section className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-zinc-800 p-5"><div className="flex items-center gap-3"><TruckIcon className="h-5 w-5 text-zinc-500" /><div><h2 className="text-sm font-medium text-zinc-200">{driver.truckUnit ? `Truck ${driver.truckUnit}` : "No assigned truck"}</h2><p className="mt-1 text-xs text-zinc-500">Owner: {currentTruck?.ownerName || "Not available"}. Ownership is separate from the operating driver.</p></div></div>{driver.truckId && <Link className="text-xs text-blue-400" href={`/trucks/detail?id=${driver.truckId}`}>View truck</Link>}</section>
        <DriverChargeSummary driverId={driver.id} />
        <section className="rounded-xl border border-zinc-800 p-5"><h2 className="text-sm font-semibold text-zinc-100">Internal notes</h2><p className="mt-3 whitespace-pre-wrap text-sm text-zinc-400">{driver.notes || "No notes recorded."}</p></section>
      </div>}
      {tab === "Pay history" && <PayHistory key={driver.updatedAt} driverId={driver.id} />}
      {tab === "Personal charges" && <div className="space-y-6"><RelatedExpenses driverId={driver.id} scope="personal" /><DriverChargeSummary driverId={driver.id} expanded /></div>}
      {tab === "Company & other expenses" && <><p className="text-xs text-zinc-500">Expenses associated with this driver that are paid by the company or another responsible party. These are not personal payroll charges.</p><RelatedExpenses driverId={driver.id} scope="non_personal" /></>}
      {tab === "Assignments" && <AssignmentHistory key={driver.updatedAt} driverId={driver.id} />}
      {editing && <DriverEditor driver={driver} onClose={() => setEditing(false)} onSaved={value => { setDriver(value); setEditing(false); }} />}
    </>}
  </div>;
}
