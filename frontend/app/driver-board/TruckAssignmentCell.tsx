"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { ChevronDown } from "lucide-react";
import { createPortal } from "react-dom";
import { assignDriverTruckToday, fetchDriverTruckAssignment } from "@/app/lib/api";
import type { DriverTruckAssignment, TruckAssignmentChoice } from "@/app/lib/types";

const statusNames: Record<string, string> = { shop: "Shop", out_of_service: "OOS", terminated: "Terminated" };

export function TruckAssignmentCell({ id, name, truckId, unit, choices, disabled, onFocus, onSaved, onError }: {
  id: string; name: string; truckId: string; unit: string; choices: TruckAssignmentChoice[] | null;
  disabled: boolean; onFocus: () => void; onSaved: (unit: string) => void; onError: (message: string) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState<{ view: DriverTruckAssignment; truck: TruckAssignmentChoice } | null>(null);
  const [confirmationError, setConfirmationError] = useState("");
  const dialog = useRef<HTMLDialogElement>(null);
  const submitting = useRef(false);
  useEffect(() => {
    if (pending && dialog.current && !dialog.current.open) dialog.current.showModal();
  }, [pending]);
  function cancel() {
    if (submitting.current) return;
    dialog.current?.close();
    setPending(null);
  }
  async function commit(view: DriverTruckAssignment, truck: TruckAssignmentChoice, confirmReassign: boolean) {
    await assignDriverTruckToday(id, { truckId: truck.id, revision: view.revision, confirmReassign });
    dialog.current?.close();
    setPending(null);
    onSaved(truck.unit);
  }
  async function confirm() {
    if (!pending || submitting.current) return;
    submitting.current = true; setBusy(true); setConfirmationError("");
    try { await commit(pending.view, pending.truck, true); }
    catch (err) { setConfirmationError(err instanceof Error ? err.message : "Could not reassign truck"); }
    finally { submitting.current = false; setBusy(false); }
  }
  async function choose(nextId: string) {
    if (!nextId || nextId === truckId || submitting.current) return;
    submitting.current = true; setBusy(true); onError("");
    try {
      const view = await fetchDriverTruckAssignment(id);
      if (view.truckId !== truckId) throw new Error("This driver's truck assignment changed. Refresh the board and try again.");
      const truck = view.choices.find(choice => choice.id === nextId);
      if (!view.active || !truck?.active || !["ready", "in_use", "shop"].includes(truck.status)) throw new Error("This truck is no longer available for assignment. Choose another truck.");
      if (truck.driverId !== view.driverId && truck.activeLoad) throw new Error(`Truck ${truck.unit} cannot be reassigned: ${truck.driverName} has active load ${truck.activeLoad}. Complete or transfer the load first.`);
      if (truck.driverId) {
        setConfirmationError(""); setPending({ view, truck });
        return;
      }
      await commit(view, truck, false);
    } catch (err) { onError(err instanceof Error ? err.message : "Could not assign truck"); }
    finally { submitting.current = false; setBusy(false); }
  }
  return <><div className="flex items-center justify-between gap-0.5">
    {truckId ? <Link href={`/trucks/detail?id=${truckId}`} className="min-w-0 truncate hover:text-blue-300">{unit}</Link> : <span>—</span>}
    <div className="relative shrink-0">
      <select aria-label={`${name} · Assign truck`} title={busy ? "Saving assignment…" : "Assign truck effective today (New York)"}
        value={truckId} disabled={disabled || busy || !!pending || choices === null} onFocus={onFocus}
        onChange={event => void choose(event.target.value)}
        className="truck-assignment-select peer block h-8 w-5 cursor-pointer appearance-none bg-transparent text-transparent disabled:cursor-default">
        <option value="" disabled className="bg-zinc-900 text-zinc-200">—</option>
        {truckId && !choices?.some(truck => truck.id === truckId) && <option value={truckId} className="bg-zinc-900 text-zinc-200">{unit}</option>}
        {choices?.map(truck => {
          const current = truck.id === truckId;
          const loadBlocked = !current && !!truck.driverId && !!truck.activeLoad;
          const unavailable = !truck.active || !["ready", "in_use", "shop"].includes(truck.status) || loadBlocked;
          const detail = loadBlocked ? `${truck.driverName} · Active load ${truck.activeLoad}`
            : [truck.driverName, statusNames[truck.status]].filter(Boolean).join(" · ") || (!truck.active ? "Unavailable" : "");
          return <option key={truck.id} value={truck.id} disabled={unavailable && !current} className="bg-zinc-900 text-zinc-200">
            {truck.unit}{!current && detail ? ` — ${detail}` : ""}
          </option>;
        })}
      </select>
      <ChevronDown aria-hidden="true" className="pointer-events-none absolute left-0.5 top-1/2 size-4 -translate-y-1/2 text-zinc-300 peer-disabled:opacity-50" />
    </div>
  </div>
    {pending && createPortal(<dialog ref={dialog} aria-label="Reassign truck" data-grid-ignore
      className="mserp-ui ui-modal-panel fixed inset-0 m-auto w-[calc(100%-32px)] max-w-sm rounded-lg border border-zinc-700 bg-zinc-950 p-4 text-sm text-zinc-200 backdrop:bg-black/60"
      onCancel={event => { event.preventDefault(); cancel(); }}>
      <h2 className="mb-2 font-semibold">Reassign truck {pending.truck.unit}?</h2>
      <p>Currently assigned to <strong>{pending.truck.driverName}</strong>. Move it to <strong>{pending.view.driverName}</strong> and end the previous assignment?</p>
      <p className="mt-2 text-xs text-zinc-400">Effective today · {pending.view.effectiveDate} (New York)</p>
      {confirmationError && <p role="alert" className="mt-3 text-red-400">{confirmationError}</p>}
      <div className="mt-4 flex justify-end gap-2">
        <button type="button" autoFocus disabled={busy} className="ui-button px-3" onClick={cancel}>Cancel</button>
        <button type="button" disabled={busy} className="ui-button ui-button-primary px-3" onClick={() => void confirm()}>{busy ? "Saving…" : "Reassign truck"}</button>
      </div>
    </dialog>, document.body)}
  </>;
}
