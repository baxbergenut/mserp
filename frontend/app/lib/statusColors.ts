// One global pair per status. The original Status Board is the palette authority.
export const statusDefinitions = [
  ["driver_ready", "Ready"],
  ["active", "Active employment / account"], ["in_use", "Truck in use"],
  ["shop", "Shop"], ["out_of_service", "OOS"], ["terminated", "Terminated / Left"],
  ["home", "Home"], ["vacation", "Vacation"], ["rest", "Rest"],
  ["family_issue", "Family issue"], ["health_issue", "Health issue"], ["new_driver", "New driver / Needs setup"],
  ["covered", "Covered"], ["dispatched", "Dispatched"], ["enroute", "Enroute"],
  ["reserved", "Reserved"], ["deadhead", "Deadhead"], ["stop", "Stop"],
  ["yard", "In the yard"], ["hold_for_load", "Hold for Load"],
  ["pickup_delivery_issue", "Issue with PU/Del"], ["accident", "Accident"],
  ["rejected", "Rejected"], ["canceled", "Canceled"], ["no_load", "No load"],
  ["stuck", "Stuck"], ["late_delivery", "Late delivery"], ["truck_issue", "Truck issue"],
  ["open", "Open"], ["in_progress", "In progress"], ["completed", "Completed / Delivered"],
  ["paid", "Paid / Released"], ["pending", "Pending"], ["needs_review", "Needs review"],
  ["discarded", "Discarded / Archived"], ["disabled", "Disabled"], ["partial", "Partially paid / released"],
  ["scheduled", "Scheduled / Booked"], ["unpaid", "Unpaid"], ["failed", "Failed"],
] as const;

export type StatusColorKey = typeof statusDefinitions[number][0];
export const legacyStatusColorKeys = ["truck_ready"] as const;
export interface StatusColorPair { foreground: string; background: string; }
const pair = (background: string, foreground = "#000000"): StatusColorPair => ({ foreground, background });
const ready = pair("#000000", "#ffffff");
const neutral = pair("#ffffff");
const covered = pair("#ffff00");
const enroute = pair("#3d85c6");
const reserved = pair("#b7e1cd");
const stop = pair("#990000");
// Exact original Status Board foreground/background pairs. Statuses outside its
// vocabulary inherit the closest existing meaning; there are no page palettes.
export const defaultStatusPairs: Record<StatusColorKey, StatusColorPair> = {
  driver_ready: ready, active: ready, in_use: enroute,
  shop: pair("#ff9900", "#ffffff"), out_of_service: stop, terminated: neutral,
  home: pair("#ff0000"), vacation: pair("#e06666"), rest: pair("#d9d9d9"),
  family_issue: neutral, health_issue: neutral, new_driver: neutral,
  covered, dispatched: pair("#fbbc04", "#ffffff"), enroute, reserved,
  deadhead: neutral, stop, yard: neutral, hold_for_load: neutral,
  pickup_delivery_issue: neutral, accident: neutral, rejected: neutral,
  canceled: neutral, no_load: neutral, stuck: neutral, late_delivery: covered,
  truck_issue: stop, open: covered, in_progress: enroute, completed: reserved,
  paid: reserved, pending: covered, needs_review: covered, discarded: neutral,
  disabled: neutral, partial: covered, scheduled: covered, unpaid: covered, failed: stop,
};
export const defaultStatusColors = Object.fromEntries(Object.entries(defaultStatusPairs).map(([key, value]) => [key, value.background])) as Record<StatusColorKey, string>;
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
  if (key === "truck_ready") return "driver_ready";
  if (scope === "driver" && key === "active") return "driver_ready";
  return Object.hasOwn(aliases, key) ? aliases[key] : Object.hasOwn(defaultStatusColors, key) ? key as StatusColorKey : "discarded";
}
export function statusClass(value: string, scope: StatusScope = "general", mode: "tone" | "text" | "dot" = "tone") {
  if (!value.trim()) return "";
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

function rgb(hex: string): number[] { return [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16)); }
function luminance(c: number[]): number { return c.map(v => v / 255).map(v => v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4).reduce((sum, v, i) => sum + v * [.2126, .7152, .0722][i], 0); }
export function contrast(a: string, b: string): number { const x = luminance(rgb(a)), y = luminance(rgb(b)); return (Math.max(x, y) + .05) / (Math.min(x, y) + .05); }
export function statusPair(key: StatusColorKey, colors: Record<string, string> = {}): StatusColorPair {
  // Read old single-hue saves and the old truck-specific Ready setting without
  // rewriting immutable history. All new edits save an explicit shared pair.
  const legacyBase = colors[key] ?? (key === "driver_ready" ? colors.truck_ready : undefined);
  const defaults = defaultStatusPairs[key];
  const background = colors[`${key}.bg`] ?? legacyBase ?? defaults.background;
  const foreground = colors[`${key}.fg`] ?? (legacyBase ? contrast("#000000", background) >= contrast("#ffffff", background) ? "#000000" : "#ffffff" : defaults.foreground);
  return { foreground, background };
}
