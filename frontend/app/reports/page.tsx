"use client";

import { Fragment, useEffect, useState } from "react";
import { CalendarDays, ChartNoAxesCombined, ChevronDown, Download } from "lucide-react";
import Link from "@/app/components/AccessLink";
import { PageHeader } from "@/app/components/PageHeader";
import { controlClass } from "@/app/components/management/ManagementUI";
import { usePermissions } from "@/app/lib/access";
import { exportReport, fetchReport } from "@/app/lib/api";
import type { ReportEvidence, ReportRow, ReportsQuery, ReportsResult, ReportView } from "@/app/lib/types";

const views: { id: ReportView; label: string; access: string; columns: string[] }[] = [
  { id: "company", label: "Company Truck P&L", access: "reports.read", columns: ["gross", "earnedPay", "fuel", "tolls", "scales", "maintenance", "equipment", "contribution", "margin", "miles", "revenuePerMile", "costPerMile", "provisionalGross", "unassignedCost"] },
  { id: "owner-operator", label: "Owner-Operator Truck P&L", access: "reports.read", columns: ["gross", "carrierFees", "carrierCosts", "contribution", "provisionalGross", "unassignedCost"] },
  { id: "dispatcher", label: "Dispatcher Performance", access: "reports.performance", columns: ["completedLoads", "gross", "truckWeeks", "grossPerTruckWeek", "loadedRPM", "deadheadPct", "provisionalLoads"] },
  { id: "driver", label: "Driver Performance", access: "reports.performance", columns: ["completedLoads", "activeDays", "miles", "pickupOnTime", "pickupCoverage", "deliveryOnTime", "deliveryCoverage", "fuelEfficiency", "provisionalLoads"] },
];

const settlementColumns = ["settlementEarnings", "reimbursements", "deductions", "escrowWithheld", "escrowReturned", "escrowApplied", "netPayable"];

const labels: Record<string, string> = {
  gross: "Load gross", earnedPay: "Earned driver pay", fuel: "Fuel", tolls: "Tolls", scales: "Scales", maintenance: "Maintenance",
  equipment: "Equipment", contribution: "Contribution", margin: "Margin", miles: "Miles", revenuePerMile: "Revenue per mile",
  costPerMile: "Cost per mile", provisionalGross: "Provisional gross", provisionalLoads: "Provisional entries", unassignedCost: "Unassigned costs",
  carrierFees: "Carrier fees", carrierCosts: "Carrier-borne costs", settlementEarnings: "Earnings", reimbursements: "Reimbursements", deductions: "Deductions",
  escrowWithheld: "Escrow withheld", escrowReturned: "Escrow returned", escrowApplied: "Escrow applied", netPayable: "Net payable", completedLoads: "Completed loads",
  truckWeeks: "Active truck-weeks", grossPerTruckWeek: "Gross per active truck-week", loadedRPM: "Loaded revenue per mile", deadheadPct: "Deadhead",
  activeDays: "Active days", pickupOnTime: "On-time pickup", pickupCoverage: "Pickup timestamp coverage", deliveryOnTime: "On-time delivery",
  deliveryCoverage: "Delivery timestamp coverage", fuelEfficiency: "Fuel efficiency (MPG)", loadedMiles: "Loaded miles", deadheadMiles: "Deadhead miles",
  amount: "Amount",
};

const currency = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD" });
const number = new Intl.NumberFormat("en-US", { maximumFractionDigits: 2 });
const percents = new Set(["margin", "deadheadPct", "pickupOnTime", "pickupCoverage", "deliveryOnTime", "deliveryCoverage"]);
const units = new Set(["completedLoads", "truckWeeks", "miles", "activeDays", "fuelEfficiency", "loadedMiles", "deadheadMiles", "provisionalLoads"]);

function formatMetric(key: string, value?: number) {
  if (value === undefined) return "";
  if (percents.has(key)) return number.format(value) + "%";
  return units.has(key) ? number.format(value) : currency.format(value / 100);
}

const filterFields = [
  ["truck", "Truck", "trucks"],
  ["driver", "Driver", "drivers"],
  ["dispatcher", "Dispatcher", "dispatchers"],
] as const;
const cell = "border-b border-zinc-500/25 px-3 py-2";
const blankFilters: ReportsQuery = {};

export default function ReportsPage() {
  const permissions = usePermissions();
  const available = views.filter(view => permissions.includes(view.access));
  const [selected, setSelected] = useState<ReportView>("company");
  const view = available.find(item => item.id === selected) ?? available[0];
  const id = view?.id;
  const [query, setQuery] = useState<ReportsQuery>(blankFilters);
  const [dates, setDates] = useState<ReportsQuery>({});
  const [editingDates, setEditingDates] = useState(false);
  const [result, setResult] = useState<{ key: string; data: ReportsResult }>();
  const [error, setError] = useState<{ key: string; message: string }>();
  const [exportError, setExportError] = useState("");
  const [exporting, setExporting] = useState(false);
  const [retry, setRetry] = useState(0);
  const [comparison, setComparison] = useState(false);
  const [expanded, setExpanded] = useState("");
  const key = JSON.stringify([id, query, retry]);
  const data = result?.key === key ? result.data : undefined;
  const previousData = result?.data;
  const currentError = error?.key === key ? error.message : "";

  useEffect(() => {
    if (!id) return;
    const abort = new AbortController();
    fetchReport(id, query, abort.signal).then(
      data => { if (!abort.signal.aborted) setResult({ key, data }); },
      reason => { if (!abort.signal.aborted) setError({ key, message: reason.message }); },
    );
    return () => abort.abort();
  }, [id, query, key]);

  if (!view) return null;
  const period = previousData?.period;
  const rows = comparison ? data?.comparison : data?.current;


  function updateFilter(field: "truck" | "driver" | "dispatcher", value: string) {
    setQuery(query => ({ ...query, [field]: value }));
    setExpanded("");
  }

  async function download() {
    if (!view || !data) return;
    setExporting(true);
    setExportError("");
    try {
      const { dateFrom: from, dateTo: to, compareFrom, compareTo } = data.period;
      const blob = await exportReport(view.id, { ...query, from, to, compareFrom, compareTo });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = view.id + "-" + from + ".csv";
      anchor.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (reason) {
      setExportError(reason instanceof Error ? reason.message : "Could not export the report.");
    } finally {
      setExporting(false);
    }
  }

  const hasFilters = Boolean(query.truck || query.driver || query.dispatcher);

  function clearFilters() {
    setQuery(({ from, to, compareFrom, compareTo }) => ({ from, to, compareFrom, compareTo }));
    setExpanded("");
  }

  return <section className="min-w-0 space-y-4">
    <PageHeader>
      <div className="flex items-center gap-3">
        <ChartNoAxesCombined aria-hidden="true" className="h-5 w-5 text-zinc-500" />
        <h1 className="text-lg font-semibold text-zinc-100">Reports</h1>
      </div>
    </PageHeader>

    <nav aria-label="Report views" className="flex flex-wrap gap-x-4 border-b border-zinc-500/30">
      {available.map(item => <button key={item.id} type="button" aria-pressed={item.id === view.id}
        className={"border-b-2 px-1 py-2 text-[13px] " + (item.id === view.id ? "border-blue-500 font-semibold" : "border-transparent text-zinc-400 hover:text-zinc-100")}
        onClick={() => { if (item.id !== view.id) { setSelected(item.id); clearFilters(); } }}>
        {item.label}
      </button>)}
    </nav>

    <div role="group" aria-label="Report controls" className="flex flex-wrap items-center gap-2">
      <div role="group" aria-label="Displayed period" className="inline-flex rounded-lg border border-zinc-500/35 p-0.5">
        <PeriodButton active={!comparison} onClick={() => { setComparison(false); setExpanded(""); }}>
          {query.from || query.to ? "Custom period" : "Last week"}
        </PeriodButton>
        <PeriodButton active={comparison} onClick={() => { setComparison(true); setExpanded(""); }}>Comparison</PeriodButton>
      </div>
      <button type="button" className="ui-button" aria-expanded={editingDates} aria-controls="report-dates"
        onClick={() => setEditingDates(open => !open)}>
        <CalendarDays aria-hidden="true" className="h-4 w-4" /> Change dates
      </button>
      {filterFields.map(([field, label, options]) =>
        <select key={field} aria-label={label} className={controlClass + " !w-auto max-w-full sm:max-w-60"}
          value={query[field] ?? ""} onChange={event => updateFilter(field, event.target.value)}>
          <option value="">All {options}</option>
          {(previousData?.options[options] ?? []).map(option => <option key={option.id} value={option.id}>{option.name}</option>)}
        </select>
      )}
      {hasFilters && <button type="button" className="ui-button" onClick={clearFilters}>Clear filters</button>}
      <button type="button" className="ui-button sm:ml-auto disabled:opacity-50" onClick={download} disabled={!data || exporting}>
        <Download aria-hidden="true" className="h-4 w-4" /> {exporting ? "Exporting…" : "Export CSV"}
      </button>
    </div>

    <form id="report-dates" hidden={!editingDates} className="rounded-lg border border-zinc-500/30 p-3"
      onSubmit={event => {
        event.preventDefault();
        setQuery(q => ({ ...q, from: dates.from ?? (dates.to ? period?.dateFrom : undefined), to: dates.to ?? (dates.from ? period?.dateTo : undefined), compareFrom: dates.compareFrom || undefined, compareTo: dates.compareTo || undefined }));
        setExpanded("");
      }}>
      <div className="flex flex-wrap items-end gap-3">
        <fieldset className="min-w-0">
          <legend className="mb-1 text-xs font-medium">Current period</legend>
          <div className="flex flex-wrap gap-2">
            <DateInput label="From" required value={dates.from ?? period?.dateFrom ?? ""} onChange={value => setDates(d => ({ ...d, from: value }))} />
            <DateInput label="Through" required value={dates.to ?? period?.dateTo ?? ""} onChange={value => setDates(d => ({ ...d, to: value }))} />
          </div>
        </fieldset>
        <fieldset className="min-w-0">
          <legend className="mb-1 text-xs font-medium">Custom comparison</legend>
          <div className="flex flex-wrap gap-2">
            <DateInput label="Compare from" caption="From" required={Boolean(dates.compareTo)} value={dates.compareFrom ?? ""} onChange={value => setDates(d => ({ ...d, compareFrom: value }))} />
            <DateInput label="Compare through" caption="Through" required={Boolean(dates.compareFrom)} value={dates.compareTo ?? ""} onChange={value => setDates(d => ({ ...d, compareTo: value }))} />
          </div>
        </fieldset>
        <button type="submit" className="ui-button ui-button-primary">Apply dates</button>
        <button type="button" className="ui-button" onClick={() => {
          setDates({}); setQuery(q => ({ truck: q.truck, driver: q.driver, dispatcher: q.dispatcher })); setExpanded(""); setComparison(false);
        }}>Last completed week</button>
      </div>
    </form>

    {currentError && <p role="alert" className="text-sm text-red-600 dark:text-red-400">{currentError}{" "}
      <button className="underline" onClick={() => setRetry(value => value + 1)}>Retry</button></p>}
    {exportError && <p role="alert" className="text-sm text-red-600 dark:text-red-400">{exportError}</p>}
    {!data && !currentError && <p role="status" className="text-sm opacity-75">Loading report…</p>}
    {data && <>
      {!rows?.length ? <p className="rounded-md border border-zinc-500/30 p-4 text-sm">No source records match this period and filters.</p> :
        <ReportTable name={view.id === "dispatcher" ? "Dispatcher" : view.id === "driver" ? "Driver" : "Truck"}
          columns={view.columns} rows={rows} other={comparison ? data.current : data.comparison} expanded={expanded}
          toggle={id => setExpanded(value => value === id ? "" : id)} />}
      {view.id === "owner-operator" && rows?.some(row => row.metrics.netPayable !== undefined) &&
        <div className="space-y-2"><h2 className="text-lg font-semibold">Operator settlement</h2><p className="text-xs opacity-75">Finalized statements are shown in full for weeks starting within the selected dates.</p>
          <ReportTable name="Truck" columns={settlementColumns} rows={rows.filter(row => row.metrics.netPayable !== undefined)}
            other={comparison ? data.current : data.comparison} expanded={expanded.startsWith("settlement:") ? expanded.slice(11) : ""}
            toggle={id => setExpanded(value => value === "settlement:" + id ? "" : "settlement:" + id)} settlements />
        </div>}
    </>}
  </section>;
}

function DateInput({ label, caption = label, value, required, onChange }: {
  label: string; caption?: string; value: string; required?: boolean; onChange: (value: string) => void;
}) {
  return <label className="grid min-w-0 gap-1 text-xs text-zinc-400">
    {caption}
    <input aria-label={label} type="date" required={required} className={controlClass + " !w-auto max-w-full"}
      value={value} onChange={event => onChange(event.target.value)} />
  </label>;
}

function PeriodButton({ active, children, onClick }: { active: boolean; children: React.ReactNode; onClick: () => void }) {
  return <button type="button" aria-pressed={active} onClick={onClick}
    className={"rounded-md px-3 py-1.5 text-[13px] " + (active ? "bg-zinc-500/20 font-semibold" : "text-zinc-400 hover:bg-zinc-500/10")}>{children}</button>;
}

function ReportTable({ name, columns, rows, other, expanded, toggle, settlements }: {
  name: string; columns: string[]; rows: ReportRow[]; other: ReportRow[]; expanded: string; toggle: (id: string) => void; settlements?: boolean;
}) {
  const previous = new Map(other.map(row => [row.id, row]));
  return <div className="overflow-x-auto rounded-md border border-zinc-500/35">
    <table className="w-full min-w-max text-sm">
      <thead className="bg-zinc-500/10 text-xs"><tr>
        <th scope="col" className={cell + " text-left"}>{name}</th>
        {columns.map(key => <th scope="col" key={key} className={cell + " max-w-40 text-right font-medium"}>{labels[key]}</th>)}
      </tr></thead>
      <tbody>
        {rows.map(row => <Fragment key={row.id}>
          <tr className="hover:bg-zinc-500/5">
            <th scope="row" className={cell + " text-left font-medium"}>
              <button type="button" aria-expanded={expanded === row.id} onClick={() => toggle(row.id)} className="flex items-center gap-2 text-left">
                <ChevronDown size={14} className={expanded === row.id ? "rotate-180" : ""} /> {row.name}
              </button>
            </th>
            {columns.map(key => {
              const value = row.metrics[key];
              const compare = previous.get(row.id)?.metrics[key];
              const delta = value === undefined || compare === undefined ? undefined : value - compare;
              return <td key={key} className={cell + " whitespace-nowrap text-right tabular-nums"}>
                {formatMetric(key, value)}
                {delta !== undefined && delta !== 0 && <span title="Difference from the other period" className="block text-[11px] opacity-65">
                  {delta > 0 ? "+" : ""}{percents.has(key) ? number.format(delta) + " pp" : formatMetric(key, delta)}
                </span>}
              </td>;
            })}
          </tr>
          {expanded === row.id && <tr><td colSpan={columns.length + 1} className="p-0"><SourceRecords sources={settlements ?
            row.sources.filter(source => source.kind === "Driver Pay" && source.values?.netPayable !== undefined) : row.sources} /></td></tr>}
        </Fragment>)}
      </tbody>
    </table>
  </div>;
}

function sourceHref(source: ReportEvidence) {
  const [driverId, date, slot] = source.id.split("/");
  if (source.kind === "Gross Board") {
    return "/gross-board?" + new URLSearchParams({ driverId, date, slot, loadNumber: source.label });
  }
  if (source.kind === "Driver Pay") {
    return "/accounting/driver-pay?" + new URLSearchParams({ driverId, weekStart: date });
  }
  if (source.kind === "Maintenance" || source.kind === "Equipment") {
    return "/expenses?" + new URLSearchParams({ expenseId: source.id });
  }
  return "";
}

function SourceRecords({ sources }: { sources: ReportEvidence[] }) {
  return <div className="max-h-96 overflow-auto border-b border-zinc-500/30 bg-zinc-500/5 px-3 py-2">
    <table className="w-full text-left text-xs">
      <caption className="pb-2 text-left font-semibold">Source records</caption>
      <thead><tr>{["Date", "Source", "Record", "Assignment", "Status", "Details"].map(label => <th key={label} scope="col" className="px-2 pb-2">{label}</th>)}</tr></thead>
      <tbody>{sources.map((source, index) => { const href = sourceHref(source); return <tr key={source.kind + source.id + index} className="border-t border-zinc-500/20 align-top">
        <td className="whitespace-nowrap px-2 py-2">{source.date}</td>
        <td className="px-2 py-2">{source.kind}</td>
        <td className="px-2 py-2"><div>{href ? <Link href={href} prefetch={false} className="text-blue-600 underline dark:text-blue-400">{source.label}</Link> : source.label}</div><div className="break-all opacity-60">{source.id}</div></td>
        <td className="px-2 py-2">{[source.truck, source.driver, source.dispatcher].filter(Boolean).join(" · ")}</td>
        <td className="px-2 py-2 capitalize">{source.state}</td>
        <td className="px-2 py-2">
          {source.note && <p>{source.note}</p>}
          {Object.entries(source.values ?? {}).map(([key, value]) => <div key={key}>{labels[key] ?? key} · {formatMetric(key, value)}</div>)}
          {source.pickupActual && <div>Pickup actual · {source.pickupActual}</div>}
          {source.pickupScheduled && <div>Pickup scheduled · {source.pickupScheduled}</div>}
          {source.deliveryActual && <div>Delivery actual · {source.deliveryActual}</div>}
          {source.deliveryScheduled && <div>Delivery scheduled · {source.deliveryScheduled}</div>}
        </td>
      </tr>; })}</tbody>
    </table>
  </div>;
}
