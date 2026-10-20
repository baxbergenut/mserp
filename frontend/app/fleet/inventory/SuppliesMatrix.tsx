"use client";

import { useRef, useState } from "react";
import Link from "@/app/components/AccessLink";
import { Plus, RefreshCw } from "lucide-react";
import {
  ManagementSearch,
  TableShell,
  controlClass,
} from "@/app/components/management/ManagementUI";
import { saveInventoryRecord } from "@/app/lib/api";
import type { InventoryRecord, TruckOption } from "@/app/lib/types";
import {
  inventoryLabel,
  newInventoryRecord,
  type InventoryContext,
} from "./InventoryForms";
import { StatusBadge, readable } from "./InventoryStatus";
import { ColumnFilter } from "@/app/components/management/ColumnFilter";

const button =
  "inline-flex shrink-0 items-center gap-2 whitespace-nowrap rounded-lg border border-zinc-800 px-3 py-2 text-xs text-zinc-300 hover:bg-zinc-800 disabled:opacity-50";
const filterClass = controlClass.replace(
  "w-full",
  "w-auto max-w-full shrink-0",
);
export function SuppliesMatrix({
  context,
  canManage,
  canTrucks,
  loading,
  onRefresh,
  onAdd,
  onEdit,
  onSaved,
  onError,
}: {
  context: InventoryContext;
  canManage: boolean;
  canTrucks: boolean;
  loading: boolean;
  onRefresh: () => void;
  onAdd: () => void;
  onEdit: (supply: InventoryRecord) => void;
  onSaved: (record: InventoryRecord) => void;
  onError: (message: string) => void;
}) {
  const [search, setSearch] = useState(""),
    [ownership, setOwnership] = useState("all"),
    [active, setActive] = useState("all");
  const [pending, setPending] = useState<string[]>([]);
  const [presence, setPresence] = useState<Record<string, string>>({});
  const busy = useRef(new Set<string>()),
    ids = useRef(new Map<string, string>());
  const attempts = useRef(
    new Map<string, { requestId: string; record: InventoryRecord }>(),
  );
  const supplies = context.data.supplies
    .filter((item) => item.data.active)
    .sort(
      (a, b) =>
        inventoryLabel(a).localeCompare(inventoryLabel(b)) ||
        (a.data.batchYear || "").localeCompare(b.data.batchYear || ""),
    );
  const trucks = context.trucks
    .filter(
      (truck) =>
        (!search ||
          [truck.unitNumber, truck.make, truck.model, truck.ownerName]
            .join(" ")
            .toLowerCase()
            .includes(search.toLowerCase())) &&
        (active === "all" ||
          (active === "active" && truck.active) ||
          (active === "inactive" && !truck.active)),
    )
    .sort((a, b) =>
      a.unitNumber.localeCompare(b.unitNumber, undefined, { numeric: true }),
    );
  const observation = (truck: TruckOption, supply: InventoryRecord) =>
    context.data.equipment.find(
      (item) =>
        item.data.active &&
        item.data.truckId === truck.id &&
        (item.data.supplyId === supply.id ||
          (!item.data.supplyId &&
            item.data.name === supply.data.name &&
            (item.data.batchYear || "") === (supply.data.batchYear || ""))),
    );
  const toggle = async (
    truck: TruckOption,
    supply: InventoryRecord,
    checked: boolean,
  ) => {
    const cell = truck.id + ":" + supply.id;
    if (busy.current.has(cell)) return;
    busy.current.add(cell);
    setPending((previous) => [...previous, cell]);
    onError("");
    const current = observation(truck, supply);
    if (!ids.current.has(cell)) ids.current.set(cell, crypto.randomUUID());
    const base = current || {
      ...newInventoryRecord(),
      id: ids.current.get(cell)!,
    };
    const state = checked ? "verified_installed" : "missing";
    const key = base.id + ":" + base.version + ":" + state;
    let attempt = attempts.current.get(key);
    if (!attempt) {
      attempt = {
        requestId: crypto.randomUUID(),
        record: {
          ...base,
          data: {
            ...base.data,
            name: supply.data.name,
            batchYear: supply.data.batchYear,
            supplyId: supply.id,
            truckId: truck.id,
            equipmentState: state,
            active: true,
          },
        },
      };
      attempts.current.set(key, attempt);
    }
    try {
      onSaved(
        await saveInventoryRecord(
          "equipment",
          attempt.record,
          attempt.requestId,
        ),
      );
      attempts.current.delete(key);
    } catch (reason) {
      onError(
        reason instanceof Error
          ? reason.message
          : "Unable to save supply presence",
      );
    } finally {
      busy.current.delete(cell);
      setPending((previous) => previous.filter((key) => key !== cell));
    }
  };
  if (!canTrucks)
    return (
      <p className="text-sm text-zinc-500">
        Truck access is required to view the supplies checklist.
      </p>
    );
  const groups = [
    {
      key: "company",
      label: "Company Trucks",
      rows: trucks.filter((truck) => truck.isCompanyOwned),
    },
    {
      key: "other",
      label: "Other Trucks",
      rows: trucks.filter((truck) => !truck.isCompanyOwned),
    },
  ]
    .filter((group) => ownership === "all" || ownership === group.key)
    .map((group) => ({
      ...group,
      rows: group.rows.filter((truck) =>
        supplies.every(
          (s) =>
            !presence[s.id] ||
            (observation(truck, s)?.data.equipmentState || "unknown") ===
              presence[s.id],
        ),
      ),
    }));
  return (
    <div className="space-y-5">
      <div
        role="group"
        aria-label="Inventory controls"
        className="flex flex-wrap items-center gap-2"
      >
        <div className="min-w-48 flex-1 basis-60 [&>div]:max-w-none">
          <ManagementSearch
            value={search}
            onChange={setSearch}
            placeholder="Search truck, make or owner…"
          />
        </div>
        <select
          aria-label="Ownership Filter"
          className={filterClass}
          value={ownership}
          onChange={(event) => setOwnership(event.target.value)}
        >
          <option value="all">All Ownership</option>
          <option value="company">Company Trucks</option>
          <option value="other">Other Trucks</option>
        </select>
        <select
          aria-label="Truck Status Filter"
          className={filterClass}
          value={active}
          onChange={(event) => setActive(event.target.value)}
        >
          <option value="all">All Trucks</option>
          <option value="active">Active Trucks</option>
          <option value="inactive">Inactive Trucks</option>
        </select>
        <button className={button} disabled={loading} onClick={onRefresh}>
          <RefreshCw className="h-4 w-4" />
          Refresh
        </button>
        {canManage && (
          <button
            className={button + " bg-blue-600 text-white hover:bg-blue-500"}
            onClick={onAdd}
          >
            <Plus className="h-4 w-4" />
            Add Supply
          </button>
        )}
      </div>
      <div
        className="flex flex-wrap items-center gap-2 text-xs text-zinc-500"
        aria-label="Presence legend"
      >
        <StatusBadge value="verified_installed" label="Verified Present" />
        <StatusBadge value="reported_present" />
        <StatusBadge value="missing" />
        <StatusBadge value="unknown" />
        <span>
          Check to verify presence. Missing is labeled; an empty gray box is
          unknown.
        </span>
      </div>
      {groups.map((group) => (
        <section key={group.key} aria-label={group.label} className="space-y-3">
          <h2 className="text-sm font-semibold text-zinc-200">
            {group.label}{" "}
            <span className="font-normal text-zinc-500">
              ({group.rows.length})
            </span>
          </h2>
          <TableShell>
            <table className="w-full min-w-[600px] table-fixed">
              <thead>
                <tr>
                  <th className="sticky left-0 z-10 w-36 bg-card px-4 py-3 text-left text-xs font-medium text-zinc-500">
                    Truck
                  </th>
                  {supplies.map((supply) => (
                    <th
                      key={supply.id}
                      className="whitespace-nowrap px-4 py-3 text-center text-xs font-medium text-zinc-500"
                    >
                      <ColumnFilter
                        label={[supply.data.name, supply.data.batchYear]
                          .filter(Boolean)
                          .join(" ")}
                        active={!!presence[supply.id]}
                      >
                        <select
                          aria-label={
                            (supply.data.name || "Supply") + " Presence Filter"
                          }
                          className={controlClass}
                          value={presence[supply.id] || ""}
                          onChange={(e) =>
                            setPresence({
                              ...presence,
                              [supply.id]: e.target.value,
                            })
                          }
                        >
                          <option value="">All States</option>
                          <option value="missing">Missing</option>
                          <option value="unknown">Unknown</option>
                          <option value="reported_present">
                            Reported Present
                          </option>
                          <option value="verified_installed">
                            Verified Present
                          </option>
                        </select>
                        {canManage && (
                          <button
                            className="text-xs text-blue-400"
                            onClick={() => onEdit(supply)}
                          >
                            Edit supply
                          </button>
                        )}
                      </ColumnFilter>
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {group.rows.map((truck) => (
                  <tr key={truck.id} className="border-t border-zinc-800/60">
                    <td className="sticky left-0 bg-card px-4 py-3 text-xs">
                      <Link
                        className="font-medium text-blue-400"
                        href={"/trucks/detail?id=" + truck.id}
                      >
                        {truck.unitNumber}
                      </Link>
                      {!truck.active && (
                        <span className="ml-2 text-zinc-500">Inactive</span>
                      )}
                    </td>
                    {supplies.map((supply) => {
                      const record = observation(truck, supply),
                        state = record?.data.equipmentState || "unknown",
                        cell = truck.id + ":" + supply.id;
                      const title =
                        readable(
                          state === "verified_installed"
                            ? "verified_installed"
                            : state,
                        ) +
                        (record?.data.verifiedAt
                          ? " · " +
                            record.data.verifiedBy +
                            " · " +
                            new Date(record.data.verifiedAt).toLocaleString()
                          : state === "reported_present"
                            ? " · Click to verify presence"
                            : " · Check when confirmed present");
                      const color =
                        state === "verified_installed"
                          ? "accent-emerald-500 ring-emerald-500/30"
                          : state === "reported_present"
                            ? "accent-amber-400 ring-amber-400/30"
                            : state === "missing"
                              ? "accent-red-500 ring-red-500/30"
                              : "accent-zinc-500 ring-zinc-500/30";
                      return (
                        <td key={supply.id} className="px-4 py-3 text-center">
                          <label
                            title={title}
                            className="inline-flex cursor-pointer items-center justify-center rounded-md p-1.5 hover:bg-zinc-800"
                          >
                            <input
                              type="checkbox"
                              aria-label={
                                truck.unitNumber +
                                " · " +
                                [supply.data.name, supply.data.batchYear]
                                  .filter(Boolean)
                                  .join(" ")
                              }
                              aria-describedby={"presence-" + cell}
                              checked={state === "verified_installed"}
                              ref={(node) => {
                                if (node)
                                  node.indeterminate =
                                    state === "reported_present";
                              }}
                              disabled={!canManage || pending.includes(cell)}
                              className={
                                "h-4 w-4 cursor-pointer rounded ring-2 disabled:cursor-default disabled:opacity-50 " +
                                color
                              }
                              onChange={(event) =>
                                void toggle(truck, supply, event.target.checked)
                              }
                            />
                            <span id={"presence-" + cell} className="sr-only">
                              {state === "verified_installed"
                                ? "Verified Present"
                                : readable(state)}
                            </span>
                            {state === "missing" && (
                              <span className="ml-1 text-[11px] text-red-400">
                                Missing
                              </span>
                            )}
                          </label>
                          {supply.data.name === "IFTA Sticker" &&
                            (record?.data.sent || record?.data.attached) && (
                              <p
                                className="text-[11px] text-zinc-500"
                                title={
                                  "Sent: " +
                                  (record.data.sentDate || "Date unknown") +
                                  " · Attached: " +
                                  (record.data.attachedDate || "Date unknown")
                                }
                              >
                                {[
                                  record.data.sent ? "Sent" : "",
                                  record.data.attached ? "Attached" : "",
                                ]
                                  .filter(Boolean)
                                  .join(" · ")}
                              </p>
                            )}
                        </td>
                      );
                    })}
                  </tr>
                ))}
                {!group.rows.length && (
                  <tr>
                    <td
                      colSpan={supplies.length + 1}
                      className="p-8 text-center text-sm text-zinc-500"
                    >
                      {loading ? "Loading…" : "No matching trucks."}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </TableShell>
        </section>
      ))}
    </div>
  );
}
