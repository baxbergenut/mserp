"use client";
import { useRef, useState } from "react";
import { applyInventoryOperation } from "@/app/lib/api";
import type { InventoryRecord } from "@/app/lib/types";
import type {
  InventoryChange,
  InventoryOperation as Operation,
} from "@/app/lib/inventoryTypes";
import {
  ErrorBanner,
  Field,
  Modal,
  controlClass,
} from "@/app/components/management/ManagementUI";
import {
  inventoryLabel,
  inventoryPhone,
  type InventoryContext,
} from "./InventoryForms";
import {
  availableToAssign,
  custodyLabel,
  deviceDescription,
  unavailable,
} from "./inventoryView";

export type DeviceAction = "transfer" | "return" | "replace" | "verify";
export function InventoryOperation({
  record,
  action,
  context,
  onClose,
  onSaved,
}: {
  record: InventoryRecord;
  action: DeviceAction;
  context: InventoryContext;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [destination, setDestination] = useState(
      action === "return" || action === "replace" ? "locationId" : "truckId",
    ),
    [destinationId, setDestinationId] = useState("");
  const [replacement, setReplacement] = useState(""),
    [moveLine, setMoveLine] = useState(false),
    [reason, setReason] = useState("");
  const [effectiveDate, setDate] = useState(
    new Intl.DateTimeFormat("en-CA", {
      timeZone: "America/New_York",
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).format(new Date()),
  );
  const [busy, setBusy] = useState(false),
    [error, setError] = useState("");
  const attempt = useRef<{ body: string; id: string } | null>(null);
  const line = context.data.lines.find(
    (l) => l.data.active && l.data.assetId === record.id,
  );
  const cameras = context.data.assets.filter(
    (a) => a.data.active && a.data.gatewayId === record.id,
  );
  const choices =
    destination === "truckId"
      ? context.trucks.map((t) => ({ id: t.id, label: t.unitNumber }))
      : destination === "gatewayId"
        ? context.data.assets
            .filter(
              (a) =>
                a.data.deviceType === "samsara_gateway" && !unavailable(a.data),
            )
            .map((a) => ({ id: a.id, label: inventoryLabel(a) }))
        : (destination === "custodianId"
            ? context.data.custodians
            : context.data.locations
          )
            .filter((a) => a.data.active)
            .map((a) => ({ id: a.id, label: inventoryLabel(a) }));
  const replacements = context.data.assets.filter(
    (a) =>
      a.id !== record.id &&
      a.data.deviceType === record.data.deviceType &&
      a.data.brand === record.data.brand &&
      availableToAssign(a, context.data) &&
      !context.data.assets.some(
        (c) => c.data.active && c.data.gatewayId === a.id,
      ) &&
      (!moveLine ||
        !context.data.lines.some(
          (l) => l.data.active && l.data.assetId === a.id,
        )),
  );
  const title = {
    transfer: "Transfer Device",
    return: "Return to Storage",
    replace: "Replace Device",
    verify: "Verify Installation",
  }[action];
  const clearHolder = {
    truckId: "",
    gatewayId: "",
    locationId: "",
    custodian: "",
    custodianId: "",
    equipmentState: "unknown",
  };
  return (
    <Modal
      title={title}
      description={inventoryLabel(record)}
      isSaving={busy}
      submitLabel={action === "verify" ? "Verify" : "Save"}
      submitDisabled={
        !reason.trim() ||
        !effectiveDate ||
        (action !== "verify" && !destinationId) ||
        (action === "replace" && !replacement)
      }
      onClose={onClose}
      onSubmit={(event) => {
        event.preventDefault();
        if (busy) return;
        let changes: InventoryChange[] = [];
        if (action === "verify")
          changes = [
            {
              collection: "assets",
              record: {
                ...record,
                data: { ...record.data, equipmentState: "verified_installed" },
              },
            },
          ];
        else {
          changes = [
            {
              collection: "assets",
              record: {
                ...record,
                data: {
                  ...record.data,
                  ...clearHolder,
                  [destination]: destinationId,
                },
              },
            },
          ];
          if (action === "replace") {
            const next = replacements.find((a) => a.id === replacement);
            if (!next) return;
            changes.push({
              collection: "assets",
              record: {
                ...next,
                data: {
                  ...next.data,
                  ...clearHolder,
                  truckId: record.data.truckId || "",
                  gatewayId: record.data.gatewayId || "",
                  locationId: record.data.locationId || "",
                  custodian: record.data.custodian || "",
                  custodianId: record.data.custodianId || "",
                  status: "active",
                },
              },
            });
            changes.push(
              ...cameras.map((camera) => ({
                collection: "assets" as const,
                record: {
                  ...camera,
                  data: {
                    ...camera.data,
                    gatewayId: next.id,
                    equipmentState: "unknown",
                  },
                },
              })),
            );
            if (moveLine && line)
              changes.push({
                collection: "lines",
                record: { ...line, data: { ...line.data, assetId: next.id } },
              });
          }
        }
        const expectedLinks = Object.fromEntries(
          changes
            .filter((c) => c.collection === "assets")
            .map((c) => [
              c.record.id,
              [
                ...context.data.assets
                  .filter(
                    (a) => a.data.active && a.data.gatewayId === c.record.id,
                  )
                  .map((a) => a.id),
                ...context.data.lines
                  .filter(
                    (l) => l.data.active && l.data.assetId === c.record.id,
                  )
                  .map((l) => l.id),
              ].sort(),
            ]),
        );
        const body = JSON.stringify({
          changes,
          reason: reason.trim(),
          effectiveDate,
          expectedLinks,
        });
        if (attempt.current?.body !== body)
          attempt.current = { body, id: crypto.randomUUID() };
        const input: Operation = {
          requestId: attempt.current.id,
          changes,
          reason: reason.trim(),
          effectiveDate,
          expectedLinks,
          verify: action === "verify",
        };
        setBusy(true);
        setError("");
        void applyInventoryOperation(input)
          .then(onSaved)
          .catch((e) => setError(e.message))
          .finally(() => setBusy(false));
      }}
    >
      {error && <ErrorBanner message={error} />}
      <div className="grid gap-4 sm:grid-cols-2">
        <p className="text-xs text-zinc-500 sm:col-span-2">
          Current custody: {custodyLabel(record, context.data, context.trucks)}
        </p>
        {action === "replace" && (
          <Field label="Replacement device" wide>
            <select
              aria-label="Replacement device"
              className={controlClass}
              value={replacement}
              onChange={(e) => setReplacement(e.target.value)}
              required
            >
              <option value="">Select available device…</option>
              {replacements.map((a) => (
                <option key={a.id} value={a.id}>
                  {inventoryLabel(a)} · {deviceDescription(a.data)}
                </option>
              ))}
            </select>
            {!replacements.length && (
              <span className="block text-xs text-zinc-500">
                No usable spare is available. Record its condition, lifecycle
                and custody first.
              </span>
            )}
          </Field>
        )}
        {action !== "verify" && (
          <>
            <Field
              label={
                action === "replace" ? "Old device destination" : "Destination"
              }
            >
              <select
                aria-label={
                  action === "replace"
                    ? "Old device destination"
                    : "Destination"
                }
                className={controlClass}
                value={destination}
                onChange={(e) => {
                  setDestination(e.target.value);
                  setDestinationId("");
                }}
              >
                <option value="locationId">Storage location</option>
                <option value="custodianId">Custodian</option>
                {action === "transfer" && !unavailable(record.data) && (
                  <>
                    <option value="truckId">Truck</option>
                    {record.data.deviceType === "dashcam" &&
                      record.data.brand === "Samsara" && (
                        <option value="gatewayId">Samsara gateway</option>
                      )}
                  </>
                )}
              </select>
            </Field>
            <Field label="Select destination">
              <select
                aria-label="Select destination"
                className={controlClass}
                value={destinationId}
                onChange={(e) => setDestinationId(e.target.value)}
                required
              >
                <option value="">Select…</option>
                {choices.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.label}
                  </option>
                ))}
              </select>
            </Field>
          </>
        )}
        {action === "replace" && line && (
          <Field label="Service line" wide>
            <span className="flex items-center gap-2 text-xs">
              <input
                type="checkbox"
                checked={moveLine}
                onChange={(e) => {
                  setMoveLine(e.target.checked);
                  setReplacement("");
                }}
              />
              Move {inventoryPhone(line.data.wirelessNumber)} to the replacement
            </span>
          </Field>
        )}
        {action === "replace" && cameras.length > 0 && (
          <p className="text-xs text-zinc-500 sm:col-span-2">
            {cameras.length} linked camera(s) will move with the replacement
            gateway. Installation must be verified again.
          </p>
        )}
        {action === "verify" && (
          <p className="text-xs sm:col-span-2">
            Confirm that you physically checked this device at its assigned
            truck. Your name and verification time will be recorded.
          </p>
        )}
        <Field label="Effective date">
          <input
            aria-label="Effective date"
            required
            className={controlClass}
            type="date"
            value={effectiveDate}
            onChange={(e) => setDate(e.target.value)}
          />
        </Field>
        <Field label="Reason" wide>
          <textarea
            aria-label="Reason"
            required
            maxLength={1000}
            className={controlClass}
            rows={2}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </Field>
      </div>
    </Modal>
  );
}
