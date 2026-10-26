"use client";

import { useEffect, useState } from "react";
import { ErrorBanner, Modal, controlClass } from "@/app/components/management/ManagementUI";
import { fetchTruckOptions, fetchTrailers, saveInventoryRecord } from "@/app/lib/api";
import { usePermissions } from "@/app/lib/access";
import type { InventoryRecord, TruckOption } from "@/app/lib/types";

export const trailerRestricted = (status?: string) => ["shop", "out_of_service", "terminated"].includes(status || "");

export function TrailerOperationDialog({ trailer, mode, onClose, onSaved }: {
  trailer: InventoryRecord; mode: "status" | "return" | "assignment";
  onClose: () => void; onSaved: (value: InventoryRecord) => void;
}) {
  const canReadTrucks = usePermissions().includes("trucks.read");
  const [status, setStatus] = useState("shop");
  const [truckId, setTruckId] = useState(trailer.data.truckId || "");
  const [reason, setReason] = useState("");
  const [trucks, setTrucks] = useState<TruckOption[]>([]);
  const [loading, setLoading] = useState(mode === "assignment" && canReadTrucks && !trailerRestricted(trailer.data.status));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [requestId] = useState(() => crypto.randomUUID());
  const restricted = trailerRestricted(trailer.data.status);
  useEffect(() => {
    if (mode !== "assignment" || !canReadTrucks || restricted) return;
    let cancelled = false;
    void Promise.all([fetchTruckOptions(), fetchTrailers()]).then(([options, trailers]) => {
      if (!cancelled) setTrucks(options.filter(truck => truck.id === trailer.data.truckId || (["ready", "in_use"].includes(truck.operatingStatus || "") && !trailers.some(other => other.id !== trailer.id && other.data.truckId === truck.id))));
    }).catch(error => { if (!cancelled) setError(error instanceof Error ? error.message : "Could not load trucks"); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [mode, canReadTrucks, restricted, trailer.id, trailer.data.truckId]);
  const title = mode === "assignment" ? "Truck assignment" : mode === "return" ? "Return to service" : "Change trailer status";
  return <Modal title={title} isSaving={saving} submitLabel="Save" submitDisabled={loading || (mode !== "assignment" && !reason.trim()) || (mode === "assignment" && truckId === (trailer.data.truckId || ""))}
    onClose={onClose} onSubmit={async event => {
      event.preventDefault(); setSaving(true); setError("");
      try {
        const data = mode === "assignment" ? {...trailer.data, truckId} : {...trailer.data, status: mode === "return" ? "ready" : status};
        onSaved(await saveInventoryRecord("trailers", {...trailer, data}, requestId, mode === "assignment" ? undefined : reason.trim()));
      } catch (error) { setError(error instanceof Error ? error.message : "Could not save trailer"); }
      finally { setSaving(false); }
    }}>
    <div className="space-y-4">
      {error && <ErrorBanner message={error} />}
      {mode === "assignment" ? <>
        <p className="text-xs text-zinc-400">{restricted ? "Return to service before assigning a new truck. You can end the existing assignment here." : "Assigning a truck makes this trailer In use. Ending the assignment makes it Ready."}</p>
        <label className="block space-y-2 text-xs text-zinc-400"><span>Truck</span><select aria-label="Truck" className={controlClass} value={truckId} disabled={loading || saving} onChange={event => setTruckId(event.target.value)}>
          <option value="">Unassigned</option>
          {trailer.data.truckId && !trucks.some(truck => truck.id === trailer.data.truckId) && <option value={trailer.data.truckId}>Truck {trailer.truckUnit || "currently assigned"}</option>}
          {trucks.map(truck => <option key={truck.id} value={truck.id}>Truck {truck.unitNumber}</option>)}
        </select></label>
        {!canReadTrucks && <p className="text-xs text-zinc-500">Truck access is required to choose a new assignment.</p>}
      </> : <>
        {mode === "status" ? <label className="block space-y-2 text-xs text-zinc-400"><span>Status</span><select aria-label="Status" className={controlClass} value={status} disabled={saving} onChange={event => setStatus(event.target.value)}>
          <option value="shop">Shop</option><option value="out_of_service">OOS</option><option value="terminated" disabled={!!trailer.data.truckId}>Terminated{trailer.data.truckId ? " — end assignment first" : ""}</option>
        </select></label> : <p className="text-sm text-zinc-300">This trailer will return to <strong>{trailer.data.truckId ? "In use" : "Ready"}</strong>{trailer.data.truckId ? " with its current truck assignment." : "."}</p>}
        {mode === "status" && <p className="text-xs text-zinc-500">Ready and In use follow the truck assignment automatically. Shop and OOS retain an existing assignment.</p>}
        <label className="block space-y-2 text-xs text-zinc-400"><span>Reason</span><textarea aria-label="Reason" className={controlClass} value={reason} required maxLength={1000} rows={3} disabled={saving} onChange={event => setReason(event.target.value)} /></label>
      </>}
    </div>
  </Modal>;
}
