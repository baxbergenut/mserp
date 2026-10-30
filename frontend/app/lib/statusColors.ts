export const statusPalette = {
  green: "#10b981", blue: "#3b82f6", purple: "#8b5cf6", amber: "#f59e0b",
  orange: "#f97316", red: "#ef4444", teal: "#14b8a6", gray: "#71717a",
};

// One key per meaning, shared by every surface. Driver readiness is waiting on
// work; truck readiness means the equipment is available for service.
export const statusDefinitions = [
  ["truck_ready", "Truck ready", "green"], ["driver_ready", "Driver ready / available", "amber"],
  ["active", "Active employment / account", "green"], ["in_use", "Truck in use", "blue"],
  ["shop", "Shop", "orange"], ["out_of_service", "OOS", "red"], ["terminated", "Terminated / Left", "gray"],
  ["home", "Home", "purple"], ["vacation", "Vacation", "purple"], ["rest", "Rest", "purple"],
  ["family_issue", "Family issue", "orange"], ["health_issue", "Health issue", "orange"], ["new_driver", "New driver / Needs setup", "teal"],
  ["covered", "Covered", "amber"], ["dispatched", "Dispatched", "blue"], ["enroute", "Enroute", "blue"],
  ["reserved", "Reserved", "amber"], ["deadhead", "Deadhead", "red"], ["stop", "Stop", "red"],
  ["yard", "In the yard", "gray"], ["hold_for_load", "Hold for Load", "amber"],
  ["pickup_delivery_issue", "Issue with PU/Del", "orange"], ["accident", "Accident", "red"],
  ["rejected", "Rejected", "gray"], ["canceled", "Canceled", "gray"], ["no_load", "No load", "gray"],
  ["stuck", "Stuck", "orange"], ["late_delivery", "Late delivery", "orange"], ["truck_issue", "Truck issue", "red"],
  ["open", "Open", "amber"], ["in_progress", "In progress", "blue"], ["completed", "Completed / Delivered", "green"],
  ["paid", "Paid / Released", "green"], ["pending", "Pending", "amber"], ["needs_review", "Needs review", "amber"],
  ["discarded", "Discarded / Archived", "gray"], ["disabled", "Disabled", "gray"], ["partial", "Partially paid / released", "amber"],
  ["scheduled", "Scheduled / Booked", "amber"], ["unpaid", "Unpaid", "amber"], ["failed", "Failed", "red"],
] as const satisfies ReadonlyArray<readonly [string, string, keyof typeof statusPalette]>;

export type StatusColorKey = typeof statusDefinitions[number][0];
export const defaultStatusColors = Object.fromEntries(statusDefinitions.map(([key, , tone]) => [key, statusPalette[tone]])) as Record<StatusColorKey, string>;
export type StatusScope = "general" | "driver" | "truck";

const aliases: Record<string, StatusColorKey> = {
  available: "driver_ready", ready: "driver_ready", assigned: "in_use", maintenance: "shop", hold: "shop",
  left: "terminated", inactive: "disabled", reset: "rest", "34_reset": "rest", home_yard: "home",
  in_transit: "enroute", intransit: "enroute", en_route: "enroute", transit: "enroute", on_the_road: "enroute",
  at_pu: "dispatched", at_del: "enroute", in_the_yard: "yard", issue_with_pu_del: "pickup_delivery_issue",
  load_cancelled: "canceled", load_canceled: "canceled", cancelled: "canceled", cancel: "canceled",
  late_del: "late_delivery", new: "new_driver", needs_setup: "new_driver", in_process: "in_progress",
  added: "completed", enrolled: "completed", finalized: "completed", queued: "pending", running: "in_progress", delivered: "completed", closed: "completed", done: "completed", released: "paid", invoiced: "pending",
  invoice: "pending", booked: "scheduled", dispatched_to_pickup: "dispatched", partially_released: "partial",
  partially_paid: "partial", partially_collected: "partial", archived: "discarded", voided: "canceled",
  needs_attention: "needs_review", review: "needs_review", confirmed: "completed", verified: "completed",
  good: "completed", verified_installed: "completed", reported_present: "pending", repair: "shop", suspended: "needs_review", missing: "failed", lost: "failed", damaged: "failed", total_loss: "failed", retired: "discarded", sold: "discarded", unknown: "discarded", unassigned: "discarded", enabled: "active", unconfirmed: "needs_review", unlinked: "needs_review", approved: "completed", rejected: "rejected", unverified: "needs_review", overdue: "needs_review",
};
export function statusColorKey(value: string, scope: StatusScope = "general"): StatusColorKey {
  const key = value.trim().toLowerCase().replace(/[^a-z0-9]+/g, "_").replace(/^_|_$/g, "");
  if (scope === "truck" && ["ready", "available"].includes(key)) return "truck_ready";
  if (scope === "driver" && key === "active") return "driver_ready";
  return Object.hasOwn(aliases, key) ? aliases[key] : Object.hasOwn(defaultStatusColors, key) ? key as StatusColorKey : "discarded";
}
export function statusClass(value: string, scope: StatusScope = "general", mode: "tone" | "text" | "dot" = "tone") {
  return `status-${mode} status-${statusColorKey(value, scope)}`;
}
export function displayStatus(value: string): string {
  const key = value.trim().toUpperCase().replace(/[_-]+/g, " ");
  if (["IN TRANSIT", "INTRANSIT", "TRANSIT", "ENROUTE", "EN ROUTE", "AT DEL"].includes(key)) return "Enroute";
  if (key === "AT PU") return "Dispatched";
  if (key === "HOME (YARD)") return "Home";
  if (["RESET", "34 RESET"].includes(key)) return "Rest";
  return value;
}

// Blend the chosen hue toward black/white until text reaches WCAG AA against
// the actual soft badge surface. This also handles custom near-white/black hues.
function rgb(hex: string): number[] { return [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16)); }
function hex(channels: number[]): string { return "#" + channels.map(v => Math.round(v).toString(16).padStart(2, "0")).join(""); }
function mix(a: number[], b: number[], amount: number): number[] { return a.map((v, i) => v * (1 - amount) + b[i] * amount); }
function luminance(c: number[]): number { return c.map(v => v / 255).map(v => v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4).reduce((sum, v, i) => sum + v * [.2126, .7152, .0722][i], 0); }
export function contrast(a: string, b: string): number { const x = luminance(rgb(a)), y = luminance(rgb(b)); return (Math.max(x, y) + .05) / (Math.min(x, y) + .05); }
export function statusSwatch(base: string, surface: string): { background: string; color: string } {
  const background = hex(mix(rgb(surface), rgb(base), .14));
  const target = luminance(rgb(background)) > .18 ? [0, 0, 0] : [255, 255, 255];
  let color = base;
  for (let i = 0; i <= 100 && contrast(color, background) < 4.5; i++) color = hex(mix(rgb(base), target, i / 100));
  return { background, color };
}
