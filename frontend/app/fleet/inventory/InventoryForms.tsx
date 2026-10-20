"use client";

import { useState } from "react";
import {
  ErrorBanner,
  Field,
  Modal,
  controlClass,
} from "@/app/components/management/ManagementUI";
import { saveInventoryRecord } from "@/app/lib/api";
import { formatPhone } from "@/app/lib/phone";
import type {
  InventoryCollection,
  InventoryData,
  InventoryFields,
  InventoryRecord,
  TruckOption,
} from "@/app/lib/types";
import {
  carrierStates,
  conditionStates,
  deviceStates,
  presenceStates,
  readable,
} from "./InventoryStatus";
import { deviceColors, proposedDetails, unavailable } from "./inventoryView";

export { readable } from "./InventoryStatus";
export const inventoryPhone = (value?: string) =>
  formatPhone(value) || value || "";
export const inventoryLabel = (record?: InventoryRecord) =>
  record
    ? record.data.unitNumber ||
      record.data.name ||
      record.data.imei ||
      record.data.serial ||
      record.data.assetLabel ||
      inventoryPhone(record.data.wirelessNumber) ||
      record.id
    : "Unrecorded";
export const newInventoryRecord = (
  data: Partial<InventoryFields> = {},
): InventoryRecord => ({
  id: crypto.randomUUID(),
  version: 0,
  data: { active: true, ...data },
  updatedAt: "",
  updatedBy: "",
});
export type InventoryContext = { data: InventoryData; trucks: TruckOption[] };
type Descriptor = {
  key: keyof InventoryFields;
  label: string;
  options?: string[];
  date?: boolean;
  required?: boolean;
};
const descriptors: Record<InventoryCollection, Descriptor[]> = {
  assets: [
    {
      key: "deviceType",
      label: "Device Type",
      options: ["tablet", "phone", "dashcam", "samsara_gateway"],
      required: true,
    },
    {
      key: "brand",
      label: "Brand",
      options: ["Samsara", "FiveELD"],
      required: true,
    },
    { key: "imei", label: "IMEI" },
    { key: "serial", label: "Serial" },
    { key: "assetLabel", label: "Internal Asset Label" },
    { key: "make", label: "Make" },
    { key: "model", label: "Model" },
    { key: "modelNumber", label: "Model Number" },
    { key: "storageGb", label: "Storage (GB)" },
    { key: "color", label: "Device Color" },
    { key: "caseColor", label: "Case Color" },
    { key: "condition", label: "Condition", options: conditionStates },
    { key: "status", label: "Lifecycle", options: deviceStates },
    {
      key: "ownership",
      label: "Ownership",
      options: ["unknown", "owned", "rented", "contracted"],
    },
    {
      key: "equipmentState",
      label: "Installation Status",
      options: presenceStates,
    },
  ],
  lines: [
    {
      key: "wirelessNumber",
      label: "Wireless Number (10 Digits)",
      required: true,
    },
    { key: "carrier", label: "Carrier" },
    { key: "iccid", label: "SIM ICCID" },
    { key: "custodian", label: "Carrier User Label" },
    { key: "status", label: "Carrier Status", options: carrierStates },
    { key: "plan", label: "Plan" },
    { key: "activationDate", label: "Activation Date", date: true },
    { key: "contractStart", label: "Contract Start", date: true },
    { key: "contractEnd", label: "Contract End", date: true },
    { key: "sourceUpdated", label: "Source Updated", date: true },
  ],
  locations: [{ key: "name", label: "Location Name", required: true }],
  custodians: [{ key: "name", label: "Custodian Name", required: true }],
  supplies: [
    { key: "name", label: "Supply Name", required: true },
    { key: "batchYear", label: "Year (Annual Stickers)" },
  ],
  equipment: [
    { key: "name", label: "Equipment Name", required: true },
    { key: "batchYear", label: "Sticker Year" },
    {
      key: "equipmentState",
      label: "Presence",
      options: presenceStates,
      required: true,
    },
  ],
  trailers: [
    { key: "unitNumber", label: "Unit Number", required: true },
    { key: "vin", label: "VIN" },
    { key: "year", label: "Year" },
    { key: "make", label: "Make" },
    { key: "plate", label: "Plate" },
    { key: "plateState", label: "Plate State" },
    { key: "storageLocation", label: "Storage Location" },
    { key: "condition", label: "Condition", options: conditionStates },
    {
      key: "status",
      label: "Availability",
      options: ["stored_unused", "needs_review", "ready", "repair", "retired"],
    },
    { key: "usage", label: "Usage Notes" },
    { key: "registrationUrl", label: "Registration HTTPS Link" },
  ],
  "transponder-notes": [
    {
      key: "providerEnvironment",
      label: "Provider Environment",
      required: true,
    },
    { key: "providerAccount", label: "Provider Account", required: true },
    { key: "serial", label: "Exact Transponder Tag", required: true },
    { key: "custodian", label: "Local Custodian" },
    { key: "storageLocation", label: "Local Location" },
    {
      key: "status",
      label: "Local Status",
      options: ["unknown", "active", "inactive", "lost", "retired"],
    },
  ],
};

export function InventoryRecordFields({
  collection,
  value,
  onChange,
  context,
}: {
  collection: InventoryCollection;
  value: InventoryFields;
  onChange: (value: InventoryFields) => void;
  context: InventoryContext;
}) {
  const set = (key: keyof InventoryFields, text: string | boolean) => {
    const next = { ...value, [key]: text };
    if (key === "deviceType") {
      next.brand = text === "dashcam" ? "Samsara" : "";
      next.gatewayId = "";
      next.equipmentState = "unknown";
    }
    if (key === "brand" && text !== "Samsara") {
      next.gatewayId = "";
      next.equipmentState = "unknown";
    }
    onChange(next);
  };
  const assignment = value.gatewayId
    ? "gatewayId"
    : value.truckId
      ? "truckId"
      : value.custodianId
        ? "custodianId"
        : value.custodian
          ? "custodian"
          : value.locationId
            ? "locationId"
            : "";
  const [holder, setHolder] = useState(assignment);
  const gateway = context.data.assets.find(
    (asset) => asset.id === value.gatewayId,
  );
  const canInstall = !!(value.truckId || gateway?.data.truckId);
  const gatewayType = value.deviceType === "samsara_gateway";
  const samsaraCamera =
    value.deviceType === "dashcam" && value.brand === "Samsara";
  const fields = descriptors[collection].filter((field) => {
    if (collection !== "assets") return true;
    if (field.key === "brand") return value.deviceType === "dashcam";
    if (field.key === "imei") return !gatewayType && !samsaraCamera;
    if (field.key === "serial") return gatewayType || samsaraCamera;
    if (field.key === "assetLabel")
      return ["tablet", "phone"].includes(value.deviceType || "");
    if (field.key === "make")
      return !gatewayType && value.deviceType !== "dashcam";
    if (field.key === "model") return !gatewayType;
    if (["storageGb", "caseColor"].includes(field.key))
      return ["tablet", "phone"].includes(value.deviceType || "");
    if (field.key === "modelNumber") return !gatewayType;
    return true;
  });
  const pick = (
    label: string,
    key: keyof InventoryFields,
    records: { id: string; label: string }[],
  ) => (
    <Field label={label}>
      <select
        aria-label={label}
        className={controlClass}
        value={String(value[key] || "")}
        onChange={(event) =>
          onChange({
            ...value,
            [key]: event.target.value,
            ...(collection === "assets" ? { equipmentState: "unknown" } : {}),
          })
        }
      >
        <option value="">Unassigned / Unknown</option>
        {records.map((record) => (
          <option key={record.id} value={record.id}>
            {record.label}
          </option>
        ))}
      </select>
    </Field>
  );
  return (
    <div className="grid gap-4 sm:grid-cols-2">
      {fields.map((field) => (
        <Field key={field.key} label={field.label}>
          {field.options ? (
            <select
              aria-label={field.label}
              className={controlClass}
              required={field.required}
              value={String(
                value[field.key] ||
                  (field.options.includes("unknown") ? "unknown" : ""),
              )}
              onChange={(event) => set(field.key, event.target.value)}
            >
              {!field.options.includes("unknown") && (
                <option value="">Select…</option>
              )}
              {value[field.key] &&
                !field.options.includes(String(value[field.key])) && (
                  <option value={String(value[field.key])}>
                    Current: {readable(String(value[field.key]))}
                  </option>
                )}
              {field.options.map((option) => (
                <option
                  key={option}
                  value={option}
                  disabled={
                    collection === "assets" &&
                    field.key === "equipmentState" &&
                    !canInstall &&
                    ["reported_present", "verified_installed"].includes(option)
                  }
                >
                  {readable(option)}
                </option>
              ))}
            </select>
          ) : (
            <>
              <input
                aria-label={field.label}
                className={controlClass}
                list={
                  ["color", "caseColor"].includes(field.key)
                    ? "inventory-colors"
                    : ["make", "model"].includes(field.key)
                      ? "inventory-" + field.key
                      : undefined
                }
                type={
                  field.date
                    ? "date"
                    : field.key === "storageGb"
                      ? "number"
                      : "text"
                }
                min={field.key === "storageGb" ? 1 : undefined}
                max={field.key === "storageGb" ? 65536 : undefined}
                step={field.key === "storageGb" ? 1 : undefined}
                required={
                  field.required ||
                  (collection === "assets" && field.key === "serial")
                }
                maxLength={
                  ["color", "caseColor", "modelNumber"].includes(field.key)
                    ? 50
                    : field.key === "registrationUrl"
                      ? 2000
                      : 250
                }
                value={String(value[field.key] || "")}
                onChange={(event) => set(field.key, event.target.value)}
              />
            </>
          )}
          {collection === "assets" &&
            field.key === "equipmentState" &&
            !canInstall && (
              <p className="mt-1 text-xs text-zinc-500">
                Assign a truck, or link a gateway assigned to a truck, before
                recording installation.
              </p>
            )}
        </Field>
      ))}
      {collection === "assets" && (
        <>
          <Field label="Assign Device To">
            <select
              aria-label="Assign Device To"
              className={controlClass}
              value={holder === "gatewayId" && !samsaraCamera ? "" : holder}
              onChange={(event) => {
                setHolder(event.target.value);
                onChange({
                  ...value,
                  truckId: "",
                  locationId: "",
                  custodian: "",
                  custodianId: "",
                  gatewayId: "",
                  equipmentState: "unknown",
                });
              }}
            >
              <option value="">Unknown</option>
              <option value="truckId">Truck</option>
              <option value="locationId">Storage Location</option>
              <option value="custodianId">Custodian</option>
              {value.custodian && (
                <option value="custodian">Legacy Custodian</option>
              )}
              {samsaraCamera && (
                <option value="gatewayId">Samsara Gateway</option>
              )}
            </select>
          </Field>
          {holder === "truckId" &&
            pick(
              "Truck",
              "truckId",
              context.trucks.map((truck) => ({
                id: truck.id,
                label: truck.unitNumber,
              })),
            )}
          {holder === "locationId" &&
            pick(
              "Storage Location",
              "locationId",
              context.data.locations
                .filter((item) => item.data.active)
                .map((item) => ({ id: item.id, label: inventoryLabel(item) })),
            )}
          {holder === "gatewayId" &&
            samsaraCamera &&
            pick(
              "Linked Gateway",
              "gatewayId",
              context.data.assets
                .filter(
                  (item) =>
                    !unavailable(item.data) &&
                    item.data.deviceType === "samsara_gateway",
                )
                .map((item) => ({ id: item.id, label: inventoryLabel(item) })),
            )}
          {holder === "custodianId" &&
            pick(
              "Custodian",
              "custodianId",
              context.data.custodians
                .filter((item) => item.data.active)
                .map((item) => ({ id: item.id, label: inventoryLabel(item) })),
            )}
          {holder === "custodian" && (
            <Field label="Custodian">
              <input
                aria-label="Custodian"
                className={controlClass}
                value={value.custodian || ""}
                onChange={(event) => set("custodian", event.target.value)}
              />
            </Field>
          )}
        </>
      )}
      {collection === "lines" &&
        pick(
          "Linked Tablet / Phone",
          "assetId",
          context.data.assets
            .filter(
              (item) =>
                !unavailable(item.data) &&
                ["tablet", "phone"].includes(item.data.deviceType || ""),
            )
            .map((item) => ({
              id: item.id,
              label: [inventoryLabel(item), item.data.make, item.data.model]
                .filter(Boolean)
                .join(" · "),
            })),
        )}
      {collection === "equipment" && (
        <>
          {pick(
            "Truck",
            "truckId",
            context.trucks.map((truck) => ({
              id: truck.id,
              label: truck.unitNumber,
            })),
          )}
          {pick(
            "Supply Batch",
            "supplyId",
            context.data.supplies
              .filter(
                (s) =>
                  s.data.active &&
                  s.data.name === value.name &&
                  (s.data.batchYear || "") === (value.batchYear || ""),
              )
              .map((s) => ({
                id: s.id,
                label: [s.data.name, s.data.batchYear]
                  .filter(Boolean)
                  .join(" "),
              })),
          )}
          {value.name === "IFTA Sticker" && (
            <>
              {(["sent", "attached"] as const).map((key) => (
                <Field key={key} label={readable(key)}>
                  <input
                    type="checkbox"
                    checked={!!value[key]}
                    onChange={(e) =>
                      onChange({
                        ...value,
                        [key]: e.target.checked,
                        ...(!e.target.checked ? { [key + "Date"]: "" } : {}),
                      })
                    }
                  />
                </Field>
              ))}
              {(["sentDate", "attachedDate"] as const).map((key) => (
                <Field
                  key={key}
                  label={
                    key === "sentDate"
                      ? "Sent Date (optional)"
                      : "Attached Date (optional)"
                  }
                >
                  <input
                    type="date"
                    className={controlClass}
                    value={value[key] || ""}
                    onChange={(e) => set(key, e.target.value)}
                  />
                </Field>
              ))}
            </>
          )}
        </>
      )}
      <datalist id="inventory-colors">
        {deviceColors.map((color) => (
          <option key={color} value={color} />
        ))}
      </datalist>
      {(["make", "model"] as const).map((key) => (
        <datalist key={key} id={"inventory-" + key}>
          {[
            ...new Set(
              context.data.assets.map((a) => a.data[key]).filter(Boolean),
            ),
          ]
            .sort()
            .map((text) => (
              <option key={text} value={text} />
            ))}
        </datalist>
      ))}
      <Field label="Active">
        <input
          type="checkbox"
          checked={value.active}
          onChange={(event) => set("active", event.target.checked)}
        />
      </Field>
      <Field label="Notes" wide>
        <textarea
          className={controlClass}
          rows={3}
          maxLength={5000}
          value={value.notes || ""}
          onChange={(event) => set("notes", event.target.value)}
        />
      </Field>
      {value.verifiedAt && (
        <p className="text-xs text-emerald-400 sm:col-span-2">
          Verified {new Date(value.verifiedAt).toLocaleString()} by{" "}
          {value.verifiedBy}
        </p>
      )}
      {value.source && (
        <p className="break-all text-xs text-zinc-500 sm:col-span-2">
          Source: {value.source}
        </p>
      )}
    </div>
  );
}

export function InventoryEditor({
  collection,
  record,
  context,
  onClose,
  onSaved,
}: {
  collection: InventoryCollection;
  record: InventoryRecord;
  context: InventoryContext;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [draft, setDraft] = useState(record.data),
    [requestId] = useState(() => crypto.randomUUID());
  const [preview, setPreview] = useState(false);
  const proposal = collection === "assets" ? proposedDetails(draft) : null;
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const label =
    collection === "assets"
      ? readable(draft.deviceType)
      : collection === "equipment"
        ? "Supply Observation"
        : collection === "trailers"
          ? "Trailer"
          : collection === "lines"
            ? "Service Line"
            : collection === "supplies"
              ? "Supply"
              : collection === "locations"
                ? "Location"
                : collection === "custodians"
                  ? "Custodian"
                  : "Transponder Notes";
  return (
    <Modal
      title={(record.version ? "Edit " : "Add ") + label}
      description={record.version ? inventoryLabel(record) : undefined}
      isSaving={busy}
      submitLabel="Save"
      onClose={onClose}
      onSubmit={(event) => {
        event.preventDefault();
        if (busy) return;
        setBusy(true);
        setError("");
        const data =
          collection === "supplies"
            ? {
                ...draft,
                unit:
                  draft.name === "IFTA Sticker"
                    ? "pairs"
                    : draft.unit || "units",
                ...(draft.name === "IFTA Sticker" ? { reusable: false } : {}),
              }
            : draft;
        void saveInventoryRecord(collection, { ...record, data }, requestId)
          .then(onSaved)
          .catch((reason) => setError(reason.message))
          .finally(() => setBusy(false));
      }}
    >
      {error && <ErrorBanner message={error} />}
      {proposal && (
        <div className="mb-4 text-xs">
          <button
            type="button"
            className="text-blue-400"
            onClick={() => setPreview(!preview)}
          >
            Organize device details
          </button>
          {preview && (
            <div className="mt-3 space-y-3">
              <p className="text-zinc-500">Original: {draft.model}</p>
              <dl className="grid grid-cols-2 gap-2">
                {Object.entries(proposal).map(([key, value]) => (
                  <div key={key}>
                    <dt className="text-zinc-500">
                      {
                        (
                          {
                            model: "Model",
                            modelNumber: "Model Number",
                            storageGb: "Storage (GB)",
                            color: "Device Color",
                          } as Record<string, string>
                        )[key]
                      }
                    </dt>
                    <dd>{value || "Unknown"}</dd>
                  </div>
                ))}
              </dl>
              <button
                type="button"
                className="ui-button"
                onClick={() => {
                  setDraft({ ...draft, ...proposal });
                  setPreview(false);
                }}
              >
                Use these details
              </button>
              <p className="text-zinc-500">
                Saved only when you save this device. The original description
                is retained.
              </p>
            </div>
          )}
        </div>
      )}
      <InventoryRecordFields
        collection={collection}
        value={draft}
        onChange={setDraft}
        context={context}
      />
    </Modal>
  );
}
