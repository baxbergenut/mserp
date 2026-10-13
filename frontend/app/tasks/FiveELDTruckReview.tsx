"use client";

import { useEffect, useRef, useState } from "react";
import { completeFiveELDTruckReview, fetchFiveELDTruckReview, fetchDrivers, fetchInvestors } from "../lib/api";
import type { CustomTask, Driver, FiveELDTruckReview, Investor, TruckInput } from "../lib/types";
import { usePermissions } from "../lib/access";
import { ErrorBanner, Modal } from "../components/management/ManagementUI";
import { emptyTruckInput, TruckForm } from "../trucks/TruckForm";

export function FiveELDTruckReviewModal({ task, onClose, onCompleted }: { task: CustomTask; onClose: () => void; onCompleted: () => void }) {
 const permissions = usePermissions();
 const canWrite = permissions.includes("tasks.write") && permissions.includes("fleet.write");
 const [review, setReview] = useState<FiveELDTruckReview | null>(null);
 const [value, setValue] = useState<TruckInput>({ ...emptyTruckInput });
 const [drivers, setDrivers] = useState<Driver[]>([]);
 const [investors, setInvestors] = useState<Investor[]>([]);
 const [error, setError] = useState("");
 const [saving, setSaving] = useState(false);
 const submitting = useRef(false);
 useEffect(() => {
  let active = true;
  Promise.all([fetchFiveELDTruckReview(task.id), fetchDrivers(), fetchInvestors()]).then(([data, d, i]) => {
   if (!active) return;
   setReview(data); setDrivers(d); setInvestors(i);
   setValue({ ...emptyTruckInput, vin: data.vin, unitNumber: data.unitNumber });
  }).catch(e => { if (active) setError(e instanceof Error ? e.message : "Could not load truck review"); });
  return () => { active = false; };
 }, [task.id]);
 async function submit() {
  if (!canWrite || review?.completed) { onClose(); return; }
  if (!review || submitting.current) return;
  submitting.current = true; setSaving(true); setError("");
  try { await completeFiveELDTruckReview(task.id, review.version, value); onCompleted(); }
  catch (e) { setError(e instanceof Error ? e.message : "Could not add truck"); }
  finally { submitting.current = false; setSaving(false); }
 }
 return <Modal title="Review Five ELD truck" isSaving={saving} onClose={onClose} submitLabel={canWrite && !review?.completed ? "Add truck to MSERP" : "Close"} onSubmit={event => { event.preventDefault(); void submit(); }}>
  <div className="space-y-4">
   {error && <ErrorBanner message={error} />}
   {!review ? <p role="status">Loading truck details…</p> : review.completed ? <p>This truck review is already complete.</p> : <>
    <p className="text-sm text-zinc-400">Five ELD reports unit {review.unitNumber}, VIN {review.vin}, working for MS Express. Review and correct the details below before adding it.</p>
    <fieldset disabled={!canWrite || saving}><TruckForm setup value={value} drivers={drivers} investors={investors} onChange={setValue} /></fieldset>
   </>}
  </div>
 </Modal>;
}
