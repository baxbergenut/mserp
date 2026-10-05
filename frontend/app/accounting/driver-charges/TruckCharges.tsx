"use client";

import { useEffect, useState } from "react";
import { fetchTruckCharges, fetchTrucks, fetchInvestors, saveTruckCharge, saveTruckTerm } from "@/app/lib/api";
import type { ChargeData, Investor, Truck, TruckChargeData, TruckChargePhase, TruckTerm } from "@/app/lib/types";
import { useViewState } from "@/app/lib/viewMemory";
import { Modal, Field, controlClass, ErrorBanner } from "@/app/components/management/ManagementUI";
import { payButtonClass } from "../driver-pay/DriverCard";
import { recurringCell, validChargeWeek } from "./charges";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";
import EffectiveWeekPicker from "./EffectiveWeekPicker";

export function TruckCharges({ charges, onChanged }: { charges: ChargeData; onChanged: () => Promise<void> }) {
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
  async function run(action: () => Promise<void>) { setBusy(true); setError(""); try { await action(); await reload(); await onChanged(); setTerm(null); setPhase(null); } catch (e) { setError(e instanceof Error ? e.message : "Unable to save truck charges"); } finally { setBusy(false); } }
  const validWeek = validChargeWeek(week);
  const visible = trucks.filter(t => {
    if (!data?.eligibleTruckIds.includes(t.id)) return false;
    return (!t.isCompanyOwned || data?.terms.some(x => x.truckId === t.id)) && `${t.unitNumber} ${t.ownerName} ${t.driverName ?? ""}`.toLowerCase().includes(search.toLowerCase());
  }).sort((a,b) => a.ownerName.localeCompare(b.ownerName) || a.unitNumber.localeCompare(b.unitNumber));
  const types = charges.types.filter(t => !t.archived || data?.phases.some(p => p.typeId === t.id && p.included));
  const money = (s: string) => decimalDisplay(hundredths(s), true);
  const truck = trucks.find(t => t.id === phase?.truckId);
  const movable = phase ? charges.schedules.filter(s => s.kind === "recurring" && s.typeId === phase.typeId && s.driverId === truck?.driverId && recurringCell(charges.schedules, s.driverId, phase.typeId, week).schedule?.id === s.id) : [];
  return <div className="space-y-3">
    {!term && !phase && error && <ErrorBanner message={error} />}
    <div className="flex flex-wrap items-center gap-3"><EffectiveWeekPicker week={week} currentWeek={charges.currentWeek} disabled={busy} onChange={setWeek} /><input aria-label="Search investor trucks" value={search} onChange={e => setSearch(e.target.value)} placeholder="Investor, truck or driver…" className={`${controlClass} !w-64`} /><button className={payButtonClass} disabled={busy} onClick={() => void run(async () => {})}>Reload</button></div>
    <p className="text-xs text-zinc-500">Fees belong to investor trucks and stay with them when drivers change. Owner-operators and trucks driven by their owner are managed in Driver charges. Set an investor share before calculating Investor Pay. Later dated changes stay in place.</p>
    {!validWeek && <p role="alert" className="text-xs text-amber-300">Choose a valid Monday.</p>}
    {!data ? <p role="status" className="p-8 text-sm text-zinc-500">Loading truck charges…</p> : <div className="overflow-x-auto rounded-lg border border-zinc-800"><table className="w-full text-left text-xs text-zinc-300"><thead className="bg-zinc-900"><tr>{["Investor / truck", "Current driver", "Investor share", ...types.map(t => t.name)].map((label,i) => <th key={i} className="min-w-44 border-b border-zinc-800 px-3 py-2">{label}</th>)}</tr></thead><tbody>{visible.map(t => {
      const applicable = data.terms.filter(v => v.truckId === t.id && v.weekStart <= week).at(-1);
      const exact = data.terms.find(v => v.truckId === t.id && v.weekStart === week);
      return <tr key={t.id} className="h-8"><th className="border-b border-zinc-800 px-3 font-medium"><span>{investors.find(i => i.id === applicable?.ownerId)?.fullName ?? t.ownerName}</span><span className="ml-2 text-blue-400">{t.unitNumber}</span></th><td className="border-b border-zinc-800 px-3">{t.driverName ?? "Unassigned"}</td><td className="border-b border-zinc-800 px-3"><button className="text-blue-400" disabled={busy || !validWeek} onClick={() => { setError(""); setTerm({ truckId: t.id, ownerId: applicable?.ownerId ?? t.ownerId, weekStart: week, sharePercent: applicable?.sharePercent ?? "", version: exact?.version ?? 0 }); }}>{applicable ? `${applicable.sharePercent}%` : "Set terms"}</button></td>{types.map(type => {
        const current = data.phases.filter(p => p.truckId === t.id && p.typeId === type.id && p.weekStart <= week).at(-1);
        const exact = data.phases.find(p => p.truckId === t.id && p.typeId === type.id && p.weekStart === week);
        return <td key={type.id} className="border-b border-zinc-800 px-3"><button aria-label={`${t.unitNumber}, ${type.name}`} title={!applicable ? "Set this truck’s terms first" : undefined} disabled={busy || !validWeek || !applicable} className={current?.included ? "text-zinc-200" : "text-zinc-500"} onClick={() => { setError(""); setPhase({ truckId: t.id, typeId: type.id, weekStart: week, amount: current?.amount ?? type.amount, included: current?.included ?? true, version: exact?.version ?? 0, typeVersion: type.version }); }}>{current?.included ? money(current.amount) : "Add / paused"}</button></td>;
      })}</tr>;
    })}</tbody></table>{!visible.length && <p className="p-8 text-center text-sm text-zinc-500">No investor trucks match this view.</p>}</div>}
    {term && <Modal title={`Truck ${trucks.find(t => t.id === term.truckId)?.unitNumber} · settlement terms`} description={`Effective ${term.weekStart}. Historical terms must identify the actual owner and agreed share for that period.`} isSaving={busy} submitLabel="Save terms" onClose={() => setTerm(null)} onSubmit={e => { e.preventDefault(); void run(() => saveTruckTerm(term)); }}><div className="space-y-4">{error && <ErrorBanner message={error} />}<Field label="Investor"><select required className={controlClass} value={term.ownerId} onChange={e => setTerm({ ...term, ownerId: e.target.value })}>{investors.filter(i => !i.isCompany && (i.active || i.id === term.ownerId)).map(i => <option key={i.id} value={i.id}>{i.fullName}</option>)}</select></Field><Field label="Investor share of Gross Board gross (%)"><input required type="number" min="0.0001" max="100" step="0.0001" value={term.sharePercent} onChange={e => setTerm({ ...term, sharePercent: e.target.value })} className={controlClass} /></Field><p className="text-xs text-zinc-500">For example, an agreed 12% company fee leaves an 88% investor share before driver earnings and truck expenses. No additional dispatch fee is deducted.</p></div></Modal>}
    {phase && <Modal title={`Truck ${truck?.unitNumber} · ${types.find(t => t.id === phase.typeId)?.name}`} description={`Effective ${phase.weekStart}. Eligibility follows the shared charge type.`} isSaving={busy} submitLabel="Save truck fee" onClose={() => setPhase(null)} onSubmit={e => { e.preventDefault(); void run(() => saveTruckCharge(phase)); }}><div className="space-y-4">{error && <ErrorBanner message={error} />}<label className="flex items-center gap-2 text-sm text-zinc-300"><input type="checkbox" checked={phase.included} onChange={e => setPhase({ ...phase, included: e.target.checked })} />Apply this recurring fee</label><Field label="Weekly amount"><select className={controlClass} value={phase.amount} onChange={e => setPhase({ ...phase, amount: e.target.value })}>{Array.from(new Set([...(types.find(t => t.id === phase.typeId)?.amounts ?? []), phase.amount])).map(a => <option key={a} value={a}>{money(a)}</option>)}</select></Field>{movable.length > 0 && <Field label="Existing driver assignment"><select className={controlClass} value={phase.moveScheduleId ?? ""} onChange={e => { const s = movable.find(s => s.id === e.target.value); setPhase({ ...phase, moveScheduleId: s?.id, moveScheduleVersion: s?.version }); }}><option value="">Keep driver assignment (personal charge)</option>{movable.map(s => <option key={s.id} value={s.id}>Move {s.driverName}’s fee to this truck</option>)}</select><p className="mt-2 text-xs text-amber-300">Moving stops the driver’s recurring fee from this week. Keep both only if the driver fee is a separate personal charge.</p></Field>}</div></Modal>}
  </div>;
}
