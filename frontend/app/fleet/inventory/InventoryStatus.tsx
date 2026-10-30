import { statusClass } from "@/app/lib/statusColors";
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
  const color = statusClass(value, "truck");
  return <span className={"inline-flex items-center whitespace-nowrap rounded-md border px-2 py-0.5 text-[11px] font-medium " + color}>{label || readable(value)}</span>;
}

