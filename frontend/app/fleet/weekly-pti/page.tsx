"use client";

import {useEffect, useRef, useState} from "react";
import {useRouter, useSearchParams} from "next/navigation";
import {ChevronLeft, ChevronRight, ListChecks, LoaderCircle, Search} from "lucide-react";
import Link from "@/app/components/AccessLink";
import {changePTI, createPTIWeek, fetchPTIWeek} from "@/app/lib/api";
import type {PTIChange, PTIRow, PTIWeek} from "@/app/lib/types";
import {usePermissions} from "@/app/lib/access";
import {currentChargeWeek, validChargeWeek} from "@/app/accounting/driver-charges/charges";
import {controlClass, ErrorBanner, ManagementHeader} from "@/app/components/management/ManagementUI";

const stamp = (value: string | null) => value ? new Date(value).toLocaleString("en-US", {timeZone: "America/New_York", month: "short", day: "numeric", hour: "numeric", minute: "2-digit"}) : "";
function shiftWeek(week: string, days: number) {
 const date = new Date(week + "T12:00:00Z"); date.setUTCDate(date.getUTCDate() + days); return date.toISOString().slice(0, 10);
}
function mergeWeek(prior: PTIWeek | null, next: PTIWeek): PTIWeek {
 if (!prior || prior.weekStart !== next.weekStart) return next;
 if (prior.exists && !next.exists) return prior;
 return {...next, rows: next.rows.map(row => {const old = prior.rows.find(v => v.truckId === row.truckId); return old && old.version > row.version ? old : row;})};
}

export default function WeeklyPTIPage() {
 const router = useRouter(), query = useSearchParams(), current = currentChargeWeek();
 const selected = query.get("weekStart") ?? "", week = validChargeWeek(selected) && selected <= current ? selected : current;
 const navigate = (value: string) => {if (validChargeWeek(value) && value <= current) router.push(`/fleet/weekly-pti?weekStart=${value}`, {scroll: false});};
 return <div className="space-y-4 p-4 md:p-6">
  <ManagementHeader title="Weekly PTI" icon={ListChecks}/>
  <div className="flex flex-wrap items-center gap-2">
   <button className="ui-button" aria-label="Previous week" onClick={() => navigate(shiftWeek(week, -7))}><ChevronLeft className="h-4 w-4"/></button>
   <label className="flex shrink-0 items-center gap-2 whitespace-nowrap text-xs">Week of <input aria-label="PTI week" type="date" min="2000-01-03" max={current} step={7} value={week} className={controlClass + " w-40"} onChange={e => navigate(e.target.value)}/></label>
   <button className="ui-button" aria-label="Next week" disabled={week >= current} onClick={() => navigate(shiftWeek(week, 7))}><ChevronRight className="h-4 w-4"/></button>
   {week !== current && <button className="ui-button" onClick={() => navigate(current)}>This week</button>}
  </div>
  <Checklist key={week} week={week}/>
 </div>;
}

type Filter = "required" | "missing" | "unchecked" | "checked" | "all";
function Checklist({week}: {week: string}) {
 const canEdit = usePermissions().includes("pti.edit");
 const [data, setData] = useState<PTIWeek | null>(null), [error, setError] = useState(""), [attempt, setAttempt] = useState(0);
 const [search, setSearch] = useState(""), [filter, setFilter] = useState<Filter>(() => week === currentChargeWeek() ? "required" : "all"), [starting, setStarting] = useState(false);
 useEffect(() => {
  const controller = new AbortController();
  fetchPTIWeek(week, controller.signal).then(value => {setData(old => mergeWeek(old, value)); setError("");}).catch(e => {if (!controller.signal.aborted) setError(e.message);});
  return () => controller.abort();
 }, [week, attempt]);
 useEffect(() => {
  const refresh = () => {if (document.visibilityState === "visible") setAttempt(v => v + 1);};
  const timer = setInterval(refresh, 30000); window.addEventListener("focus", refresh);
  return () => {clearInterval(timer); window.removeEventListener("focus", refresh);};
 }, []);
 const accept = (row: PTIRow) => setData(old => old ? {...old, rows: old.rows.map(v => v.truckId === row.truckId && v.version <= row.version ? row : v)} : old);
 const start = async () => {
  setStarting(true);
  try {setData(await createPTIWeek(week)); setError("");} catch (e) {setError(e instanceof Error ? e.message : "Checklist could not be created");} finally {setStarting(false);}
 };
 const all = data?.rows ?? [], required = all.filter(row => row.eligibility === "required");
 const counts = {required: required.length, missing: required.filter(row => !row.received).length, unchecked: required.filter(row => row.received && row.review === "pending").length, checked: required.filter(row => row.review !== "pending").length, all: all.length};
 const filters: [Filter, string][] = [["required", "On the road"], ["missing", "Missing video"], ["unchecked", "To check"], ["checked", "Checked"], ["all", "All trucks"]];
 const rows = all.filter(row => (filter === "all" || (row.eligibility === "required" && (filter === "required" || (filter === "missing" && !row.received) || (filter === "unchecked" && row.received && row.review === "pending") || (filter === "checked" && row.review !== "pending")))) &&
  [row.truckUnit, ...row.crew.map(driver => driver.name), row.notes].join(" ").toLowerCase().includes(search.trim().toLowerCase()));
 return <>
  {error && <ErrorBanner message={error}/>}
  {!data ? <div className="py-8 text-sm text-zinc-500">{error ? <button className="ui-button" onClick={() => setAttempt(v => v + 1)}>Retry</button> : "Loading weekly PTI…"}</div> : !data.exists ? <div className="rounded-xl border border-zinc-800 p-6 text-sm text-zinc-400">
   <p>No checklist has been created for this week.</p>{week === currentChargeWeek() && canEdit && <button className="ui-button mt-3" disabled={starting} onClick={() => void start()}>Start this week</button>}
  </div> : <>
   <div className="flex flex-wrap items-center gap-3">
    <div className="flex max-w-full gap-1 overflow-x-auto whitespace-nowrap" role="group" aria-label="PTI filters">{filters.map(([value, label]) => <button key={value} aria-pressed={filter === value} onClick={() => setFilter(value)} className={`shrink-0 rounded-lg px-3 py-2 text-xs transition-colors ${filter === value ? "bg-blue-500/15 text-blue-300" : "text-zinc-400 hover:bg-zinc-800"}`}>{label}<span className="ml-2 tabular-nums opacity-75">{counts[value]}</span></button>)}</div>
    <label className="relative ml-auto min-w-52 flex-1 sm:max-w-xs"><Search aria-hidden className="absolute left-3 top-2.5 h-4 w-4 text-zinc-500"/><input aria-label="Search PTI" placeholder="Search truck, driver or notes" className={controlClass + " pl-9"} value={search} onChange={e => setSearch(e.target.value)}/></label>
    <button className="ui-button" onClick={() => setAttempt(v => v + 1)}>Refresh</button>
   </div>
   <div className="max-h-[calc(100dvh-280px)] overflow-auto rounded-xl border border-zinc-800">
    <table className="w-full min-w-[720px] text-left text-[13px]"><thead className="sticky top-0 z-10 bg-zinc-900 text-xs text-zinc-400"><tr>
     <th className="w-24 px-4 py-3 font-medium">Truck</th><th className="w-56 px-4 py-3 font-medium">Driver / team</th><th className="w-32 px-3 py-3 text-center font-medium">Video received</th><th className="w-24 px-3 py-3 text-center font-medium">Checked</th><th className="px-4 py-3 font-medium">Notes</th><th className="w-44 px-4 py-3 font-medium">Checked by</th>
    </tr></thead><tbody>{rows.map(row => <ChecklistRow key={row.truckId} row={row} canEdit={canEdit} accept={accept} refresh={() => setAttempt(v => v + 1)}/>)}</tbody></table>
    {!rows.length && <p className="p-8 text-center text-sm text-zinc-500">{search ? "No trucks match your search." : filter === "missing" ? "All required videos have been received." : filter === "unchecked" ? "No videos waiting to be checked." : "No trucks in this view."}</p>}
   </div>
  </>}
 </>;
}

function ChecklistRow({row, canEdit, accept, refresh}: {row: PTIRow; canEdit: boolean; accept: (row: PTIRow) => void; refresh: () => void}) {
 const [draft, setDraft] = useState<{value: string; base: PTIRow} | null>(null), [pending, setPending] = useState<PTIChange | null>(null), [error, setError] = useState("");
 const saving = useRef(false);
 const notes = draft?.value ?? row.notes;
 const dirty = draft !== null && draft.value !== draft.base.notes;
 useEffect(() => {
  if (!dirty && !pending) return;
  const warn = (event: BeforeUnloadEvent) => {event.preventDefault();};
  window.addEventListener("beforeunload", warn); return () => window.removeEventListener("beforeunload", warn);
 }, [dirty, pending]);
 const save = async (overrides: Partial<PTIChange> = {}, base = draft?.base ?? row) => {
  if (saving.current) return;
  const input: PTIChange = {requestId: crypto.randomUUID(), version: base.version, received: base.received, review: base.review, notes, ...overrides};
  saving.current = true; setPending(input); setError("");
  try {const value = await changePTI(row.weekStart, row.truckId, input); accept(value); setDraft(null);}
  catch (e) {setError(e instanceof Error ? e.message : "Could not save. Try again."); refresh();}
  finally {saving.current = false; setPending(null);}
 };
 const received = pending?.received ?? row.received, checked = (pending?.review ?? row.review) !== "pending";
 const receiptTitle = row.received ? `Received by ${row.receivedBy || "Unknown"}${row.receivedAt ? ` · ${stamp(row.receivedAt)} (New York)` : ""}` : "Video not received";
 const checkboxClass = "h-4 w-4 cursor-pointer rounded border-zinc-600 accent-blue-500 disabled:cursor-default disabled:opacity-60";
 return <tr aria-label={`Truck ${row.truckUnit}`} className="border-t border-zinc-800/80 hover:bg-zinc-800/20">
  <td className="px-4 py-3 align-top font-mono"><Link className="text-blue-400" href={`/trucks/detail?id=${row.truckId}`}>{row.truckUnit}</Link>{row.eligibility !== "required" && <span className="mt-1 block font-sans text-[10px] text-zinc-500" title={row.eligibilityReason}>{row.eligibility === "unknown" ? "Not recorded" : "Not required"}</span>}</td>
  <td className="px-4 py-3 align-top">{row.crew.length ? row.crew.map(driver => <div key={driver.id}><Link className="hover:text-blue-300" href={`/drivers/detail?id=${driver.id}`}>{driver.name}</Link></div>) : <span className="text-zinc-500">No driver</span>}</td>
  <td className="px-3 py-2 text-center align-top"><label title={receiptTitle} className="inline-flex min-h-9 min-w-9 items-center justify-center"><input type="checkbox" className={checkboxClass} aria-label={`Truck ${row.truckUnit} video received`} title={receiptTitle} checked={received} disabled={!canEdit || !!pending} onChange={e => void save({received: e.target.checked, ...(!e.target.checked ? {review: "pending"} : {})})}/></label></td>
  <td className="px-3 py-2 text-center align-top"><label className="inline-flex min-h-9 min-w-9 items-center justify-center"><input type="checkbox" className={checkboxClass} aria-label={`Truck ${row.truckUnit} checked`} checked={checked} disabled={!canEdit || !received || !!pending} onChange={e => void save({review: e.target.checked ? "checked" : "pending"})}/></label></td>
  <td className="min-w-64 px-3 py-2 align-top">{canEdit ? <input type="text" aria-label={`Truck ${row.truckUnit} notes`} maxLength={5000} className="h-9 w-full rounded-lg border border-transparent bg-transparent px-2 py-2 text-xs placeholder:text-zinc-600 hover:border-zinc-700 focus:border-blue-500 focus:outline-none" placeholder="Add a note…" value={notes} readOnly={!!pending} onChange={e => setDraft(old => ({base: old?.base ?? row, value: e.target.value}))} onBlur={e => {if (dirty && !(e.relatedTarget instanceof HTMLElement && e.currentTarget.closest("tr")?.contains(e.relatedTarget))) void save();}} onKeyDown={e => {if (e.key === "Enter") {e.preventDefault(); void save();}}}/> : <p title={row.notes} className="line-clamp-1 px-2 py-2 text-xs text-zinc-400">{row.notes || "—"}</p>}
   {pending && <span role="status" className="flex items-center gap-1 px-2 text-[11px] text-zinc-500"><LoaderCircle className="h-3 w-3 animate-spin"/>Saving…</span>}
   {error && <div role="alert" className="px-2 text-xs text-red-400">{error}{draft && <button className="ml-2 underline" onClick={() => void save({}, row)}>Save my note on the latest entry</button>}</div>}
   {dirty && !pending && <button className="px-2 text-[11px] text-blue-400" onClick={() => void save()}>Save note</button>}
  </td>
  <td className="px-4 py-3 align-top text-xs text-zinc-400">{checked && row.review !== "pending" && <span title={row.reviewedAt ? new Date(row.reviewedAt).toLocaleString("en-US", {timeZone: "America/New_York", year: "numeric", month: "short", day: "numeric", hour: "numeric", minute: "2-digit", timeZoneName: "short"}) : undefined}>{row.reviewedBy || "—"}</span>}</td>
 </tr>;
}
