import type { InventoryData, InventoryFields, InventoryRecord } from "@/app/lib/types";

export const presenceStates = ["unknown", "reported_present", "verified_installed", "missing"];
export const conditionStates = ["unknown", "good", "damaged", "repair", "lost", "retired"];
export const deviceStates = ["unknown", "active", "inactive", "sold", "total_loss"];
export const carrierStates = ["unknown", "active", "suspended", "cancelled", "pending"];
const labels: Record<string, string> = {
  unknown: "Unknown", reported_present: "Reported Present", verified_installed: "Verified Installed",
  missing: "Missing", repair: "Under Repair", total_loss: "Total Loss", stored_unused: "Stored / Unused",
  needs_review: "Needs Review", samsara_gateway: "Samsara Gateway", dashcam: "Dashcam",
  phone: "Phone", tablet: "Tablet", not_installed: "Not Installed", unassigned: "Unassigned",
};
export const readable = (value?: string) => labels[value || "unknown"] || (value || "unknown").split("_").map(word => word.charAt(0).toUpperCase() + word.slice(1)).join(" ");
export const visibleDevice = (d: InventoryFields) => ["tablet", "phone", "dashcam", "samsara_gateway"].includes(d.deviceType || "");
export function effectiveTruck(record: InventoryRecord, data: InventoryData): string {
  const parent = data.assets.find(asset => asset.id === (record.data.gatewayId || record.data.assetId));
  return record.data.truckId || parent?.data.truckId || "";
}
export function installationState(record: InventoryRecord, data: InventoryData): string {
  if (record.data.equipmentState === "missing") return "missing";
  if (effectiveTruck(record, data)) return record.data.equipmentState || "unknown";
  return record.data.custodian || record.data.locationId ? "not_installed" : "unassigned";
}
export function StatusBadge({ value = "unknown", label }: { value?: string; label?: string }) {
  value = value || "unknown";
  const color = ["verified_installed", "good", "ready", "active"].includes(value)
    ? "border-emerald-500/25 bg-emerald-500/10 text-emerald-400"
    : ["reported_present", "repair", "pending", "suspended", "needs_review"].includes(value)
      ? "border-amber-500/25 bg-amber-500/10 text-amber-300"
      : ["unknown", "missing", "lost", "damaged", "total_loss", "cancelled"].includes(value)
        ? "border-red-500/25 bg-red-500/10 text-red-400"
        : "border-zinc-700 bg-zinc-800/40 text-zinc-400";
  return <span className={"inline-flex items-center whitespace-nowrap rounded-md border px-2 py-0.5 text-[11px] font-medium " + color}>{label || readable(value)}</span>;
}

