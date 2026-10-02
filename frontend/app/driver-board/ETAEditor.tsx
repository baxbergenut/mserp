"use client";

import { useState } from "react";
import { Modal, controlClass } from "@/app/components/management/ManagementUI";
import { parseETA } from "./board";

export function ETAEditor({ name, value, onApply, onClose }: {
  name: string; value: string; onApply: (value: string) => void; onClose: () => void;
}) {
  const initial = parseETA(value);
  const [date, setDate] = useState(initial?.date ?? "");
  const [time, setTime] = useState(initial?.time ?? "");
  return <Modal title={`${name} · ETA`} description="New York time. Choose the arrival date; add a time when you know it." isSaving={false} submitLabel="Apply ETA" onClose={onClose} onSubmit={event => {
    event.preventDefault();
    const next = date + (time ? `T${time}` : "");
    if (date && parseETA(next)) onApply(next);
  }}>
    {!initial && <p className="mb-4 rounded-lg bg-amber-500/10 p-3 text-sm text-amber-200">Existing ETA: {value}. Choose a date to replace it, or cancel to keep it.</p>}
    <div className="grid gap-4 sm:grid-cols-2">
      <label className="text-sm text-zinc-300">Arrival date<input autoFocus required type="date" min="2000-01-01" max="2100-12-31" value={date} onChange={e => setDate(e.target.value)} className={`${controlClass} mt-2`} /></label>
      <label className="text-sm text-zinc-300">Arrival time (optional)<input type="time" step={60} value={time} onChange={e => setTime(e.target.value)} className={`${controlClass} mt-2`} /></label>
    </div>
    <p className="mt-3 text-xs text-zinc-500">Leave time blank for a date-only ETA. Changes use the board’s normal autosave.</p>
    <button type="button" className="mt-4 text-sm text-zinc-400 hover:text-zinc-200" onClick={() => onApply("")}>Clear ETA</button>
  </Modal>;
}
