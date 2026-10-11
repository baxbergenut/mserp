"use client";

import { RelayBulkActions } from "./RelayBulkActions";
import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { AlertTriangle, ExternalLink, Plus, RefreshCw } from "lucide-react";
import { changeRelayDriver, fetchRelayDrivers, linkRelayRosterDriver, refreshRelayDrivers, verifyRelayDriverOperation } from "../lib/api";
import { usePermissions } from "../lib/access";
import { useViewState } from "../lib/viewMemory";
import { formatPhone } from "../lib/phone";
import type { RelayDriverAction, RelayDriverChange, RelayDriverList, RelayDriverOperation, RelayRosterEntry } from "../lib/types";
import { controlClass, EmptyState, ErrorBanner, Field, LoadingTable, ManagementSearch, Modal, TablePagination, TableShell } from "../components/management/ManagementUI";

type Dialog = { kind: RelayDriverAction | "link"; entry?: RelayRosterEntry; requestId: string } | { kind: "verify"; operation: RelayDriverOperation };
const nameOf = (entry: RelayRosterEntry) => `${entry.driver.first_name} ${entry.driver.last_name}`.trim();
const actionLabel = { add: "Add driver", edit: "Edit driver", remove: "Remove driver", enable: "Enable policy", disable: "Disable policy", assign: "Assign policy", unassign: "Unassign policy", link: "Link driver", verify: "Verify Relay result" };
const emptyForm = { driverId: "", firstName: "", lastName: "", phone: "", email: "", policyId: "", driverPin: "", truckNumber: "" };
const compactSelectClass = controlClass.replace("w-full", "w-auto max-w-full");

export function RelayDriversTab() {
  const permissions = usePermissions();
  const canWrite = permissions.includes("fuel.drivers.write");
  const canProfile = permissions.includes("fleet.read");
  const [data, setData] = useState<RelayDriverList | null>(null);
  const [loading, setLoading] = useState(true);
  const [now, setNow] = useState(0);
  const [busy, setBusy] = useState(false);
  const saving = useRef(false);
  const generation = useRef(0);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [search, setSearch] = useViewState("relay:search", "");
  const [status, setStatus] = useViewState("relay:status", "");
  const [policy, setPolicy] = useViewState("relay:policy", "");
  const [page, setPage] = useViewState("relay:page", 1);
  const [pageSize, setPageSize] = useViewState("relay:pageSize", 25);
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const [form, setForm] = useState(emptyForm);
  const [selected,setSelected] = useState<string[]>([]);
  const [reviewed, setReviewed] = useState(false);

  const load = useCallback(async (signal?: AbortSignal) => {
    const current = ++generation.current;
    try { const result = await fetchRelayDrivers(signal); if (!signal?.aborted && current === generation.current) { setData(result); setNow(Date.now()); } }
    catch (e) { if (!signal?.aborted && current === generation.current) setError(e instanceof Error ? e.message : "Could not load Relay drivers."); }
    finally { if (current === generation.current) setLoading(false); }
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    const timer = setTimeout(() => void load(controller.signal), 0);
    const refresh = () => { if (!saving.current && document.visibilityState === "visible") void load(controller.signal); };
    const interval = setInterval(refresh, 30000);
    window.addEventListener("focus", refresh);
    return () => { controller.abort(); clearTimeout(timer); clearInterval(interval); window.removeEventListener("focus", refresh); };
  }, [load]);
  useEffect(() => { if (!notice) return; const timer = setTimeout(() => setNotice(""), 5000); return () => clearTimeout(timer); }, [notice]);

  async function run(operation: () => Promise<unknown>, message: string) {
    if (saving.current) return;
    saving.current = true; generation.current++; setBusy(true); setError(""); setNotice("");
    try { await operation(); setNotice(message); setDialog(null); setForm(emptyForm); }
    catch (e) {
      const message = e instanceof Error ? e.message : "Relay could not confirm this change.";
      setError(message);
      if (message.startsWith("Relay rejected the request")) setDialog(current => current && "requestId" in current ? { ...current, requestId: crypto.randomUUID() } : current);
    }
    finally { await load(); saving.current = false; setBusy(false); }
  }
  function open(kind: RelayDriverAction | "link", entry?: RelayRosterEntry) {
    setError(""); setReviewed(false);
    const local = data?.drivers.find(d => d.id === entry?.driverId);
    const relayTruck = entry?.driver.data_fields?.find(field => field.field_name === "Truck #")?.field_value;
    setForm({ ...emptyForm, firstName: entry?.driver.first_name ?? "", lastName: entry?.driver.last_name ?? "", phone: entry?.driver.phone ?? "", email: entry?.driver.email ?? "", truckNumber: relayTruck ?? (kind === "assign" ? local?.truck ?? "" : "") });
    setDialog({ kind, entry, requestId: crypto.randomUUID() });
  }
  const items = data?.items ?? [];
  const available = (data?.drivers ?? []).filter(d => d.status === "active" && !items.some(e => e.driverId === d.id) && !data?.operations.some(o => o.driverId === d.id && o.action === "add"));
  const filtered = items.filter(e => {
    const text = `${nameOf(e)} ${e.driverName} ${e.driver.phone} ${e.driver.email} ${e.driver.id} ${e.driver.integration_id}`.toLowerCase();
    const matchesStatus = !status || (status === "attention" ? !!e.warning : status === "no_policy" ? !e.assignment : status === "enabled" ? e.assignment?.enabled : status === "disabled" ? e.assignment && !e.assignment.enabled : e.status === status);
    return text.includes(search.trim().toLowerCase()) && matchesStatus && (!policy || e.assignment?.policy_id === policy);
  });
  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize));
  const currentPage = Math.min(page, totalPages);
  const shown = filtered.slice((currentPage - 1) * pageSize, currentPage * pageSize);
  const selectedPolicy = data?.policies.find(p => p.id === form.policyId);
  const isNonELD = selectedPolicy?.name.trim().toLowerCase() === "non eld policy";
  const entry = dialog && "entry" in dialog ? dialog.entry : undefined;
  const portal = data?.environment === "staging" ? "https://staging.relaypayments.com/sender/drivers" : "https://app.relaypayments.com/sender/drivers";
  const blockedEntry = !!entry && !!data?.operations.some(op => op.relayDriverId === entry.driver.id);

  function submit() {
    if (!dialog) return;
    if (dialog.kind === "verify") { if (!reviewed) return; void run(() => verifyRelayDriverOperation(dialog.operation.id), "Relay result acknowledged. Review the refreshed roster before making another change."); return; }
    if (dialog.kind === "link") { if (entry && form.driverId) void run(() => linkRelayRosterDriver(entry.driver.id, form.driverId), "Driver linked; unassigned historical fuel transactions updated."); return; }
    if (blockedEntry) { setError("Verify the pending Relay request before making another change."); return; }
    const input: RelayDriverChange = { requestId: dialog.requestId, action: dialog.kind, relayDriverId: entry?.driver.id, revision: entry?.revision };
    if (dialog.kind === "add") Object.assign(input, { driverId: form.driverId, firstName: form.firstName, lastName: form.lastName, phone: form.phone, email: form.email, driverPin: form.driverPin, truckNumber: form.truckNumber });
    if (dialog.kind === "edit") Object.assign(input, { firstName: form.firstName, lastName: form.lastName, email: form.email, driverPin: form.driverPin, truckNumber: form.truckNumber });
    if (dialog.kind === "assign") Object.assign(input, { policyId: form.policyId, driverPin: isNonELD ? form.driverPin : "", truckNumber: isNonELD ? form.truckNumber : "" });
    void run(() => changeRelayDriver(input), dialog.kind === "add" ? "Driver added and linked. Select Assign policy to finish setup." : `${actionLabel[dialog.kind]} completed.`);
  }

  return <section className="space-y-4" aria-label="Relay drivers">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <div className="flex flex-wrap items-center gap-3 text-[12px] text-zinc-500"><span>{items.length} drivers</span><span>{items.filter(e => e.assignment?.enabled).length} enabled</span><span>{items.filter(e => !e.assignment).length} no policy</span>{items.some(e => e.status === "terminated") && <span className="text-amber-400">{items.filter(e => e.status === "terminated").length} terminated in Relay</span>}{data?.environment === "staging" && <span>Staging</span>}</div>
      <div className="flex flex-wrap gap-2"><a href={portal} target="_blank" rel="noreferrer" className="ui-button"><ExternalLink className="h-4 w-4" />Relay portal</a><button className="ui-button" disabled={busy || !data?.configured} onClick={() => void run(refreshRelayDrivers, "Relay roster refreshed.")}><RefreshCw className={`h-4 w-4 ${busy ? "animate-spin" : ""}`} />Refresh Relay</button>{canWrite && <button className="ui-button ui-button-primary" disabled={busy || !data?.configured || !data.refreshedAt} onClick={() => open("add")}><Plus className="h-4 w-4" />Add driver</button>}</div>
    </div>
    {error && !dialog && <ErrorBanner message={error} />}
    {notice && <p role="status" className="text-[13px] text-emerald-400">{notice}</p>}
    {data && !data.configured && <ErrorBanner message="Relay driver management is not configured on the server." />}
    {data?.configured && !data.refreshedAt && <p className="text-[13px] text-zinc-400">Select Refresh Relay to load the current roster, including ELD drivers without a policy.</p>}
    {data?.refreshedAt && <p className="text-[12px] text-zinc-500">Relay checked {new Date(data.refreshedAt).toLocaleString()}{now - Date.parse(data.refreshedAt) > 300000 ? " · Refresh Relay for the latest portal changes." : ""}</p>}
    {data?.operations.map(op => <div key={op.id} role="alert" className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-amber-500/30 p-3 text-[13px] text-amber-400"><span>Unconfirmed {op.action}: {items.find(e => e.driver.id === op.relayDriverId)?.driverName || data.drivers.find(d => d.id === op.driverId)?.name || op.relayDriverId || "driver"}. Verify the result in Relay before retrying.</span>{canWrite && <button className="ui-button" disabled={busy || now - Date.parse(op.createdAt) < 120000} onClick={() => { setError(""); setReviewed(false); setDialog({ kind: "verify", operation: op }); }}>Review result</button>}</div>)}
    <div className="flex flex-wrap items-center gap-2">
      <ManagementSearch value={search} onChange={v => { setSearch(v); setPage(1); }} placeholder="Search driver, phone or Relay ID" />
      <select aria-label="Driver status" className={compactSelectClass} value={status} onChange={e => { setStatus(e.target.value); setPage(1); }}>{[["", "All drivers"], ["attention", "Needs attention"], ["terminated", "Terminated in Relay"], ["unlinked", "Unlinked"], ["no_policy", "No policy"], ["enabled", "Policy enabled"], ["disabled", "Policy disabled"]].map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select>
      <select aria-label="Policy filter" className={compactSelectClass} value={policy} onChange={e => { setPolicy(e.target.value); setPage(1); }}><option value="">All policies</option>{data?.policies.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</select>
    </div>
    {canWrite && data && <RelayBulkActions data={data} entries={items.filter(e=>selected.includes(e.driver.id) && !e.pending)} onBusy={value=>{saving.current=value;setBusy(value);if(value)generation.current++}} onClear={()=>setSelected([])} onComplete={async ids=>{setSelected(current=>current.filter(id=>!ids.includes(id)));await load()}} />}
    <TableShell>{loading ? <LoadingTable columns={6} /> : !shown.length ? <EmptyState message={data?.refreshedAt ? "No Relay drivers match this view." : "Relay roster has not been loaded."} /> : <table className="w-full min-w-[920px] text-left text-[13px]">
      <thead className="bg-zinc-900/60 text-[12px] text-zinc-500"><tr>{canWrite && <th className="px-3"><input type="checkbox" aria-label="Select drivers on this page" disabled={busy} checked={shown.some(e=>!e.pending) && shown.filter(e=>!e.pending).every(e=>selected.includes(e.driver.id))} onChange={event=>setSelected(current=>event.target.checked?Array.from(new Set([...current,...shown.filter(e=>!e.pending).map(e=>e.driver.id)])):current.filter(id=>!shown.some(e=>e.driver.id===id)))} /></th>}{["Driver", "Phone", "Policy", "Policy status", "Relay Driver ID", "Actions"].map(label => <th key={label} className="h-8 px-3 font-medium">{label}</th>)}</tr></thead>
      <tbody className="divide-y divide-zinc-800/60">{shown.map(e => <tr key={e.driver.id} className={e.warning ? "bg-amber-500/5" : "hover:bg-zinc-800/20"}>
        {canWrite && <td className="px-3"><input type="checkbox" aria-label={`Select ${nameOf(e)}`} disabled={busy || e.pending} checked={selected.includes(e.driver.id)} onChange={event=>setSelected(current=>event.target.checked?[...current,e.driver.id]:current.filter(id=>id!==e.driver.id))} /></td>}
        <td className="px-3 py-1.5"><div className="flex items-center gap-2">{e.driverId && canProfile ? <Link href={`/drivers/detail?id=${e.driverId}`} className="text-zinc-200 hover:text-accent">{nameOf(e)}</Link> : <span>{nameOf(e)}</span>}{e.warning && <span title={e.warning}><AlertTriangle aria-label={e.warning} className="h-4 w-4 text-amber-400" /></span>}{e.status !== "active" && <span className={e.status === "terminated" ? "text-red-400" : "text-zinc-500"}>{e.status === "unlinked" ? "Unlinked" : e.status}</span>}</div>{e.driverName && e.driverName.toLowerCase() !== nameOf(e).toLowerCase() && <div className="text-[12px] text-zinc-500">{e.driverName}</div>}</td>
        <td className="whitespace-nowrap px-3 py-1.5 text-zinc-400">{formatPhone(e.driver.phone) || "—"}</td>
        <td className="px-3 py-1.5">{e.assignment?.policy_name || "—"}</td>
        <td className="whitespace-nowrap px-3 py-1.5"><span className={e.pending ? "text-amber-400" : e.assignment?.enabled ? "text-emerald-400" : "text-zinc-500"}>{e.pending ? "Unconfirmed" : e.assignment ? e.assignment.enabled ? "Enabled" : "Disabled" : "No policy"}</span></td>
        <td className="px-3 py-1.5 font-mono text-[12px] text-zinc-400" title={`API ID: ${e.driver.id}`}>{e.driver.integration_id || e.driver.id}</td>
        <td className="px-3 py-1.5"><div className="flex justify-end gap-2">{!e.driverId && data?.canLink && <button className="ui-button" disabled={busy || e.pending} onClick={() => open("link", e)}>Link driver</button>}{canWrite && <select aria-label={`Actions for ${nameOf(e)}`} value="" disabled={busy || e.pending} className={compactSelectClass} onChange={event => { if (event.target.value) open(event.target.value as RelayDriverAction, e); }}><option value="">Manage…</option><option value="edit">Edit driver</option><option value="assign">Assign policy</option>{e.assignment && <><option value={e.assignment.enabled ? "disable" : "enable"}>{e.assignment.enabled ? "Disable policy" : "Enable policy"}</option><option value="unassign">Unassign policy</option></>}<option value="remove">Remove driver</option></select>}</div></td>
      </tr>)}</tbody>
    </table>}</TableShell>
    <TablePagination page={currentPage} pageSize={pageSize} totalItems={filtered.length} totalPages={totalPages} onPageChange={setPage} onPageSizeChange={value => { setPageSize(value); setPage(1); }} />
    {dialog && <Modal title={`${actionLabel[dialog.kind]}${entry ? `: ${nameOf(entry)}` : ""}`} isSaving={busy} submitLabel={actionLabel[dialog.kind]} onClose={() => { setDialog(null); setForm(emptyForm); }} onSubmit={event => { event.preventDefault(); submit(); }}>
      <fieldset disabled={busy} className="space-y-4">
        {error && <ErrorBanner message={error} />}
        {dialog.kind === "verify" ? <><p className="text-[13px] text-zinc-300">Check the driver and policy in the <a href={portal} target="_blank" rel="noreferrer" className="text-blue-400 underline">Relay portal</a>. This acknowledges the uncertain request and refreshes the roster; it does not resend the change. If an add succeeded, link the existing Relay account.</p><label className="flex items-center gap-2 text-[13px]"><input type="checkbox" required checked={reviewed} onChange={e => setReviewed(e.target.checked)} />I verified the result in Relay.</label></> : <>
          {(dialog.kind === "add" || dialog.kind === "link") && <Field label={dialog.kind === "add" ? "Active MSERP driver" : "MSERP driver"}><select required className={controlClass} value={form.driverId} onChange={e => { const id = e.target.value; const d = data?.drivers.find(v => v.id === id); const parts = d?.name.split(/\s+/) ?? []; setForm({ ...form, driverId: id, ...(dialog.kind === "add" ? { firstName: parts[0] ?? "", lastName: parts.slice(1).join(" "), phone: d?.phone ?? "", email: d?.email ?? "", truckNumber: d?.truck ?? "" } : {}) }); }}><option value="">Select driver</option>{(dialog.kind === "add" ? available : data?.drivers ?? []).map(d => <option key={d.id} value={d.id}>{d.name}{d.status !== "active" ? ` · ${d.status}` : ""}</option>)}</select></Field>}
          {dialog.kind === "link" && <><p className="text-[13px] text-zinc-400">Confirm this is the same person. Linking assigns this Relay account’s unassigned historical fuel transactions to the selected driver.</p>{entry?.suggestions.map(s => <button key={s.driverId} type="button" className={`ui-button mr-2 ${form.driverId === s.driverId ? "text-blue-400" : ""}`} onClick={() => setForm({ ...form, driverId: s.driverId })}>{s.name} · {s.reasons.join(", ")}</button>)}</>}
          {(dialog.kind === "add" || dialog.kind === "edit") && <><div className="grid gap-3 sm:grid-cols-2"><Field label="First name"><input required maxLength={150} className={controlClass} value={form.firstName} onChange={e => setForm({ ...form, firstName: e.target.value })} /></Field><Field label="Last name"><input required maxLength={150} className={controlClass} value={form.lastName} onChange={e => setForm({ ...form, lastName: e.target.value })} /></Field><Field label="Phone"><input required type="tel" disabled={dialog.kind === "edit"} className={controlClass} value={form.phone} onChange={e => setForm({ ...form, phone: e.target.value })} /></Field><Field label="Email"><input type="email" maxLength={254} className={controlClass} value={form.email} onChange={e => setForm({ ...form, email: e.target.value })} /></Field></div>{dialog.kind === "add" && <p className="text-[12px] text-zinc-500">ELD drivers may already exist in Relay without a policy. Refresh and link that account before adding a new one. New drivers are added without a policy.</p>}</>}
          {dialog.kind === "assign" && <><Field label="Policy"><select required className={controlClass} value={form.policyId} onChange={e => setForm({ ...form, policyId: e.target.value, driverPin: "" })}><option value="">Select policy</option>{data?.policies.map(p => <option key={p.id} value={p.id} disabled={p.id === entry?.assignment?.policy_id}>{p.name}</option>)}</select></Field>{entry?.assignment && <p className="text-[13px] text-amber-400">Assigning a different policy resets usage counters to zero. Use Enable policy to keep the current policy and usage.</p>}{selectedPolicy && !isNonELD && <p className="text-[13px] text-zinc-400">Uses the driver’s existing Relay ELD connection. If the driver is not connected, have them log into their ELD app and check the connection in Relay.</p>}</>}
          {(isNonELD && dialog.kind === "assign" || dialog.kind === "edit" || dialog.kind === "add") && <div className="grid gap-3 sm:grid-cols-2"><Field label="Driver Pin" hint={dialog.kind === "assign" ? "Leave blank to keep the existing PIN. Relay requires a PIN for Non ELD Policy." : "Optional; leave blank to preserve an existing PIN."}><input type="password" autoComplete="new-password" maxLength={100} className={controlClass} value={form.driverPin} onChange={e => setForm({ ...form, driverPin: e.target.value })} /></Field><Field label="Truck #" hint="Leave blank to preserve an existing value."><input maxLength={100} className={controlClass} value={form.truckNumber} onChange={e => setForm({ ...form, truckNumber: e.target.value })} /></Field></div>}
          {dialog.kind === "remove" && <p className="text-[13px] text-zinc-300">Remove {entry && nameOf(entry)} from Relay? Their MSERP profile, identity link and historical fuel transactions will be retained.</p>}
          {dialog.kind === "unassign" && <p className="text-[13px] text-zinc-300">Remove this driver’s policy assignment? They will no longer be able to fuel with a policy code. Reassigning a policy resets its usage.</p>}
          {(dialog.kind === "enable" || dialog.kind === "disable") && <p className="text-[13px] text-zinc-300">{dialog.kind === "enable" ? "Allow this driver to fuel using their assigned policy?" : "Stop this driver from fueling using their assigned policy?"} Current usage counters are preserved.</p>}
          {entry?.status === "terminated" && dialog.kind !== "remove" && <p className="text-[13px] text-amber-400">This driver is terminated in MSERP.</p>}
        </>}
      </fieldset>
    </Modal>}
  </section>;
}
