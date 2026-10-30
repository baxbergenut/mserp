"use client";
import { formatDriverLeave } from "@/app/lib/driverLeave";
import { employmentTypeLabel } from "../../lib/driverType";

import { formatPhone } from "../../lib/phone";

import { PageHeader } from "@/app/components/PageHeader";

import { useViewState } from "@/app/lib/viewMemory";

import { useEffect, useState } from "react";
import Link from "@/app/components/AccessLink";
import { Pencil, UserRound, Mail, Phone, FileBadge } from "lucide-react";
import { fetchDriver, fetchTruck, fetchInvestorOptions, fileDownloadUrl } from "@/app/lib/api";
import type { Driver, Truck, InvestorOption } from "@/app/lib/types";
import { ErrorBanner } from "@/app/components/management/ManagementUI";
import { RelatedExpenses } from "@/app/components/expenses/RelatedExpenses";
import { DriverChargeSummary } from "./DriverChargeSummary";
import { AssignmentHistory } from "./AssignmentHistory";
import { DriverStatusDialog } from "../DriverStatusDialog";
import {FleetStatusDialog} from "@/app/components/fleet/FleetStatusDialog";
import {FleetStatusHistory} from "@/app/components/fleet/FleetStatusHistory";
import {statusLabel} from "@/app/lib/fleetStatus";
import { DriverEditor } from "./DriverEditor";
import { DriverServiceCards } from "../DriverServices";
import { DriverTeamPanel } from "./DriverTeamPanel";
import { ProfileNotes } from "@/app/components/fleet/ProfileNotes";
import { OwnershipPanel } from "@/app/components/fleet/OwnershipPanel";
import { PayHistory } from "./PayHistory";
import { TruckLocationPanel } from "@/app/components/fleet/TruckLocationPanel";
import { EscrowTable } from "@/app/accounting/escrow/EscrowTable";
import { useExpenseCategoryAccess, usePermissions } from "@/app/lib/access";

const tabs = ["Overview", "Status history", "Ownership", "Pay history", "Escrow", "Personal charges", "Company & other expenses", "Assignments"] as const;
const showDate = (value: string | null | undefined) => value ? value.slice(0, 10) : "Not recorded";
export default function DriverDetailPage() {
  const permissions = usePermissions();
  const canReadEscrow = permissions.includes("escrow.read");
  const [investor, setInvestor] = useState<InvestorOption | null>(null);
  const expenseCategoryAccess = useExpenseCategoryAccess();
  const expenseTabsVisible = expenseCategoryAccess.some(a=>a.canView);
  const canPrivate=permissions.includes("drivers.private.read"),canDocuments=permissions.includes("drivers.documents.read"),canCompensation=permissions.includes("driver_compensation.read");
  const visibleTabs: readonly (typeof tabs)[number][] = tabs.filter(name => (name !== "Ownership" || investor) && (name !== "Pay history" || permissions.includes("driver_pay.read")) && (name !== "Escrow" || canReadEscrow) && (expenseTabsVisible || (name !== "Personal charges" && name !== "Company & other expenses")));
  const [driver, setDriver] = useState<Driver | null>(null);
  const [truck, setTruck] = useState<Truck | null>(null);
  const [error, setError] = useState("");
  const [savedTab, setTab] = useViewState<typeof tabs[number]>("page:tab", "Overview");
  const tab=visibleTabs.includes(savedTab)?savedTab:"Overview";
  const [changingStatus, setChangingStatus] = useState(false);
  const [changingAvailability,setChangingAvailability]=useState(false);
  const [editing, setEditing] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(()=>{if(editing||changingStatus||changingAvailability)return;const refresh=()=>{if(document.visibilityState==="visible")setAttempt(v=>v+1);};const timer=setInterval(refresh,30000);window.addEventListener("focus",refresh);return()=>{clearInterval(timer);window.removeEventListener("focus",refresh);};},[editing,changingStatus,changingAvailability]);
  useEffect(() => { if (new URLSearchParams(window.location.search).has("services")) setTab("Overview"); }, [setTab]);
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
    const truckId = driver?.truckId ?? driver?.teamTruckId;
    if (truckId && permissions.includes("trucks.read")) fetchTruck(truckId).then(value => { if (!cancelled) setTruck(value); }).catch(() => { if (!cancelled) setTruck(null); });
    return () => { cancelled = true; };
  }, [driver?.truckId, driver?.teamTruckId, driver?.updatedAt,permissions]);
  useEffect(() => {
    let cancelled = false;
    if (driver && permissions.includes("investors.read")) fetchInvestorOptions().then(rows => { if (!cancelled) setInvestor(rows.find(i => i.driverId === driver.id) ?? null); }).catch(e => { if (!cancelled) setError(e.message); });
    return () => { cancelled = true; };
  }, [driver,permissions]);
  const displayedDriver = driver ? { ...driver, truckId: driver.truckId ?? driver.teamTruckId ?? null, truckUnit: driver.truckUnit ?? driver.teamTruckUnit ?? null } : null;
  const currentTruck = truck?.id === displayedDriver?.truckId ? truck : null;
  const availabilityStatus = driver?.active ? driver.availabilityStatus ?? "active" : "terminated";
  const availabilityColor = availabilityStatus === "active" ? "bg-emerald-500/10 text-emerald-400" : availabilityStatus === "terminated" ? "bg-zinc-800 text-zinc-400" : "bg-blue-500/10 text-blue-400";
  const rate = driver && canCompensation && driver.payRate !== undefined ? driver.teamRole === "team" ? "Main driver’s statement" : driver.payType === "cpm" ? `$${driver.payRate.toFixed(2)} / mile` : `${driver.payRate}% of driver gross` : "";
  return <div className="space-y-5 animate-fade-in">
    {error && <><ErrorBanner message={error} /><button className="text-sm text-blue-400" onClick={() => setAttempt(v => v + 1)}>Retry driver</button></>}
    {!driver && !error && <p className="py-12 text-center text-zinc-500">Loading driver…</p>}
    {driver && <>
      <header className="overflow-hidden rounded-2xl border border-zinc-800 bg-zinc-900/30">
        <div className="flex flex-wrap items-start justify-between gap-4 p-5 sm:p-6"><div className="flex min-w-0 gap-4"><div className="min-w-0 break-words"><PageHeader><div><div className="flex flex-wrap items-center gap-3"><UserRound className="h-5 w-5 shrink-0 text-zinc-500" /><h1 className="text-xl font-semibold text-zinc-100">{driver.fullName}</h1><span className={`rounded-full px-2 py-0.5 text-xs ${availabilityColor}`}>{statusLabel(availabilityStatus)}</span></div></div></PageHeader><div className="mt-3 flex flex-wrap gap-4 text-xs text-zinc-400"><span className="flex items-center gap-1.5"><Phone className="h-3 w-3" />{formatPhone(driver.phone) || "No phone"}</span><span className="flex items-center gap-1.5"><Mail className="h-3 w-3" />{driver.email || "No email"}</span></div></div></div>{permissions.includes("drivers.edit") && <div className="flex gap-2">{driver.active && <button className="ui-button" onClick={()=>setChangingAvailability(true)}>Change availability</button>}<button className="ui-button" onClick={() => setChangingStatus(true)}>Employment status</button><button className="ui-button ui-button-primary" onClick={() => setEditing(true)}><Pencil />Edit driver</button></div>}</div>
        <dl className="grid grid-cols-[repeat(auto-fit,minmax(150px,1fr))] gap-px border-t border-zinc-800 bg-zinc-950/90">{[...(canCompensation ? [["Compensation", rate]] : []), ["Availability", statusLabel(availabilityStatus)], ["Home time / return", formatDriverLeave(driver.leavePlan, driver.availabilityStatus ?? "active")], ["Current truck", displayedDriver?.truckUnit || "Unassigned"], ["Truck status",driver.truckOperatingStatus?statusLabel(driver.truckOperatingStatus):"Unassigned"], ["Dispatcher", driver.dispatcherName || "Unassigned"], ["Truck owner", currentTruck?.ownerName || "No truck assigned"]].map(([label, value]) => <div key={label} className="min-w-0 break-words bg-zinc-950/90 px-5 py-4"><dt className="text-xs text-zinc-500">{label}</dt><dd className="mt-1 text-sm font-medium text-zinc-200">{label === "Current truck" && displayedDriver?.truckId ? <Link href={`/trucks/detail?id=${displayedDriver.truckId}`}>{value}</Link> : label === "Dispatcher" && driver.dispatcherId ? <Link href={`/dispatchers/detail?id=${driver.dispatcherId}`}>{value}</Link> : label === "Truck owner" && currentTruck?.ownerId ? <Link href={`/investors/detail?id=${currentTruck.ownerId}`}>{value}</Link> : value}</dd></div>)}</dl>
      </header>
      <nav className="flex flex-wrap gap-1 border-b border-zinc-800" aria-label="Driver sections">{visibleTabs.map(name => <button key={name} role="tab" aria-selected={tab === name} onClick={() => setTab(name)} className={`border-b-2 px-3 py-3 text-xs font-medium ${tab === name ? "border-blue-500 text-blue-400" : "border-transparent text-zinc-500 hover:text-zinc-300"}`}>{name}</button>)}</nav>
      {tab === "Overview" && <div className="grid items-start gap-5 xl:grid-cols-[minmax(0,1fr)_320px]"><div className="min-w-0 space-y-5">
        <div className="grid gap-5 lg:grid-cols-2"><section className="rounded-xl border border-zinc-800 p-5"><h2 className="mb-4 text-sm font-semibold text-zinc-100">Contact & personal details</h2><dl className="grid grid-cols-1 gap-5 text-xs sm:grid-cols-2">{[["Driver type", employmentTypeLabel(driver.employmentType)], ["Phone", formatPhone(driver.phone)], ["Email", driver.email], ...(canPrivate ? [["Date of birth", showDate(driver.dateOfBirth)], ["Address", [driver.address, driver.city, driver.state, driver.postalCode].filter(Boolean).join(", ")], ["Emergency contact", driver.emergencyContact]] : []), ["Hire date", showDate(driver.hireDate)], ["Last updated", showDate(driver.updatedAt)]].map(([label, value]) => <div key={label}><dt className="text-zinc-500">{label}</dt><dd className="mt-1 break-words text-zinc-300">{value || "Not recorded"}</dd></div>)}</dl></section>
          {(canPrivate || canDocuments) && <section className="rounded-xl border border-zinc-800 p-5"><div className="mb-4 flex items-center gap-2"><FileBadge className="h-4 w-4 text-zinc-500" /><h2 className="text-sm font-semibold text-zinc-100">License & documents</h2></div>{canPrivate && <dl className="grid grid-cols-1 gap-5 text-xs sm:grid-cols-2">{[["License number", driver.licenseNumber], ["License state", driver.licenseState], ["Expires", showDate(driver.licenseExpires)]].map(([label,value]) => <div key={label}><dt className="text-zinc-500">{label}</dt><dd className="mt-1 text-zinc-300">{value || "Not recorded"}</dd></div>)}</dl>}{canDocuments && driver.cdlFileId && <a className="mt-5 inline-block text-xs text-blue-400" href={fileDownloadUrl(driver.cdlFileId)} target="_blank" rel="noreferrer">Open CDL · {driver.cdlFileName}</a>}</section>}</div>
        {driver.teamRole === "main" && <DriverTeamPanel driver={driver} />}
        <DriverServiceCards key={`services-${driver.id}`} driverId={driver.id} />
        {permissions.includes("charges.read") && <DriverChargeSummary driverId={driver.id} />}
        <ProfileNotes key={driver.id} kind="drivers" id={driver.id} legacy={driver.notes} />
      </div><aside className="min-w-0"><TruckLocationPanel key={`${driver.id}:${driver.updatedAt}`} driverId={driver.id} /></aside></div>}
      {canReadEscrow && tab === "Escrow" && <EscrowTable driverId={driver.id} />}
      {permissions.includes("driver_pay.read") && tab === "Pay history" && <PayHistory key={driver.updatedAt} driverId={driver.id} />}
      {expenseTabsVisible && tab === "Personal charges" && <div className="space-y-6"><RelatedExpenses driverId={driver.id} scope="personal" />{permissions.includes("charges.read") && <DriverChargeSummary driverId={driver.id} expanded />}</div>}
      {expenseCategoryAccess.length > 0 && tab === "Company & other expenses" && <><RelatedExpenses driverId={driver.id} scope="non_personal" /></>}
      {tab === "Ownership" && investor && <OwnershipPanel key={investor.id} investor={investor} />}
      {tab === "Assignments" && <AssignmentHistory key={driver.updatedAt} driverId={driver.id} />}
      {tab === "Status history" && <FleetStatusHistory kind="drivers" id={driver.id}/>}
      {changingAvailability&&<FleetStatusDialog kind="drivers" value={driver} onClose={()=>setChangingAvailability(false)} onSaved={v=>{setDriver(v);setChangingAvailability(false);}}/>}
      {changingStatus && <DriverStatusDialog driver={driver} onClose={() => setChangingStatus(false)} onSaved={value => { setDriver(value); setChangingStatus(false); }} />}
      {editing && <DriverEditor driver={driver} onClose={() => setEditing(false)} onSaved={value => { setDriver(value); setEditing(false); }} />}
    </>}
  </div>;
}
