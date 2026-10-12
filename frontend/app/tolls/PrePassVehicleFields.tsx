"use client";

import type { Dispatch, SetStateAction } from "react";
import type { PrePassFleetTruck, PrePassVehicleInput } from "../lib/types";
import { controlClass, Field } from "../components/management/ManagementUI";

const stateCodes = new Set("AL AK AZ AR CA CO CT DE FL GA HI ID IL IN IA KS KY LA ME MD MA MI MN MS MO MT NE NV NH NJ NM NY NC ND OH OK OR PA RI SC SD TN TX UT VT VA WA WV WI WY AB BC MB NB NL NS NT ON PE QC SK YT AG BS CH CI CL CM CU DF DU GE GJ HD JA MC MR MX NA NU OA PU QE QI SI SL SO TB TL TM VE YU BN TA".split(" "));
export const validPrePassVIN = (vin: string) => /^[A-Z0-9]{16,19}$/.test(vin.trim().toUpperCase());

export function prePassVehicleFromTruck(truck?: PrePassFleetTruck): PrePassVehicleInput {
  return {
    vehicleNumber: truck?.unit.trim() ?? "",
    licensePlate: { temporary: false, number: truck?.plate.trim().toUpperCase() ?? "", state: truck?.plateState.trim().toUpperCase() ?? "" },
    declaredWeight: Number.NaN,
    irp: { accountNumber: "", state: "" },
    ifta: { accountNumber: "", state: "" },
    vrn: "",
    hazmatHauler: false,
  };
}

// Add asks only for missing/unsupported values. Edit retains the full form.
export function PrePassVehicleFields({ truck, vehicle, setVehicle, vin, setVIN }: {
  truck?: PrePassFleetTruck;
  vehicle: PrePassVehicleInput;
  setVehicle: Dispatch<SetStateAction<PrePassVehicleInput>>;
  vin: string;
  setVIN: (vin: string) => void;
}) {
  const known = {
    unit: !!truck && /^[A-Za-z0-9-]{1,8}$/.test(truck.unit.trim()),
    plate: !!truck && /^[A-Z0-9-]{5,9}$/.test(truck.plate.trim().toUpperCase()),
    state: !!truck && stateCodes.has(truck.plateState.trim().toUpperCase()),
  };
  const field = (name: "unit" | "plate" | "state") => {
    switch (name) {
      case "unit": return <Field key={name} label="PrePass unit number" hint="Up to 8 letters, digits or hyphens. MSERP’s unit name stays unchanged."><input className={controlClass} required maxLength={8} pattern="[A-Za-z0-9\-]{1,8}" value={vehicle.vehicleNumber} onChange={e => setVehicle(v => ({ ...v, vehicleNumber: e.target.value }))} /></Field>;
      case "plate": return <Field key={name} label="Plate number"><input className={controlClass} required={!vehicle.licensePlate.temporary} maxLength={9} pattern="[A-Z0-9\-]{5,9}" value={vehicle.licensePlate.number} onChange={e => setVehicle(v => ({ ...v, licensePlate: { ...v.licensePlate, number: e.target.value.toUpperCase() } }))} /></Field>;
      case "state": return <Field key={name} label="Plate state"><input className={controlClass} required minLength={2} maxLength={2} value={vehicle.licensePlate.state} onChange={e => setVehicle(v => ({ ...v, licensePlate: { ...v.licensePlate, state: e.target.value.toUpperCase() } }))} /></Field>;
    }
  };
  const optionalFields = <>
    <label className="flex items-center gap-2 text-[13px]"><input type="checkbox" checked={vehicle.licensePlate.temporary} onChange={e => setVehicle(v => ({ ...v, licensePlate: { ...v.licensePlate, temporary: e.target.checked } }))} />Temporary plate</label>
    <Field label="Vehicle reference number (VRN)"><input className={controlClass} maxLength={20} value={vehicle.vrn} onChange={e => setVehicle(v => ({ ...v, vrn: e.target.value }))} /></Field>
    <label className="flex items-center gap-2 text-[13px]"><input type="checkbox" checked={vehicle.hazmatHauler} onChange={e => setVehicle(v => ({ ...v, hazmatHauler: e.target.checked }))} />Hazmat hauler</label>
  </>;

  return <div className="space-y-4">
    {truck && <>
      <div className="rounded-lg border border-zinc-800 p-3 text-[13px]" aria-label="Saved truck details">
        <p className="mb-2 font-medium">Using saved truck details</p>
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
          <dt className="text-zinc-500">Unit</dt><dd className="break-all">{known.unit ? vehicle.vehicleNumber : `${truck.unit || "Missing"} · PrePass unit needed`}</dd>
          <dt className="text-zinc-500">VIN</dt><dd className="break-all font-mono">{truck.vin.trim() || "Missing"}</dd>
          <dt className="text-zinc-500">Plate</dt><dd className="break-all">{[known.plate ? vehicle.licensePlate.number : "Number needed", known.state ? vehicle.licensePlate.state : "State needed"].join(" · ")}</dd>
        </dl>
      </div>
      <p className="text-[13px] text-zinc-400">Complete the details MSERP doesn’t have for this truck.</p>
    </>}
    <div className="grid gap-4 sm:grid-cols-2">
      {truck && !truck.vin.trim() && <Field label="VIN" hint="Used for this PrePass enrollment; the MSERP truck profile stays unchanged."><input className={controlClass} required minLength={16} maxLength={19} pattern="[A-Z0-9]{16,19}" value={vin} onChange={e => setVIN(e.target.value.toUpperCase())} /></Field>}
      {(["unit", "plate", "state"] as const).filter(name => !known[name]).map(field)}
      <Field label="Declared weight (lb)"><input className={controlClass} type="number" required min={0} max={600000} step={1} value={Number.isFinite(vehicle.declaredWeight) ? vehicle.declaredWeight : ""} onChange={e => setVehicle(v => ({ ...v, declaredWeight: e.target.value === "" ? Number.NaN : Number(e.target.value) }))} /></Field>
      {(["irp", "ifta"] as const).map(kind => <div key={kind} className="contents">
        <Field label={`${kind.toUpperCase()} account number`}><input className={controlClass} required maxLength={10} value={vehicle[kind].accountNumber} onChange={e => setVehicle(v => ({ ...v, [kind]: { ...v[kind], accountNumber: e.target.value } }))} /></Field>
        <Field label={`${kind.toUpperCase()} state`}><input className={controlClass} required minLength={2} maxLength={2} value={vehicle[kind].state} onChange={e => setVehicle(v => ({ ...v, [kind]: { ...v[kind], state: e.target.value.toUpperCase() } }))} /></Field>
      </div>)}
      {!truck && optionalFields}
    </div>
    {truck && <details className="rounded-lg border border-zinc-800 p-3" onInvalidCapture={event => { event.currentTarget.open = true; }}>
      <summary className="cursor-pointer text-[13px] text-zinc-400">Review or change other details</summary>
      <div className="mt-4 grid gap-4 sm:grid-cols-2">
        {(["unit", "plate", "state"] as const).filter(name => known[name]).map(field)}
        {optionalFields}
      </div>
    </details>}
  </div>;
}
