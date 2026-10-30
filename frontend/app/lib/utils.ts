import type { Load, SortKey, SortDir, PeriodKey, ChartDataPoint } from "./types";

// ---------------------------------------------------------------------------
// Formatting
// ---------------------------------------------------------------------------

export function formatMoney(value: string | number): string {
  const n = typeof value === "string" ? parseFloat(value) : value;
  if (Number.isNaN(n)) return "—";
  return n.toLocaleString("en-US", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}

export function formatMiles(value: string | number): string {
  const n = typeof value === "string" ? parseFloat(value) : value;
  if (Number.isNaN(n)) return "—";
  return `${n.toLocaleString("en-US", { maximumFractionDigits: 1 })} mi`;
}

export function formatRate(value: string | number): string {
  const n = typeof value === "string" ? parseFloat(value) : value;
  if (Number.isNaN(n)) return "—";
  return `$${n.toFixed(2)}/mi`;
}

export function formatDate(iso: string | null | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

export function formatCompact(value: number): string {
  if (value >= 1_000_000) return `$${(value / 1_000_000).toFixed(2)}M`;
  if (value >= 1_000) return `$${(value / 1_000).toFixed(1)}K`;
  return formatMoney(value);
}

/** Normalizes a status string into a stable, comparable key. */
export function statusKey(status: string): string {
  return status.trim().toLowerCase();
}

/** Human-readable label from a raw status key, e.g. "in_transit" -> "In transit". */
export function statusLabel(status: string): string {
  const s = status.replace(/[_-]+/g, " ").trim();
  if (!s) return "Unknown";
  return s.charAt(0).toUpperCase() + s.slice(1);
}

// ---------------------------------------------------------------------------
// Date ranges for dashboard periods
// ---------------------------------------------------------------------------

export function getDateRange(period: PeriodKey): { start: Date; end: Date } {
  const now = new Date();
  const endOfToday = new Date(now);
  endOfToday.setHours(23, 59, 59, 999);

  switch (period) {
    case "today": {
      const start = new Date(now);
      start.setHours(0, 0, 0, 0);
      return { start, end: endOfToday };
    }
    case "thisWeek": {
      const start = new Date(now);
      start.setDate(now.getDate() - now.getDay());
      start.setHours(0, 0, 0, 0);
      return { start, end: endOfToday };
    }
    case "thisMonth": {
      const start = new Date(now.getFullYear(), now.getMonth(), 1);
      return { start, end: endOfToday };
    }
    case "lastMonth": {
      const start = new Date(now.getFullYear(), now.getMonth() - 1, 1);
      const end = new Date(now.getFullYear(), now.getMonth(), 0, 23, 59, 59, 999);
      return { start, end };
    }
    case "allTime":
      return { start: new Date(0), end: endOfToday };
  }
}

export function getPreviousDateRange(
  period: PeriodKey,
): { start: Date; end: Date } | null {
  if (period === "allTime") return null;

  const current = getDateRange(period);
  const durationMs = current.end.getTime() - current.start.getTime();

  return {
    start: new Date(current.start.getTime() - durationMs - 1),
    end: new Date(current.start.getTime() - 1),
  };
}

// ---------------------------------------------------------------------------
// Filtering + Sorting
// ---------------------------------------------------------------------------

export interface Filters {
  status: string;
  customer: string;
  dispatcher: string;
  driver: string;
  pickupFrom: string;
  pickupTo: string;
}

export const EMPTY_FILTERS: Filters = {
  status: "",
  customer: "",
  dispatcher: "",
  driver: "",
  pickupFrom: "",
  pickupTo: "",
};

export function filterLoads(
  loads: Load[],
  search: string,
  filters: Filters,
): Load[] {
  let result = loads;

  if (search) {
    const q = search.toLowerCase();
    result = result.filter(
      (l) =>
        l.LoadID?.toLowerCase().includes(q) ||
        l.ShipmentID?.toLowerCase().includes(q) ||
        l.CustomerName?.toLowerCase().includes(q) ||
        l.DriverName?.toLowerCase().includes(q) ||
        l.TruckUnit?.toLowerCase().includes(q) ||
        l.DispatcherName?.toLowerCase().includes(q),
    );
  }

  if (filters.status)
    result = result.filter((l) => statusKey(l.Status) === filters.status);
  if (filters.customer)
    result = result.filter((l) => l.CustomerName === filters.customer);
  if (filters.driver)
    result = result.filter((l) => l.DriverName === filters.driver);
  if (filters.dispatcher)
    result = result.filter((l) => l.DispatcherName === filters.dispatcher);

  if (filters.pickupFrom) {
    const from = new Date(filters.pickupFrom);
    result = result.filter((l) => l.PickupTime && new Date(l.PickupTime) >= from);
  }
  if (filters.pickupTo) {
    const to = new Date(filters.pickupTo);
    to.setHours(23, 59, 59, 999);
    result = result.filter((l) => l.PickupTime && new Date(l.PickupTime) <= to);
  }

  return result;
}

export function sortLoads(
  loads: Load[],
  key: SortKey,
  dir: SortDir,
): Load[] {
  return [...loads].sort((a, b) => {
    let aVal: number;
    let bVal: number;

    switch (key) {
      case "TotalPay":
      case "TotalMiles":
      case "PerMileRevenue":
        aVal = parseFloat(a[key] || "0");
        bVal = parseFloat(b[key] || "0");
        break;
      case "PickupTime":
      case "DeliveryTime":
        aVal = a[key] ? new Date(a[key]).getTime() : 0;
        bVal = b[key] ? new Date(b[key]).getTime() : 0;
        break;
      default:
        return 0;
    }

    return dir === "asc" ? aVal - bVal : bVal - aVal;
  });
}

// ---------------------------------------------------------------------------
// Dashboard metrics
// ---------------------------------------------------------------------------

export interface DashboardMetrics {
  grossRevenue: number;
  avgRpm: number;
  totalMiles: number;
  loadCount: number;
  avgPayPerLoad: number;
}

function loadsInRange(loads: Load[], start: Date, end: Date): Load[] {
  return loads.filter((l) => {
    if (!l.PickupTime) return false;
    const d = new Date(l.PickupTime);
    return d >= start && d <= end;
  });
}

export function computeMetrics(loads: Load[], period: PeriodKey): DashboardMetrics {
  const { start, end } = getDateRange(period);
  const filtered = loadsInRange(loads, start, end);

  const grossRevenue = filtered.reduce(
    (sum, l) => sum + parseFloat(l.TotalPay || "0"),
    0,
  );
  const totalMiles = filtered.reduce(
    (sum, l) => sum + parseFloat(l.TotalMiles || "0"),
    0,
  );
  const avgRpm = totalMiles > 0 ? grossRevenue / totalMiles : 0;
  const avgPayPerLoad = filtered.length > 0 ? grossRevenue / filtered.length : 0;

  return {
    grossRevenue,
    avgRpm,
    totalMiles,
    loadCount: filtered.length,
    avgPayPerLoad,
  };
}

export function computeDelta(
  loads: Load[],
  period: PeriodKey,
): number | null {
  const prev = getPreviousDateRange(period);
  if (!prev) return null;

  const current = computeMetrics(loads, period);
  const previous = loadsInRange(loads, prev.start, prev.end);
  const prevRevenue = previous.reduce(
    (sum, l) => sum + parseFloat(l.TotalPay || "0"),
    0,
  );

  if (prevRevenue === 0) return null;
  return ((current.grossRevenue - prevRevenue) / prevRevenue) * 100;
}

export function computeRevenueTrend(
  loads: Load[],
  period: PeriodKey,
): ChartDataPoint[] {
  const { start, end } = getDateRange(period);
  const filtered = loadsInRange(loads, start, end);

  const useMonthBuckets = period === "allTime";

  const buckets = new Map<string, number>();

  for (const l of filtered) {
    const d = new Date(l.PickupTime);
    const sortKey = useMonthBuckets
      ? `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`
      : `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

    buckets.set(sortKey, (buckets.get(sortKey) || 0) + parseFloat(l.TotalPay || "0"));
  }

  return Array.from(buckets.entries())
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, value]) => {
      const parts = key.split("-").map(Number);
      const label = useMonthBuckets
        ? new Date(parts[0], parts[1] - 1).toLocaleDateString("en-US", {
            month: "short",
            year: "2-digit",
          })
        : new Date(parts[0], parts[1] - 1, parts[2]).toLocaleDateString("en-US", {
            month: "short",
            day: "numeric",
          });
      return { label, value };
    });
}
