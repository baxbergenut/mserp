"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { AlertTriangle, Plus, RefreshCw } from "lucide-react";
import { changePrePassFleet, fetchPrePassFleet, refreshPrePassFleet, verifyPrePassFleetOperation } from "../lib/api";
import { usePermissions } from "../lib/access";
import { useViewState } from "../lib/viewMemory";
import type { PrePassFleetChange, PrePassFleetEntry, PrePassFleetList, PrePassFleetOperation, PrePassFleetTruck, PrePassVehicleInput } from "../lib/types";
import { controlClass, EmptyState, ErrorBanner, Field, LoadingTable, ManagementSearch, Modal, TablePagination, TableShell } from "../components/management/ManagementUI";
import { PrePassVehicleFields, prePassVehicleFromTruck, validPrePassVIN } from "./PrePassVehicleFields";

type Dialog = { kind: PrePassFleetChange["action"]; requestId: string; entry?: PrePassFleetEntry } | { kind: "verify"; operation: PrePassFleetOperation };
const labels = { add: "Add truck", edit: "Edit PrePass truck", remove: "Remove from PrePass", assign: "Assign transponder", link: "Link MSERP truck", verify: "Review PrePass result" };
const selectClass = controlClass.replace("w-full", "w-auto max-w-full");

export function PrePassFleetTab() {
  const permissions = usePermissions();
  const canWrite = permissions.includes("tolls.fleet.write"), canLink = permissions.includes("tolls.link"), canProfile = permissions.includes("fleet.read");
  const [data, setData] = useState<PrePassFleetList | null>(null);
  const [loading, setLoading] = useState(true), [busy, setBusy] = useState(false);
  const [error, setError] = useState(""), [notice, setNotice] = useState("");
  const [view, setView] = useViewState<"trucks" | "transponders">("prepass:view", "trucks");
  const [search, setSearch] = useViewState("prepass:search", "");
  const [accountFilter, setAccountFilter] = useViewState("prepass:account", "");
  const [filter, setFilter] = useViewState("prepass:filter", "");
  const [page, setPage] = useViewState("prepass:page", 1), [pageSize, setPageSize] = useViewState("prepass:pageSize", 25);
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const [account, setAccount] = useState(""), [device, setDevice] = useState("");
  const [selectedTruck, setSelectedTruck] = useState<PrePassFleetTruck | null>(null);
  const [vehicle, setVehicle] = useState<PrePassVehicleInput>(() => prePassVehicleFromTruck());
  const [vin, setVIN] = useState("");
  const [reviewed, setReviewed] = useState(false);
  const saving = useRef(false), generation = useRef(0);

  const load = useCallback(async (signal?: AbortSignal) => {
    const current = ++generation.current;
    try {
      const result = await fetchPrePassFleet(signal);
      if (!signal?.aborted && current === generation.current) setData(result);
      return result;
    } catch (e) { if (!signal?.aborted && current === generation.current) setError(e instanceof Error ? e.message : "Could not load PrePass."); }
    finally { if (!signal?.aborted && current === generation.current) setLoading(false); }
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    const timer = setTimeout(() => void load(controller.signal), 0);
    const refresh = () => { if (!saving.current && document.visibilityState === "visible") void load(controller.signal); };
    const interval = setInterval(refresh, 30000); window.addEventListener("focus", refresh);
    return () => { controller.abort(); clearTimeout(timer); clearInterval(interval); window.removeEventListener("focus", refresh); };
  }, [load]);
  useEffect(() => { if (!notice) return; const timer = setTimeout(() => setNotice(""), 5000); return () => clearTimeout(timer); }, [notice]);
  async function run(operation: () => Promise<unknown>, message: string) {
    if (saving.current) return;
    saving.current = true; generation.current++; setBusy(true); setError(""); setNotice("");
    try { await operation(); setNotice(message); setDialog(null); }
    catch (e) { setError(e instanceof Error ? e.message : "PrePass could not confirm the change."); }
    finally {
      const next = await load();
      setDialog(current => {
        if (!current || !("requestId" in current)) return current;
        const receipt = next?.operations.find(o => o.id === current.requestId);
        if (receipt?.state === "confirmed") return null;
        return receipt?.state === "failed" ? { ...current, requestId: crypto.randomUUID() } : current;
      });
      saving.current = false; setBusy(false);
    }
  }
  function open(kind: PrePassFleetChange["action"], entry?: PrePassFleetEntry, selectedDevice = "") {
    const v = entry?.vehicle;
    setAccount(v?.accountNumber ?? data?.accounts[0] ?? ""); setSelectedTruck(null); setDevice(selectedDevice); setReviewed(false); setError(""); setVIN("");
    setVehicle(v ? { vehicleNumber: v.vehicleNumber, licensePlate: { temporary: v.tempPlate, number: v.licensePlateNumber, state: v.licensePlateState }, declaredWeight: v.declaredWeight, irp: { accountNumber: v.irpNumber, state: v.irpState }, ifta: { accountNumber: v.iftaNumber, state: v.iftaState }, vrn: v.vrn, hazmatHauler: v.hazmatHauler } : prePassVehicleFromTruck());
    setDialog({ kind, requestId: crypto.randomUUID(), entry });
  }
  const pending = data?.operations.filter(o => o.state === "pending") ?? [];
  const locked = busy || pending.length > 0;
  const entries = data?.vehicles ?? [];
  const devices = data?.transponders ?? [];
  const available = devices.filter(d => !d.vin.trim() && !d.vehicleNumber.trim()
    && !devices.some(other => other.transponderNumber === d.transponderNumber && (other.accountNumber !== d.accountNumber || !!other.vin.trim() || !!other.vehicleNumber.trim()))
    && !entries.some(e => e.vehicle.transponderNumber === d.transponderNumber));
  const entry = dialog && "entry" in dialog ? dialog.entry : undefined;
  // Keep the identity/version the operator selected, even when cached reads
  // refresh underneath a form. A later profile change must fail the server check.
  const truckId = selectedTruck?.id ?? "";
  const choices = (data?.trucks ?? []).filter(t => dialog?.kind === "link"
    ? t.vin.trim().toUpperCase() === entry?.vehicle.vin && !entries.some(e => e.truckId === t.id && e !== entry && e.vehicle.vin !== entry?.vehicle.vin)
    : t.active && !entries.some(e => e.truckId === t.id || e.vehicle.vin === t.vin.trim().toUpperCase()));
  const invalidSavedVIN = dialog?.kind === "add" && !!selectedTruck?.vin.trim() && !validPrePassVIN(selectedTruck.vin);

  function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!dialog || busy) return;
    if (dialog.kind === "verify") { if (reviewed) void run(() => verifyPrePassFleetOperation(dialog.operation.id), "Result acknowledged. Review the refreshed roster before making another change."); return; }
    if (locked || invalidSavedVIN) return;
    const input: PrePassFleetChange = { requestId: dialog.requestId, action: dialog.kind, accountNumber: account, vin: entry?.vehicle.vin ?? vin.trim().toUpperCase(), revision: entry?.revision };
    if (dialog.kind === "add" || dialog.kind === "link") { input.truckId = truckId; input.truckUpdatedAt = selectedTruck?.updatedAt; }
    if (dialog.kind === "add" || dialog.kind === "edit") input.vehicle = vehicle;
    if (dialog.kind === "assign") input.transponderNumber = device;
    if (dialog.kind !== "link") input.fleetServiceConfirmed = reviewed;
    void run(() => changePrePassFleet(input), dialog.kind === "link" ? "Truck linked. Historical toll mappings are unchanged." : "PrePass change confirmed.");
  }
  const matches = (values: string[]) => values.join(" ").toLowerCase().includes(search.toLowerCase().trim());
  const shownTrucks = entries.filter(e => (!accountFilter || e.vehicle.accountNumber === accountFilter) && (!filter || filter === "unlinked" && !e.truckId || filter === "attention" && !!e.warning || filter === "no-device" && !e.vehicle.transponderNumber) && matches([e.truckUnit, e.vehicle.vehicleNumber, e.vehicle.vin, e.vehicle.licensePlateNumber, e.vehicle.transponderNumber]));
  const shownDevices = devices.filter(d => (!accountFilter || d.accountNumber === accountFilter) && (!filter || filter === "available" && available.includes(d) || filter === "assigned" && !!(d.vin || d.vehicleNumber)) && matches([d.transponderNumber, d.vehicleNumber, d.vin]));
  const count = view === "trucks" ? shownTrucks.length : shownDevices.length;
  const pages = Math.max(1, Math.ceil(count / pageSize)), currentPage = Math.max(1, Math.min(page, pages)), offset = (currentPage - 1) * pageSize;
  const formBlocked = busy || invalidSavedVIN || (dialog?.kind === "verify" ? !reviewed : locked || !account || ((dialog?.kind === "add" || dialog?.kind === "link") && !selectedTruck) || (dialog?.kind === "assign" && !device) || (dialog?.kind !== "link" && !reviewed));

  return <section className="space-y-4" aria-label="PrePass management">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <div className="flex flex-wrap gap-3 text-[12px] text-zinc-500"><span>{entries.length} trucks</span><span>{devices.length} transponders</span><span>{available.length} available</span>{data?.environment === "nonproduction" && <span>Nonproduction</span>}</div>
      <div className="flex flex-wrap gap-2"><button className="ui-button" disabled={busy || !data?.configured} onClick={() => void run(refreshPrePassFleet, "PrePass roster refreshed.")}><RefreshCw className={`h-4 w-4 ${busy ? "animate-spin" : ""}`} />Refresh PrePass</button>{canWrite && <button className="ui-button ui-button-primary" disabled={locked || !data?.refreshedAt} onClick={() => open("add")}><Plus className="h-4 w-4" />Add truck</button>}</div>
    </div>
    {error && !dialog && <ErrorBanner message={error} />}
    {notice && <p role="status" className="text-[13px] text-emerald-400">{notice}</p>}
    {data && !data.configured && <ErrorBanner message="PrePass management is not configured on the server." />}
    {data?.configured && !data.refreshedAt && <p className="text-[13px] text-zinc-400">Select Refresh PrePass to load trucks and transponders.</p>}
    {data?.refreshedAt && <p className="text-[12px] text-zinc-500">PrePass checked {new Date(data.refreshedAt).toLocaleString()}</p>}
    {pending.map(o => <div key={o.id} className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-amber-500/30 p-3 text-[13px] text-amber-400"><span>Unconfirmed {o.action} · VIN {o.vin}. Further changes are paused until the result is reviewed.</span>{canWrite && <button className="ui-button" disabled={busy} onClick={() => { setReviewed(false); setError(""); setDialog({ kind: "verify", operation: o }); }}>Review result</button>}</div>)}
    <div className="flex flex-wrap items-center gap-2">
      {(["trucks", "transponders"] as const).map(v => <button key={v} className="ui-button" aria-pressed={view === v} onClick={() => { setView(v); setFilter(""); setPage(1); }}>{v === "trucks" ? "Trucks" : "Transponders"}</button>)}
      <ManagementSearch value={search} onChange={v => { setSearch(v); setPage(1); }} placeholder="Search unit, VIN, plate or transponder…" />
      <select aria-label="PrePass account filter" className={selectClass} value={accountFilter} onChange={e => { setAccountFilter(e.target.value); setPage(1); }}><option value="">All accounts</option>{data?.accounts.map(a => <option key={a}>{a}</option>)}</select>
      <select aria-label="PrePass status filter" className={selectClass} value={filter} onChange={e => { setFilter(e.target.value); setPage(1); }}><option value="">All {view}</option>{view === "trucks" ? <><option value="attention">Needs attention</option><option value="unlinked">Unlinked</option><option value="no-device">No transponder</option></> : <><option value="available">Available</option><option value="assigned">Assigned</option></>}</select>
    </div>
    <TableShell>{loading ? <LoadingTable columns={7} /> : !count ? <EmptyState message="No PrePass records match this view." /> : view === "trucks" ? <table className="w-full min-w-[1100px] text-left text-[13px]">
      <thead><tr className="border-b border-zinc-800 text-zinc-500">{["MSERP truck", "PrePass unit", "VIN", "Plate", "Transponder", "Account", "Actions"].map(h => <th key={h} className="px-3 py-2 font-medium">{h}</th>)}</tr></thead>
      <tbody>{shownTrucks.slice(offset, offset + pageSize).map(e => <tr key={`${e.vehicle.accountNumber}:${e.vehicle.vin}`} className="border-b border-zinc-800/60">
        <td className="whitespace-nowrap px-3 py-2">{e.truckId ? canProfile ? <Link className="text-blue-400" href={`/trucks/detail?id=${e.truckId}`}>{e.truckUnit}</Link> : e.truckUnit : <span className="text-zinc-500">Unlinked</span>}{e.warning && <span className="ml-2 inline-flex align-middle text-amber-400" title={e.warning}><AlertTriangle aria-label={e.warning} className="h-4 w-4" /></span>}</td>
        <td className="px-3 py-2">{e.vehicle.vehicleNumber}</td><td className="whitespace-nowrap px-3 py-2 font-mono text-[12px]">{e.vehicle.vin}</td><td className="whitespace-nowrap px-3 py-2">{e.vehicle.licensePlateNumber} {e.vehicle.licensePlateState}{e.vehicle.tempPlate ? " · Temp" : ""}</td><td className="px-3 py-2 font-mono">{e.vehicle.transponderNumber || "—"}</td><td className="px-3 py-2">{e.vehicle.accountNumber}</td>
        <td className="px-3 py-1"><div className="flex gap-2">{canLink && <button className="ui-button" disabled={locked} onClick={() => open("link", e)}>{e.truckId ? "Relink" : "Link"}</button>}{canWrite && <><button className="ui-button" disabled={locked} onClick={() => open("edit", e)}>Edit</button>{!e.vehicle.transponderNumber && <button className="ui-button" disabled={locked || !available.some(d => d.accountNumber === e.vehicle.accountNumber)} onClick={() => open("assign", e)}>Assign</button>}<button className="ui-button text-red-400" disabled={locked} onClick={() => open("remove", e)}>Remove</button></>}</div></td>
      </tr>)}</tbody>
    </table> : <table className="w-full min-w-[740px] text-left text-[13px]"><thead><tr className="border-b border-zinc-800 text-zinc-500">{["Transponder", "Assignment", "PrePass unit", "VIN", "Account"].map(h => <th key={h} className="px-3 py-2 font-medium">{h}</th>)}</tr></thead><tbody>{shownDevices.slice(offset, offset + pageSize).map(d => <tr key={`${d.accountNumber}:${d.transponderNumber}`} className="border-b border-zinc-800/60"><td className="px-3 py-2 font-mono">{d.transponderNumber}</td><td className="px-3 py-2">{available.includes(d) ? "Available" : d.vin || d.vehicleNumber ? "Assigned" : "Needs review"}</td><td className="px-3 py-2">{d.vehicleNumber || "—"}</td><td className="px-3 py-2 font-mono text-[12px]">{d.vin || "—"}</td><td className="px-3 py-2">{d.accountNumber}</td></tr>)}</tbody></table>}</TableShell>
    <TablePagination page={currentPage} pageSize={pageSize} totalItems={count} totalPages={pages} onPageChange={setPage} onPageSizeChange={n => { setPageSize(n); setPage(1); }} />
    {!!data?.operations.length && <details className="rounded-lg border border-zinc-800 p-3 text-[12px]"><summary className="cursor-pointer text-zinc-400">Recent changes</summary><ul className="mt-2 space-y-2">{data.operations.map(o => <li key={o.id} className="break-words text-zinc-500">{new Date(o.createdAt).toLocaleString()} · {o.actor} · {o.action} · {o.vin} · {o.state === "pending" ? "Unconfirmed" : o.state === "verified" ? "Reviewed in PrePass" : o.state}</li>)}</ul></details>}
    {dialog && <Modal title={labels[dialog.kind]} isSaving={busy} submitLabel={dialog.kind === "verify" ? "Record reviewed result" : labels[dialog.kind]} submitDisabled={formBlocked} onClose={() => setDialog(null)} onSubmit={submit} wide={dialog.kind === "add" || dialog.kind === "edit"}>
      {error && <ErrorBanner message={error} />}
      {entry && <p className="text-[13px] text-zinc-400">Unit {entry.vehicle.vehicleNumber} · {entry.vehicle.vin}</p>}
      {dialog.kind === "verify" ? <><p className="text-[13px] text-zinc-400">Check this {dialog.operation.action} request for VIN {dialog.operation.vin} in PrePass. Acknowledgment becomes available two minutes after the request and refreshes the roster. It does not resend the action.</p><label className="flex items-center gap-2 text-[13px]"><input type="checkbox" checked={reviewed} onChange={e => setReviewed(e.target.checked)} />I checked the outcome in PrePass.</label></> : <>
        {dialog.kind === "add" && (data?.accounts.length ?? 0) > 1 && <Field label="PrePass account"><select className={controlClass} value={account} onChange={e => setAccount(e.target.value)} required>{data?.accounts.map(a => <option key={a}>{a}</option>)}</select></Field>}
        {(dialog.kind === "add" || dialog.kind === "link") && <><Field label="MSERP truck"><select className={controlClass} required value={truckId} onChange={e => { const t = data?.trucks.find(t => t.id === e.target.value); setSelectedTruck(t ?? null); if (dialog.kind === "add") { setVehicle(prePassVehicleFromTruck(t)); setVIN(t?.vin.trim().toUpperCase() ?? ""); setReviewed(false); } }}><option value="">Select truck</option>{choices.map(t => <option key={t.id} value={t.id}>{t.unit}{dialog.kind === "link" ? ` · ${t.vin}` : ""}{t.active ? "" : " · Inactive"}</option>)}</select></Field>{!choices.length && <p className="text-[13px] text-amber-400">{dialog.kind === "add" ? "No active trucks are available to add." : "No eligible truck with a matching VIN. Check its MSERP profile and existing links."}</p>}</>}
        {invalidSavedVIN && <ErrorBanner message="The saved VIN is invalid. Correct it in the MSERP truck profile before adding this truck to PrePass." />}
        {dialog.kind === "link" && <p className="text-[13px] text-zinc-400">Confirm the matching VIN. This links the fleet roster only; use Truck linking for dated toll corrections.</p>}
        {(dialog.kind === "edit" || dialog.kind === "add" && selectedTruck && !invalidSavedVIN) && <PrePassVehicleFields truck={dialog.kind === "add" ? selectedTruck ?? undefined : undefined} vehicle={vehicle} setVehicle={setVehicle} vin={vin} setVIN={setVIN} />}
        {dialog.kind === "assign" && <Field label="Available transponder"><select className={controlClass} required value={device} onChange={e => setDevice(e.target.value)}><option value="">Select transponder</option>{available.filter(d => d.accountNumber === account).map(d => <option key={d.transponderNumber}>{d.transponderNumber}</option>)}</select></Field>}
        {dialog.kind === "remove" && <p className="text-[13px] text-amber-400">Remove this vehicle from PrePass? This changes its PrePass enrollment. The MSERP truck and historical tolls are retained; physical device returns must be handled separately.</p>}
        {dialog.kind !== "link" && (dialog.kind !== "add" || selectedTruck && !invalidSavedVIN) && <label className="flex items-start gap-2 text-[13px] text-zinc-400"><input className="mt-1" type="checkbox" checked={reviewed} onChange={e => setReviewed(e.target.checked)} />This truck is not enrolled in the Toll Violation Prevention Program (VPP).</label>}
      </>}
    </Modal>}
  </section>;
}
