"use client";

import { useMemo, useState } from "react";
import { geoAlbersUsa, geoPath } from "d3-geo";
import { feature } from "topojson-client";
import type { FeatureCollection, Geometry } from "geojson";
import type { GeometryCollection, Topology } from "topojson-specification";
import { MapPinned } from "lucide-react";
import statesTopology from "us-atlas/states-10m.json";
import { STATE_CODES, STATE_NAMES } from "../lib/usStates";
import type { TollDashboard } from "../lib/types";

const money = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD" });
const compactMoney = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", notation: "compact", maximumFractionDigits: 1 });

export function TollSpendingMap({ dashboard }: { dashboard: TollDashboard }) {
  const [selected, setSelected] = useState<string | null>(null);
  const [hovered, setHovered] = useState<string | null>(null);
  const ranked = useMemo(() => [...dashboard.states].sort((a, b) => b.spend - a.spend || a.label.localeCompare(b.label)), [dashboard.states]);
  const byState = useMemo(() => new Map(ranked.map((point) => [point.label, point])), [ranked]);
  const activeCode = hovered ?? selected ?? ranked[0]?.label;
  const active = activeCode ? byState.get(activeCode) : undefined;
  const maxSpend = Math.max(0, ...ranked.map((point) => point.spend));
  const unmappedCount = dashboard.unmapped.reduce((count, point) => count + point.transactionCount, 0);
  // Convert the cent-precision API amounts to integer cents before adding them.
  const unmappedCents = dashboard.unmapped.reduce((cents, point) => cents + Math.round(point.spend * 100), 0);
  const paths = useMemo(() => {
    const topology = statesTopology as unknown as Topology;
    const collection = feature(topology, topology.objects.states as GeometryCollection) as unknown as FeatureCollection<Geometry, { name?: string }>;
    const projection = geoAlbersUsa().fitExtent([[18, 18], [942, 502]], collection);
    const path = geoPath(projection);
    return collection.features.map((item) => ({
      name: item.properties?.name ?? "", code: STATE_CODES[item.properties?.name ?? ""], path: path(item) ?? "",
    })).filter((item) => item.code);
  }, []);
  const colorFor = (code: string) => {
    const point = byState.get(code);
    if (!point) return "#27272a";
    if (point.spend < 0) return "#34d399";
    const ratio = maxSpend > 0 ? Math.max(0, point.spend) / maxSpend : 0;
    return `rgb(${Math.round(30 + 29 * ratio)}, ${Math.round(58 + 72 * ratio)}, ${Math.round(95 + 151 * ratio)})`;
  };

  return (
    <section className="min-w-0 rounded-xl border border-zinc-800/60 bg-card p-4 sm:p-5" aria-labelledby="toll-map-title">
      <div className="flex items-center gap-2">
        <MapPinned className="h-4 w-4 text-blue-400" />
        <h2 id="toll-map-title" className="text-sm font-medium text-zinc-200">Where toll money goes</h2>
      </div>
      <p className="mt-1 text-[12px] text-zinc-500">Net spending by state for the selected posting dates. Brighter blue means higher spending. Hover, tap, or select a state to see its total.</p>
      <div className="mt-4 grid min-w-0 gap-4 xl:grid-cols-[minmax(0,1fr)_280px]">
        <div className="min-w-0">
          <div className="rounded-xl border border-zinc-800/60 bg-zinc-950/35 p-2">
            <svg viewBox="0 0 960 520" className="block h-auto w-full" role="group" aria-label="Toll spending by state map">
              {paths.map(({ name, code, path }) => (
                <path key={code} d={path} fill={colorFor(code)} stroke={activeCode === code ? "#e4e4e7" : "#09090b"} strokeWidth={activeCode === code ? 2.5 : 1.5}
                  role="button" tabIndex={0} aria-label={`${name}: ${byState.has(code) ? money.format(byState.get(code)!.spend) : "No mapped transactions"}`} aria-pressed={selected === code}
                  className="cursor-pointer outline-none transition-colors hover:opacity-80 focus:stroke-white focus:stroke-[3px]"
                  onMouseEnter={() => setHovered(code)} onMouseLeave={() => setHovered(null)}
                  onFocus={() => setHovered(code)} onBlur={() => setHovered(null)} onClick={() => setSelected(code)}
                  onKeyDown={(event) => { if (event.key === "Enter" || event.key === " ") { event.preventDefault(); setSelected(code); } }}>
                  <title>{name}: {byState.has(code) ? money.format(byState.get(code)!.spend) : "No mapped transactions"}</title>
                </path>
              ))}
            </svg>
            <div className="flex flex-wrap items-center justify-between gap-2 px-2 pb-2 text-[10px] text-zinc-500">
              <div className="flex items-center gap-2"><span>$0</span><span className="h-2 w-24 rounded-full bg-gradient-to-r from-[#1e3a5f] to-blue-500" /><span>{compactMoney.format(maxSpend)}</span></div>
              <div className="flex items-center gap-3"><span>Gray: no mapped transactions</span><span className="text-emerald-400">Green: net credit</span></div>
            </div>
          </div>
          <div aria-live="polite" className="mt-3 flex min-h-20 flex-wrap items-center justify-between gap-3 rounded-lg border border-zinc-800 bg-zinc-950/40 px-4 py-3">
            <div><div className="text-[13px] font-medium text-zinc-200">{activeCode ? STATE_NAMES[activeCode] : "No mapped states"}</div><div className="mt-1 text-[11px] text-zinc-500">{active ? `${active.transactionCount.toLocaleString()} transactions · ${dashboard.totals.spend > 0 ? `${(active.spend / dashboard.totals.spend * 100).toFixed(1)}% of total net toll spend` : "Net charges after credits"}` : "No mapped transactions in this range"}</div></div>
            <div className="font-mono text-xl font-semibold tabular-nums text-zinc-100">{money.format(active?.spend ?? 0)}</div>
          </div>
        </div>
        <div className="min-w-0">
          <h3 className="text-[12px] font-medium text-zinc-300">Highest spending states</h3>
          <div className="mt-2 max-h-[420px] space-y-1 overflow-y-auto">
            {ranked.length === 0 && <p className="py-4 text-[12px] text-zinc-500">These agencies could not be assigned to a single state.</p>}
            {ranked.map((point, index) => (
              <button key={point.label} type="button" onClick={() => { setSelected(point.label); setHovered(null); }} aria-pressed={selected === point.label}
                className={`block w-full rounded-lg px-3 py-2 text-left transition hover:bg-zinc-800/50 ${activeCode === point.label ? "bg-zinc-800/50" : ""}`}>
                <div className="flex items-center justify-between gap-2 text-[12px]"><span className="text-zinc-300"><span className="mr-2 text-zinc-600">{index + 1}</span>{STATE_NAMES[point.label]}</span><span className="font-mono tabular-nums text-zinc-200">{money.format(point.spend)}</span></div>
                <div className="mt-2 h-1 rounded-full bg-zinc-800"><div className="h-full rounded-full bg-blue-500" style={{ width: `${maxSpend > 0 ? Math.max(0, point.spend) / maxSpend * 100 : 0}%` }} /></div>
              </button>
            ))}
          </div>
        </div>
      </div>
      <p className="mt-4 text-[11px] leading-relaxed text-zinc-500">Uses the toll agency state supplied by PrePass. Older records without a state use verified single-state agency locations. Records that still cannot be located are listed below.</p>
      {unmappedCount > 0 && (
        <details className="mt-3 rounded-lg border border-amber-500/20 bg-amber-500/5 px-3 py-2 text-[12px]">
          <summary className="cursor-pointer text-amber-200/80">Unmapped location: {money.format(unmappedCents / 100)} · {unmappedCount.toLocaleString()} transactions</summary>
          <div className="mt-2 grid gap-x-6 gap-y-1 sm:grid-cols-2">
            {[...dashboard.unmapped].sort((a, b) => b.spend - a.spend).map((point) => <div key={point.label} className="flex justify-between gap-3 py-1 text-zinc-400"><span>{point.label || "Unknown agency"}</span><span className="font-mono">{money.format(point.spend)}</span></div>)}
          </div>
        </details>
      )}
    </section>
  );
}
