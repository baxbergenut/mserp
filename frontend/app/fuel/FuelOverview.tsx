"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import {
  Bar,
  BarChart,
  CartesianGrid,
  Legend,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { geoAlbersUsa, geoPath } from "d3-geo";
import { feature } from "topojson-client";
import type { Feature, FeatureCollection, Geometry } from "geojson";
import type { GeometryCollection, Topology } from "topojson-specification";
import {
  AlertCircle,
  BadgeDollarSign,
  CalendarDays,
  CircleDollarSign,
  Droplets,
  Info,
  MapPinned,
} from "lucide-react";
import statesTopology from "us-atlas/states-10m.json";
import { fetchFuelDashboard } from "../lib/api";
import type { FuelDashboard } from "../lib/types";

const inputClass =
  "rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-1.5 text-[13px] text-zinc-300 outline-none transition-colors focus:border-zinc-600";

const money = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  maximumFractionDigits: 0,
});

const price = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

const compact = new Intl.NumberFormat("en-US", {
  notation: "compact",
  maximumFractionDigits: 1,
});

const gallons = new Intl.NumberFormat("en-US", { maximumFractionDigits: 0 });

const STATE_CODES: Record<string, string> = {
  Alabama: "AL", Alaska: "AK", Arizona: "AZ", Arkansas: "AR", California: "CA",
  Colorado: "CO", Connecticut: "CT", Delaware: "DE", "District of Columbia": "DC",
  Florida: "FL", Georgia: "GA", Hawaii: "HI", Idaho: "ID", Illinois: "IL",
  Indiana: "IN", Iowa: "IA", Kansas: "KS", Kentucky: "KY", Louisiana: "LA",
  Maine: "ME", Maryland: "MD", Massachusetts: "MA", Michigan: "MI", Minnesota: "MN",
  Mississippi: "MS", Missouri: "MO", Montana: "MT", Nebraska: "NE", Nevada: "NV",
  "New Hampshire": "NH", "New Jersey": "NJ", "New Mexico": "NM", "New York": "NY",
  "North Carolina": "NC", "North Dakota": "ND", Ohio: "OH", Oklahoma: "OK", Oregon: "OR",
  Pennsylvania: "PA", "Rhode Island": "RI", "South Carolina": "SC", "South Dakota": "SD",
  Tennessee: "TN", Texas: "TX", Utah: "UT", Vermont: "VT", Virginia: "VA",
  Washington: "WA", "West Virginia": "WV", Wisconsin: "WI", Wyoming: "WY",
};

type MapFeature = Feature<Geometry, { name?: string }>;

type TooltipEntry = {
  color?: string;
  dataKey?: unknown;
  name?: string | number;
  value?: unknown;
};

function isoDate(date: Date) {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}

function monthLabel(value: string) {
  const [year, month] = value.split("-").map(Number);
  return new Intl.DateTimeFormat("en-US", { month: "short" }).format(
    new Date(year, month - 1, 1),
  );
}

function weekLabel(value: string) {
  const [year, month, day] = value.split("-").map(Number);
  return new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric" }).format(
    new Date(year, month - 1, day),
  );
}

function chartValue(dataKey: unknown, value: number) {
  switch (dataKey) {
    case "spend":
    case "grossRevenue":
    case "fuelSpend":
      return money.format(value);
    case "gallons":
      return `${gallons.format(value)} gal`;
    case "fuelToGrossRatio":
      return `${value.toFixed(1)}%`;
    case "pricePerGallon":
    case "discountPerGallon":
    case "averageFuelPrice":
    case "revenuePerMile":
      return price.format(value);
    default:
      return value.toLocaleString();
  }
}

function ChartTooltip({
  active,
  payload,
  label,
}: {
  active?: boolean;
  payload?: readonly TooltipEntry[];
  label?: string | number;
}) {
  if (!active || !payload?.length) return null;
  return (
    <div className="rounded-lg border border-zinc-700 bg-zinc-950/95 px-3 py-2 shadow-xl backdrop-blur">
      <div className="mb-1.5 text-[11px] font-medium text-zinc-500">{label}</div>
      <div className="space-y-1">
        {payload.map((item) => (
          <div key={String(item.dataKey)} className="flex items-center justify-between gap-5 text-[12px]">
            <span className="flex items-center gap-1.5 text-zinc-400">
              <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: item.color }} />
              {item.name}
            </span>
            <span className="font-mono font-medium tabular-nums text-zinc-100">
              {chartValue(item.dataKey, Number(item.value ?? 0))}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

function ChartCard({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <section className="min-w-0 rounded-xl border border-zinc-800/60 bg-card p-4 sm:p-5">
      <h2 className="text-sm font-medium text-zinc-200">{title}</h2>
      <p className="mt-1 text-[11px] leading-relaxed text-zinc-600">{description}</p>
      <div className="mt-5 h-[270px]">{children}</div>
    </section>
  );
}

function KpiCard({
  label,
  value,
  detail,
  icon: Icon,
  accent,
}: {
  label: string;
  value: string;
  detail: string;
  icon: typeof CircleDollarSign;
  accent: string;
}) {
  return (
    <div className="rounded-xl border border-zinc-800/60 bg-card p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-[11px] font-medium uppercase tracking-wider text-zinc-600">{label}</div>
          <div className="mt-2 font-mono text-2xl font-semibold tabular-nums text-zinc-100">{value}</div>
          <div className="mt-1 text-[11px] text-zinc-600">{detail}</div>
        </div>
        <div className="rounded-lg border border-zinc-800 bg-zinc-950/60 p-2">
          <Icon className={`h-4 w-4 ${accent}`} />
        </div>
      </div>
    </div>
  );
}

function interpolateColor(ratio: number) {
  const start = [39, 39, 42];
  const end = [59, 130, 246];
  const clamped = Math.max(0, Math.min(1, ratio));
  const channel = (index: number) => Math.round(start[index] + (end[index] - start[index]) * clamped);
  return `rgb(${channel(0)}, ${channel(1)}, ${channel(2)})`;
}

function FuelMap({ dashboard }: { dashboard: FuelDashboard }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [tooltip, setTooltip] = useState<{
    x: number;
    y: number;
    state: string;
    data?: FuelDashboard["statePrices"][number];
  } | null>(null);

  const features = useMemo(() => {
    const topology = statesTopology as unknown as Topology;
    const collection = feature(
      topology,
      topology.objects.states as GeometryCollection,
    ) as unknown as FeatureCollection<Geometry, { name?: string }>;
    return collection.features.filter((item) => Boolean(STATE_CODES[item.properties?.name ?? ""])) as MapFeature[];
  }, []);

  const paths = useMemo(() => {
    const collection: FeatureCollection<Geometry, { name?: string }> = {
      type: "FeatureCollection",
      features,
    };
    const projection = geoAlbersUsa().fitExtent([[18, 18], [942, 502]], collection);
    const path = geoPath(projection);
    return features.map((item) => ({ feature: item, path: path(item) ?? "" }));
  }, [features]);

  const pricesByState = useMemo(
    () => new Map(dashboard.statePrices.map((item) => [item.state.toUpperCase(), item])),
    [dashboard.statePrices],
  );
  const values = dashboard.statePrices.map((item) => item.averagePrice);
  const min = values.length ? Math.min(...values) : 0;
  const max = values.length ? Math.max(...values) : 0;

  const colorFor = (state: string) => {
    const value = pricesByState.get(state)?.averagePrice;
    if (value === undefined) return "#27272a";
    return interpolateColor(max === min ? 0.7 : (value - min) / (max - min));
  };

  return (
    <div ref={containerRef} className="relative mt-4 overflow-hidden rounded-xl border border-zinc-800/60 bg-zinc-950/35">
      <svg viewBox="0 0 960 520" className="block h-auto w-full" role="img" aria-label="Average fuel price by state">
        {paths.map(({ feature: item, path }) => {
          const name = item.properties?.name ?? "Unknown";
          const code = STATE_CODES[name];
          return (
            <path
              key={code}
              d={path}
              fill={colorFor(code)}
              stroke="#09090b"
              strokeWidth={1.5}
              className="cursor-default outline-none transition-opacity hover:opacity-80 focus:opacity-80"
              tabIndex={0}
              onMouseMove={(event) => {
                const bounds = containerRef.current?.getBoundingClientRect();
                if (!bounds) return;
                setTooltip({
                  x: event.clientX - bounds.left,
                  y: event.clientY - bounds.top,
                  state: `${name} (${code})`,
                  data: pricesByState.get(code),
                });
              }}
              onMouseLeave={() => setTooltip(null)}
              onFocus={() => setTooltip({ x: 20, y: 20, state: `${name} (${code})`, data: pricesByState.get(code) })}
              onBlur={() => setTooltip(null)}
            />
          );
        })}
      </svg>
      {tooltip && (
        <div
          className="pointer-events-none absolute z-10 min-w-40 -translate-x-1/2 -translate-y-full rounded-lg border border-zinc-700 bg-zinc-950/95 px-3 py-2 shadow-xl"
          style={{ left: tooltip.x, top: Math.max(tooltip.y - 10, 70) }}
        >
          <div className="text-[11px] font-medium text-zinc-400">{tooltip.state}</div>
          {tooltip.data ? (
            <>
              <div className="mt-1 font-mono text-base font-semibold text-zinc-100">
                {price.format(tooltip.data.averagePrice)} <span className="text-[11px] font-normal text-zinc-500">/ gal</span>
              </div>
              <div className="mt-0.5 text-[10px] text-zinc-600">
                {gallons.format(tooltip.data.gallons)} gal · {tooltip.data.transactionCount.toLocaleString()} transactions
              </div>
            </>
          ) : (
            <div className="mt-1 text-[11px] text-zinc-600">No purchases in this range</div>
          )}
        </div>
      )}
      <div className="absolute bottom-3 left-3 rounded-lg border border-zinc-800 bg-zinc-950/85 px-3 py-2 text-[10px] text-zinc-500">
        <div className="mb-1.5 flex items-center justify-between gap-8">
          <span>{values.length ? price.format(min) : "No data"}</span>
          <span>{values.length ? price.format(max) : ""}</span>
        </div>
        <div className="h-1.5 w-40 rounded-full bg-gradient-to-r from-zinc-700 to-blue-500" />
      </div>
    </div>
  );
}

export function FuelOverview({ refreshKey }: { refreshKey: number }) {
  const today = useMemo(() => new Date(), []);
  const [dateFrom, setDateFrom] = useState(() => `${today.getFullYear()}-01-01`);
  const [dateTo, setDateTo] = useState(() => isoDate(today));
  const [dashboard, setDashboard] = useState<FuelDashboard | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    fetchFuelDashboard({ dateFrom, dateTo })
      .then((result) => {
        if (!cancelled) {
          setDashboard(result);
          setError("");
        }
      })
      .catch((reason) => {
        if (!cancelled) setError(reason instanceof Error ? reason.message : "Failed to load fuel overview.");
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });
    return () => { cancelled = true; };
  }, [dateFrom, dateTo, refreshKey]);

  if (isLoading && !dashboard) {
    return (
      <div className="space-y-4">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          {Array.from({ length: 3 }).map((_, index) => (
            <div key={index} className="h-28 animate-pulse rounded-xl border border-zinc-800/60 bg-zinc-900/30" />
          ))}
        </div>
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          {Array.from({ length: 4 }).map((_, index) => (
            <div key={index} className="h-[350px] animate-pulse rounded-xl border border-zinc-800/60 bg-zinc-900/30" />
          ))}
        </div>
      </div>
    );
  }

  if (!dashboard) {
    return (
      <div className="flex items-center gap-2 rounded-xl border border-red-500/20 bg-red-500/5 px-4 py-8 text-[13px] text-red-300">
        <AlertCircle className="h-4 w-4" /> {error || "Fuel overview is unavailable."}
      </div>
    );
  }

  const monthly = dashboard.monthly.map((item) => ({ ...item, label: monthLabel(item.month) }));
  const weekly = dashboard.weekly.map((item) => ({ ...item, label: weekLabel(item.weekStart) }));
  const averagePaid = dashboard.totals.gallons > 0 ? dashboard.totals.spend / dashboard.totals.gallons : 0;

  return (
    <div className="space-y-4">
      {error && (
        <div className="flex items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/5 px-3 py-2 text-[12px] text-red-300">
          <AlertCircle className="h-3.5 w-3.5" /> {error}
        </div>
      )}

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <KpiCard label={`${dashboard.year} fuel expense`} value={money.format(dashboard.totals.spend)} detail={`${price.format(averagePaid)} weighted avg / gal`} icon={CircleDollarSign} accent="text-blue-400" />
        <KpiCard label={`${dashboard.year} gallons purchased`} value={gallons.format(dashboard.totals.gallons)} detail="Diesel gallons only" icon={Droplets} accent="text-cyan-400" />
        <KpiCard label={`${dashboard.year} discount saved`} value={money.format(dashboard.totals.saved)} detail={`${dashboard.totals.gallons > 0 ? price.format(dashboard.totals.saved / dashboard.totals.gallons) : "$0.00"} saved / gal`} icon={BadgeDollarSign} accent="text-emerald-400" />
      </div>

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <ChartCard title="Monthly fuel spending" description={`${dashboard.year} diesel spend and gallons purchased.`}>
          <ResponsiveContainer width="100%" height="100%">
            <BarChart data={monthly} margin={{ top: 5, right: 4, left: 0, bottom: 0 }} accessibilityLayer>
              <CartesianGrid stroke="#27272a" vertical={false} />
              <XAxis dataKey="label" tick={{ fill: "#71717a", fontSize: 11 }} tickLine={false} axisLine={false} />
              <YAxis yAxisId="money" tickFormatter={(value) => `$${compact.format(Number(value))}`} tick={{ fill: "#52525b", fontSize: 10 }} tickLine={false} axisLine={false} width={50} />
              <YAxis yAxisId="gallons" orientation="right" tickFormatter={(value) => compact.format(Number(value))} tick={{ fill: "#52525b", fontSize: 10 }} tickLine={false} axisLine={false} width={40} />
              <Tooltip content={({ active, payload, label }) => <ChartTooltip active={active} payload={payload} label={label} />} cursor={{ fill: "#ffffff", fillOpacity: 0.025 }} />
              <Legend iconType="circle" iconSize={7} wrapperStyle={{ fontSize: 11, color: "#a1a1aa" }} />
              <Bar yAxisId="money" dataKey="spend" name="Fuel spend" fill="#3b82f6" radius={[3, 3, 0, 0]} />
              <Bar yAxisId="gallons" dataKey="gallons" name="Gallons" fill="#22d3ee" radius={[3, 3, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        </ChartCard>

        <ChartCard title="Price vs. discount per gallon" description="Weighted monthly averages; savings are retail price minus paid price.">
          <ResponsiveContainer width="100%" height="100%">
            <BarChart data={monthly} margin={{ top: 5, right: 4, left: 0, bottom: 0 }} accessibilityLayer>
              <CartesianGrid stroke="#27272a" vertical={false} />
              <XAxis dataKey="label" tick={{ fill: "#71717a", fontSize: 11 }} tickLine={false} axisLine={false} />
              <YAxis tickFormatter={(value) => `$${Number(value).toFixed(2)}`} tick={{ fill: "#52525b", fontSize: 10 }} tickLine={false} axisLine={false} width={46} />
              <Tooltip content={({ active, payload, label }) => <ChartTooltip active={active} payload={payload} label={label} />} cursor={{ fill: "#ffffff", fillOpacity: 0.025 }} />
              <Legend iconType="circle" iconSize={7} wrapperStyle={{ fontSize: 11, color: "#a1a1aa" }} />
              <Bar dataKey="pricePerGallon" name="Paid / gal" fill="#3b82f6" radius={[3, 3, 0, 0]} />
              <Bar dataKey="discountPerGallon" name="Saved / gal" fill="#34d399" radius={[3, 3, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        </ChartCard>

        <ChartCard title="Weekly fuel to gross ratio" description="Fuel spend ÷ invoiced load gross; weeks begin Monday.">
          <div className="h-full overflow-x-auto pb-2">
            <div className="h-full w-full" style={{ minWidth: weekly.length * 42 }}>
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={weekly} margin={{ top: 5, right: 4, left: 0, bottom: 0 }} accessibilityLayer>
                  <CartesianGrid stroke="#27272a" vertical={false} />
                  <XAxis dataKey="label" interval={Math.max(0, Math.ceil(weekly.length / 10) - 1)} tick={{ fill: "#71717a", fontSize: 10 }} tickLine={false} axisLine={false} />
                  <YAxis tickFormatter={(value) => `${Number(value).toFixed(0)}%`} tick={{ fill: "#52525b", fontSize: 10 }} tickLine={false} axisLine={false} width={42} />
                  <Tooltip content={({ active, payload, label }) => <ChartTooltip active={active} payload={payload} label={label} />} cursor={{ fill: "#ffffff", fillOpacity: 0.025 }} />
                  <Bar dataKey="fuelToGrossRatio" name="Fuel / gross" fill="#a78bfa" radius={[3, 3, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </div>
          </div>
        </ChartCard>

        <ChartCard title="Weekly fuel price vs. RPM" description="Weighted diesel price compared with invoiced gross revenue per mile.">
          <div className="h-full overflow-x-auto pb-2">
            <div className="h-full w-full" style={{ minWidth: weekly.length * 50 }}>
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={weekly} margin={{ top: 5, right: 4, left: 0, bottom: 0 }} accessibilityLayer>
                  <CartesianGrid stroke="#27272a" vertical={false} />
                  <XAxis dataKey="label" interval={Math.max(0, Math.ceil(weekly.length / 10) - 1)} tick={{ fill: "#71717a", fontSize: 10 }} tickLine={false} axisLine={false} />
                  <YAxis tickFormatter={(value) => `$${Number(value).toFixed(2)}`} tick={{ fill: "#52525b", fontSize: 10 }} tickLine={false} axisLine={false} width={46} />
                  <Tooltip content={({ active, payload, label }) => <ChartTooltip active={active} payload={payload} label={label} />} cursor={{ fill: "#ffffff", fillOpacity: 0.025 }} />
                  <Legend iconType="circle" iconSize={7} wrapperStyle={{ fontSize: 11, color: "#a1a1aa" }} />
                  <Bar dataKey="averageFuelPrice" name="Fuel price" fill="#3b82f6" radius={[3, 3, 0, 0]} />
                  <Bar dataKey="revenuePerMile" name="RPM" fill="#f59e0b" radius={[3, 3, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </div>
          </div>
        </ChartCard>
      </div>

      <section className="rounded-xl border border-zinc-800/60 bg-card p-4 sm:p-5">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <div className="flex items-center gap-2">
              <MapPinned className="h-4 w-4 text-blue-400" />
              <h2 className="text-sm font-medium text-zinc-200">Fuel price map</h2>
            </div>
            <p className="mt-1 text-[11px] text-zinc-600">Weighted average paid price per diesel gallon by state.</p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <CalendarDays className="h-3.5 w-3.5 text-zinc-600" />
            <input aria-label="Map start date" type="date" value={dateFrom} max={dateTo} onChange={(event) => setDateFrom(event.target.value)} className={inputClass} />
            <span className="text-[12px] text-zinc-700">to</span>
            <input aria-label="Map end date" type="date" value={dateTo} min={dateFrom} onChange={(event) => setDateTo(event.target.value)} className={inputClass} />
          </div>
        </div>
        <FuelMap dashboard={dashboard} />
      </section>

      <details className="group rounded-xl border border-zinc-800/60 bg-zinc-950/20 px-4 py-3">
        <summary className="flex cursor-pointer list-none items-center gap-2 text-[12px] font-medium text-zinc-400">
          <Info className="h-3.5 w-3.5 text-zinc-600" /> How these metrics are calculated
        </summary>
        <div className="mt-3 grid gap-2 text-[11px] leading-relaxed text-zinc-600 sm:grid-cols-2">
          <p><span className="text-zinc-400">Fuel:</span> {dashboard.methodology.fuelScope}</p>
          <p><span className="text-zinc-400">Gross and RPM:</span> {dashboard.methodology.revenueScope} {dashboard.methodology.revenueDate}</p>
          <p><span className="text-zinc-400">Weeks:</span> {dashboard.methodology.weekStartsOn} through Sunday.</p>
          <p><span className="text-zinc-400">Purchase dates:</span> {dashboard.methodology.fuelDateTimezone}</p>
        </div>
      </details>
    </div>
  );
}
