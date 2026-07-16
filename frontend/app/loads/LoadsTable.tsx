import type { Load, SortDir, SortKey } from "../types";
import { formatDate, formatMiles, formatMoney, formatRate } from "../utils";
import { StatusDot } from "./StatusDot";

interface LoadsTableProps {
  loads: Load[];
  isLoading: boolean;
  sortKey: SortKey;
  sortDir: SortDir;
  onSort: (key: SortKey) => void;
}

const COLUMNS: Array<{ key: SortKey | null; label: string; align?: "right" }> =
  [
    { key: null, label: "Load" },
    { key: null, label: "Customer" },
    { key: null, label: "Driver" },
    { key: null, label: "Truck" },
    { key: null, label: "Status" },
    { key: "PickupTime", label: "Pickup" },
    { key: "DeliveryTime", label: "Delivery" },
    { key: "TotalMiles", label: "Miles", align: "right" },
    { key: "PerMileRevenue", label: "Rate", align: "right" },
    { key: "TotalPay", label: "Pay", align: "right" },
  ];

function SortIcon({ active, dir }: { active: boolean; dir: SortDir }) {
  return (
    <svg
      viewBox="0 0 24 24"
      className={`h-3 w-3 transition-transform ${active ? "text-zinc-400" : "text-zinc-700"} ${
        active && dir === "asc" ? "rotate-180" : ""
      }`}
      fill="currentColor"
    >
      <path d="M12 16L6 9h12z" />
    </svg>
  );
}

export function LoadsTable({
  loads,
  isLoading,
  sortKey,
  sortDir,
  onSort,
}: LoadsTableProps) {
  if (isLoading) {
    return (
      <div className="overflow-hidden rounded-lg border border-zinc-800">
        {Array.from({ length: 8 }).map((_, i) => (
          <div
            key={i}
            className="h-11 animate-pulse border-b border-zinc-900 last:border-0"
          />
        ))}
      </div>
    );
  }

  if (loads.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center rounded-lg border border-zinc-800 py-20 text-center">
        <p className="text-[13px] text-zinc-500">
          No loads match these filters.
        </p>
        <p className="mt-1 text-[13px] text-zinc-700">
          Try clearing a filter or widening the date range.
        </p>
      </div>
    );
  }

  return (
    <div className="overflow-x-auto rounded-lg border border-zinc-800">
      <table className="w-full min-w-[900px] border-collapse text-left text-[13px]">
        <thead>
          <tr className="border-b border-zinc-800 text-zinc-500">
            {COLUMNS.map((col) => (
              <th
                key={col.label}
                className={`px-3 py-2 font-medium ${col.align === "right" ? "text-right" : "text-left"}`}
              >
                {col.key ? (
                  <button
                    onClick={() => onSort(col.key as SortKey)}
                    className={`inline-flex items-center gap-1 transition-colors hover:text-zinc-300 ${
                      col.align === "right" ? "flex-row-reverse" : ""
                    }`}
                  >
                    {col.label}
                    <SortIcon active={sortKey === col.key} dir={sortDir} />
                  </button>
                ) : (
                  col.label
                )}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {loads.map((load) => (
            <tr
              key={load.ID}
              className="border-b border-zinc-900 text-zinc-300 transition-colors last:border-0 hover:bg-zinc-900/40"
            >
              <td className="px-3 py-2.5">
                <div className="font-mono text-zinc-200">{load.LoadID}</div>
                <div className="font-mono text-[11px] text-zinc-600">
                  {load.ShipmentID}
                </div>
              </td>
              <td className="px-3 py-2.5">{load.CustomerName}</td>
              <td className="px-3 py-2.5">
                {load.DriverName}
                {load.TeamDriverName && (
                  <span className="text-zinc-600">
                    {" "}
                    / {load.TeamDriverName}
                  </span>
                )}
              </td>
              <td className="px-3 py-2.5 font-mono text-zinc-400">
                {load.TruckUnit}
              </td>
              <td className="px-3 py-2.5">
                <StatusDot status={load.Status} />
              </td>
              <td className="px-3 py-2.5 font-mono tabular-nums text-zinc-400">
                {formatDate(load.PickupTime)}
              </td>
              <td className="px-3 py-2.5 font-mono tabular-nums text-zinc-400">
                {formatDate(load.DeliveryTime)}
              </td>
              <td className="px-3 py-2.5 text-right font-mono tabular-nums text-zinc-400">
                {formatMiles(load.TotalMiles)}
              </td>
              <td className="px-3 py-2.5 text-right font-mono tabular-nums text-zinc-400">
                {formatRate(load.PerMileRevenue)}
              </td>
              <td className="px-3 py-2.5 text-right font-mono tabular-nums text-zinc-200">
                {formatMoney(load.TotalPay)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
