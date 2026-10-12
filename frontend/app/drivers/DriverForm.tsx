"use client";

import { AssignmentWeekField } from "../components/management/AssignmentWeekField";

import { currentChargeWeek, validChargeWeek } from "@/app/accounting/driver-charges/charges";
import { useEffect, useState } from "react";
import { usePermissions } from "../lib/access";
import { ExternalLink, FileBadge, LoaderCircle, Sparkles, Upload, X } from "lucide-react";
import { PhoneInput } from "../components/management/PhoneInput";

import type { Dispatcher, Driver, DriverInput, DriverTeamTruck, Truck } from "../lib/types";
import { fileDownloadUrl, fetchDriverTeamTrucks } from "../lib/api";
import {
  controlClass,
  Field,
  FormSection,
} from "../components/management/ManagementUI";

export const emptyDriverInput: DriverInput = {
  fullName: "",
  driverHome: "",
  homeVersion: 0,
  escrowAmount: "2500.00",
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
  status: "active",
  terminationDate: "",
  notes: "",
  cdlFileId: null,
};

export function driverToInput(driver: Driver): DriverInput {
  return {
    isTeamDriver: driver.teamRole === "team" ? true : undefined,
    teamVersion: driver.teamVersion,
    teamMainDriverId: driver.teamRole === "team" ? driver.teamPartnerId : undefined,
    assignmentWeek: driver.teamRole === "team" && driver.teamStartWeek && driver.teamStartWeek > currentChargeWeek() ? driver.teamStartWeek : undefined,
    fullName: driver.fullName,
    driverHome: driver.driverHome ?? "",
    homeVersion: driver.homeVersion ?? 0,
    escrowAmount: "",
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
    truckId: driver.teamRole === "team" ? driver.teamTruckId ?? null : driver.truckId,
    active: driver.active,
    status: driver.status ?? (driver.active ? "active" : "terminated"),
    terminationDate: driver.terminationDate?.slice(0,10) ?? "",
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
  showEscrow = false,
  editingDriverId,
}: {
  value: DriverInput;
  dispatchers: Dispatcher[];
  trucks: Truck[];
  onChange: (value: DriverInput) => void;
  cdlFileName: string | null;
  isUploadingCDL: boolean;
  onUploadCDL: (file: File) => Promise<void>;
  onRemoveCDL: () => void;
  showEscrow?: boolean;
  editingDriverId?: string;
}) {
  const permissions = usePermissions();
  const isTeam = !!value.isTeamDriver;
  const [choices, setChoices] = useState<{ week: string; rows: DriverTeamTruck[]; error: string } | null>(null);
  const [attempt, setAttempt] = useState(0);
  let teamWeek = value.assignmentWeek || currentChargeWeek();
  if (showEscrow && value.hireDate) {
    const date = new Date(`${value.hireDate}T00:00:00Z`);
    if (Number.isFinite(date.getTime())) { date.setUTCDate(date.getUTCDate() - (date.getUTCDay() + 6) % 7); teamWeek = date.toISOString().slice(0, 10); }
  }
  useEffect(() => {
    if (!isTeam || !validChargeWeek(teamWeek)) return;
    let cancelled = false;
    const controller = new AbortController();
    fetchDriverTeamTrucks(teamWeek, controller.signal).then(rows => { if (!cancelled) setChoices({ week: teamWeek, rows, error: "" }); }).catch(e => { if (!cancelled) setChoices({ week: teamWeek, rows: [], error: e.message }); });
    return () => { cancelled = true; controller.abort(); };
  }, [isTeam, teamWeek, attempt]);
  const teamChoices = choices?.week === teamWeek ? choices.rows.filter(row => row.mainDriverId !== editingDriverId && (!row.teamDriverId || row.teamDriverId === editingDriverId)) : [];
  const set = <K extends keyof DriverInput>(key: K, next: DriverInput[K]) =>
    onChange({ ...value, [key]: next, ...(isTeam && showEscrow && key === "hireDate" ? { truckId: null, teamMainDriverId: undefined, teamMainVersion: undefined } : {}), ...(!showEscrow && ["dispatcherId","truckId"].includes(key) ? { assignmentWeek: value.assignmentWeek ?? "" } : {}) });

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

      <section className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-x-3 border-b border-zinc-800/50 pb-2">
          <h3 className="text-[12px] font-semibold uppercase tracking-wider text-zinc-500">Driver profile</h3>
          <div className="flex items-center gap-2 whitespace-nowrap text-[12px]">
            <span className={isTeam ? "text-zinc-500" : "font-medium text-zinc-200"}>Solo driver</span>
            <button
              type="button"
              role="switch"
              aria-label="Team driver"
              aria-checked={isTeam}
              disabled={!permissions.includes("payroll.write")}
              title={!permissions.includes("payroll.write") ? "Payroll write permission is required to change teams" : undefined}
              onClick={() => onChange({ ...value, isTeamDriver: !isTeam, truckId: null, dispatcherId: null, teamMainDriverId: undefined, teamMainVersion: undefined, ...(!showEscrow ? { assignmentWeek: value.assignmentWeek ?? "" } : {}) })}
              className="relative h-8 w-9 shrink-0 rounded-full focus-visible:outline-2 focus-visible:outline-blue-500 disabled:opacity-50"
            >
              <span className={`absolute inset-x-0 top-1.5 h-5 rounded-full transition-colors ${isTeam ? "bg-blue-600" : "bg-zinc-700"}`}>
                <span className={`absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-white transition-transform ${isTeam ? "translate-x-4" : ""}`} />
              </span>
            </button>
            <span className={isTeam ? "font-medium text-blue-400" : "text-zinc-500"}>Team driver</span>
          </div>
        </div>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
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
        <Field label={showEscrow ? "Driver started working" : "Hire date"} hint={showEscrow ? isTeam ? "Team membership starts in this date’s Monday–Sunday week." : "Truck, dispatcher and roster assignments start in this date’s Monday–Sunday week. Escrow starts on this date." : undefined}>
          <input
            type="date"
            aria-label={showEscrow ? "Driver started working" : "Hire date"}
            required={showEscrow}
            value={value.hireDate}
            onChange={(event) => set("hireDate", event.target.value)}
            className={controlClass}
          />
        </Field>
        <Field label="Phone">
          <PhoneInput value={value.phone} onChange={(phone) => set("phone", phone)} />
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
        </div>
      </section>

      {!isTeam && <FormSection title="Compensation">
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
        {showEscrow && <Field label="Escrow amount ($)" hint="Creates a driver escrow account. The unpaid balance carries into weekly Driver Pay.">
          <input
            required
            type="number"
            min="0.01"
            step="0.01"
            value={value.escrowAmount}
            onChange={(event) => set("escrowAmount", event.target.value)}
            className={controlClass}
          />
        </Field>}
      </FormSection>}

      <FormSection title="Assignments">
        {!showEscrow && <AssignmentWeekField value={value.assignmentWeek} onChange={week => onChange({ ...value, assignmentWeek: week, ...(isTeam ? { truckId: null, teamMainDriverId: undefined, teamMainVersion: undefined } : {}) })} />}
        {!isTeam && <Field label="Dispatcher">
          <select
            value={value.dispatcherId ?? ""}
            disabled={!value.active}
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
        </Field>}
        <Field label="Truck" hint={isTeam && choices?.week === teamWeek && !choices.error && teamChoices.length === 0 ? "No available trucks with a main driver in this week." : undefined}>
          <select
            aria-label="Truck"
            required={isTeam && value.active}
            value={value.truckId ?? ""}
            disabled={!value.active || (isTeam && !permissions.includes("payroll.write"))}
            onChange={(event) => { const selected = teamChoices.find(row => row.truckId === event.target.value); if (isTeam) onChange({ ...value, truckId: event.target.value || null, teamMainDriverId: selected?.mainDriverId, teamMainVersion: selected?.mainVersion, dispatcherId: selected?.dispatcherId ?? null, ...(!showEscrow ? { assignmentWeek: value.assignmentWeek ?? "" } : {}) }); else set("truckId", event.target.value || null); }}
            className={controlClass}
          >
            <option value="">{isTeam ? choices?.week === teamWeek ? "Select truck" : "Loading trucks…" : "Unassigned"}</option>
            {isTeam ? teamChoices.map(row => <option key={row.truckId} value={row.truckId}>{row.unitNumber} — {row.mainDriverName}</option>) : trucks.filter((truck) => truck.active || truck.id === value.truckId).map((truck) => (
              <option key={truck.id} value={truck.id}>
                {truck.unitNumber}
                {truck.driverName ? ` — currently ${truck.driverName}` : ""}
              </option>
            ))}
          </select>
        </Field>
        {isTeam && choices?.week === teamWeek && choices.error && <p role="alert" className="text-xs text-red-400">{choices.error} <button type="button" className="text-blue-400" onClick={() => setAttempt(v => v + 1)}>Retry trucks</button></p>}
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
        <Field label="Driver home" wide>
          <input value={value.driverHome ?? ""} onChange={(event) => set("driverHome", event.target.value)} maxLength={300} placeholder="City, state" className={controlClass} />
        </Field>
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
        <Field label="Driver status"><select aria-label="Driver status" className={controlClass} value={value.status ?? (value.active ? "active" : "terminated")} onChange={event => {
          const status = event.target.value as NonNullable<DriverInput["status"]>;
          const active = status !== "terminated";
          onChange({ ...value, status, active, terminationDate: active ? "" : value.terminationDate || new Intl.DateTimeFormat("en-CA", { timeZone: "America/New_York" }).format(new Date()), chargePauseWeek: value.chargePauseWeek ?? currentChargeWeek(), ...(!active ? { truckId: null, dispatcherId: null, assignmentWeek: value.assignmentWeek || currentChargeWeek() } : {}) });
        }}>{["active", "vacation", "home", "terminated"].map(status => <option key={status} value={status}>{status[0].toUpperCase() + status.slice(1)}</option>)}</select></Field>
        {!value.active && <Field label="Termination date"><input type="date" required className={controlClass} value={value.terminationDate ?? ""} onChange={event => set("terminationDate", event.target.value)} /></Field>}
        {!value.active && <Field label="Pause charges from (Monday)">
          <input type="date" min={currentChargeWeek()} required value={value.chargePauseWeek ?? currentChargeWeek()} onChange={event => set("chargePauseWeek", event.target.value)} className={controlClass} />
          <p className="mt-1 text-xs text-zinc-500">Charges pause from this week. Outstanding balances remain; reactivation does not resume charges.</p>
        </Field>}
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
