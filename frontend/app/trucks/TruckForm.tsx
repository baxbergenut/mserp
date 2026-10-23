"use client";
import type { DriverOption, InvestorOption } from "@/app/lib/types";
import { usePermissions } from "../lib/access";

import { AssignmentWeekField } from "../components/management/AssignmentWeekField";
import { currentChargeWeek } from "@/app/accounting/driver-charges/charges";

import { ExternalLink, FileText, LoaderCircle, Sparkles, Upload, X } from "lucide-react";
import type { Truck, TruckInput } from "../lib/types";
import { fileDownloadUrl } from "../lib/api";
import {
  controlClass,
  Field,
  FormSection,
  Toggle,
} from "../components/management/ManagementUI";

export const emptyTruckInput: TruckInput = {
  addToDataTruck: true,
  ownerId: "00000000-0000-0000-0000-000000000001",
  unitNumber: "",
  vin: "",
  year: null,
  make: "",
  model: "",
  color: "",
  licensePlate: "",
  licenseState: "",
  isCompanyOwned: true,
  status: "available",
  mileage: null,
  registrationExpires: "",
  insuranceExpires: "",
  lastServiceDate: "",
  nextServiceMiles: null,
  driverId: null,
  active: true,
  notes: "",
  irpFileId: null,
};

export function truckToInput(truck: Truck): TruckInput {
  return {
	statusVersion:truck.statusVersion,
    ownershipEffectiveDate: truck.ownerSince ?? "",
    expectedOwnershipVersion: truck.ownershipVersion,
    ownerId: truck.ownerId,
    unitNumber: truck.unitNumber,
    vin: truck.vin ?? "",
    year: truck.year,
    make: truck.make ?? "",
    model: truck.model ?? "",
    color: truck.color ?? "",
    licensePlate: truck.licensePlate ?? "",
    licenseState: truck.licenseState ?? "",
    isCompanyOwned: truck.isCompanyOwned,
    status: truck.status,
    mileage: truck.mileage,
    registrationExpires: truck.registrationExpires?.slice(0, 10) ?? "",
    insuranceExpires: truck.insuranceExpires?.slice(0, 10) ?? "",
    lastServiceDate: truck.lastServiceDate?.slice(0, 10) ?? "",
    nextServiceMiles: truck.nextServiceMiles,
    driverId: truck.driverId,
    active: truck.active,
    notes: truck.notes ?? "",
    irpFileId: truck.irpFileId ?? null,
  };
}

export function TruckForm({
  value,
  drivers,
  investors,
  onChange,
  irpFileName,
  isUploadingIRP,
  onUploadIRP,
  onRemoveIRP,
  setup = false,
  creating = false,
}: {
  value: TruckInput;
  drivers: DriverOption[];
  investors: InvestorOption[];
  onChange: (value: TruckInput) => void;
  irpFileName?: string | null;
  isUploadingIRP?: boolean;
  onUploadIRP?: (file: File) => Promise<void>;
  onRemoveIRP?: () => void;
  setup?: boolean;
  creating?: boolean;
}) {
  const set = <K extends keyof TruckInput>(key: K, next: TruckInput[K]) =>
    onChange({ ...value, [key]: next, ...(["driverId"].includes(key) ? { assignmentWeek: value.assignmentWeek ?? "" } : {}) });
  const numberOrNull = (raw: string) => (raw === "" ? null : Number(raw));
  const today = new Intl.DateTimeFormat("en-CA", {timeZone: "America/New_York", year: "numeric", month: "2-digit", day: "2-digit"}).format(new Date());

  const permissions=usePermissions();
  return (
    <div className="space-y-6">
      {(setup || creating) && <section aria-label="Truck services"><Toggle checked={value.addToDataTruck ?? true} onChange={checked => set("addToDataTruck", checked)} label="Add to DataTruck after saving" description="Create this truck in DataTruck using the saved truck details." /></section>}
      {!setup && permissions.includes("trucks.documents.read") && <fieldset disabled={!permissions.includes("trucks.documents.edit")}><FormSection title="IRP cab card">
        <div className="sm:col-span-2">
          <div className="rounded-xl border border-dashed border-blue-500/30 bg-blue-500/[0.04] p-4">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex min-w-0 items-start gap-3">
                <div className="rounded-lg bg-blue-500/10 p-2 text-blue-400">
                  {isUploadingIRP ? (
                    <LoaderCircle className="h-5 w-5 animate-spin" />
                  ) : (
                    <FileText className="h-5 w-5" />
                  )}
                </div>
                <div className="min-w-0">
                  <p className="truncate text-[13px] font-medium text-zinc-200">
                    {isUploadingIRP
                      ? "Reading cab card with GROQ…"
                      : irpFileName ?? "Upload an IRP cab card"}
                  </p>
                  <p className="mt-1 text-[11px] leading-5 text-zinc-500">
                    PDF, PNG, JPEG, or WEBP, up to 10 MB. Extracted values fill the form below so you can review them before saving.
                  </p>
                </div>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                {value.irpFileId && (
                  <a
                    href={fileDownloadUrl(value.irpFileId)}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1.5 rounded-lg border border-zinc-800 px-3 py-2 text-[12px] font-medium text-zinc-400 transition hover:bg-zinc-800/60 hover:text-zinc-200"
                  >
                    <ExternalLink className="h-3.5 w-3.5" />
                    Open
                  </a>
                )}
                {value.irpFileId && (
                  <button
                    type="button"
                    onClick={onRemoveIRP}
                    disabled={isUploadingIRP}
                    className="rounded-lg border border-zinc-800 p-2 text-zinc-500 transition hover:bg-zinc-800/60 hover:text-zinc-200 disabled:opacity-50"
                    aria-label="Remove cab card from this truck"
                  >
                    <X className="h-4 w-4" />
                  </button>
                )}
                <label className="inline-flex cursor-pointer items-center gap-2 rounded-lg bg-blue-600 px-3 py-2 text-[12px] font-medium text-white transition hover:bg-blue-500 has-[:disabled]:cursor-wait has-[:disabled]:opacity-60">
                  <Upload className="h-3.5 w-3.5" />
                  {value.irpFileId ? "Replace" : "Upload"}
                  <input
                    type="file"
                    className="sr-only"
                    accept="application/pdf,image/png,image/jpeg,image/webp"
                    disabled={isUploadingIRP}
                    onChange={(event) => {
                      const file = event.currentTarget.files?.[0];
                      event.currentTarget.value = "";
                      if (file) void onUploadIRP?.(file);
                    }}
                  />
                </label>
              </div>
            </div>
            {value.irpFileId && !isUploadingIRP && (
              <div className="mt-3 flex items-center gap-1.5 border-t border-blue-500/10 pt-3 text-[11px] text-emerald-400">
                <Sparkles className="h-3.5 w-3.5" />
                Cab card stored. Review the extracted truck and registration values below.
              </div>
            )}
          </div>
        </div>
      </FormSection></fieldset>}

      <FormSection title="Truck profile">
        <Field label="Unit number">
          <input
            required
            autoFocus
            value={value.unitNumber}
            onChange={(event) => set("unitNumber", event.target.value)}
            className={controlClass}
            placeholder="e.g. 1042"
          />
        </Field>
        <Field label="VIN">
          <input
            value={value.vin}
            onChange={(event) => set("vin", event.target.value.toUpperCase())}
            className={controlClass}
            maxLength={17}
          />
        </Field>
        <Field label="Year">
          <input
            type="number"
            min="1900"
            max="2200"
            value={value.year ?? ""}
            onChange={(event) => set("year", numberOrNull(event.target.value))}
            className={controlClass}
          />
        </Field>
        <Field label="Make">
          <input
            value={value.make}
            onChange={(event) => set("make", event.target.value)}
            className={controlClass}
            placeholder="Freightliner"
          />
        </Field>
        <Field label="Model">
          <input
            value={value.model}
            onChange={(event) => set("model", event.target.value)}
            className={controlClass}
            placeholder="Cascadia"
          />
        </Field>
        <Field label="Color">
          <input
            value={value.color}
            onChange={(event) => set("color", event.target.value)}
            className={controlClass}
            maxLength={30}
            placeholder="e.g. White"
          />
        </Field>
        <Field label="Owner">
          <select required aria-label="Truck owner" value={value.ownerDriverId ? `driver:${value.ownerDriverId}` : value.ownerId ?? ""}
            onChange={(event) => { const owner = event.target.value; onChange({ ...value, ownershipEffectiveDate: today, ownerDriverId: owner.startsWith("driver:") ? owner.slice(7) : null, ownerId: owner.startsWith("driver:") ? null : owner, isCompanyOwned: investors.find(i => i.id === owner)?.isCompany ?? false }); }}
            className={controlClass}>
            {investors.filter((i) => (i.active || i.id === value.ownerId) && (!i.driverId || !drivers.some(d => d.id === i.driverId) || i.id === value.ownerId)).map((i) => (
              <option key={i.id} value={i.id}>{i.fullName}{i.driverId ? " — Driver" : ""}{!i.active ? " (inactive)" : ""}</option>
            ))}
            <optgroup label="Drivers">{drivers.filter(d => !investors.some(i => i.driverId === d.id && i.id === value.ownerId)).map(d => <option key={d.id} value={`driver:${d.id}`} disabled={!investors.some(i=>i.driverId===d.id) && !permissions.includes("investors.create")}>{d.fullName}{!d.active ? " (terminated)" : ""}</option>)}</optgroup>
          </select>
        </Field>
        <Field label="Ownership effective date">
          <input aria-label="Ownership effective date" type="date" min="1900-01-01" max={today} value={value.ownershipEffectiveDate ?? ""} onChange={event => set("ownershipEffectiveDate", event.target.value)} className={controlClass} />
        </Field>
      </FormSection>

      <FormSection title="Registration and insurance">
        <Field label="License plate">
          <input
            value={value.licensePlate}
            onChange={(event) => set("licensePlate", event.target.value)}
            className={controlClass}
          />
        </Field>
        <Field label="Plate state">
          <input
            value={value.licenseState}
            onChange={(event) => set("licenseState", event.target.value)}
            className={controlClass}
            maxLength={2}
            placeholder="IL"
          />
        </Field>
        <Field label="Registration expires">
          <input
            type="date"
            value={value.registrationExpires}
            onChange={(event) => set("registrationExpires", event.target.value)}
            className={controlClass}
          />
        </Field>
        <Field label="Insurance expires">
          <input
            type="date"
            value={value.insuranceExpires}
            onChange={(event) => set("insuranceExpires", event.target.value)}
            className={controlClass}
          />
        </Field>
      </FormSection>

      {!setup && <FormSection title="Operations and assignment">
        <AssignmentWeekField value={value.assignmentWeek} onChange={week => set("assignmentWeek", week)} />
        <Field label="Assigned driver" hint="Selecting a driver releases any truck currently assigned to them.">
          <select
            value={value.driverId ?? ""}
            disabled={!value.active}
            onChange={(event) => set("driverId", event.target.value || null)}
            className={controlClass}
          >
            <option value="">Unassigned</option>
            {drivers.filter((driver) => driver.active || driver.id === value.driverId).map((driver) => (
              <option key={driver.id} value={driver.id}>
                {driver.fullName}{driver.truckUnit ? ` — currently ${driver.truckUnit}` : ""}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Status" hint="Work-order holds also apply until Fleet releases them.">
          <select
            value={value.status}
            onChange={(event) => set("status", event.target.value as TruckInput["status"])}
            className={controlClass}
          >
            <option value="available">Ready</option>
            {value.driverId !== null && <option value="assigned">Ready (driver assigned)</option>}
            <option value="maintenance">Shop</option>
            <option value="out_of_service">OOS</option>
            <option value="hold">Hold</option>
          </select>
        </Field>
        <Field label="Current mileage">
          <input
            type="number"
            min="0"
            value={value.mileage ?? ""}
            onChange={(event) => set("mileage", numberOrNull(event.target.value))}
            className={controlClass}
          />
        </Field>
        <Field label="Next service at (miles)">
          <input
            type="number"
            min="0"
            value={value.nextServiceMiles ?? ""}
            onChange={(event) => set("nextServiceMiles", numberOrNull(event.target.value))}
            className={controlClass}
          />
        </Field>
        <Field label="Last service date">
          <input
            type="date"
            value={value.lastServiceDate}
            onChange={(event) => set("lastServiceDate", event.target.value)}
            className={controlClass}
          />
        </Field>
      </FormSection>}

      <FormSection title={setup ? "Notes" : "Status and notes"}>
        {!setup && <Toggle
          checked={value.active}
          onChange={(checked) => onChange({ ...value, active: checked, ...(!checked ? { driverId: null, status: value.status === "assigned" ? "available" : value.status, assignmentWeek: value.assignmentWeek || currentChargeWeek() } : {}) })}
          label="Active truck"
          description="Marking inactive disconnects the current driver, except a terminated owner’s existing link to their own truck. Historical records are retained."
        />}
        <Field label="Internal notes" wide>
          <textarea
            rows={3}
            value={value.notes}
            onChange={(event) => set("notes", event.target.value)}
            className={controlClass}
            placeholder="Maintenance details, equipment notes, or other context"
          />
        </Field>
      </FormSection>
    </div>
  );
}
