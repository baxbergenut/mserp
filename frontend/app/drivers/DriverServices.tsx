"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "@/app/components/AccessLink";
import { CheckCircle2, Clock3, Fuel, Scale, Truck, Loader2, AlertCircle } from "lucide-react";
import { usePermissions } from "../lib/access";
import { changeRelayDriver, fetchDriverServices, linkRelayRosterDriver, refreshRelayDrivers, requestDriverService, verifyRelayDriverOperation, verifyWeighMyTruck } from "../lib/api";
import type { DriverService, DriverServiceIntent, DriverServiceSetup, DriverServiceState } from "../lib/types";
import { controlClass, ErrorBanner, Field } from "../components/management/ManagementUI";

export const serviceNames = { relay: "Relay", weighmytruck: "WeighMyTruck", datatruck: "DataTruck" };
const names = serviceNames;
const writePermission = { relay: "fuel.drivers.create", weighmytruck: "weighmytruck.create", datatruck: "drivers.create" };
const services: DriverService[] = ["relay", "weighmytruck", "datatruck"];
export const defaultDriverServices = (): DriverServiceIntent[] => [{service:"datatruck",action:"add"}];
const button = "ui-button !text-xs aria-pressed:border-blue-500/40 aria-pressed:bg-blue-500/15 aria-pressed:text-blue-300";
const labels: Record<string, string> = { not_requested: "Not requested", queued: "Waiting to add", running: "Adding…", added: "Added", needs_setup: "Added · policy needed", review: "Needs attention", unconfirmed: "Check result", deferred: "Reminder set", canceled: "Setup canceled" };
function tomorrowNY() {
  const today = new Intl.DateTimeFormat("en-CA", { timeZone: "America/New_York", year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
  return new Date(Date.parse(`${today}T12:00:00Z`) + 86400000).toISOString().slice(0, 10);
}
function ServiceTitle({ service, state }: { service: DriverService; state: string }) {
  const Icon = service === "relay" ? Fuel : service === "datatruck" ? Truck : Scale;
  const StatusIcon = state === "added" ? CheckCircle2 : ["queued", "running"].includes(state) ? Loader2 : ["review", "unconfirmed", "needs_setup"].includes(state) ? AlertCircle : Clock3;
  return <div className="flex flex-wrap items-center justify-between gap-2"><h3><Link href={service === "relay" ? "/fuel?tab=drivers" : service === "datatruck" ? "https://app.datatruck.io/hr-management/driver/drivers" : "/weighmytruck"} target="_blank" className="flex items-center gap-2 text-sm font-semibold text-zinc-200 hover:text-blue-400"><Icon className="h-4 w-4 text-zinc-400" />{names[service]}</Link></h3><span role="status" className={`inline-flex items-center gap-1.5 text-xs ${state === "added" ? "text-emerald-400" : ["review", "unconfirmed", "needs_setup"].includes(state) ? "text-amber-300" : "text-zinc-400"}`}><StatusIcon className={`h-3.5 w-3.5 ${state === "running" ? "animate-spin" : ""}`} />{labels[state] || state}</span></div>;
}

// Draft selections never call a mutation endpoint. Membership checks are read-only.
export function DriverServiceChoices({ value, onChange, driverId, disabled }: { value: DriverServiceIntent[]; onChange: (value: DriverServiceIntent[]) => void; driverId?: string; disabled: boolean }) {
  const permissions = usePermissions();
  const [existing, setExisting] = useState<DriverServiceSetup | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!driverId) return;
    const controller = new AbortController();
    fetchDriverServices(driverId, controller.signal).then(setExisting).catch(e => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [driverId]);
  const shown = services.filter(service => permissions.includes(writePermission[service]));
  if (!shown.length) return null;
  const current = existing?.driverId === driverId ? existing : null;
  const choose = (service: DriverService, action?: "add" | "defer", followUpOn = current?.tomorrow || tomorrowNY()) => onChange([...value.filter(v => v.service !== service), ...(action ? [{ service, action, followUpOn }] : [])]);
  return <section aria-label="Driver services" className="space-y-3">
    {error && <ErrorBanner message="Could not check existing service accounts. You can save the driver and review services afterward." />}
    <div className="grid gap-3 sm:grid-cols-2">{shown.map(service => {
      const intent = value.find(v => v.service === service);
      const membership = current?.items.find(v => v.service === service);
      const already = membership && (membership.version > 0 || ["added", "needs_setup", "unconfirmed", "queued", "running", "canceled"].includes(membership.state));
      return <fieldset key={service} disabled={disabled || Boolean(driverId && !current)} className="space-y-3 rounded-xl border border-zinc-800 bg-zinc-900/30 p-4">
        <ServiceTitle service={service} state={already ? membership.state : intent ? intent.action === "add" ? "Add after saving" : "Remind after saving" : driverId && !current ? "Checking…" : "not_requested"} />
        {already ? <p className="text-xs leading-5 text-zinc-400">{membership.message || "An existing request is in progress."} Review this after saving.</p> : <>
          <div className="flex flex-wrap gap-2"><button type="button" className={button} aria-pressed={intent?.action === "add"} onClick={() => choose(service, "add")}>Add after saving</button><button type="button" className={button} aria-pressed={intent?.action === "defer"} onClick={() => choose(service, "defer")}>Remind me later</button>{intent && <button type="button" className="text-xs text-zinc-400 hover:text-zinc-200" onClick={() => choose(service)}>Undo</button>}</div>
          {intent?.action === "defer" && <Field label={`${names[service]} follow-up date`}><input required type="date" className={controlClass} value={intent.followUpOn} onChange={e => choose(service, "defer", e.target.value)} /></Field>}
        </>}
      </fieldset>;
    })}</div>
  </section>;
}

export function DriverServiceCards({ driverId, allowReminders = false }: { driverId: string; allowReminders?: boolean }) {
  const permissions = usePermissions();
  const visible = permissions.some(p => ["drivers.read", "fuel.read", "fuel.drivers.create", "weighmytruck.read", "weighmytruck.create"].includes(p));
  return visible ? <SavedDriverServices key={driverId} driverId={driverId} allowReminders={allowReminders} /> : null;
}
function SavedDriverServices({ driverId, allowReminders }: { driverId: string; allowReminders: boolean }) {
  const [data, setData] = useState<DriverServiceSetup | null>(null);
  const [error, setError] = useState("");
  const pollingFast = !data || data.items.some(item => ["queued", "running"].includes(item.requestState) || ["queued", "running"].includes(item.dataTruckSyncState ?? ""));
  const sequence = useRef(0);
  const mounted = useRef(true);
  const refresh = useCallback(async () => {
    const request = ++sequence.current;
    try { const next = await fetchDriverServices(driverId); if (mounted.current && request === sequence.current) { setData(next); setError(""); } }
    catch (e) { if (mounted.current && request === sequence.current) setError(e instanceof Error ? e.message : "Could not check service status."); }
  }, [driverId]);
  useEffect(() => {
    mounted.current = true;
    const initial = window.setTimeout(() => void refresh(), 0);
    const requestSequence = sequence;
    const check = () => { if (!document.hidden) void refresh(); };
    const interval = window.setInterval(check, pollingFast ? 2000 : 15000);
    window.addEventListener("focus", check);
    return () => { mounted.current = false; requestSequence.current++; window.clearTimeout(initial); window.clearInterval(interval); window.removeEventListener("focus", check); };
  }, [refresh, pollingFast]);
  return <section id="services" aria-label="Driver services" className="space-y-3 rounded-xl border border-zinc-800 p-5">
    <h2 className="text-sm font-semibold text-zinc-200">Driver services</h2>
    {error && <ErrorBanner message={error} />}
    {!data && <p className="text-xs text-zinc-400">Checking service status…</p>}
    <div className="grid gap-3 sm:grid-cols-2">{data?.items.map(item => <ServiceCard key={item.service} item={item} data={data} refresh={refresh} allowReminders={allowReminders} />)}</div>
  </section>;
}
function ServiceCard({ item, data, refresh, allowReminders }: { item: DriverServiceState; data: DriverServiceSetup; refresh: () => Promise<void>; allowReminders: boolean }) {
  const permissions = usePermissions();
  const writable = permissions.includes(writePermission[item.service]) && data.driverStatus !== "terminated";
  const [busy, setBusy] = useState(false);
  const latch = useRef(false);
  const [error, setError] = useState("");
  const [reminder, setReminder] = useState(false);
  const [date, setDate] = useState(data.tomorrow);
  const [policyId, setPolicyId] = useState("");
  const [pin, setPin] = useState("");
  const [truck, setTruck] = useState("");
  const [candidate, setCandidate] = useState("");
  const [verifiedKey, setVerifiedKey] = useState("");
  const [membershipResult, setMembershipResult] = useState("");
  const verificationKey = item.state === "unconfirmed" ? JSON.stringify([item.membership?.id, item.membership?.version, item.operations.map(op => op.id)]) : candidate;
  const verified = Boolean(verifiedKey) && verifiedKey === verificationKey;
  const working = ["queued", "running"].includes(item.state);
  const policies = data.policies ?? [];
  const policy = policies.find(p => p.id === policyId);
  const nonELD = policy?.name.trim().toLowerCase() === "non eld policy";
  async function act(fn: () => Promise<unknown>) {
    if (latch.current) return;
    latch.current = true; setBusy(true); setError("");
    try { await fn(); setReminder(false); setPin(""); setVerifiedKey(""); }
    catch (e) { setError(e instanceof Error ? e.message : "Service setup could not be confirmed."); }
    finally { await refresh(); latch.current = false; setBusy(false); }
  }
  const queue = (action: "add" | "defer") => writable ? act(() => requestDriverService(data.driverId, { service: item.service, action, version: item.version, followUpOn: date })) : Promise.resolve();
  return <article aria-label={`${names[item.service]} setup`} className="space-y-3 rounded-xl border border-zinc-800 bg-zinc-900/30 p-4">
    <ServiceTitle service={item.service} state={item.state} />
    {item.message && <p className="text-xs leading-5 text-zinc-400">{item.message}</p>}
    {item.dataTruckId && <p className="text-xs text-zinc-500">DataTruck ID: {item.dataTruckId}</p>}
    {item.dataTruckSyncState && <p className={`text-xs ${["review","unconfirmed"].includes(item.dataTruckSyncState) ? "text-amber-300" : "text-zinc-500"}`}>{item.dataTruckSyncMessage || (({queued:"Profile update queued",running:"Updating profile",accepted:"Profile update accepted",superseded:"Profile unchanged",canceled:"Profile update canceled"} as Record<string,string>)[item.dataTruckSyncState] ?? item.dataTruckSyncState)}</p>}
    {item.requestState === "deferred" && item.state !== "added" && <p className="text-xs text-zinc-400">Follow up {item.followUpOn}. Enrollment will wait for you.</p>}
    {!item.configured && <p className="text-xs text-amber-300">Service connection is not configured.</p>}
    {error && <ErrorBanner message={error} />}
    {item.relay && <p className="break-all text-xs text-zinc-500">Relay ID: {item.relay.driver.id}{item.relay.assignment && <span className="block">{item.relay.assignment.policy_name} · {item.relay.assignment.enabled ? "Enabled" : "Disabled"}</span>}</p>}
    {item.service !== "datatruck" && ["review", "unconfirmed"].includes(item.state) && <details className="text-xs leading-5 text-zinc-500"><summary className="cursor-pointer">Account with another carrier?</summary><p className="mt-1">Ask the previous carrier to release the account, then retry. This response does not identify the carrier.</p></details>}
    <fieldset disabled={busy || working} className="space-y-3">
      {writable && !item.relay && !["added", "unconfirmed"].includes(item.state) && <button type="button" className={`${button} ui-button-primary`} disabled={!item.configured || item.candidates.length > 0} onClick={() => void queue("add")}>{item.state === "review" ? "Retry enrollment" : `Add to ${names[item.service]}`}</button>}
      {item.service === "relay" && !item.relay && item.candidates.length > 0 && <div className="space-y-2">
        <p className="text-xs text-amber-300">Possible existing account. Confirm the person before linking.</p>
        {data.canLinkRelay ? <><Field label="Existing Relay account"><select className={controlClass} value={candidate} onChange={e => { setCandidate(e.target.value); setVerifiedKey(""); }}><option value="">Choose account</option>{item.candidates.map(v => <option key={v.driver.id} value={v.driver.id}>{v.driver.first_name} {v.driver.last_name} · {v.driver.phone} · {v.driver.id}</option>)}</select></Field><label className="flex gap-2 text-xs text-zinc-400"><input type="checkbox" checked={verified} onChange={e => setVerifiedKey(e.target.checked ? verificationKey : "")} />I confirmed this is {data.name}.</label><button type="button" className={button} disabled={!candidate || !verified || item.state === "unconfirmed"} onClick={() => void act(() => linkRelayRosterDriver(candidate, data.driverId))}>Confirm and link</button></> : <p className="text-xs text-zinc-400">Ask the Relay account review assignee to confirm the link.</p>}
      </div>}
      {permissions.includes("fuel.drivers.edit") && item.relay && item.state === "needs_setup" && <div className="space-y-3">
        {item.relay.assignment && !item.relay.assignment.enabled && <button type="button" className={button} onClick={() => void act(() => changeRelayDriver({ requestId: crypto.randomUUID(), action: "enable", relayDriverId: item.relay!.driver.id, revision: item.relay!.revision }))}>Enable existing policy</button>}
        <Field label="Relay policy"><select className={controlClass} value={policyId} onChange={e => { setPolicyId(e.target.value); setPin(""); }}><option value="">Choose policy</option>{policies.map(p => <option key={p.id} value={p.id} disabled={p.id === item.relay?.assignment?.policy_id}>{p.name}</option>)}</select></Field>
        {item.relay.assignment && policy && <p className="text-xs text-amber-300">Changing policy resets usage counters. Enable the current policy to keep its usage.</p>}
        {policy && (nonELD ? <><Field label="Driver PIN" hint="Required by Non ELD Policy. Leave blank only if already set in Relay."><input type="password" autoComplete="new-password" maxLength={100} className={controlClass} value={pin} onChange={e => setPin(e.target.value)} /></Field><Field label="Truck number" hint="Leave blank to keep the existing Relay value."><input maxLength={100} className={controlClass} value={truck} onChange={e => setTruck(e.target.value)} /></Field></> : <p className="text-xs leading-5 text-zinc-400">Uses the driver’s existing ELD connection. If missing, have the driver log into their ELD app and check the connection in Relay.</p>)}
        <button type="button" className={`${button} ui-button-primary`} disabled={!policyId} onClick={() => void act(() => changeRelayDriver({ requestId: crypto.randomUUID(), action: "assign", relayDriverId: item.relay!.driver.id, revision: item.relay!.revision, policyId, driverPin: nonELD ? pin : "", truckNumber: nonELD ? truck : "" }))}>Assign policy</button>
      </div>}
      {item.service !== "datatruck" && permissions.includes(item.service === "relay" ? "fuel.drivers.recover" : "weighmytruck.recover") && item.state === "unconfirmed" && <div className="space-y-3">
        <p className="text-xs leading-5 text-amber-300">The previous result is uncertain. Check the service website before recording the result; don’t submit another enrollment yet.</p>
        {item.service === "weighmytruck" && item.membership?.canVerify && <Field label="Verified WeighMyTruck membership"><select className={controlClass} value={membershipResult} onChange={e => { setMembershipResult(e.target.value); setVerifiedKey(""); }}><option value="">Choose what you verified</option><option value="added">Driver is in our company account</option><option value="absent">Driver is not in our company account</option></select></Field>}
        <label className="flex gap-2 text-xs text-zinc-400"><input type="checkbox" checked={verified} onChange={e => setVerifiedKey(e.target.checked ? verificationKey : "")} />I checked the result on the service website.</label>
        {item.service === "weighmytruck" && item.membership && <button type="button" className={button} disabled={!verified || !membershipResult || !item.membership.canVerify} onClick={() => void act(() => verifyWeighMyTruck(item.membership!, membershipResult === "added", "Membership checked on the provider website during driver setup."))}>Record verified result</button>}
        {item.service === "relay" && item.operations.map(op => <button key={op.id} type="button" className={button} disabled={!verified || Date.parse(data.checkedAt) - Date.parse(op.createdAt) < 120000} onClick={() => void act(() => verifyRelayDriverOperation(op.id))}>Record review of {op.action}</button>)}
      </div>}
      {allowReminders && writable && item.state !== "added" && <div className="space-y-2"><button type="button" className={button} onClick={() => setReminder(!reminder)}>Remind me later</button>{reminder && <><Field label={`${names[item.service]} follow-up date`}><input type="date" className={controlClass} value={date} onChange={e => setDate(e.target.value)} /></Field><button type="button" disabled={!date} className={button} onClick={() => void queue("defer")}>Save reminder</button></>}</div>}
    </fieldset>
    {busy && <p role="status" className="text-xs text-zinc-400">Saving service setup…</p>}
    <div className="flex flex-wrap gap-3 text-xs text-blue-400">
      {item.service === "relay" && permissions.includes("fuel.drivers.sync") && <button type="button" disabled={busy || working} onClick={() => void act(refreshRelayDrivers)}>Refresh Relay accounts</button>}
    </div>
  </article>;
}
