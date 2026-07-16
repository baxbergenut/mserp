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
  return `${n.toFixed(2)}/mi`;
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
