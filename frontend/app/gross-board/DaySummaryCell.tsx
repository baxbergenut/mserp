"use client";

import { memo, useCallback, useId, useState } from "react";
import { createPortal } from "react-dom";
import { Pencil, Plus } from "lucide-react";
import type { GrossBoardEntry } from "@/app/lib/types";
import { Modal } from "@/app/components/management/ManagementUI";
import { DayCell } from "./DayCell";
import { additionalLoadEntries, decimalDisplay, emptyEntry, mismatch, dayStatuses, totals, validDecimal } from "./board";

type Props = {
  entries: GrossBoardEntry[];
  driverId: string;
  driverName: string;
  date: string;
  onChange: (driverId: string, date: string, slot: number, update: (current: GrossBoardEntry) => GrossBoardEntry) => void;
};

export const DaySummaryCell = memo(function DaySummaryCell({ entries, driverId, driverName, date, onChange }: Props) {
  const [draft, setDraft] = useState<GrossBoardEntry[] | null>(null);
  const [hover, setHover] = useState<{ top: number; left: number } | null>(null);
  const tooltipId = useId();
  const visible = entries.filter(entry => !entry.deleted);
  const displayed = visible.length ? visible : [emptyEntry(driverId, date)];
  const sum = totals(displayed);
  const confirmed = displayed.every(entry => entry.loadRecordId !== null);
  const needsReview = displayed.some(entry => entry.loadRecordId !== null && !entry.acceptSystemValues && (mismatch(entry.enteredOriginalRate, entry.originalRate) || mismatch(entry.enteredMiles, entry.miles)));
  const label = `${driverName}, ${date}`;
  const openEditor = (add = false) => {
    setHover(null);
    const initial = entries.length ? entries : displayed;
    setDraft(add ? [...initial.filter(entry => !entry.deleted), ...additionalLoadEntries(initial, driverId, date)] : initial);
  };
  const editDraft = useCallback((_id: string, _date: string, slot: number, update: (current: GrossBoardEntry) => GrossBoardEntry) => {
    setDraft(current => current?.map(entry => entry.slot === slot ? update(entry) : entry) ?? null);
  }, []);
  const addDraft = useCallback((id: string, day: string) => {
    setDraft(current => {
      if (!current) return current;
      const added = additionalLoadEntries(current, id, day);
      return [...current.filter(entry => !added.some(next => next.slot === entry.slot)), ...added].sort((a, b) => a.slot - b.slot);
    });
  }, []);
  const invalid = draft?.some(entry => !validDecimal(entry.originalRate) || !validDecimal(entry.driverRate) || !validDecimal(entry.miles, true));
  const showBreakdown = (element: HTMLElement) => {
    const rect = element.getBoundingClientRect();
    setHover({ top: Math.min(rect.bottom + 6, window.innerHeight - 240), left: Math.max(8, Math.min(rect.left, window.innerWidth - 488)) });
  };
  const field = "flex h-8 w-full items-center justify-center border-b border-zinc-800/70 px-2 font-mono text-xs text-zinc-300 hover:bg-blue-500/10 focus-visible:outline-2 focus-visible:outline-blue-500";

  return <>
    {displayed.length === 1 ? <DayCell entry={displayed[0]} driverName={driverName} disabled={false} onChange={onChange} onAdd={() => openEditor(true)} /> : <td className="border-b border-r border-zinc-800/70 p-0 align-top">
      <div className="flex h-6 items-center justify-between border-b border-zinc-800/70 px-1 text-[10px] text-zinc-500"><span>{displayed.length} loads</span><div className="flex gap-1">
        <button type="button" aria-label={`${label}, edit loads`} className="rounded p-0.5 hover:bg-zinc-700 hover:text-blue-300" onClick={() => openEditor()}><Pencil className="h-3 w-3" /></button>
        {displayed.length < 100 && <button type="button" aria-label={`${label}, add another load`} className="rounded p-0.5 hover:bg-zinc-700 hover:text-blue-300" onClick={() => openEditor(true)}><Plus className="h-3 w-3" /></button>}
      </div></div>
      <button type="button" aria-label={`${label}, load numbers and details`} aria-describedby={hover ? tooltipId : undefined}
        onMouseEnter={event => showBreakdown(event.currentTarget)} onMouseLeave={() => setHover(null)} onFocus={event => showBreakdown(event.currentTarget)} onBlur={() => setHover(null)}
        onClick={() => openEditor()} className={`${field} !justify-start !font-sans ${confirmed ? "bg-emerald-500/15 !text-emerald-300" : "bg-amber-500/10 !text-amber-200"}`}>
        <span className="truncate">{displayed.map(entry => entry.dayStatus || entry.loadNumber || "Empty").join(" / ")}</span>
      </button>
      {([["original rate", sum.original], ["driver rate", sum.driver], ["miles", sum.miles]] as const).map(([name, value]) => <button key={name} type="button" aria-label={`${label}, total ${name}`} className={`${field} ${needsReview && name !== "driver rate" ? "!bg-red-500/15 !text-red-300" : ""}`} onClick={() => openEditor()}>{decimalDisplay(value)}</button>)}
    </td>}
    {hover && createPortal(<div id={tooltipId} role="tooltip" style={hover} className="pointer-events-none fixed z-50 w-[480px] rounded-lg border border-zinc-700 bg-zinc-900 p-3 text-xs text-zinc-300 shadow-xl">
      <div className="mb-2 flex justify-between"><span>{driverName} · {date}</span><span className="text-zinc-500">Click to edit</span></div>
      <div className="max-h-40 overflow-hidden"><table className="w-full text-left"><thead className="text-[10px] text-zinc-500"><tr><th className="h-7 font-medium">Load</th><th className="text-right font-medium">Original</th><th className="text-right font-medium">Driver</th><th className="text-right font-medium">Miles</th></tr></thead><tbody>{displayed.map(entry => <tr key={entry.slot} className="h-7 border-t border-zinc-800"><td className={entry.dayStatus ? dayStatuses.find(status => status.value === entry.dayStatus)?.color : entry.loadRecordId === null ? "text-amber-300" : "text-emerald-300"}>{entry.dayStatus || entry.loadNumber || "Empty"}</td>{[entry.originalRate, entry.driverRate, entry.miles].map((value, index) => <td key={index} className="text-right font-mono">{value || "—"}</td>)}</tr>)}</tbody></table></div>
      {displayed.length > 4 && <p className="mt-2 text-[10px] text-zinc-500">Open to view all {displayed.length} loads.</p>}
    </div>, document.body)}
    {draft && <Modal title="Daily loads" description={`${driverName} · ${date}. Original rates and miles stay linked to system loads.`} isSaving={false} submitLabel="Apply changes" onClose={() => setDraft(null)} onSubmit={event => {
      event.preventDefault();
      if (invalid) return;
      for (const entry of draft) {
        const previous = entries.find(saved => saved.slot === entry.slot);
        if (JSON.stringify(previous) !== JSON.stringify(entry)) onChange(driverId, date, entry.slot, current => ({ ...entry, version: current.version }));
      }
      setDraft(null);
    }} wide>
      <div className="overflow-x-auto pb-32"><table className="w-full table-fixed border-collapse"><colgroup><col style={{ width: 80 }} />{draft.filter(entry => !entry.deleted).map(entry => <col key={entry.slot} style={{ width: 220 }} />)}</colgroup><tbody><tr>
        <td className="p-0 align-top text-[11px] text-zinc-500"><div className="h-6" />{["Load #", "Original", "Driver", "Miles"].map(name => <div key={name} className="flex h-8 items-center">{name}</div>)}</td>
        {draft.filter(entry => !entry.deleted).map(entry => <DayCell key={entry.slot} entry={entry} driverName={driverName} disabled={false} onChange={editDraft} />)}
      </tr></tbody></table></div>
      <button type="button" disabled={draft.filter(entry => !entry.deleted).length >= 100} onClick={() => addDraft(driverId, date)} className="inline-flex items-center gap-1.5 rounded border border-zinc-700 px-3 py-2 text-xs text-blue-300"><Plus className="h-3 w-3" />Add load</button>
      {invalid && <p role="alert" className="mt-3 text-xs text-red-300">Correct invalid rates or mileage before applying.</p>}
    </Modal>}
  </>;
}, (previous, next) => previous.driverId === next.driverId && previous.driverName === next.driverName
  && previous.date === next.date && previous.onChange === next.onChange
  && previous.entries.length === next.entries.length
  && previous.entries.every((entry, index) => entry === next.entries[index] || JSON.stringify(entry) === JSON.stringify(next.entries[index])));
