"use client";

import { formatPhone } from "../../lib/phone";

import { PageHeader } from "@/app/components/PageHeader";

import { useViewState } from "@/app/lib/viewMemory";

import { useEffect, useState } from "react";
import Link from "next/link";
import { Pencil, UserRound, Mail, Phone, FileBadge } from "lucide-react";
import { fetchDriver, fetchTruck, fetchInvestors, fileDownloadUrl } from "@/app/lib/api";
import type { Driver, Truck, Investor } from "@/app/lib/types";
import { ErrorBanner } from "@/app/components/management/ManagementUI";
import { RelatedExpenses } from "@/app/components/expenses/RelatedExpenses";
import { DriverChargeSummary } from "./DriverChargeSummary";
import { AssignmentHistory } from "./AssignmentHistory";
import { DriverStatusDialog } from "../DriverStatusDialog";
import { DriverEditor } from "./DriverEditor";
import { ProfileNotes } from "@/app/components/fleet/ProfileNotes";
import { OwnershipPanel } from "@/app/components/fleet/OwnershipPanel";
import { PayHistory } from "./PayHistory";
import { TruckLocationPanel } from "@/app/components/fleet/TruckLocationPanel";
import { EscrowTable } from "@/app/accounting/escrow/EscrowTable";
import { useExpenseCategoryAccess, usePermissions } from "@/app/lib/access";

const tabs = ["Overview", "Ownership", "Pay history", "Escrow", "Personal charges", "Company & other expenses", "Assignments"] as const;
const showDate = (value: string | null) => value ? value.slice(0, 10) : "Not recorded";
export default function DriverDetailPage() {
  const permissions = usePermissions();
  const canReadEscrow = permissions.includes("escrow.read");
  const [investor, setInvestor] = useState<Investor | null>(null);
  const expenseCategoryAccess = useExpenseCategoryAccess();
  const expenseTabsVisible = expenseCategoryAccess.length > 0;
  const visibleTabs: readonly (typeof tabs)[number][] = tabs.filter(name => (name !== "Ownership" || investor) && (name !== "Pay history" || permissions.includes("payroll.read")) && (name !== "Escrow" || canReadEscrow) && (expenseTabsVisible || (name !== "Personal charges" && name !== "Company & other expenses")));
  const [driver, setDriver] = useState<Driver | null>(null);
  const [truck, setTruck] = useState<Truck | null>(null);
  const [error, setError] = useState("");
  const [tab, setTab] = useViewState<typeof tabs[number]>("page:tab", "Overview");
  const [changingStatus, setChangingStatus] = useState(false);
  const [editing, setEditing] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!canReadEscrow && tab === "Escrow") setTab("Overview");
    if (!expenseTabsVisible && (tab === "Personal charges" || tab === "Company & other expenses")) setTab("Overview");
  }, [canReadEscrow, expenseTabsVisible, setTab, tab]);
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
  useEffect(() => {
    let cancelled = false;
    if (driver) fetchInvestors().then(rows => { if (!cancelled) setInvestor(rows.find(i => i.driverId === driver.id) ?? null); }).catch(e => { if (!cancelled) setError(e.message); });
    return () => { cancelled = true; };
  }, [driver]);
  const currentTruck = truck?.id === driver?.truckId ? truck : null;
  const rate = driver ? driver.payType === "cpm" ? `$${driver.payRate.toFixed(2)} / mile` : `${driver.payRate}% of driver gross` : "";
  return <div className="space-y-5 animate-fade-in">
    {error && <><ErrorBanner message={error} /><button className="text-sm text-blue-400" onClick={() => setAttempt(v => v + 1)}>Retry driver</button></>}
    {!driver && !error && <p className="py-12 text-center text-zinc-500">Loading driver…</p>}
    {driver && <>
      <header className="overflow-hidden rounded-2xl border border-zinc-800 bg-zinc-900/30">
        <div className="flex flex-wrap items-start justify-between gap-4 p-5 sm:p-6"><div className="flex min-w-0 gap-4"><div className="min-w-0 break-words"><PageHeader><div><div className="flex flex-wrap items-center gap-3"><UserRound className="h-5 w-5 shrink-0 text-zinc-500" /><h1 className="text-xl font-semibold text-zinc-100">{driver.fullName}</h1><span className={`rounded-full px-2 py-0.5 text-xs ${driver.active ? "bg-emerald-500/10 text-emerald-400" : "bg-zinc-800 text-zinc-400"}`}>{(driver.status ?? (driver.active ? "active" : "terminated")).replace(/^./, c => c.toUpperCase())}</span></div></div></PageHeader><div className="mt-3 flex flex-wrap gap-4 text-xs text-zinc-400"><span className="flex items-center gap-1.5"><Phone className="h-3 w-3" />{formatPhone(driver.phone) || "No phone"}</span><span className="flex items-center gap-1.5"><Mail className="h-3 w-3" />{driver.email || "No email"}</span></div></div></div>{permissions.includes("fleet.write") && <div className="flex gap-2"><button className="ui-button" onClick={() => setChangingStatus(true)}>Change status</button><button className="ui-button ui-button-primary" onClick={() => setEditing(true)}><Pencil />Edit driver</button></div>}</div>
        <dl className="grid grid-cols-1 gap-px sm:grid-cols-2 border-t border-zinc-800 bg-zinc-800 md:grid-cols-4">{[["Compensation", rate], ["Current truck", driver.truckUnit || "Unassigned"], ["Dispatcher", driver.dispatcherName || "Unassigned"], ["Truck owner", currentTruck?.ownerName || "No truck assigned"]].map(([label, value]) => <div key={label} className="min-w-0 break-words bg-zinc-950/90 px-5 py-4"><dt className="text-xs text-zinc-500">{label}</dt><dd className="mt-1 text-sm font-medium text-zinc-200">{label === "Current truck" && driver.truckId ? <Link href={`/trucks/detail?id=${driver.truckId}`}>{value}</Link> : label === "Dispatcher" && driver.dispatcherId ? <Link href={`/dispatchers/detail?id=${driver.dispatcherId}`}>{value}</Link> : label === "Truck owner" && currentTruck?.ownerId ? <Link href={`/investors/detail?id=${currentTruck.ownerId}`}>{value}</Link> : value}</dd></div>)}</dl>
      </header>
      <nav className="flex flex-wrap gap-1 border-b border-zinc-800" aria-label="Driver sections">{visibleTabs.map(name => <button key={name} role="tab" aria-selected={tab === name} onClick={() => setTab(name)} className={`border-b-2 px-3 py-3 text-xs font-medium ${tab === name ? "border-blue-500 text-blue-400" : "border-transparent text-zinc-500 hover:text-zinc-300"}`}>{name}</button>)}</nav>
      {tab === "Overview" && <div className="grid items-start gap-5 xl:grid-cols-[minmax(0,1fr)_320px]"><div className="min-w-0 space-y-5">
        <div className="grid gap-5 lg:grid-cols-2"><section className="rounded-xl border border-zinc-800 p-5"><h2 className="mb-4 text-sm font-semibold text-zinc-100">Contact & personal details</h2><dl className="grid grid-cols-1 gap-5 text-xs sm:grid-cols-2">{[["Driver home", driver.driverHome], ["Phone", formatPhone(driver.phone)], ["Email", driver.email], ["Address", [driver.address, driver.city, driver.state, driver.postalCode].filter(Boolean).join(", ")], ["Emergency contact", driver.emergencyContact], ["Hire date", showDate(driver.hireDate)], ["Last updated", showDate(driver.updatedAt)]].map(([label, value]) => <div key={label}><dt className="text-zinc-500">{label}</dt><dd className="mt-1 break-words text-zinc-300">{value || "Not recorded"}</dd></div>)}</dl></section>
          <section className="rounded-xl border border-zinc-800 p-5"><div className="mb-4 flex items-center gap-2"><FileBadge className="h-4 w-4 text-zinc-500" /><h2 className="text-sm font-semibold text-zinc-100">License & documents</h2></div><dl className="grid grid-cols-1 gap-5 text-xs sm:grid-cols-2">{[["License number", driver.licenseNumber], ["License state", driver.licenseState], ["Expires", showDate(driver.licenseExpires)]].map(([label,value]) => <div key={label}><dt className="text-zinc-500">{label}</dt><dd className="mt-1 text-zinc-300">{value || "Not recorded"}</dd></div>)}</dl>{driver.cdlFileId && <a className="mt-5 inline-block text-xs text-blue-400" href={fileDownloadUrl(driver.cdlFileId)} target="_blank" rel="noreferrer">Open CDL · {driver.cdlFileName}</a>}</section></div>
        <DriverChargeSummary driverId={driver.id} />
        <ProfileNotes key={driver.id} kind="drivers" id={driver.id} legacy={driver.notes} />
      </div><aside className="min-w-0"><TruckLocationPanel key={`${driver.id}:${driver.updatedAt}`} driverId={driver.id} /></aside></div>}
      {canReadEscrow && tab === "Escrow" && <EscrowTable driverId={driver.id} />}
      {permissions.includes("payroll.read") && tab === "Pay history" && <PayHistory key={driver.updatedAt} driverId={driver.id} />}
      {expenseCategoryAccess.length > 0 && tab === "Personal charges" && <div className="space-y-6"><RelatedExpenses driverId={driver.id} scope="personal" /><DriverChargeSummary driverId={driver.id} expanded /></div>}
      {expenseCategoryAccess.length > 0 && tab === "Company & other expenses" && <><RelatedExpenses driverId={driver.id} scope="non_personal" /></>}
      {tab === "Ownership" && investor && <OwnershipPanel key={investor.id} investor={investor} />}
      {tab === "Assignments" && <AssignmentHistory key={driver.updatedAt} driverId={driver.id} />}
      {changingStatus && <DriverStatusDialog driver={driver} onClose={() => setChangingStatus(false)} onSaved={value => { setDriver(value); setChangingStatus(false); }} />}
      {editing && <DriverEditor driver={driver} onClose={() => setEditing(false)} onSaved={value => { setDriver(value); setEditing(false); }} />}
    </>}
  </div>;
}
