"use client";

import type { Dispatcher, DispatcherInput, Driver } from "../lib/types";
import {
  controlClass,
  Field,
  FormSection,
  Toggle,
} from "../components/management/ManagementUI";

export const emptyDispatcherInput: DispatcherInput = {
  fullName: "",
  email: "",
  phone: "",
  payPercentage: null,
  driverIds: [],
  active: true,
  notes: "",
};

export function dispatcherToInput(
  dispatcher: Dispatcher,
  drivers: Driver[],
): DispatcherInput {
  return {
    fullName: dispatcher.fullName,
    email: dispatcher.email ?? "",
    phone: dispatcher.phone ?? "",
    payPercentage: dispatcher.payPercentage,
    driverIds: drivers
      .filter((driver) => driver.dispatcherId === dispatcher.id)
      .map((driver) => driver.id),
    active: dispatcher.active,
    notes: dispatcher.notes ?? "",
  };
}

export function DispatcherForm({
  value,
  drivers,
  dispatcherId,
  onChange,
}: {
  value: DispatcherInput;
  drivers: Driver[];
  dispatcherId: string | null;
  onChange: (value: DispatcherInput) => void;
}) {
  const set = <K extends keyof DispatcherInput>(
    key: K,
    next: DispatcherInput[K],
  ) => onChange({ ...value, [key]: next });

  const toggleDriver = (id: string) => {
    const selected = value.driverIds.includes(id);
    set(
      "driverIds",
      selected
        ? value.driverIds.filter((driverId) => driverId !== id)
        : [...value.driverIds, id],
    );
  };

  return (
    <div className="space-y-6">
      <FormSection title="Dispatcher profile">
        <Field label="Full name" wide>
          <input
            required
            autoFocus
            value={value.fullName}
            onChange={(event) => set("fullName", event.target.value)}
            className={controlClass}
            placeholder="e.g. Dana Mitchell"
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
            placeholder="dispatcher@company.com"
          />
        </Field>
        <Field
          label="Commission from gross (%)"
          hint="Optional dispatcher compensation percentage, from 0 to 100."
        >
          <input
            type="number"
            min="0"
            max="100"
            step="0.01"
            value={value.payPercentage ?? ""}
            onChange={(event) =>
              set(
                "payPercentage",
                event.target.value === "" ? null : Number(event.target.value),
              )
            }
            className={controlClass}
          />
        </Field>
      </FormSection>

      <FormSection title="Assigned drivers">
        <div className="sm:col-span-2">
          {drivers.length === 0 ? (
            <div className="rounded-lg border border-dashed border-zinc-800 px-4 py-6 text-center text-[12px] text-zinc-600">
              Add drivers before assigning them to a dispatcher.
            </div>
          ) : (
            <div className="grid max-h-52 grid-cols-1 gap-2 overflow-y-auto rounded-lg border border-zinc-800/70 bg-zinc-950/30 p-2 sm:grid-cols-2">
              {drivers.map((driver) => {
                const assignedElsewhere =
                  driver.dispatcherId !== null &&
                  driver.dispatcherId !== dispatcherId;
                return (
                  <label
                    key={driver.id}
                    className="flex cursor-pointer items-start gap-2.5 rounded-md px-2.5 py-2 transition hover:bg-zinc-800/50"
                  >
                    <input
                      type="checkbox"
                      checked={value.driverIds.includes(driver.id)}
                      onChange={() => toggleDriver(driver.id)}
                      className="mt-0.5 h-4 w-4 rounded border-zinc-700 bg-zinc-900 accent-blue-600"
                    />
                    <span>
                      <span className="block text-[13px] text-zinc-300">
                        {driver.fullName}
                      </span>
                      <span className="block text-[11px] text-zinc-600">
                        {assignedElsewhere
                          ? `Currently ${driver.dispatcherName}; selecting will reassign`
                          : driver.truckUnit
                            ? `Truck ${driver.truckUnit}`
                            : "No truck assigned"}
                      </span>
                    </span>
                  </label>
                );
              })}
            </div>
          )}
        </div>
      </FormSection>

      <FormSection title="Status and notes">
        <Toggle
          checked={value.active}
          onChange={(checked) => set("active", checked)}
          label="Active dispatcher"
          description="Inactive dispatchers remain connected to historical records."
        />
        <Field label="Internal notes" wide>
          <textarea
            rows={3}
            value={value.notes}
            onChange={(event) => set("notes", event.target.value)}
            className={controlClass}
            placeholder="Optional notes about this dispatcher"
          />
        </Field>
      </FormSection>
    </div>
  );
}
