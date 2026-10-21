"use client";
import type { DriverOption, TruckOption, DispatcherOption, InvestorOption } from "@/app/lib/types";

import { usePermissions } from "../lib/access";
import { useRef, useState } from "react";
import { Check } from "lucide-react";
import { Modal, ErrorBanner, Field, controlClass } from "../components/management/ManagementUI";
import type { DriverInput, DriverIntake, DriverServiceIntent, Investor } from "../lib/types";
import { DriverForm } from "./DriverForm";
import { DriverServiceChoices, serviceNames } from "./DriverServices";
import { TruckForm } from "../trucks/TruckForm";
import { formatPhone } from "../lib/phone";

type Step = "information" | "assignments" | "truck" | "services" | "finalize";
const labels: Record<Step, string> = { information: "Driver information", assignments: "Assignments", truck: "Truck details", services: "Driver services", finalize: "Finalize" };

export function DriverSetup({ value, onChange, intake, drivers, trucks, dispatchers, investors, linkId, onLink, separateConfirmed, onConfirmSeparate, services, onServices, saving, loading, error, onClose, onSave }: {
  value: DriverInput; onChange: (value: DriverInput) => void; intake: DriverIntake | null;
  drivers: DriverOption[]; trucks: TruckOption[]; dispatchers: DispatcherOption[]; investors: InvestorOption[];
  linkId: string; onLink: (id: string) => void; separateConfirmed: boolean; onConfirmSeparate: (value: boolean) => void;
  services: DriverServiceIntent[]; onServices: (value: DriverServiceIntent[]) => void;
  saving: boolean; loading: boolean; error: string; onClose: () => void; onSave: () => Promise<void>;
}) {
  const permissions=usePermissions();
  const [step, setStep] = useState<Step>("information");
  const [validation, setValidation] = useState("");
  const content = useRef<HTMLDivElement>(null);
  const steps: Step[] = linkId ? ["information", "services", "finalize"] : ["information", "assignments", ...(value.newTruck && !value.isTeamDriver ? ["truck" as const] : []), "services", "finalize"];
  const index = steps.indexOf(step);
  const existing = drivers.find(d => d.id === linkId);
  const move = (next: Step) => {
    setStep(next); setValidation("");
    window.requestAnimationFrame(() => { content.current?.closest(".overflow-y-auto")?.scrollTo(0, 0); content.current?.querySelector<HTMLElement>("h3")?.focus(); });
  };
  const selfOwner: Investor = { id: "__driver__", fullName: `${value.fullName || "This driver"} (driver-owned)`, driverId: "new", isCompany: false, active: true, email: null, phone: null, notes: null, trucks: [], createdAt: "", updatedAt: "" };
  const next = () => {
    if (step === "assignments" && intake && !value.isTeamDriver && value.payRate <= 0) { setValidation("Enter a pay rate greater than zero."); return; }
    if (step !== "finalize") { move(steps[index + 1]); return; }
    if (!linkId && value.isTeamDriver && value.active && (!value.truckId || !value.teamMainDriverId)) {
      move("assignments"); setValidation("Select the main driver’s truck for this start date."); return;
    }
    if (!linkId && value.newTruck && !value.active) {
      setValidation("A new truck can only be assigned to an active driver. Choose an existing truck or Unassigned in Assignments."); return;
    }
    void onSave();
  };
  return <Modal title={intake ? `Set up ${intake.driver.fullName}` : "Add driver"} isSaving={saving}
    submitDisabled={loading} submitLabel={step === "finalize" ? linkId ? "Confirm link" : intake ? "Complete setup" : "Create driver" : "Continue"}
    onClose={onClose} onSubmit={event => { event.preventDefault(); if (!loading && !saving) next(); }}
    footerStart={index > 0 && <button type="button" disabled={saving} className="ui-button" onClick={() => move(steps[index - 1])}>Previous</button>}>
    <div className="mb-6 space-y-3">
      <div className="h-1.5 overflow-hidden rounded-full bg-zinc-800" role="progressbar" aria-label="Driver setup progress" aria-valuemin={1} aria-valuemax={steps.length} aria-valuenow={index + 1} aria-valuetext={`Step ${index + 1} of ${steps.length}: ${labels[step]}`}><div className="h-full rounded-full bg-blue-500 transition-all" style={{ width: `${(index + 1) / steps.length * 100}%` }} /></div>
      <ol className="flex justify-between gap-2">{steps.map((item, position) => <li key={item} className="min-w-0 flex-1"><button type="button" disabled={saving || position >= index} aria-label={`Step ${position + 1}: ${labels[item]}`} aria-current={item === step ? "step" : undefined} onClick={() => move(item)} className={`flex w-full items-center gap-1.5 text-left text-xs ${item === step ? "font-semibold text-blue-400" : "text-zinc-500"}`}><span className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-full border ${position <= index ? "border-blue-500/40 bg-blue-500/10 text-blue-400" : "border-zinc-700"}`}>{position < index ? <Check className="h-3.5 w-3.5" /> : position + 1}</span><span className="hidden truncate sm:block">{{ information: "Information", assignments: "Assignments", truck: "Truck", services: "Services", finalize: "Finalize" }[item]}</span></button></li>)}</ol>
    </div>
    <div ref={content} className="space-y-4">
      {(error || validation) && <ErrorBanner message={validation || error} />}
      {loading && <p role="status" className="text-xs text-zinc-400">Loading setup options…</p>}
      <h3 tabIndex={-1} className="text-sm font-semibold text-zinc-200 outline-none">{labels[step]}</h3>
      <fieldset disabled={saving || loading} className="space-y-4">
        {step === "information" && <>
          {intake && <Field label="Driver record"><select className={controlClass} value={linkId} onChange={e => onLink(e.target.value)}><option value="">New driver</option>{drivers.map(d => <option key={d.id} value={d.id}>{d.fullName} · {formatPhone(d.phone) || d.email || "No contact details"}{d.active ? "" : " (terminated)"}</option>)}</select></Field>}
          {intake && !linkId && intake.candidates.length > 0 && <div className="rounded-lg border border-amber-500/30 p-3 text-xs text-amber-300"><p>Possible matches: {intake.candidates.map(d => d.fullName).join(", ")}.</p><label className="mt-2 flex items-center gap-2"><input type="checkbox" required checked={separateConfirmed} onChange={e => onConfirmSeparate(e.target.checked)} />This is a different person.</label></div>}
          {linkId ? <p className="text-xs text-zinc-400">Link to {existing?.fullName}. Existing profile and assignments stay unchanged.</p> : <DriverForm section="information" showEscrow value={value} onChange={onChange} trucks={trucks} dispatchers={dispatchers} />}
        </>}
        {step === "assignments" && <DriverForm section="assignments" showEscrow allowNewTruck value={value} onChange={onChange} trucks={trucks} dispatchers={dispatchers} />}
        {step === "truck" && value.newTruck && <TruckForm setup value={{ ...value.newTruck, ownerId: value.newTruckOwnedByDriver ? "__driver__" : value.newTruck.ownerId }} drivers={[]} investors={[...investors, ...(permissions.includes("investors.create") ? [selfOwner] : [])]} onChange={truck => onChange({ ...value, newTruckOwnedByDriver: truck.ownerId === "__driver__", newTruck: { ...truck, ownerId: truck.ownerId === "__driver__" ? null : truck.ownerId } })} />}
        {step === "services" && <DriverServiceChoices key={linkId || "new"} value={services} onChange={onServices} driverId={linkId || undefined} disabled={saving} />}
        {step === "finalize" && <>
          {!linkId && <DriverForm section="finalize" showEscrow value={value} onChange={onChange} trucks={trucks} dispatchers={dispatchers} />}
          <dl className="grid grid-cols-2 gap-3 rounded-lg border border-zinc-800 p-4 text-xs">
            <dt className="text-zinc-500">Driver</dt><dd>{existing?.fullName || value.fullName}</dd>
            {!linkId && <><dt className="text-zinc-500">Truck</dt><dd>{value.newTruck ? `${value.newTruck.unitNumber} · New` : trucks.find(t => t.id === value.truckId)?.unitNumber || "Unassigned"}</dd><dt className="text-zinc-500">Mode</dt><dd>{value.isTeamDriver ? "Team driver" : "Solo driver"}</dd>{!value.isTeamDriver && <><dt className="text-zinc-500">Escrow</dt><dd>${value.escrowAmount}</dd></>}</>}
            <dt className="text-zinc-500">Services</dt><dd>{services.length ? services.map(s => `${serviceNames[s.service]} · ${s.action === "add" ? "Add" : "Remind later"}`).join(", ") : "Not requested"}</dd>
          </dl>
        </>}
      </fieldset>
    </div>
  </Modal>;
}
