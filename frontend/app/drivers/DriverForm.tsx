"use client";
import type { TruckOption, DispatcherOption } from "@/app/lib/types";

import { emptyTruckInput } from "../trucks/TruckForm";
import { AssignmentWeekField } from "../components/management/AssignmentWeekField";

import { currentChargeWeek, validChargeWeek } from "@/app/accounting/driver-charges/charges";
import { useEffect, useState } from "react";
import { usePermissions } from "../lib/access";
import { PhoneInput } from "../components/management/PhoneInput";

import type { Driver, DriverInput, DriverTeamTruck } from "../lib/types";
import { fetchDriverTeamTrucks } from "../lib/api";
import {
  controlClass,
  Field,
  FormSection,
} from "../components/management/ManagementUI";

function startWeek(date: string) {
  const day = new Date(`${date}T00:00:00Z`);
  if (!Number.isFinite(day.getTime())) return currentChargeWeek();
  day.setUTCDate(day.getUTCDate() - (day.getUTCDay() + 6) % 7);
  return day.toISOString().slice(0, 10);
}

export const emptyDriverInput: DriverInput = {
  fullName: "",
  firstName: "",
  lastName: "",
  escrowAmount: "2500.00",
  payType: "cpm",
  payRate: 0,
  phone: "",
  email: "",
  licenseNumber: "",
  licenseState: "",
  licenseExpires: "",
  hireDate: "",
	dateOfBirth: "",
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
	statusVersion:driver.statusVersion,
    isTeamDriver: driver.teamRole === "team" ? true : undefined,
    teamVersion: driver.teamVersion,
    teamMainDriverId: driver.teamRole === "team" ? driver.teamPartnerId : undefined,
    assignmentWeek: driver.teamRole === "team" && driver.teamStartWeek && driver.teamStartWeek > currentChargeWeek() ? driver.teamStartWeek : undefined,
    fullName: driver.fullName,
    firstName: driver.firstName ?? undefined,
    lastName: driver.lastName ?? undefined,
    escrowAmount: "",
    payType: driver.payType ?? "cpm",
    payRate: driver.payRate ?? 0,
    phone: driver.phone ?? "",
    email: driver.email ?? "",
    licenseNumber: driver.licenseNumber ?? "",
    licenseState: driver.licenseState ?? "",
    licenseExpires: driver.licenseExpires?.slice(0, 10) ?? "",
    hireDate: driver.hireDate?.slice(0, 10) ?? "",
	dateOfBirth: driver.dateOfBirth?.slice(0, 10) ?? "",
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
    cdlFileId: driver.cdlFileId ?? null,
  };
}

export function DriverForm({
  value,
  dispatchers,
  trucks,
  onChange,
  showEscrow = false,
  editingDriverId,
  section = "all",
  allowNewTruck = false,
}: {
  value: DriverInput;
  dispatchers: DispatcherOption[];
  trucks: TruckOption[];
  onChange: (value: DriverInput) => void;
  showEscrow?: boolean;
  editingDriverId?: string;
  section?: "all" | "information" | "assignments" | "finalize";
  allowNewTruck?: boolean;
}) {
  const permissions = usePermissions();
  const canCreateTruck = allowNewTruck && permissions.includes("trucks.create");
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
    onChange({ ...value, [key]: next, ...(isTeam && showEscrow && key === "hireDate" && startWeek(String(next)) !== teamWeek ? { truckId: null, teamMainDriverId: undefined, teamMainVersion: undefined } : {}), ...(!showEscrow && ["dispatcherId","truckId"].includes(key) ? { assignmentWeek: value.assignmentWeek ?? "" } : {}) });

  const setName = (key: "firstName" | "lastName", text: string) => {
    const next = { ...value, firstName: value.firstName ?? "", lastName: value.lastName ?? "", [key]: text };
    next.fullName = `${next.firstName} ${next.lastName}`.trim().replace(/\s+/g, " ");
    onChange(next);
  };
  const requireNameParts = !editingDriverId || value.firstName !== undefined || value.lastName !== undefined;

  return (
    <div className="space-y-6">
      {(section === "all" || section === "information") && <><section className="space-y-3">
        {section === "all" && <div className="flex flex-wrap items-center justify-between gap-x-3 border-b border-zinc-800/50 pb-2">
          <h3 className="text-[12px] font-semibold uppercase tracking-wider text-zinc-500">Driver profile</h3>
        </div>}
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {value.firstName === undefined && value.lastName === undefined && <p className="text-sm text-zinc-500 sm:col-span-2">Recorded name: <strong>{value.fullName}</strong>. Confirm the given and family names below when known. Other details can be saved without splitting the name.</p>}
        {!editingDriverId && !value.firstName && !value.lastName && value.fullName && <p className="text-sm text-zinc-500 sm:col-span-2">Imported name: <strong>{value.fullName}</strong>. Confirm its given and family names.</p>}
        <Field label="First name">
          <input
            required={requireNameParts}
            autoFocus
            autoComplete="given-name"
            maxLength={50}
            value={value.firstName ?? ""}
            onChange={(event) => setName("firstName", event.target.value)}
            className={controlClass}
            placeholder="e.g. Marcus"
          />
        </Field>
        <Field label="Last name">
          <input required={requireNameParts} autoComplete="family-name" maxLength={50}
            value={value.lastName ?? ""} onChange={(event) => setName("lastName", event.target.value)}
            className={controlClass} placeholder="e.g. Reed" />
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
        {permissions.includes("drivers.private.read") && <fieldset className="contents" disabled={!permissions.includes("drivers.private.edit")}><Field label="Emergency contact" wide>
          <input
            value={value.emergencyContact}
            onChange={(event) => set("emergencyContact", event.target.value)}
            className={controlClass}
            placeholder="Name and phone number"
          />
        </Field></fieldset>}
        </div>
      </section>

      {permissions.includes("drivers.private.read") && <fieldset disabled={!permissions.includes("drivers.private.edit")}><FormSection title="License and address">
        <Field label="Date of birth"><input type="date" value={value.dateOfBirth ?? ""} onChange={event => set("dateOfBirth", event.target.value)} className={controlClass} /></Field>
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
      </FormSection></fieldset>}

      <FormSection title="Notes">        <Field label="Internal notes" wide>
          <textarea
            rows={3}
            value={value.notes}
            onChange={(event) => set("notes", event.target.value)}
            className={controlClass}
            placeholder="Optional notes about this driver"
          />
        </Field></FormSection></>}
      {(section === "all" || section === "assignments") && <><div className="mb-4 flex justify-end">          <div className="flex items-center gap-2 whitespace-nowrap text-[12px]">
            <span className={isTeam ? "text-zinc-500" : "font-medium text-zinc-200"}>Solo driver</span>
            <button
              type="button"
              role="switch"
              aria-label="Team driver"
              aria-checked={isTeam}
              disabled={!permissions.includes("driver_pay.edit")}
              title={!permissions.includes("driver_pay.edit") ? "Payroll write permission is required to change teams" : undefined}
              onClick={() => onChange({ ...value, isTeamDriver: !isTeam, newTruck: !isTeam ? undefined : canCreateTruck ? { ...emptyTruckInput } : undefined, newTruckOwnedByDriver: false, truckId: null, dispatcherId: null, teamMainDriverId: undefined, teamMainVersion: undefined, ...(!showEscrow ? { assignmentWeek: value.assignmentWeek ?? "" } : {}) })}
              className="relative h-8 w-9 shrink-0 rounded-full focus-visible:outline-2 focus-visible:outline-blue-500 disabled:opacity-50"
            >
              <span className={`absolute inset-x-0 top-1.5 h-5 rounded-full transition-colors ${isTeam ? "bg-blue-600" : "bg-zinc-700"}`}>
                <span className={`absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-white transition-transform ${isTeam ? "translate-x-4" : ""}`} />
              </span>
            </button>
            <span className={isTeam ? "font-medium text-blue-400" : "text-zinc-500"}>Team driver</span>
          </div></div><FormSection title="Assignments" hideTitle={section === "assignments"}>
        {!showEscrow && <AssignmentWeekField team={value.isTeamDriver !== undefined} value={value.assignmentWeek} onChange={week => onChange({ ...value, assignmentWeek: week, ...(isTeam ? { truckId: null, teamMainDriverId: undefined, teamMainVersion: undefined } : {}) })} />}
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
            value={value.newTruck && !isTeam ? "__new__" : value.truckId ?? ""}
            disabled={!value.active || (isTeam && !permissions.includes("driver_pay.edit"))}
            onChange={(event) => { if (event.target.value === "__new__") { onChange({ ...value, truckId: null, newTruck: { ...emptyTruckInput }, newTruckOwnedByDriver: false }); return; } const selected = teamChoices.find(row => row.truckId === event.target.value); if (isTeam) onChange({ ...value, truckId: event.target.value || null, teamMainDriverId: selected?.mainDriverId, teamMainVersion: selected?.mainVersion, dispatcherId: selected?.dispatcherId ?? null, ...(!showEscrow ? { assignmentWeek: value.assignmentWeek ?? "" } : {}) }); else onChange({ ...value, truckId: event.target.value || null, newTruck: undefined, newTruckOwnedByDriver: false, ...(!showEscrow ? { assignmentWeek: value.assignmentWeek ?? "" } : {}) }); }}
            className={controlClass}
          >
            {canCreateTruck && !isTeam && <option value="__new__">New truck</option>}
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

      {!isTeam && permissions.includes("driver_compensation.read") && <fieldset disabled={!permissions.includes("driver_compensation.edit")}><FormSection title="Compensation">
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
        {showEscrow && <Field label="Escrow amount ($)">
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
      </FormSection></fieldset>}

      </>}
      {(section === "all" || section === "finalize") && <FormSection title="Finalize" hideTitle={section === "finalize"}>        <Field label={showEscrow ? "Driver started working" : "Hire date"}>
          <input
            type="date"
            aria-label={showEscrow ? "Driver started working" : "Hire date"}
            required={showEscrow}
            value={value.hireDate}
            onChange={(event) => set("hireDate", event.target.value)}
            className={controlClass}
          />
        </Field>

        <Field label="Driver status"><select aria-label="Driver status" className={controlClass} value={value.status ?? (value.active ? "active" : "terminated")} onChange={event => {
          const status = event.target.value as NonNullable<DriverInput["status"]>;
          const active = status !== "terminated";
          onChange({ ...value, status, active, terminationDate: active ? "" : value.terminationDate || new Intl.DateTimeFormat("en-CA", { timeZone: "America/New_York" }).format(new Date()), chargePauseWeek: value.chargePauseWeek ?? currentChargeWeek(), ...(!active ? { newTruck: undefined, newTruckOwnedByDriver: false, truckId: null, dispatcherId: null, assignmentWeek: value.assignmentWeek || currentChargeWeek() } : {}) });
        }}>{["active", "vacation", "home", "terminated"].map(status => <option key={status} value={status}>{status[0].toUpperCase() + status.slice(1)}</option>)}</select></Field>
        {!value.active && <Field label="Termination date"><input type="date" required className={controlClass} value={value.terminationDate ?? ""} onChange={event => set("terminationDate", event.target.value)} /></Field>}
        {!value.active && <Field label="Pause charges from (Monday)">
          <input type="date" min={currentChargeWeek()} required value={value.chargePauseWeek ?? currentChargeWeek()} onChange={event => set("chargePauseWeek", event.target.value)} className={controlClass} />
          <p className="mt-1 text-xs text-zinc-500">Charges pause from this week. Outstanding balances remain; reactivation does not resume charges.</p>
        </Field>}

      </FormSection>}
    </div>
  );
}
