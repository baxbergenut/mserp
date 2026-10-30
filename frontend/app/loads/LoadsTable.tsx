import { ChevronDown, ChevronUp } from "lucide-react";
import { useState } from "react";
import type { Load, SortDir, SortKey } from "../lib/types";
import { formatDate, formatMiles, formatMoney, formatRate } from "../lib/utils";
import { StatusDot } from "../components/StatusDot";

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
  if (!active)
    return (
      <ChevronDown className="h-3 w-3 text-zinc-700" />
    );
  return dir === "asc" ? (
    <ChevronUp className="h-3 w-3 text-zinc-400" />
  ) : (
    <ChevronDown className="h-3 w-3 text-zinc-400" />
  );
}

function LoadRow({ load }: { load: Load }) {
  const [open, setOpen] = useState(false);
  const detailsId = `load-${load.ID}-stops`;
  return <>
    <tr onClick={() => setOpen(value => !value)} className={`cursor-pointer whitespace-nowrap border-b border-zinc-800 text-zinc-300 transition-colors ${open ? "bg-zinc-950" : "hover:bg-zinc-800/20"}`}>
      <th scope="row" className="px-3 py-2 text-left font-normal">
        <div className="flex items-center gap-2">
          <button type="button" aria-label={`Load ${load.LoadID} stops`} aria-expanded={open} aria-controls={detailsId} onClick={event => { event.stopPropagation(); setOpen(value => !value); }} className="shrink-0 rounded focus-visible:outline-2 focus-visible:outline-blue-500">
            <ChevronDown className={`h-3.5 w-3.5 text-zinc-500 ${open ? "rotate-180" : ""}`} />
          </button>
          <div><div className="font-mono text-zinc-200">{load.LoadID}</div>{load.ShipmentID && <div className="font-mono text-[11px] text-zinc-600">{load.ShipmentID}</div>}</div>
        </div>
      </th>
      <td className="px-3 py-2">{load.CustomerName}</td>
      <td className="px-3 py-2">{load.DriverName}{load.TeamDriverName && <span className="text-zinc-600"> / {load.TeamDriverName}</span>}</td>
      <td className="px-3 py-2 font-mono text-zinc-400">{load.TruckUnit}</td>
      <td className="px-3 py-2"><StatusDot status={load.Status} /></td>
      <td className="whitespace-nowrap px-3 py-2 font-mono tabular-nums text-zinc-400">{formatDate(load.PickupTime)}</td>
      <td className="whitespace-nowrap px-3 py-2 font-mono tabular-nums text-zinc-400">{formatDate(load.DeliveryTime)}</td>
      <td className="px-3 py-2 text-right font-mono tabular-nums text-zinc-400">{formatMiles(load.TotalMiles ?? NaN)}</td>
      <td className="px-3 py-2 text-right font-mono tabular-nums text-zinc-400">{formatRate(load.PerMileRevenue ?? NaN)}</td>
      <td className="px-3 py-2 text-right font-mono tabular-nums text-zinc-200">{formatMoney(load.TotalPay ?? NaN)}</td>
    </tr>
    {open && <tr><td colSpan={COLUMNS.length} className="border-b border-zinc-700 bg-zinc-950 p-0">
      <div id={detailsId} role="region" aria-label={`Load ${load.LoadID} stops`} className="max-w-4xl">
        {load.Stops?.length ? <table className="w-full table-fixed border-collapse text-left text-xs">
          <thead className="text-[10px] text-zinc-400"><tr>
            <th scope="col" className="w-10 border-b border-r border-zinc-800/70 px-3 py-1.5 font-medium">#</th>
            <th scope="col" className="w-24 border-b border-r border-zinc-800/70 px-2 py-1.5 font-medium">Stop</th>
            <th scope="col" className="border-b border-zinc-800/70 px-2 py-1.5 font-medium">Address</th>
          </tr></thead>
          <tbody>{load.Stops.map((stop, index) => {
            const locality = [stop.city, [stop.state, stop.zipCode].filter(Boolean).join(" ")].filter(Boolean).join(", ");
            return <tr key={index}>
              <td className="border-b border-r border-zinc-800/70 px-3 py-2 align-top font-mono text-zinc-500">{index + 1}</td>
              <td className="border-b border-r border-zinc-800/70 px-2 py-2 align-top text-zinc-300">{stop.type === "pickup" ? "Pickup" : "Delivery"}</td>
              <td className="break-words border-b border-zinc-800/70 px-2 py-2 text-zinc-400">{[stop.address || "Address unavailable", locality].filter(Boolean).join(", ")}</td>
            </tr>;
          })}</tbody>
        </table> : <p className="px-3 py-3 text-xs text-zinc-500">Pickup and delivery stops are unavailable.</p>}
      </div>
    </td></tr>}
  </>;
}

export function LoadsTable({
  loads,
  isLoading,
  sortKey,
  sortDir,
  onSort,
}: LoadsTableProps) {
  return (
    <div className="overflow-x-auto rounded-xl border border-zinc-800/60">
      <table className="w-full min-w-[900px] border-collapse text-left text-[13px]">
        <thead>
          <tr className="border-b border-zinc-800 bg-zinc-900/30 text-zinc-500">
            {COLUMNS.map((col) => (
              <th
                key={col.label}
                className={`sticky top-0 bg-zinc-900/30 px-3 py-2.5 font-medium backdrop-blur ${
                  col.align === "right" ? "text-right" : "text-left"
                }`}
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
          {isLoading ? <tr><td colSpan={COLUMNS.length} className="p-8 text-center text-zinc-500" role="status">Loading…</td></tr> : loads.length === 0 ? <tr><td colSpan={COLUMNS.length} className="p-8 text-center text-zinc-500">No loads match these filters.</td></tr> : loads.map(load => <LoadRow key={load.ID} load={load} />)}
        </tbody>
      </table>
    </div>
  );
}
