"use client";

import { useEffect, useState } from "react";
import Link from "@/app/components/AccessLink";
import { fetchInventoryHistory } from "@/app/lib/api";
import type {
  InventoryCollection,
  InventoryEvent,
  InventoryFields,
  InventoryRecord,
} from "@/app/lib/types";
import { ErrorBanner, Modal } from "@/app/components/management/ManagementUI";
import {
  inventoryLabel,
  inventoryPhone,
  type InventoryContext,
} from "./InventoryForms";
import { readable } from "./InventoryStatus";
import { custodyLabel } from "./inventoryView";

const labels: Partial<Record<keyof InventoryFields, string>> = {
  imei: "IMEI",
  serial: "Serial",
  assetLabel: "Asset Label",
  make: "Make",
  model: "Model",
  modelNumber: "Model Number",
  storageGb: "Storage (GB)",
  color: "Device Color",
  caseColor: "Case Color",
  originalModel: "Original Description",
  truckId: "Truck",
  gatewayId: "Gateway",
  assetId: "Device",
  locationId: "Storage Location",
  custodianId: "Custodian",
  custodian: "Source User / Legacy Custodian",
  equipmentState: "Installation",
  wirelessNumber: "Wireless Number",
  iccid: "SIM ICCID",
  sourceUpdated: "Source Updated",
  verifiedAt: "Verified At",
  verifiedBy: "Verified By",
  batchYear: "Sticker Year",
  sentDate: "Sent Date",
  attachedDate: "Attached Date",
  status: "Status",
  active: "Active Record",
};
export function InventoryDetail({
  collection,
  record,
  context,
  canTrucks,
  onClose,
  onEdit,
}: {
  collection: InventoryCollection;
  record: InventoryRecord;
  context: InventoryContext;
  canTrucks: boolean;
  onClose: () => void;
  onEdit?: () => void;
}) {
  const [section, setSection] = useState("details"),
    [events, setEvents] = useState<InventoryEvent[] | null>(null),
    [error, setError] = useState("");
  useEffect(() => {
    if (section !== "history") return;
    let alive = true;
    void fetchInventoryHistory(collection, record.id)
      .then((value) => {
        if (alive) setEvents(value);
      })
      .catch((e) => {
        if (alive) setError(e.message);
      });
    return () => {
      alive = false;
    };
  }, [section, collection, record.id]);
  const display = (key: string, value: unknown) => {
    if (value === undefined || value === null || value === "") return "Unknown";
    if (key === "wirelessNumber") return inventoryPhone(String(value));
    if (key === "truckId")
      return (
        context.trucks.find((t) => t.id === value)?.unitNumber ||
        "Assigned Truck"
      );
    if (key === "gatewayId" || key === "assetId")
      return inventoryLabel(context.data.assets.find((a) => a.id === value));
    if (key === "locationId")
      return inventoryLabel(context.data.locations.find((a) => a.id === value));
    if (key === "custodianId")
      return inventoryLabel(
        context.data.custodians.find((a) => a.id === value),
      );
    if (key === "supplyId")
      return inventoryLabel(context.data.supplies.find((a) => a.id === value));
    if (typeof value === "boolean") return value ? "Yes" : "No";
    if (["condition", "status", "equipmentState", "deviceType"].includes(key))
      return readable(String(value));
    return String(value);
  };
  const groups: [string, (keyof InventoryFields)[]][] =
    collection === "assets"
      ? [
          [
            "Identity",
            [
              "deviceType",
              "brand",
              "imei",
              "serial",
              "assetLabel",
              "make",
              "model",
              "modelNumber",
              "storageGb",
              "color",
              "caseColor",
              "ownership",
            ],
          ],
          [
            "Assignment",
            [
              "truckId",
              "gatewayId",
              "locationId",
              "custodianId",
              "custodian",
              "condition",
              "status",
              "equipmentState",
              "verifiedAt",
              "verifiedBy",
            ],
          ],
          ["Source", ["notes", "source", "originalModel", "active"]],
        ]
      : [["Details", Object.keys(record.data) as (keyof InventoryFields)[]]];
  const line = context.data.lines.find(
    (l) => l.data.active && l.data.assetId === record.id,
  );
  return (
    <Modal
      title={inventoryLabel(record)}
      isSaving={false}
      submitLabel="Close"
      hideSubmit
      cancelLabel="Close"
      onClose={onClose}
      onSubmit={(e) => e.preventDefault()}
    >
      <div className="mb-4 flex items-center gap-4 border-b border-zinc-800">
        {["details", "history"].map((key) => (
          <button
            key={key}
            type="button"
            className={
              "border-b-2 py-2 text-xs " +
              (key === section
                ? "border-blue-500 text-blue-400"
                : "border-transparent text-zinc-500")
            }
            onClick={() => setSection(key)}
          >
            {readable(key)}
          </button>
        ))}
        {onEdit && (
          <button
            type="button"
            className="ml-auto text-xs text-blue-400"
            onClick={onEdit}
          >
            Edit
          </button>
        )}
      </div>
      {error && <ErrorBanner message={error} />}
      {section === "details" ? (
        <div className="space-y-4">
          {groups.map(([title, keys]) => (
            <section key={title}>
              <h3 className="mb-2 text-xs font-medium text-zinc-500">
                {title}
              </h3>
              <dl className="grid gap-3 text-[13px] sm:grid-cols-2">
                {keys
                  .filter(
                    (key) =>
                      record.data[key] !== undefined && record.data[key] !== "",
                  )
                  .map((key) => (
                    <div key={key}>
                      <dt className="text-xs text-zinc-500">
                        {labels[key] ||
                          key
                            .replace(/([A-Z])/g, " $1")
                            .replace(/^./, (c) => c.toUpperCase())}
                      </dt>
                      <dd className="mt-1 break-words">
                        {key === "truckId" && canTrucks ? (
                          <Link
                            className="text-blue-400"
                            href={"/trucks/detail?id=" + record.data.truckId}
                          >
                            {display(key, record.data[key])}
                          </Link>
                        ) : key === "registrationUrl" ? (
                          <a
                            className="text-blue-400"
                            href={String(record.data[key])}
                            target="_blank"
                            rel="noreferrer"
                          >
                            Open registration
                          </a>
                        ) : (
                          display(key, record.data[key])
                        )}
                      </dd>
                    </div>
                  ))}
              </dl>
            </section>
          ))}
          {record.data.gatewayId && (
            <p className="text-xs text-zinc-500">
              Custody through gateway:{" "}
              {custodyLabel(record, context.data, context.trucks)}
            </p>
          )}
          {line && (
            <section>
              <h3 className="text-xs font-medium text-zinc-500">
                Service line
              </h3>
              <p className="mt-2 text-[13px]">
                {inventoryPhone(line.data.wirelessNumber)} ·{" "}
                {readable(line.data.status)}
              </p>
              <p className="text-xs text-zinc-500">
                Source updated: {line.data.sourceUpdated || "Unknown"}
              </p>
            </section>
          )}
        </div>
      ) : events === null ? (
        <p role="status" className="text-xs text-zinc-500">
          Loading history…
        </p>
      ) : !events.length ? (
        <p className="text-xs text-zinc-500">No recorded changes.</p>
      ) : (
        <ol className="divide-y divide-zinc-800">
          {events.map((event) => {
            const before: Partial<InventoryFields> = event.before?.data || {},
              after: Partial<InventoryFields> = event.after?.data || {};
            const keys = [
              ...new Set([...Object.keys(before), ...Object.keys(after)]),
            ] as (keyof InventoryFields)[];
            return (
              <li key={event.id} className="space-y-2 py-3 text-xs">
                <p>
                  <span className="font-medium">
                    {event.action === "normalize"
                      ? "Migration normalization"
                      : readable(event.action)}
                  </span>{" "}
                  · {event.actor || "System"} ·{" "}
                  {new Date(event.createdAt).toLocaleString()}
                </p>
                {event.reason && (
                  <p>
                    {event.reason}
                    {event.effectiveDate
                      ? " · Effective " + event.effectiveDate
                      : ""}
                  </p>
                )}
                <dl className="space-y-1 text-zinc-400">
                  {keys
                    .filter(
                      (key) =>
                        key !== "source" &&
                        (before[key] ?? "") !== (after[key] ?? ""),
                    )
                    .map((key) => (
                      <div key={key}>
                        <dt className="inline text-zinc-500">
                          {labels[key] || readable(key)}:{" "}
                        </dt>
                        <dd className="inline break-words">
                          {display(key, before[key])} →{" "}
                          {display(key, after[key])}
                        </dd>
                      </div>
                    ))}
                </dl>
              </li>
            );
          })}
        </ol>
      )}
    </Modal>
  );
}
