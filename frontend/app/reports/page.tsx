"use client";

import { createContext, Fragment, useContext, useEffect, useMemo, useState } from "react";
import { ChartNoAxesCombined, ChevronDown, Download, RefreshCw } from "lucide-react";
import Link from "@/app/components/AccessLink";
import { MetricCard } from "@/app/components/MetricCard";
import { WeekSelector } from "@/app/components/WeekSelector";
import { useLatestPayrollWeek } from "@/app/accounting/components/useLatestPayrollWeek";
import { clampWeek } from "@/app/accounting/payrollWeek";
import { PageHeader } from "@/app/components/PageHeader";
import { controlClass } from "@/app/components/management/ManagementUI";
import { usePermissions } from "@/app/lib/access";
import { exportReport, fetchReport, fetchReportSources } from "@/app/lib/api";
import type { ReportEvidence, ReportRow, ReportsQuery, ReportsResult, ReportView } from "@/app/lib/types";
import { SortHeader, useTableSort } from "./tableSort";

const views: { id: ReportView; label: string; access: string; columns: string[] }[] = [
  { id: "company", label: "Company Truck P&L", access: "reports.read", columns: ["gross", "miles", "earnedPay", "fuel", "tolls", "scales", "maintenance", "margin", "revenuePerMile", "costPerMile", "contribution"] },
  { id: "owner-operator", label: "Owner-Operator Truck P&L", access: "reports.read", columns: ["gross", "miles", "carrierFees", "dispatchBalance", "recurringCharges", "contribution"] },
  { id: "dispatcher", label: "Dispatcher Performance", access: "reports.performance", columns: ["completedLoads", "gross", "miles", "truckWeeks", "grossPerTruckWeek", "loadedRPM", "deadheadPct"] },
  { id: "driver", label: "Driver Performance", access: "reports.performance", columns: ["completedLoads", "activeDays", "miles", "pickupOnTime", "pickupCoverage", "deliveryOnTime", "deliveryCoverage", "fuelEfficiency"] },
];

const settlementColumns = ["settlementEarnings", "reimbursements", "deductions", "escrowWithheld", "escrowReturned", "escrowApplied", "netPayable"];

const labels: Record<string, string> = {
  gross: "Load gross", earnedPay: "Earned driver pay", fuel: "Fuel", tolls: "Tolls", scales: "Scales", maintenance: "Maintenance",
  equipment: "Equipment", contribution: "Profit", margin: "Margin", miles: "Miles", revenuePerMile: "RPM",
  costPerMile: "CPM", provisionalGross: "Provisional gross", provisionalLoads: "Provisional entries", unassignedCost: "Unassigned costs",
  dispatchBalance: "Dispatch adjustment balance", carrierFees: "Carrier fees", carrierCosts: "Carrier-borne costs", settlementEarnings: "Earnings", reimbursements: "Reimbursements", deductions: "Deductions",
  escrowWithheld: "Escrow withheld", escrowReturned: "Escrow returned", escrowApplied: "Escrow applied", netPayable: "Net payable", completedLoads: "Loads",
  truckWeeks: "Active truck-weeks", grossPerTruckWeek: "Gross per active truck-week", loadedRPM: "Loaded RPM", deadheadPct: "Deadhead",
  activeDays: "Active days", pickupOnTime: "On-time pickup", pickupCoverage: "Pickup timestamp coverage", deliveryOnTime: "On-time delivery",
  deliveryCoverage: "Delivery timestamp coverage", fuelEfficiency: "Fuel efficiency (MPG)", loadedMiles: "Loaded miles", deadheadMiles: "Deadhead miles",
  amount: "Amount",
  recurringCharges: "RC (recurring charges)",
};

const currency = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD" });
const number = new Intl.NumberFormat("en-US", { maximumFractionDigits: 2 });
const percents = new Set(["margin", "deadheadPct", "pickupOnTime", "pickupCoverage", "deliveryOnTime", "deliveryCoverage"]);
const units = new Set(["completedLoads", "truckWeeks", "miles", "activeDays", "fuelEfficiency", "loadedMiles", "deadheadMiles"]);

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
const SourceContext = createContext<{ view: ReportView; query: ReportsQuery; revision?: string }>({ view: "company", query: {} });

export default function ReportsPage() {
  const permissions = usePermissions();
  const available = views.filter(view => permissions.includes(view.access));
  const [selected, setSelected] = useState<ReportView>("company");
  const view = available.find(item => item.id === selected) ?? available[0];
  const id = view?.id;
  const [filters, setQuery] = useState<ReportsQuery>(blankFilters);
  const latestWeek = useLatestPayrollWeek();
  const [selectedWeek, setWeek] = useState("");
  const week = clampWeek(selectedWeek || latestWeek, latestWeek);
  const query = useMemo<ReportsQuery>(() => ({ ...filters, week }), [filters, week]);
  const [result, setResult] = useState<{ key: string; data: ReportsResult }>();
  const [error, setError] = useState<{ key: string; message: string }>();
  const [exportError, setExportError] = useState("");
  const [exporting, setExporting] = useState(false);
  const [retry, setRetry] = useState(0);
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
  const rows = data?.current;


  function updateFilter(field: "truck" | "driver" | "dispatcher", value: string) {
    setQuery(query => ({ ...query, [field]: value }));
    setExpanded("");
  }

  async function download() {
    if (!view || !data) return;
    setExporting(true);
    setExportError("");
    try {
      const { dateFrom: from } = data.period;
      const blob = await exportReport(view.id, { ...query, revision: data.revision });
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
    setQuery({});
    setExpanded("");
  }

  return <SourceContext.Provider value={{ view: view.id, query, revision:data?.revision }}><section className="min-w-0 space-y-4" data-report-ready={Boolean(data)}>
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

    <ReportSummary rows={rows} financial={view.id === "company" || view.id === "owner-operator"} loading={!data && !currentError} />
    <div role="group" aria-label="Report controls" className="flex flex-wrap items-center gap-2">
      <WeekSelector week={week} lastWeek={latestWeek} onChange={value => { setWeek(value); setExpanded(""); }}
        jump={{ label: "Last completed week", onClick: () => { setWeek(""); setExpanded(""); }, disabled: week === latestWeek }} />
      {filterFields.map(([field, label, options]) =>
        <div key={field} className="relative max-w-full sm:max-w-60">
        <select aria-label={label} className={controlClass + " !w-auto max-w-full appearance-none !pr-9"}
          value={query[field] ?? ""} onChange={event => updateFilter(field, event.target.value)}>
          <option value="">All {options}</option>
          {(previousData?.options[options] ?? []).map(option => <option key={option.id} value={option.id}>{option.name}</option>)}
        </select>
        <ChevronDown aria-hidden="true" className="pointer-events-none absolute right-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-zinc-500" />
        </div>
      )}
      {hasFilters && <button type="button" className="ui-button" onClick={clearFilters}>Clear filters</button>}
      <button type="button" aria-label="Refresh report" title="Refresh report" className="ui-button sm:ml-auto" onClick={()=>{setExpanded("");setRetry(n=>n+1);}}><RefreshCw aria-hidden="true" className="h-4 w-4" /></button>
      <button type="button" className="ui-button disabled:opacity-50" onClick={download} disabled={!data || exporting}>
        <Download aria-hidden="true" className="h-4 w-4" /> {exporting ? "Exporting…" : "Export CSV"}
      </button>
    </div>

    {currentError && <p role="alert" className="text-sm text-red-600 dark:text-red-400">{currentError}{" "}
      <button className="underline" onClick={() => setRetry(value => value + 1)}>Retry</button></p>}
    {exportError && <p role="alert" className="text-sm text-red-600 dark:text-red-400">{exportError}</p>}
    {!data && !currentError && <p role="status" className="text-sm opacity-75">Loading report…</p>}
    {data && <>
      {!rows?.length ? <p className="rounded-md border border-zinc-500/30 p-4 text-sm">No source records match this period and filters.</p> :
        <ReportTable key={key} name={view.id === "dispatcher" ? "Dispatcher" : view.id === "driver" ? "Driver" : "Truck"}
          columns={view.columns} rows={rows} expanded={expanded}
          toggle={id => setExpanded(value => value === id ? "" : id)} />}
      {view.id === "owner-operator" && rows?.some(row => row.metrics.netPayable !== undefined) &&
        <div className="space-y-2"><h2 className="text-lg font-semibold">Operator settlement</h2><p className="text-xs opacity-75">Finalized statements for the selected week.</p>
          <ReportTable key={key+"settlements"} name="Truck" columns={settlementColumns} rows={rows.filter(row => row.metrics.netPayable !== undefined)}
            expanded={expanded.startsWith("settlement:") ? expanded.slice(11) : ""}
            toggle={id => setExpanded(value => value === "settlement:" + id ? "" : "settlement:" + id)} settlements />
        </div>}
    </>}
  </section></SourceContext.Provider>;
}

function ReportTable({ name, columns, rows, expanded, toggle, settlements }: {
  name: string; columns: string[]; rows: ReportRow[]; expanded: string; toggle: (id: string) => void; settlements?: boolean;
}) {
  const [page, setPage] = useState(1);
  const { sorted, sort, changeSort, register } = useTableSort(rows, reportSortValue);
  const trucks = name === "Truck";
  const onSort = (key: string) => { changeSort(key); setPage(1); };
  return <div className="overflow-x-auto rounded-md border border-zinc-500/35">
    <table aria-label={settlements ? "Operator settlement" : `${name} report`} className="w-full min-w-max text-sm">
      <thead className="bg-zinc-500/10 text-xs"><tr>
        <SortHeader column="name" label={name} sort={sort} onSort={onSort} className={cell} />
        {trucks && <SortHeader column="assignedDrivers" label="Assigned driver" sort={sort} onSort={onSort} className={cell} />}
        {columns.map(key => <SortHeader key={key} column={key} label={labels[key]} sort={sort} onSort={onSort} right className={cell} />)}
      </tr></thead>
      <tbody>
        {sorted.slice((page-1)*50,page*50).map(row => <Fragment key={row.id}>
          <tr ref={element => register(row.id, element)} data-report-row={row.id} onClick={() => toggle(row.id)} className="cursor-pointer hover:bg-zinc-500/5">
            <th scope="row" className={cell + " text-left font-medium"}>
              <div className="flex items-center gap-1.5">
              <button type="button" aria-label={`Source records for ${row.name}`} aria-expanded={expanded === row.id} onClick={event => { event.stopPropagation(); toggle(row.id); }} className="-ml-1 flex h-6 w-6 shrink-0 items-center justify-center rounded hover:bg-zinc-500/10 focus-visible:outline-2 focus-visible:outline-blue-500">
                <ChevronDown aria-hidden="true" size={14} className={expanded === row.id ? "rotate-180" : ""} />
              </button>
              {row.id !== "unassigned" && (trucks || name === "Driver") ?
                <Link href={`/${trucks ? "trucks" : "drivers"}/detail?id=${encodeURIComponent(row.id)}`} prefetch={false} onClick={event => event.stopPropagation()} className="text-blue-600 hover:underline dark:text-blue-400">{row.name}</Link> : row.name}
              </div>
            </th>
            {trucks && <td className={cell + " whitespace-nowrap"}>{row.assignedDrivers?.map((driver, index) => <Fragment key={driver.id}>
              {index > 0 && <span className="text-zinc-500"> · </span>}
              <Link href={`/drivers/detail?id=${encodeURIComponent(driver.id)}`} prefetch={false} onClick={event => event.stopPropagation()} className="text-blue-600 hover:underline dark:text-blue-400">{driver.name}</Link>
            </Fragment>)}</td>}
            {columns.map(key => {
              const value = row.metrics[key];
              return <td key={key} className={cell + " whitespace-nowrap text-right tabular-nums"}>
                {formatMetric(key, value)}
              </td>;
            })}
          </tr>
          {expanded === row.id && <tr ref={element => register(row.id + ":sources", element)}><td colSpan={columns.length + (trucks ? 2 : 1)} className="p-0"><SourceRecords group={row.id} settlements={settlements} /></td></tr>}
        </Fragment>)}
      </tbody>
    </table>
    {rows.length > 50 && <div className="flex items-center justify-end gap-3 p-2 text-xs"><span>{(page-1)*50+1}–{Math.min(page*50,rows.length)} of {rows.length}</span><button className="ui-button" disabled={page===1} onClick={() => setPage(p=>p-1)}>Previous</button><button className="ui-button" disabled={page*50>=rows.length} onClick={() => setPage(p=>p+1)}>Next</button></div>}
  </div>;
}

function reportSortValue(row: ReportRow, key: string) {
  if (key === "name") return row.name;
  if (key === "assignedDrivers") return row.assignedDrivers?.map(driver => driver.name).join(" · ");
  return row.metrics[key];
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

function SourceRecords({ group, settlements }: { group: string; settlements?: boolean }) {
  const {view, query, revision} = useContext(SourceContext);
  const [records, setRecords] = useState<ReportEvidence[]>();
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const abort = new AbortController();
    fetchReportSources(view, { ...query, revision }, group, abort.signal).then(data => { if (!abort.signal.aborted) setRecords(data); }, reason => { if (!abort.signal.aborted) setError(reason.message); });
    return () => abort.abort();
  }, [view, query, group, revision, retry]);
  if (!records) return <div className="p-4 text-xs" role={error ? "alert" : "status"}>{error || "Loading source records…"}{error && <button className="ui-button ml-2" onClick={() => setRetry(n=>n+1)}>Retry</button>}</div>;
  return <SourceTable sources={settlements ? records.filter(s => s.kind === "Driver Pay" && s.values?.netPayable !== undefined) : records} />;
}

function sourceSortValue(source: ReportEvidence, key: string) {
  if (key === "assignment") return [source.truck, source.driver, source.dispatcher].filter(Boolean).join(" · ");
  if (key === "details") return [source.note, ...Object.entries(source.values ?? {}).map(([key, value]) => `${labels[key] ?? key} · ${formatMetric(key, value)}`), source.pickupActual, source.pickupScheduled, source.deliveryActual, source.deliveryScheduled].filter(Boolean).join(" · ");
  return source[key as "date" | "kind" | "label"];
}

function SourceTable({ sources }: { sources: ReportEvidence[] }) {
  const { sorted, sort, changeSort, register } = useTableSort(sources, sourceSortValue);
  const sourceKeys = useMemo(() => new Map(sources.map((source, index) => [source, `${source.kind}/${source.id}/${index}`])), [sources]);
  return <div className="max-h-96 overflow-auto border-b border-zinc-500/30 bg-zinc-500/5 px-3 py-2">
    <table className="w-full text-left text-xs">
      <caption className="pb-2 text-left font-semibold">Source records</caption>
      <thead><tr>{[["date", "Date"], ["kind", "Source"], ["label", "Record"], ["assignment", "Assignment"], ["details", "Details"]].map(([key, label]) => <SortHeader key={key} column={key} label={label} sort={sort} onSort={changeSort} className="px-2 pb-2" />)}</tr></thead>
      <tbody>{sorted.map(source => { const href = sourceHref(source); const key = sourceKeys.get(source)!; return <tr key={key} ref={element => register(key, element)} className="border-t border-zinc-500/20 align-top">
        <td className="whitespace-nowrap px-2 py-2">{source.date}</td>
        <td className="px-2 py-2">{source.kind}</td>
        <td className="px-2 py-2"><div>{href ? <Link href={href} prefetch={false} className="text-blue-600 underline dark:text-blue-400">{source.label}</Link> : source.label}</div><div className="break-all opacity-60">{source.id}</div></td>
        <td className="px-2 py-2">{[source.truck, source.driver, source.dispatcher].filter(Boolean).join(" · ")}</td>
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

function ReportSummary({rows, financial, loading}: { rows?: ReportRow[]; financial: boolean; loading: boolean }) {
  const sum = (metric: string) => rows?.length && rows.every(r=>r.metrics[metric] !== undefined) ? rows.reduce((total,r)=>total+(r.metrics[metric]??0),0) : undefined;
  return <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
    <MetricCard icon={ChartNoAxesCombined} label="Load gross" value={formatMetric("gross",sum("gross"))} loading={loading} />
    <MetricCard icon={ChartNoAxesCombined} label={financial ? "Profit" : "Reported groups"} value={financial ? formatMetric("contribution",sum("contribution")) : number.format(rows?.length ?? 0)} loading={loading} />
    <MetricCard icon={ChartNoAxesCombined} label="Source records" value={number.format(rows?.reduce((total,r)=>total+r.sourceCount,0) ?? 0)} loading={loading} />
    <MetricCard icon={ChartNoAxesCombined} label="Miles" value={formatMetric("miles",sum("miles"))} loading={loading} />
  </div>;
}
