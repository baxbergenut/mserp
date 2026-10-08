"use client";

import Link from "next/link";
import { History } from "lucide-react";
import { usePermissions } from "@/app/lib/access";
import { useEffect, useState } from "react";
import { fetchTruckCharges, fetchTrucks, fetchInvestors, saveTruckCharge, saveTruckTerm } from "@/app/lib/api";
import type { ChargeData, Investor, Truck, TruckChargeData, TruckChargePhase, TruckTerm } from "@/app/lib/types";
import { useViewState } from "@/app/lib/viewMemory";
import { Modal, Field, controlClass, ErrorBanner } from "@/app/components/management/ManagementUI";
import { payButtonClass } from "../driver-pay/DriverCard";
import { recurringCell, validChargeWeek } from "./charges";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";
import EffectiveWeekPicker from "./EffectiveWeekPicker";

export function TruckCharges({ charges, onChanged, onCountChange }: { charges: ChargeData; onCountChange: (count: number) => void; onChanged: () => Promise<void> }) {
  const canWrite = usePermissions().includes("charges.write");
  const [archived, setArchived] = useViewState("truckCharges:archived", false);
  const [typeFilter, setTypeFilter] = useViewState("truckCharges:typeFilter", "");
  const [data, setData] = useState<TruckChargeData | null>(null);
  const [trucks, setTrucks] = useState<Truck[]>([]);
  const [investors, setInvestors] = useState<Investor[]>([]);
  const [rememberedWeek, setWeek] = useViewState("truckCharges:week", charges.currentWeek);
  const week = validChargeWeek(rememberedWeek) ? rememberedWeek : charges.currentWeek;
  const [search, setSearch] = useViewState("truckCharges:search", "");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [term, setTerm] = useState<TruckTerm | null>(null);
  const [phase, setPhase] = useState<TruckChargePhase | null>(null);
  async function reload() {
    const [d, t, i] = await Promise.all([fetchTruckCharges(), fetchTrucks(), fetchInvestors()]);
    setData(d); setTrucks(t); setInvestors(i);
  }
  useEffect(() => {
    let cancelled = false;
    Promise.all([fetchTruckCharges(), fetchTrucks(), fetchInvestors()]).then(([d, t, i]) => { if (!cancelled) { setData(d); setTrucks(t); setInvestors(i); } }).catch(e => { if (!cancelled) setError(e.message); });
    return () => { cancelled = true; };
  }, []);
  async function run(action: () => Promise<void>) { setBusy(true); setError(""); try { await action(); await reload(); await onChanged(); setTerm(null); setPhase(null); } catch (e) { setError(e instanceof Error ? e.message : "Unable to save truck charges"); try { await reload(); await onChanged(); } catch { /* Keep the original save error visible. */ } } finally { setBusy(false); } }
  function saveCells(updates: TruckChargePhase[]) {
    setData(current => current && ({ ...current, phases: [...current.phases.filter(p => !updates.some(v => v.truckId === p.truckId && v.typeId === p.typeId && v.weekStart === p.weekStart)), ...updates].sort((a,b) => a.weekStart.localeCompare(b.weekStart)) }));
    void run(async () => { for (const value of updates) await saveTruckCharge(value); });
  }
  const validWeek = validChargeWeek(week);
  const visible = trucks.filter(t => {
    if (!t.active || t.isCompanyOwned || !data?.eligibleTruckIds.includes(t.id)) return false;
    return (!t.isCompanyOwned || data?.terms.some(x => x.truckId === t.id)) && `${t.unitNumber} ${t.ownerName} ${t.driverName ?? ""}`.toLowerCase().includes(search.toLowerCase());
  }).sort((a,b) => a.ownerName.localeCompare(b.ownerName) || a.unitNumber.localeCompare(b.unitNumber));
  useEffect(() => { onCountChange(visible.length); }, [visible.length, onCountChange]);
  const availableTypes = charges.types.filter(t => !t.archived || archived);
  const effectiveType = availableTypes.some(t => t.id === typeFilter) ? typeFilter : "";
  const types = availableTypes.filter(t => !effectiveType || t.id === effectiveType);
  const money = (s: string) => decimalDisplay(hundredths(s), true);
  const truck = trucks.find(t => t.id === phase?.truckId);
  const movable = phase ? charges.schedules.filter(s => s.kind === "recurring" && s.typeId === phase.typeId && s.driverId === truck?.driverId && recurringCell(charges.schedules, s.driverId, phase.typeId, week).schedule?.id === s.id) : [];
  return <div className="space-y-3">
    {!term && !phase && error && <ErrorBanner message={error} />}
    <div className="overflow-x-auto"><div className="flex min-w-max items-center gap-3"><EffectiveWeekPicker week={week} currentWeek={charges.currentWeek} disabled={busy} onChange={setWeek} /><input aria-label="Search investor trucks" value={search} onChange={e => setSearch(e.target.value)} placeholder="Investor, truck or driver…" className={`${controlClass} !w-48`} /><select aria-label="Filter truck charge type" className={`${controlClass} !w-40`} value={effectiveType} onChange={e => setTypeFilter(e.target.value)}><option value="">All charge types</option>{availableTypes.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}</select><label className="flex items-center gap-2 text-xs text-zinc-400"><input aria-label="Show archived charge types" type="checkbox" checked={archived} onChange={e => setArchived(e.target.checked)} />Archived types</label><button className={payButtonClass} disabled={busy} onClick={() => void run(async () => {})}>Reload</button></div></div>
    {!validWeek && <p role="alert" className="text-xs text-amber-300">Choose a valid Monday.</p>}
    {!data ? <p role="status" className="p-8 text-sm text-zinc-500">Loading truck charges…</p> : <div data-scroll-key="truck-recurring-charges" className="overflow-x-auto rounded-lg border border-zinc-800"><table className="w-full text-left text-xs text-zinc-300"><thead><tr className="bg-zinc-900"><th className="sticky left-0 z-10 min-w-52 border-b border-r border-zinc-800 bg-zinc-900 p-3">Investor</th>{["Truck", "Current driver", "Investor share"].map(label => <th key={label} className="min-w-36 border-b border-zinc-800 p-3">{label}</th>)}{types.map(type => {
      const targets = visible.filter(t => data.terms.some(v => v.truckId === t.id && v.weekStart <= week));
      const included = targets.filter(t => data.phases.filter(p => p.truckId === t.id && p.typeId === type.id && p.weekStart <= week).at(-1)?.included).length;
      const scope = search ? "visible trucks" : "all trucks";
      return <th key={type.id} scope="col" aria-label={`${type.name}${type.archived ? " (archived)" : ""}`} className="min-w-56 border-b border-zinc-800 p-3 font-medium">{type.name}{type.archived && " (archived)"}{!type.archived && <label className="mt-1 flex w-fit items-center gap-1.5 text-[11px] font-normal text-zinc-400"><input type="checkbox" aria-label={`${type.name}: select ${scope}`} checked={targets.length > 0 && included === targets.length} ref={node => { if (node) node.indeterminate = included > 0 && included < targets.length; }} disabled={busy || !canWrite || !validWeek || !targets.length} className="h-3.5 w-3.5 accent-blue-500" onChange={() => {
        const updates: TruckChargePhase[] = [];
        for (const t of targets) {
          const current = data.phases.filter(p => p.truckId === t.id && p.typeId === type.id && p.weekStart <= week).at(-1);
          const include = included === 0;
          if (!!current?.included === include) continue;
          const exact = data.phases.find(p => p.truckId === t.id && p.typeId === type.id && p.weekStart === week);
          updates.push({ truckId: t.id, typeId: type.id, weekStart: week, amount: current?.amount ?? type.amount, included: include, version: exact?.version ?? 0, typeVersion: type.version });
        }
        saveCells(updates);
      }} />{scope}</label>}</th>;
    })}</tr></thead><tbody>{visible.map(t => {
      const applicable = data.terms.filter(v => v.truckId === t.id && v.weekStart <= week).at(-1);
      const exactTerm = data.terms.find(v => v.truckId === t.id && v.weekStart === week);
      const ownerId = applicable?.ownerId ?? t.ownerId;
      return <tr key={t.id} className="group h-8"><th scope="row" className="sticky left-0 z-10 h-8 whitespace-nowrap border-b border-r border-zinc-800 bg-zinc-950 px-3 py-0 font-medium"><Link className="text-blue-400" href={`/investors/detail?id=${ownerId}`}>{investors.find(i => i.id === ownerId)?.fullName ?? t.ownerName}</Link></th><td className="h-8 border-b border-zinc-800 px-3 py-0"><Link className="text-blue-400" href={`/trucks/detail?id=${t.id}`}>{t.unitNumber}</Link></td><td className="h-8 border-b border-zinc-800 px-3 py-0">{t.driverId ? <Link className="text-blue-400" href={`/drivers/detail?id=${t.driverId}`}>{t.driverName}</Link> : "Unassigned"}</td><td className="h-8 border-b border-zinc-800 px-3 py-0"><button className="text-blue-400" disabled={busy || !canWrite || !validWeek} onClick={() => { setError(""); setTerm({ truckId: t.id, ownerId, weekStart: week, sharePercent: applicable?.sharePercent ?? "", version: exactTerm?.version ?? 0 }); }}>{applicable ? `${applicable.sharePercent}%` : "Set terms"}</button></td>{types.map(type => {
        const current = data.phases.filter(p => p.truckId === t.id && p.typeId === type.id && p.weekStart <= week).at(-1);
        const exact = data.phases.find(p => p.truckId === t.id && p.typeId === type.id && p.weekStart === week);
        const value = { truckId: t.id, typeId: type.id, weekStart: week, amount: current?.amount ?? type.amount, included: current?.included ?? false, version: exact?.version ?? 0, typeVersion: type.version };
        const options = Array.from(new Set([...type.amounts, value.amount]));
        const disabled = busy || !canWrite || !validWeek || !applicable;
        return <td key={type.id} className="h-8 border-b border-zinc-800 px-3 py-0"><div className="flex h-[31px] items-center gap-3"><input aria-label={`${t.unitNumber}, ${type.name}`} type="checkbox" className="h-4 w-4 accent-blue-500" checked={value.included} disabled={disabled || (type.archived && !value.included)} title={!applicable ? "Set terms first" : undefined} onChange={e => saveCells([{ ...value, included: e.target.checked }])} />{options.length > 1 ? <select aria-label={`${t.unitNumber}, ${type.name} amount`} className={`${controlClass} !h-7 !w-28 !py-0 font-mono`} value={value.amount} disabled={disabled || !value.included || type.archived} onChange={e => saveCells([{ ...value, amount: e.target.value }])}>{options.map(a => <option key={a} value={a}>{money(a)}{!type.amounts.includes(a) ? " (existing)" : ""}</option>)}</select> : <span className={`min-w-20 font-mono ${value.included ? "text-zinc-200" : "text-zinc-600"}`}>{money(value.amount)}</span>}{current && <button type="button" aria-label={`${t.unitNumber}, ${type.name} history`} title="Schedule / history" disabled={busy} onClick={() => { setError(""); setPhase(value); }} className="rounded p-1 text-zinc-500 hover:text-blue-300"><History size={14} /></button>}</div></td>;
      })}</tr>;
    })}</tbody></table>{!visible.length && <p className="p-8 text-center text-sm text-zinc-500">No investor trucks match this view.</p>}</div>}
    {term && <Modal title={`Truck ${trucks.find(t => t.id === term.truckId)?.unitNumber} · settlement terms`} description={`Effective ${term.weekStart}`} isSaving={busy} submitLabel="Save terms" onClose={() => setTerm(null)} onSubmit={e => { e.preventDefault(); void run(() => saveTruckTerm(term)); }}><div className="space-y-4">{error && <ErrorBanner message={error} />}<Field label="Investor"><select required className={controlClass} value={term.ownerId} onChange={e => setTerm({ ...term, ownerId: e.target.value })}>{investors.filter(i => !i.isCompany && (i.active || i.id === term.ownerId)).map(i => <option key={i.id} value={i.id}>{i.fullName}</option>)}</select></Field><Field label="Investor share of driver gross (%)"><input required type="number" min="0.0001" max="100" step="0.0001" value={term.sharePercent} onChange={e => setTerm({ ...term, sharePercent: e.target.value })} className={controlClass} /></Field></div></Modal>}
    {phase && <Modal title={`Truck ${truck?.unitNumber} · ${types.find(t => t.id === phase.typeId)?.name}`} description={`Effective ${phase.weekStart}`} isSaving={busy || !canWrite} submitLabel="Save truck fee" onClose={() => setPhase(null)} onSubmit={e => { e.preventDefault(); void run(() => saveTruckCharge(phase)); }}><div className="space-y-4">{error && <ErrorBanner message={error} />}<div className="space-y-1 text-xs text-zinc-400">{data?.phases.filter(p => p.truckId === phase.truckId && p.typeId === phase.typeId).map(p => <p key={p.weekStart}>{p.weekStart} · {money(p.amount)} · {p.included ? "Included" : "Paused"}</p>)}</div><label className="flex items-center gap-2 text-sm text-zinc-300"><input type="checkbox" checked={phase.included} onChange={e => setPhase({ ...phase, included: e.target.checked })} />Apply this recurring fee</label><Field label="Weekly amount"><select className={controlClass} value={phase.amount} onChange={e => setPhase({ ...phase, amount: e.target.value })}>{Array.from(new Set([...(types.find(t => t.id === phase.typeId)?.amounts ?? []), phase.amount])).map(a => <option key={a} value={a}>{money(a)}</option>)}</select></Field>{movable.length > 0 && <Field label="Existing driver assignment"><select className={controlClass} value={phase.moveScheduleId ?? ""} onChange={e => { const s = movable.find(s => s.id === e.target.value); setPhase({ ...phase, moveScheduleId: s?.id, moveScheduleVersion: s?.version }); }}><option value="">Keep driver assignment (personal charge)</option>{movable.map(s => <option key={s.id} value={s.id}>Move {s.driverName}’s fee to this truck</option>)}</select><p className="mt-2 text-xs text-amber-300">Moving stops the driver’s recurring fee from this week. Keep both only if the driver fee is a separate personal charge.</p></Field>}</div></Modal>}
  </div>;
}
