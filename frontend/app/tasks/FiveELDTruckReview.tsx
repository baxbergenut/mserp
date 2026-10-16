"use client";
import type { DriverOption, InvestorOption } from "@/app/lib/types";

import { useEffect, useRef, useState } from "react";
import { completeFiveELDTruckReview, fetchFiveELDTruckReview, fetchDriverOptions, fetchInvestorOptions } from "../lib/api";
import type { CustomTask, FiveELDTruckReview, TruckInput } from "../lib/types";
import { usePermissions } from "../lib/access";
import { ErrorBanner, Modal } from "../components/management/ManagementUI";
import { emptyTruckInput, TruckForm } from "../trucks/TruckForm";

export function FiveELDTruckReviewModal({ task, onClose, onCompleted }: { task: CustomTask; onClose: () => void; onCompleted: () => void }) {
 const permissions = usePermissions();
 const canWrite = permissions.includes("tasks.review") && permissions.includes("trucks.create");
 const [review, setReview] = useState<FiveELDTruckReview | null>(null);
 const [value, setValue] = useState<TruckInput>({ ...emptyTruckInput });
 const [drivers, setDrivers] = useState<DriverOption[]>([]);
 const [investors, setInvestors] = useState<InvestorOption[]>([]);
 const [error, setError] = useState("");
 const [saving, setSaving] = useState(false);
 const submitting = useRef(false);
 useEffect(() => {
  let active = true;
  Promise.all([fetchFiveELDTruckReview(task.id), fetchDriverOptions(), fetchInvestorOptions()]).then(([data, d, i]) => {
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
