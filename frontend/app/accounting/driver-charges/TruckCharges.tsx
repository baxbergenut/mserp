"use client";
import type { TruckOption, InvestorOption } from "@/app/lib/types";

import { TableFilters } from "@/app/components/management/TableFilters";
import type { ReactNode } from "react";

import Link from "@/app/components/AccessLink";
import { History } from "lucide-react";
import { usePermissions } from "@/app/lib/access";
import { useEffect, useMemo, useState } from "react";
import { fetchTruckCharges, fetchTruckOptions, fetchInvestorOptions, saveTruckCharge, saveTruckCharges, saveTruckTerm } from "@/app/lib/api";
import type { ChargeData, TruckChargeBulkResult, TruckChargeData, TruckChargePhase, TruckTerm } from "@/app/lib/types";
import { useViewState } from "@/app/lib/viewMemory";
import { Modal, Field, controlClass, ErrorBanner } from "@/app/components/management/ManagementUI";
import { payButtonClass } from "../driver-pay/DriverCard";
import { validChargeWeek } from "./charges";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";
import EffectiveWeekPicker from "./EffectiveWeekPicker";

export function TruckCharges({ charges, onChanged, onCountChange, actions }: { actions?: ReactNode; charges: ChargeData; onCountChange: (count: number) => void; onChanged: () => Promise<void> }) {
  const permissions=usePermissions();
 const canWrite=permissions.includes("charges.edit"),canTerms=permissions.includes("investor_terms.edit"),canReadTerms=permissions.includes("investor_terms.read");
  const [archived, setArchived] = useViewState("truckCharges:archived", false);
  const [typeFilter, setTypeFilter] = useViewState("truckCharges:typeFilter", "");
  const [data, setData] = useState<TruckChargeData | null>(null);
  const [loadedWeek, setLoadedWeek] = useState("");
  const [trucks, setTrucks] = useState<TruckOption[]>([]);
  const [investors, setInvestors] = useState<InvestorOption[]>([]);
  const [rememberedWeek, setWeek] = useViewState("truckCharges:week", charges.currentWeek);
  const week = validChargeWeek(rememberedWeek) ? rememberedWeek : charges.currentWeek;
  const [inclusion, setInclusion] = useViewState("truckCharges:inclusion", "all");
  const [search, setSearch] = useViewState("truckCharges:search", "");
  const [error, setError] = useState("");
  const [failures, setFailures] = useState<TruckChargeBulkResult[]>([]);
  const [busy, setBusy] = useState(false);
  const [term, setTerm] = useState<TruckTerm | null>(null);
  const [phase, setPhase] = useState<TruckChargePhase | null>(null);
  async function reload() {
    const [d, t, i] = await Promise.all([fetchTruckCharges(week), fetchTruckOptions(), fetchInvestorOptions()]);
    setData(d); setTrucks(t); setInvestors(i); setLoadedWeek(week);
  }
  useEffect(() => {
    let cancelled = false;
    Promise.all([fetchTruckCharges(week), fetchTruckOptions(), fetchInvestorOptions()]).then(([d, t, i]) => { if (!cancelled) { setData(d); setTrucks(t); setInvestors(i); setLoadedWeek(week); } }).catch(e => { if (!cancelled) setError(e.message); });
    return () => { cancelled = true; };
  }, [week]);
  useEffect(() => {
    let cancelled = false;
    const refresh = () => {
      if (busy || term || phase || document.visibilityState === "hidden") return;
      Promise.all([fetchTruckCharges(week), fetchTruckOptions(), fetchInvestorOptions()]).then(([d, t, i]) => {
        if (!cancelled) { setData(d); setTrucks(t); setInvestors(i); setLoadedWeek(week); }
      }).catch(e => { if (!cancelled) setError(e.message); });
    };
    window.addEventListener("focus", refresh);
    document.addEventListener("visibilitychange", refresh);
    return () => { cancelled = true; window.removeEventListener("focus", refresh); document.removeEventListener("visibilitychange", refresh); };
  }, [week, busy, term, phase]);
  async function run(action: () => Promise<void>) { setBusy(true); setError(""); setFailures([]); try { await action(); await reload(); await onChanged(); setTerm(null); setPhase(null); } catch (e) { setError(e instanceof Error ? e.message : "Unable to save truck charges"); try { await reload(); await onChanged(); } catch { /* Keep the original save error visible. */ } } finally { setBusy(false); } }
  async function saveCells(updates: TruckChargePhase[]) {
    if (!canWrite || !data || busy || !updates.length) return;
    const previous = data;
    const key = (v: TruckChargePhase | TruckChargeBulkResult) => `${v.truckId}:${v.typeId}:${v.weekStart}`;
    const apply = (values: TruckChargePhase[]) => {
      const keys = new Set(values.map(key));
      return { ...previous, phases: [...previous.phases.filter(p => !keys.has(key(p))), ...values].sort((a, b) => a.weekStart.localeCompare(b.weekStart)) };
    };
    setData(apply(updates));
    setBusy(true); setError(""); setFailures([]);
    let saved = false;
    try {
      const results = updates.length === 1
        ? await saveTruckCharge(updates[0]).then(() => [{ ...updates[0], error: undefined }])
        : await saveTruckCharges(updates);
      saved = true;
      const failed = results.filter(r => r.error);
      const failedKeys = new Set(failed.map(key));
      // Revert rejected cells immediately and retain confirmed versions even if
      // the refresh fails. Every other truck in the selection still saves.
      setData(apply(updates.filter(v => !failedKeys.has(key(v))).map(v => ({ ...v, version: v.version + 1 }))));
      setFailures(failed);
      if (failed.length) setError(`${updates.length - failed.length} truck charges updated; ${failed.length} could not be saved. Review the trucks below.`);
      setData(await fetchTruckCharges(week));
    } catch (e) {
      if (!saved) {
        setData(previous);
        setError(e instanceof Error ? e.message : "Unable to save truck charges");
        try { setData(await fetchTruckCharges(week)); } catch { /* Keep the save error visible. */ }
      } else {
        setError(current => [current, "Changes saved, but the latest charges could not be loaded. Reload before making more changes."].filter(Boolean).join(" "));
      }
    } finally { setBusy(false); }
  }
  const phaseIndex = useMemo(() => {
    const current = new Map<string, TruckChargePhase>();
    const exact = new Map<string, TruckChargePhase>();
    for (const p of data?.phases ?? []) {
      const key = `${p.truckId}:${p.typeId}`;
      if (p.weekStart <= week && (!current.has(key) || current.get(key)!.weekStart < p.weekStart)) current.set(key, p);
      if (p.weekStart === week) exact.set(key, p);
    }
    return { current, exact };
  }, [data, week]);
  const owners = useMemo(() => new Map(data?.owners?.map(owner => [owner.truckId, owner])), [data]);
  const lockedCharges = useMemo(() => new Set(data?.lockedCharges?.map(v => `${v.truckId}:${v.typeId}`)), [data]);
  const eligibleTruckIds = useMemo(() => new Set(data?.eligibleTruckIds), [data]);
  const validWeek = validChargeWeek(week);
  const availableTypes = charges.types.filter(t => !t.archived || archived);
  const effectiveType = availableTypes.some(t => t.id === typeFilter) ? typeFilter : "";
  const types = availableTypes.filter(t => !effectiveType || t.id === effectiveType);
  const includedInView = (truckId: string) => types.some(type => phaseIndex.current.get(`${truckId}:${type.id}`)?.included);
  const visible = trucks.map(t => ({ ...t, ...(owners.get(t.id) ?? {}) })).filter(t => eligibleTruckIds.has(t.id)
    && `${t.unitNumber} ${t.ownerName} ${t.driverName ?? ""}`.toLowerCase().includes(search.toLowerCase())
    && (inclusion === "all" || includedInView(t.id) === (inclusion === "included")))
    .sort((a,b) => a.ownerName.localeCompare(b.ownerName) || a.unitNumber.localeCompare(b.unitNumber));
  useEffect(() => { onCountChange(visible.length); }, [visible.length, onCountChange]);
  const money = (s: string) => decimalDisplay(hundredths(s), true);
  const truck = trucks.find(t => t.id === phase?.truckId);
  const canEditPhase = canWrite && !!phase && !lockedCharges.has(`${phase.truckId}:${phase.typeId}`);
  return <div className="space-y-3">
    {!term && !phase && error && <ErrorBanner message={error} />}
    {!!failures.length && <ul aria-label="Truck charges that could not be saved" className="max-h-48 overflow-y-auto text-xs text-red-300">{failures.map(f => <li key={`${f.truckId}:${f.typeId}:${f.weekStart}`}>Truck {trucks.find(t => t.id === f.truckId)?.unitNumber ?? f.truckId}: {f.error}</li>)}</ul>}
    <div><div className="flex flex-wrap items-center gap-2"><EffectiveWeekPicker week={week} currentWeek={charges.currentWeek} disabled={busy} onChange={setWeek} /><input aria-label="Search trucks" value={search} onChange={e => setSearch(e.target.value)} placeholder="Owner, truck or driver…" className={`${controlClass} !w-40`} />
        <TableFilters>
          <select aria-label="Filter truck charge type" className={`${controlClass} !w-36`} value={effectiveType} onChange={e => setTypeFilter(e.target.value)}><option value="">All charge types</option>{availableTypes.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}</select><select aria-label="Truck inclusion" className={`${controlClass} !w-36`} value={inclusion} onChange={e => setInclusion(e.target.value)}><option value="all">All trucks</option><option value="included">Included trucks</option><option value="excluded">Excluded trucks</option></select><label className="flex items-center gap-2 text-xs text-zinc-400"><input aria-label="Show archived charge types" type="checkbox" checked={archived} onChange={e => setArchived(e.target.checked)} />Archived types</label>
        </TableFilters><button className={payButtonClass} disabled={busy} onClick={() => void run(async () => {})}>Reload</button><div className="ml-auto">{actions}</div></div></div>
    {!validWeek && <p role="alert" className="text-xs text-amber-300">Choose a valid Monday.</p>}
    {!data || loadedWeek !== week ? <p role="status" className="p-8 text-sm text-zinc-500">Loading truck charges…</p> : <div data-scroll-key="truck-recurring-charges" className="overflow-x-auto rounded-lg border border-zinc-800"><table className="w-full text-left text-xs text-zinc-300"><thead><tr className="bg-zinc-900"><th className="sticky left-0 z-10 min-w-52 border-b border-r border-zinc-800 bg-zinc-900 p-3">Owner</th>{["Truck", "Current driver", ...(canReadTerms ? ["Settlement"] : [])].map(label => <th key={label} className="min-w-36 border-b border-zinc-800 p-3">{label}</th>)}{types.map(type => {
      const targets = visible.filter(t => !lockedCharges.has(`${t.id}:${type.id}`));
      const included = targets.filter(t => phaseIndex.current.get(`${t.id}:${type.id}`)?.included).length;
      const lockedCount = visible.length - targets.length;
      const scope = search || inclusion !== "all" ? "visible trucks" : "all trucks";
      return <th key={type.id} scope="col" aria-label={`${type.name}${type.archived ? " (archived)" : ""}`} className="min-w-56 border-b border-zinc-800 p-3 font-medium">{type.name}{type.archived && " (archived)"}<button type="button" aria-label={`Show trucks included in ${type.name}`} className="ml-2 text-blue-400 font-normal" onClick={() => { setTypeFilter(type.id); setInclusion("included"); }}>{trucks.filter(t => eligibleTruckIds.has(t.id) && phaseIndex.current.get(`${t.id}:${type.id}`)?.included).length} trucks</button>{!type.archived && <label className="mt-1 flex w-fit items-center gap-1.5 text-[11px] font-normal text-zinc-400"><input type="checkbox" aria-label={`${type.name}: select ${scope}`} checked={targets.length > 0 && included === targets.length} ref={node => { if (node) node.indeterminate = included > 0 && included < targets.length; }} disabled={busy || !canWrite || !validWeek || !targets.length} className="h-3.5 w-3.5 accent-blue-500" onChange={() => {
        const updates: TruckChargePhase[] = [];
        for (const t of targets) {
          const current = phaseIndex.current.get(`${t.id}:${type.id}`);
          const include = included !== targets.length;
          if (!!current?.included === include) continue;
          const exact = phaseIndex.exact.get(`${t.id}:${type.id}`);
          updates.push({ truckId: t.id, typeId: type.id, weekStart: week, amount: current?.amount ?? type.amount, included: include, version: exact?.version ?? 0, typeVersion: type.version });
        }
        void saveCells(updates);
      }} />{scope}{lockedCount > 0 && <span className="text-amber-400" title="Charges affecting finalized statements stay unchanged">({lockedCount} finalized skipped)</span>}</label>}</th>;
    })}</tr></thead><tbody>{visible.map(t => {
      const applicable = (data.terms ?? []).filter(v => v.truckId === t.id && v.weekStart <= week).at(-1);
      const exactTerm = (data.terms ?? []).find(v => v.truckId === t.id && v.weekStart === week);
      const ownerId = t.ownerId;
      const termsMismatch = !!applicable && applicable.ownerId !== ownerId;
      const ownTruck = !!t.driverId && investors.find(i => i.id === ownerId)?.driverId === t.driverId;
      return <tr key={t.id} className="group h-8"><th scope="row" className="sticky left-0 z-10 h-8 whitespace-nowrap border-b border-r border-zinc-800 bg-zinc-950 px-3 py-0 font-medium"><Link className="text-blue-400" href={`/investors/detail?id=${ownerId}`}>{investors.find(i => i.id === ownerId)?.fullName ?? t.ownerName}</Link></th><td className="h-8 border-b border-zinc-800 px-3 py-0"><Link className="text-blue-400" href={`/trucks/detail?id=${t.id}`}>{t.unitNumber}</Link>{!t.active && <span className="ml-2 text-zinc-500">Inactive</span>}</td><td className="h-8 border-b border-zinc-800 px-3 py-0">{t.driverId ? <Link className="text-blue-400" href={`/drivers/detail?id=${t.driverId}`}>{t.driverName}</Link> : "Unassigned"}</td>{canReadTerms && <td className="h-8 border-b border-zinc-800 px-3 py-0">{ownTruck && !applicable ? <span className="text-zinc-400" title="Truck fees appear on the owner’s Driver Pay statement">Driver tariff</span> : <button className={termsMismatch ? "text-amber-400" : "text-blue-400"} title={termsMismatch ? `Agreement names ${investors.find(i => i.id === applicable.ownerId)?.fullName ?? "a previous owner"}; review terms for ${t.ownerName}` : undefined} hidden={!canReadTerms} disabled={busy || !canTerms || !validWeek} onClick={() => { setError(""); setTerm({ truckId: t.id, ownerId, weekStart: week, sharePercent: applicable?.sharePercent ?? "", version: exactTerm?.version ?? 0 }); }}>{termsMismatch ? "Review terms" : applicable ? `${applicable.sharePercent}%` : "Set terms"}</button>}</td>}{types.map(type => {
        const current = phaseIndex.current.get(`${t.id}:${type.id}`);
        const exact = phaseIndex.exact.get(`${t.id}:${type.id}`);
        const value = { truckId: t.id, typeId: type.id, weekStart: week, amount: current?.amount ?? type.amount, included: current?.included ?? false, version: exact?.version ?? 0, typeVersion: type.version };
        const options = Array.from(new Set([...type.amounts, value.amount]));
        const locked = lockedCharges.has(`${t.id}:${type.id}`);
        const disabled = busy || !canWrite || !validWeek || locked;
        return <td key={type.id} title={locked ? "Finalized statement: charge stays unchanged" : undefined} className="h-8 border-b border-zinc-800 px-3 py-0"><div className="flex h-[31px] items-center gap-3"><input aria-label={`${t.unitNumber}, ${type.name}`} type="checkbox" className="h-4 w-4 accent-blue-500" checked={value.included} disabled={disabled || (type.archived && !value.included)} onChange={e => void saveCells([{ ...value, included: e.target.checked }])} />{options.length > 1 ? <select aria-label={`${t.unitNumber}, ${type.name} amount`} className={`${controlClass} !h-7 !w-28 !py-0 font-mono`} value={value.amount} disabled={disabled || !value.included || type.archived} onChange={e => void saveCells([{ ...value, amount: e.target.value }])}>{options.map(a => <option key={a} value={a}>{money(a)}{!type.amounts.includes(a) ? " (existing)" : ""}</option>)}</select> : <span className={`min-w-20 font-mono ${value.included ? "text-zinc-200" : "text-zinc-600"}`}>{money(value.amount)}</span>}{current && <button type="button" aria-label={`${t.unitNumber}, ${type.name} history`} title="Schedule / history" disabled={busy} onClick={() => { setError(""); setPhase(value); }} className="rounded p-1 text-zinc-500 hover:text-blue-300"><History size={14} /></button>}</div></td>;
      })}</tr>;
    })}</tbody></table>{!visible.length && <p className="p-8 text-center text-sm text-zinc-500">No trucks match this view.</p>}</div>}
    {term && <Modal title={`Truck ${trucks.find(t => t.id === term.truckId)?.unitNumber} · settlement terms`} description={`Effective ${term.weekStart}`} isSaving={busy} submitLabel="Save terms" onClose={() => setTerm(null)} onSubmit={e => { e.preventDefault(); if(canTerms)void run(() => saveTruckTerm(term)); }}><div className="space-y-4">{error && <ErrorBanner message={error} />}<Field label="Investor"><select required className={controlClass} value={term.ownerId} onChange={e => setTerm({ ...term, ownerId: e.target.value })}>{investors.filter(i => !i.isCompany && (i.active || i.id === term.ownerId)).map(i => <option key={i.id} value={i.id}>{i.fullName}</option>)}</select></Field><Field label="Investor share of driver gross (%)"><input required type="number" min="0.0001" max="100" step="0.0001" value={term.sharePercent} onChange={e => setTerm({ ...term, sharePercent: e.target.value })} className={controlClass} /></Field></div></Modal>}
    {phase && <Modal title={`Truck ${truck?.unitNumber} · ${types.find(t => t.id === phase.typeId)?.name}`} description={`Effective ${phase.weekStart}`} isSaving={busy} hideSubmit={!canEditPhase} cancelLabel={canEditPhase ? "Cancel" : "Close"} submitLabel="Save truck fee" onClose={() => setPhase(null)} onSubmit={e => { e.preventDefault(); if (canEditPhase) void run(() => saveTruckCharge(phase)); }}><div className="space-y-4">{error && <ErrorBanner message={error} />}<div className="space-y-1 text-xs text-zinc-400">{data?.phases.filter(p => p.truckId === phase.truckId && p.typeId === phase.typeId).map(p => <p key={p.weekStart}>{p.weekStart} · {money(p.amount)} · {p.included ? "Included" : "Paused"}</p>)}</div><label className="flex items-center gap-2 text-sm text-zinc-300"><input type="checkbox" checked={phase.included} disabled={!canEditPhase} onChange={e => { if (canEditPhase) setPhase({ ...phase, included: e.target.checked }); }} />Apply this recurring fee</label><Field label="Weekly amount"><select className={controlClass} value={phase.amount} disabled={!canEditPhase} onChange={e => { if (canEditPhase) setPhase({ ...phase, amount: e.target.value }); }}>{Array.from(new Set([...(types.find(t => t.id === phase.typeId)?.amounts ?? []), phase.amount])).map(a => <option key={a} value={a}>{money(a)}</option>)}</select></Field></div></Modal>}
  </div>;
}
