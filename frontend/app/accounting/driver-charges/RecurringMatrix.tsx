"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { History, LoaderCircle } from "lucide-react";
import type { ChargeCell, ChargeData, ChargeSchedule, ChargeType, Driver } from "@/app/lib/types";
import { controlClass } from "@/app/components/management/ManagementUI";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";
import { recurringCell } from "./charges";

const money = (s: string) => decimalDisplay(hundredths(s), true);

export default function RecurringMatrix({ data, drivers, search, driverFilter, typeFilter, busy, pending, onSave, showHistory }: {
  data: ChargeData; drivers: Driver[]; search: string; driverFilter: string; typeFilter: string; busy: boolean;
  pending: Record<string, ChargeCell>; onSave: (input: ChargeCell, driverName: string, typeName: string) => void;
  showHistory: (schedule: ChargeSchedule) => void;
}) {
  const [week, setWeek] = useState(data.currentWeek);
  const [archived, setArchived] = useState(false);
  const schedulesByDriver = useMemo(() => {
    const grouped = new Map<string, ChargeSchedule[]>();
    for (const schedule of data.schedules) {
      const rows = grouped.get(schedule.driverId) ?? [];
      rows.push(schedule); grouped.set(schedule.driverId, rows);
    }
    return grouped;
  }, [data.schedules]);
  const visible = drivers.filter(d => (!driverFilter || driverFilter === d.id) && d.fullName.toLowerCase().includes(search.toLowerCase()));
  const types = data.types.filter(t => (!typeFilter || t.id === typeFilter) && (!t.archived || archived));
  const validWeek = week >= "2000-01-03" && week <= "2100-12-27" && new Date(`${week}T12:00:00Z`).getUTCDay() === 1;
  function save(driver: Driver, type: ChargeType, included: boolean, amount: string) {
    const { schedule } = recurringCell(schedulesByDriver.get(driver.id) ?? [], driver.id, type.id, week);
    onSave({ driverId: driver.id, typeId: type.id, weekStart: week, included, amount, scheduleId: schedule?.id ?? "", version: schedule?.version ?? 0, typeVersion: type.version }, driver.fullName, type.name);
  }
  return <div className="space-y-3">
    <div className="flex flex-wrap items-end gap-4">
      <label className="space-y-1 text-xs text-zinc-400">Effective week (Monday)<input aria-label="Matrix effective week" type="date" min="2000-01-03" max="2100-12-27" value={week} disabled={busy} onChange={e => setWeek(e.target.value)} className={`${controlClass} !w-44 block`} /></label>
      <label className="flex items-center gap-2 py-2 text-xs text-zinc-400"><input type="checkbox" checked={archived} onChange={e => setArchived(e.target.checked)} />Show archived charge types</label>
      <span className="py-2 text-xs text-zinc-500">{visible.length} drivers</span>
    </div>
    <p className="text-xs text-zinc-500">Check a fee to include a driver. Changes save immediately from the selected week; unchecking pauses it. Later scheduled changes remain in place. Each charge type controls which weeks qualify.</p>
    {!validWeek && <p role="alert" className="text-xs text-amber-300">Choose a Monday between 2000 and 2100.</p>}
    <div className="overflow-x-auto rounded-lg border border-zinc-800">
      <table className="w-full text-left text-xs text-zinc-300">
        <thead><tr className="bg-zinc-900"><th scope="col" className="sticky left-0 z-10 min-w-52 border-b border-r border-zinc-800 bg-zinc-900 p-3">Driver</th>{types.map(t => <th scope="col" key={t.id} className="min-w-56 border-b border-zinc-800 p-3 font-medium"><span>{t.name}{t.archived && " (archived)"}</span></th>)}</tr></thead>
        <tbody>{visible.map(driver => <tr key={driver.id} className="group h-8">
          <th scope="row" className="sticky left-0 z-10 border-b border-r border-zinc-800 bg-zinc-950 h-8 whitespace-nowrap px-3 py-0 font-medium"><Link className="text-blue-400" href={`/drivers/detail?id=${driver.id}`}>{driver.fullName}</Link>{!driver.active && <span className="ml-2 text-[10px] text-zinc-500">Inactive</span>}</th>
          {types.map(type => {
            const cell = recurringCell(schedulesByDriver.get(driver.id) ?? [], driver.id, type.id, week);
            const { schedule, phase } = cell;
            const saving = pending[driver.id];
            const change = saving?.typeId === type.id && saving.weekStart === week ? saving : null;
            const included = change?.included ?? cell.included;
            const amount = change?.amount ?? phase?.amount ?? type.amount;
            const options = type.amounts.includes(amount) ? type.amounts : [...type.amounts, amount];
            return <td key={type.id} className="h-8 border-b border-zinc-800 px-3 py-0"><div className="flex h-[31px] items-center gap-3">
              <input aria-label={`${driver.fullName}, ${type.name}`} type="checkbox" className="h-4 w-4 accent-blue-500" checked={included} disabled={busy || !!saving || !validWeek || ((!driver.active || type.archived) && !included)} onChange={e => save(driver, type, e.target.checked, amount)} />
              {options.length > 1 ? <select aria-label={`${driver.fullName}, ${type.name} amount`} className={`${controlClass} !h-7 !w-28 !py-0 font-mono`} value={amount} disabled={busy || !!saving || !validWeek || !included || !driver.active} onChange={e => save(driver, type, true, e.target.value)}>{options.map(option => <option key={option} value={option}>{money(option)}{!type.amounts.includes(option) ? " (existing)" : ""}</option>)}</select> : <span className={`min-w-20 font-mono ${included ? "text-zinc-200" : "text-zinc-600"}`}>{money(amount)}</span>}
              {change ? <LoaderCircle size={14} role="status" aria-label={`Saving ${driver.fullName}, ${type.name}`} className="shrink-0 animate-spin text-blue-400" /> : schedule && <button type="button" aria-label={`${driver.fullName}, ${type.name} history`} title="Schedule / history" disabled={busy || !!saving} onClick={() => showHistory(schedule)} className="rounded p-1 text-zinc-500 hover:text-blue-300"><History size={14} /></button>}
            </div></td>;
          })}
        </tr>)}</tbody>
      </table>
      {!visible.length && <p className="p-8 text-center text-sm text-zinc-500">No drivers match this search.</p>}
      {!types.length && <p className="p-6 text-sm text-zinc-500">Create a charge type to add a column, or change the charge type filter.</p>}
    </div>
  </div>;
}
