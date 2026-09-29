import type { Driver, ExpenseInput, Investor, Truck } from "@/app/lib/types";

export function expenseAssignment(value: ExpenseInput, kind: "driver" | "truck", id: string, drivers: Driver[], trucks: Truck[], owners: Investor[]): ExpenseInput {
  const driver = kind === "driver" ? drivers.find(d => d.id === id) : drivers.find(d => d.id === trucks.find(t => t.id === id)?.driverId);
  const truck = kind === "truck" ? trucks.find(t => t.id === id) : trucks.find(t => t.id === driver?.truckId);
  const owner = owners.find(o => o.id === truck?.ownerId);
  const ownerCovered = driver ? driver.isOwnerOperator : !!truck && !truck.isCompanyOwned;
  const today = new Intl.DateTimeFormat("en-CA", {timeZone:"America/New_York",year:"numeric",month:"2-digit",day:"2-digit"}).format(new Date());
  return { ...value, driverId: driver?.id ?? null, driverName: driver?.fullName ?? "", truckId: truck?.id ?? null, unitNumber: truck?.unitNumber ?? "",
    coveredBy: ownerCovered && !owner?.isCompany ? "Truck Owner" : "Company", ownerId: ownerCovered && !owner?.isCompany ? (value.expenseDate === today ? owner?.id ?? null : null) : null };
}
