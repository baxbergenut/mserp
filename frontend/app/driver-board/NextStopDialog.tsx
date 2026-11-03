"use client";

import { useState } from "react";
import type { BoardLoads } from "@/app/lib/types";
import { Modal } from "@/app/components/management/ManagementUI";
import { nextDeliveryStops } from "./board";

export function NextStopDialog({ name, status, loads, onApply, onClose }: {
  name: string; status: string; loads: BoardLoads;
  onApply: (stopKey: string) => Promise<unknown>; onClose: () => void;
}) {
  const stops = nextDeliveryStops(status, loads);
  const [selected, setSelected] = useState(stops[0]?.key ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  return <Modal title={`Next stop · ${name}`} isSaving={busy} submitLabel="Go to next stop" submitDisabled={!selected}
    onClose={onClose} onSubmit={async event => {
      event.preventDefault(); if (busy || !selected) return;
      setBusy(true); setError("");
      try { await onApply(selected); onClose(); }
      catch (error) { setError(error instanceof Error ? error.message : "Could not change the stop."); }
      finally { setBusy(false); }
    }}>
    <fieldset className="space-y-2" disabled={busy}>
      <legend className="mb-3 text-sm text-zinc-400">Choose the next delivery for {loads.current?.number}.</legend>
      {stops.map(stop => <label key={stop.key} className="flex cursor-pointer items-start gap-3 rounded-lg border border-zinc-700 p-3 hover:bg-zinc-800">
        <input type="radio" name="next-stop" value={stop.key} checked={selected === stop.key} onChange={() => setSelected(stop.key)} className="mt-0.5" />
        <span className="min-w-0 text-sm text-zinc-200">{stop.location || "Delivery address unavailable"}{stop.schedule?.label && <span className="mt-1 block text-xs text-zinc-400">{stop.schedule.label}</span>}</span>
      </label>)}
    </fieldset>
    {error && <p role="alert" className="mt-3 text-sm text-red-300">{error}</p>}
  </Modal>;
}
