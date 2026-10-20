import type {
  InventoryData,
  InventoryFields,
  InventoryRecord,
} from "@/app/lib/types";

export const deviceColors = [
  "Black",
  "White",
  "Gray",
  "Silver",
  "Graphite",
  "Mocha",
  "Blue",
  "Red",
  "Pink",
  "Gold",
  "Green",
  "Purple",
  "Other",
];
export const lifecycleStates = [
  "unknown",
  "active",
  "spare",
  "inactive",
  "lost",
  "retired",
  "sold",
  "total_loss",
];
export const unavailable = (d: InventoryFields) =>
  !d.active ||
  ["lost", "retired"].includes(d.condition || "") ||
  ["sold", "total_loss", "lost", "retired"].includes(d.status || "");
export const deviceDescription = (d: InventoryFields) =>
  [
    d.make || d.brand,
    d.model,
    d.modelNumber && !d.model?.includes(d.modelNumber) ? d.modelNumber : "",
    d.storageGb ? `${d.storageGb} GB` : "",
    d.color,
  ]
    .filter(Boolean)
    .join(" · ");

// Extract only explicitly present terminal attributes. No model-to-spec lookup,
// capacity inference, case-color guessing, or automatic saves.
export function proposedDetails(
  d: InventoryFields,
): Partial<InventoryFields> | null {
  if (!["tablet", "phone"].includes(d.deviceType || "") || !d.model)
    return null;
  let model = d.model.trim();
  const result: Partial<InventoryFields> = {};
  const color = model.match(
    /\s+(Black|White|Gray|Grey|Silver|Graphite|Mocha|Blue|Red|Pink|Gold|Green|Purple)$/i,
  );
  if (color && !d.color) {
    result.color =
      color[1].toLowerCase() === "grey"
        ? "Gray"
        : color[1][0].toUpperCase() + color[1].slice(1).toLowerCase();
    model = model.slice(0, color.index).trim();
  }
  const capacity = model.match(/\s+(\d+)\s*GB$/i);
  if (capacity && !d.storageGb && +capacity[1] > 0 && +capacity[1] <= 65536) {
    result.storageGb = String(+capacity[1]);
    model = model.slice(0, capacity.index).trim();
  }
  const number = model.match(/(?:^|\s)(SM-[A-Z0-9]+|A\d{4})$/);
  if (number && !d.modelNumber) {
    result.modelNumber = number[1];
    model = model.slice(0, number.index).trim();
  }
  if (!Object.keys(result).length) return null;
  result.model = model;
  return result;
}

export function holder(
  record: InventoryRecord,
  data: InventoryData,
): InventoryFields {
  return record.data.gatewayId
    ? data.assets.find((a) => a.id === record.data.gatewayId)?.data ||
        ({} as InventoryFields)
    : record.data;
}
export function custodyKind(
  record: InventoryRecord,
  data: InventoryData,
): string {
  const d = holder(record, data);
  return d.truckId
    ? "truck"
    : d.locationId
      ? "storage"
      : d.custodianId || d.custodian
        ? "person"
        : "unknown";
}
export function custodyLabel(
  record: InventoryRecord,
  data: InventoryData,
  trucks: { id: string; unitNumber: string }[],
): string {
  const d = holder(record, data);
  return d.truckId
    ? trucks.find((t) => t.id === d.truckId)?.unitNumber || "Assigned Truck"
    : d.locationId
      ? data.locations.find((l) => l.id === d.locationId)?.data.name ||
        "Storage"
      : d.custodianId
        ? data.custodians.find((c) => c.id === d.custodianId)?.data.name ||
          "Custodian"
        : d.custodian || "Unknown";
}
export function availableToAssign(
  record: InventoryRecord,
  data: InventoryData,
): boolean {
  return (
    !unavailable(record.data) &&
    record.data.condition === "good" &&
    ["active", "spare"].includes(record.data.status || "") &&
    !record.data.gatewayId &&
    ["storage", "person"].includes(custodyKind(record, data))
  );
}
export function inventoryIssues(
  collection: string,
  record: InventoryRecord,
  data: InventoryData,
): string[] {
  if (!record.data.active) return [];
  const d = record.data,
    issues: string[] = [];
  if (collection === "assets") {
    if (
      !["tablet", "phone", "dashcam", "samsara_gateway"].includes(
        d.deviceType || "",
      )
    )
      issues.push("Unclassified device");
    if (
      unavailable(d) &&
      (d.truckId ||
        d.gatewayId ||
        data.assets.some(
          (a) => a.data.active && a.data.gatewayId === record.id,
        ) ||
        data.lines.some((l) => l.data.active && l.data.assetId === record.id))
    )
      issues.push("Lifecycle conflicts with assignment");
    if (!unavailable(d) && custodyKind(record, data) === "unknown")
      issues.push("Unknown custody");
    if (
      !unavailable(d) &&
      custodyKind(record, data) === "truck" &&
      d.equipmentState !== "verified_installed"
    )
      issues.push(
        d.equipmentState === "missing"
          ? "Missing device"
          : "Installation not verified",
      );
    if (!unavailable(d) && (!d.condition || d.condition === "unknown"))
      issues.push("Condition not recorded");
    if (proposedDetails(d)) issues.push("Device details to organize");
  } else if (collection === "lines") {
    const asset = data.assets.find((a) => a.id === d.assetId);
    if (d.status === "active" && (!asset || unavailable(asset.data)))
      issues.push("Active line without usable hardware");
    if (
      !d.sourceUpdated ||
      d.sourceUpdated <
        new Date(Date.now() - 180 * 86400000).toISOString().slice(0, 10)
    )
      issues.push("Carrier source needs refresh");
  } else if (collection === "equipment") {
    if (d.name === "IFTA Sticker" && !d.batchYear)
      issues.push("Sticker year unknown");
  }
  return issues;
}
