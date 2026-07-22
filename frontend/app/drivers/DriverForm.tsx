"use client";

import { ExternalLink, FileBadge, LoaderCircle, Sparkles, Upload, X } from "lucide-react";
import type { Dispatcher, Driver, DriverInput, Truck } from "../lib/types";
import { fileDownloadUrl } from "../lib/api";
import {
  controlClass,
  Field,
  FormSection,
  Toggle,
} from "../components/management/ManagementUI";

export const emptyDriverInput: DriverInput = {
  fullName: "",
  isOwnerOperator: false,
  payType: "cpm",
  payRate: 0,
  phone: "",
  email: "",
  licenseNumber: "",
  licenseState: "",
  licenseExpires: "",
  hireDate: "",
  address: "",
  city: "",
  state: "",
  postalCode: "",
  emergencyContact: "",
  dispatcherId: null,
  truckId: null,
  active: true,
  notes: "",
  cdlFileId: null,
};

export function driverToInput(driver: Driver): DriverInput {
  return {
    fullName: driver.fullName,
    isOwnerOperator: driver.isOwnerOperator,
    payType: driver.payType,
    payRate: driver.payRate,
    phone: driver.phone ?? "",
    email: driver.email ?? "",
    licenseNumber: driver.licenseNumber ?? "",
    licenseState: driver.licenseState ?? "",
    licenseExpires: driver.licenseExpires?.slice(0, 10) ?? "",
    hireDate: driver.hireDate?.slice(0, 10) ?? "",
    address: driver.address ?? "",
    city: driver.city ?? "",
    state: driver.state ?? "",
    postalCode: driver.postalCode ?? "",
    emergencyContact: driver.emergencyContact ?? "",
    dispatcherId: driver.dispatcherId,
    truckId: driver.truckId,
    active: driver.active,
    notes: driver.notes ?? "",
    cdlFileId: driver.cdlFileId,
  };
}

export function DriverForm({
  value,
  dispatchers,
  trucks,
  onChange,
  cdlFileName,
  isUploadingCDL,
  onUploadCDL,
  onRemoveCDL,
}: {
  value: DriverInput;
  dispatchers: Dispatcher[];
  trucks: Truck[];
  onChange: (value: DriverInput) => void;
  cdlFileName: string | null;
  isUploadingCDL: boolean;
  onUploadCDL: (file: File) => Promise<void>;
  onRemoveCDL: () => void;
}) {
  const set = <K extends keyof DriverInput>(key: K, next: DriverInput[K]) =>
    onChange({ ...value, [key]: next });

  return (
    <div className="space-y-6">
      <FormSection title="Commercial driver's license">
        <div className="sm:col-span-2">
          <div className="rounded-xl border border-dashed border-blue-500/30 bg-blue-500/[0.04] p-4">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex min-w-0 items-start gap-3">
                <div className="rounded-lg bg-blue-500/10 p-2 text-blue-400">
                  {isUploadingCDL ? (
                    <LoaderCircle className="h-5 w-5 animate-spin" />
                  ) : (
                    <FileBadge className="h-5 w-5" />
                  )}
                </div>
                <div className="min-w-0">
                  <p className="truncate text-[13px] font-medium text-zinc-200">
                    {isUploadingCDL
                      ? "Reading CDL with GROQ…"
                      : cdlFileName ?? "Upload a driver's CDL"}
                  </p>
                  <p className="mt-1 text-[11px] leading-5 text-zinc-500">
                    PDF, PNG, JPEG, or WEBP, up to 10 MB. Include front and back in one PDF when available.
                  </p>
                </div>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                {value.cdlFileId && (
                  <a
                    href={fileDownloadUrl(value.cdlFileId)}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1.5 rounded-lg border border-zinc-800 px-3 py-2 text-[12px] font-medium text-zinc-400 transition hover:bg-zinc-800/60 hover:text-zinc-200"
                  >
                    <ExternalLink className="h-3.5 w-3.5" />
                    Open
                  </a>
                )}
                {value.cdlFileId && (
                  <button
                    type="button"
                    onClick={onRemoveCDL}
                    disabled={isUploadingCDL}
                    className="rounded-lg border border-zinc-800 p-2 text-zinc-500 transition hover:bg-zinc-800/60 hover:text-zinc-200 disabled:opacity-50"
                    aria-label="Remove CDL from this driver"
                  >
                    <X className="h-4 w-4" />
                  </button>
                )}
                <label className="inline-flex cursor-pointer items-center gap-2 rounded-lg bg-blue-600 px-3 py-2 text-[12px] font-medium text-white transition hover:bg-blue-500 has-[:disabled]:cursor-wait has-[:disabled]:opacity-60">
                  <Upload className="h-3.5 w-3.5" />
                  {value.cdlFileId ? "Replace" : "Upload"}
                  <input
                    type="file"
                    className="sr-only"
                    accept="application/pdf,image/png,image/jpeg,image/webp"
                    disabled={isUploadingCDL}
                    onChange={(event) => {
                      const file = event.currentTarget.files?.[0];
                      event.currentTarget.value = "";
                      if (file) void onUploadCDL(file);
                    }}
                  />
                </label>
              </div>
            </div>
            {value.cdlFileId && !isUploadingCDL && (
              <div className="mt-3 flex items-center gap-1.5 border-t border-blue-500/10 pt-3 text-[11px] text-emerald-400">
                <Sparkles className="h-3.5 w-3.5" />
                CDL stored. Review the extracted identity, license, and address fields below.
              </div>
            )}
          </div>
        </div>
      </FormSection>

      <FormSection title="Driver profile">
        <Field label="Full name" wide>
          <input
            required
            autoFocus
            value={value.fullName}
            onChange={(event) => set("fullName", event.target.value)}
            className={controlClass}
            placeholder="e.g. Marcus Reed"
          />
        </Field>
        <Field label="Driver type">
          <select
            value={value.isOwnerOperator ? "owner" : "company"}
            onChange={(event) =>
              set("isOwnerOperator", event.target.value === "owner")
            }
            className={controlClass}
          >
            <option value="company">Company driver</option>
            <option value="owner">Owner-operator</option>
          </select>
        </Field>
        <Field label="Hire date">
          <input
            type="date"
            value={value.hireDate}
            onChange={(event) => set("hireDate", event.target.value)}
            className={controlClass}
          />
        </Field>
        <Field label="Phone">
          <input
            type="tel"
            value={value.phone}
            onChange={(event) => set("phone", event.target.value)}
            className={controlClass}
            placeholder="(555) 555-0123"
          />
        </Field>
        <Field label="Email">
          <input
            type="email"
            value={value.email}
            onChange={(event) => set("email", event.target.value)}
            className={controlClass}
            placeholder="driver@company.com"
          />
        </Field>
        <Field label="Emergency contact" wide>
          <input
            value={value.emergencyContact}
            onChange={(event) => set("emergencyContact", event.target.value)}
            className={controlClass}
            placeholder="Name and phone number"
          />
        </Field>
      </FormSection>

      <FormSection title="Compensation">
        <Field label="Pay basis">
          <select
            value={value.payType}
            onChange={(event) =>
              set(
                "payType",
                event.target.value as DriverInput["payType"],
              )
            }
            className={controlClass}
          >
            <option value="cpm">Cents per mile (CPM)</option>
            <option value="gross_percentage">Percentage of gross</option>
          </select>
        </Field>
        <Field
          label={value.payType === "cpm" ? "Rate per mile ($)" : "Gross percentage (%)"}
          hint={value.payType === "cpm" ? "Example: 0.65 means 65 cents per mile" : "Enter a value from 0 to 100"}
        >
          <input
            required
            type="number"
            min="0"
            max={value.payType === "gross_percentage" ? "100" : undefined}
            step="0.0001"
            value={value.payRate}
            onChange={(event) => set("payRate", Number(event.target.value))}
            className={controlClass}
          />
        </Field>
      </FormSection>

      <FormSection title="Assignments">
        <Field label="Dispatcher">
          <select
            value={value.dispatcherId ?? ""}
            onChange={(event) =>
              set(
                "dispatcherId",
                event.target.value || null,
              )
            }
            className={controlClass}
          >
            <option value="">Unassigned</option>
            {dispatchers.map((dispatcher) => (
              <option key={dispatcher.id} value={dispatcher.id}>
                {dispatcher.fullName}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Truck">
          <select
            value={value.truckId ?? ""}
            onChange={(event) =>
              set("truckId", event.target.value || null)
            }
            className={controlClass}
          >
            <option value="">Unassigned</option>
            {trucks.map((truck) => (
              <option key={truck.id} value={truck.id}>
                {truck.unitNumber}
                {truck.driverName ? ` — currently ${truck.driverName}` : ""}
              </option>
            ))}
          </select>
        </Field>
      </FormSection>

      <FormSection title="License and address">
        <Field label="CDL / license number">
          <input
            value={value.licenseNumber}
            onChange={(event) => set("licenseNumber", event.target.value)}
            className={controlClass}
          />
        </Field>
        <Field label="License state">
          <input
            value={value.licenseState}
            onChange={(event) => set("licenseState", event.target.value)}
            className={controlClass}
            maxLength={2}
            placeholder="IL"
          />
        </Field>
        <Field label="License expiration">
          <input
            type="date"
            value={value.licenseExpires}
            onChange={(event) => set("licenseExpires", event.target.value)}
            className={controlClass}
          />
        </Field>
        <div className="hidden sm:block" />
        <Field label="Street address" wide>
          <input
            value={value.address}
            onChange={(event) => set("address", event.target.value)}
            className={controlClass}
          />
        </Field>
        <Field label="City">
          <input
            value={value.city}
            onChange={(event) => set("city", event.target.value)}
            className={controlClass}
          />
        </Field>
        <Field label="State / province">
          <input
            value={value.state}
            onChange={(event) => set("state", event.target.value)}
            className={controlClass}
          />
        </Field>
        <Field label="Postal code">
          <input
            value={value.postalCode}
            onChange={(event) => set("postalCode", event.target.value)}
            className={controlClass}
          />
        </Field>
      </FormSection>

      <FormSection title="Status and notes">
        <Toggle
          checked={value.active}
          onChange={(checked) => set("active", checked)}
          label="Active driver"
          description="Inactive drivers stay in historical records but are visually marked."
        />
        <Field label="Internal notes" wide>
          <textarea
            rows={3}
            value={value.notes}
            onChange={(event) => set("notes", event.target.value)}
            className={controlClass}
            placeholder="Optional notes about this driver"
          />
        </Field>
      </FormSection>
    </div>
  );
}
