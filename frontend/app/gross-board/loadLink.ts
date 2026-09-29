import { addDays } from "./board";

export function parseBoardLoadTarget(search: string) {
  const params = new URLSearchParams(search);
  const driverId = params.get("driverId") ?? "";
  const date = params.get("date") ?? "";
  const rawSlot = params.get("slot") ?? "";
  const loadNumber = params.get("loadNumber") ?? "";
  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(driverId)
    || !/^\d{4}-\d{2}-\d{2}$/.test(date) || date < "2000-01-03" || date > "2101-01-02"
    || !/^\d{1,2}$/.test(rawSlot) || !loadNumber.trim() || loadNumber.length > 200) return null;
  const day = new Date(`${date}T12:00:00Z`);
  if (!Number.isFinite(day.getTime()) || day.toISOString().slice(0, 10) !== date) return null;
  return { fromPay: params.get("from") === "driver-pay", driverId, date, slot: Number(rawSlot), loadNumber, week: addDays(date, -(day.getUTCDay() + 6) % 7) };
}
