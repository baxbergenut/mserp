import type { DriverBoardDriver } from "@/app/lib/types";

// Render transfers using ordinary table layout, including the destination's
// group metadata. No translated row is left floating over its neighbours.
export function transferDriverRows<T extends DriverBoardDriver>(drivers: T[], id: string, dispatcherId: string, targetId: string, after: boolean): T[] {
  const driver = drivers.find(d => d.id === id);
  if (!driver) return drivers;
  const group = drivers.find(d => d.dispatcherId === dispatcherId);
  const moved: T = { ...driver, dispatcherId,
    dispatcherName: group?.dispatcherName ?? "Unassigned",
    dispatcherExtension: group?.dispatcherExtension ?? null,
    mainUpdaterName: group?.mainUpdaterName ?? "",
    mainUpdaterExtension: group?.mainUpdaterExtension ?? null,
    afterHoursUpdaterName: group?.afterHoursUpdaterName ?? "",
    afterHoursUpdaterExtension: group?.afterHoursUpdaterExtension ?? null,
  };
  const rows = drivers.filter(d => d.id !== id);
  let index = rows.findIndex(d => d.id === targetId);
  if (index >= 0) index += after ? 1 : 0;
  else {
    const last = rows.findLastIndex(d => d.dispatcherId === dispatcherId);
    index = last >= 0 ? last + 1 : dispatcherId === "" ? 0 : rows.length;
  }
  rows.splice(index, 0, moved);
  return rows;
}
