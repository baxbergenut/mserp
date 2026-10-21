import type { DriverOption, ExpenseInput, InvestorOption, TruckOption } from "@/app/lib/types";

export function expenseResponsibility(value: ExpenseInput, trucks: TruckOption[], owners: InvestorOption[]) {
  const truck = trucks.find(t => t.id === value.truckId);
  const owner = owners.find(o => o.id === truck?.ownerId);
  const privatelyOwned = !!truck?.ownerId && !truck.isCompanyOwned && !owner?.isCompany;
  const driverOwnsTruck = privatelyOwned && !!value.driverId && owner?.driverId === value.driverId;
  return { truck, owner, privatelyOwned, driverOwnsTruck };
}

export function defaultExpenseResponsibility(value: ExpenseInput, trucks: TruckOption[], owners: InvestorOption[]): ExpenseInput {
  const { truck, privatelyOwned, driverOwnsTruck } = expenseResponsibility(value, trucks, owners);
  return { ...value, coveredBy: privatelyOwned ? driverOwnsTruck ? "Driver" : "Truck Owner" : "Company", ownerId: privatelyOwned && !driverOwnsTruck ? truck!.ownerId : null };
}

export function expenseAssignment(value: ExpenseInput, kind: "driver" | "truck", id: string, drivers: DriverOption[], trucks: TruckOption[], owners: InvestorOption[]): ExpenseInput {
  if (kind === "truck" && !value.driverId && value.driverName.trim()) {
    const truck = trucks.find(t => t.id === id);
    return defaultExpenseResponsibility({ ...value, truckId: truck?.id ?? null, unitNumber: truck?.unitNumber ?? "" }, trucks, owners);
  }
  const driver = kind === "driver" ? drivers.find(d => d.id === id) : drivers.find(d => d.id === trucks.find(t => t.id === id)?.driverId);
  const truck = kind === "truck" ? trucks.find(t => t.id === id) : trucks.find(t => t.id === driver?.truckId);
  return defaultExpenseResponsibility({ ...value, applicantId: null, driverId: driver?.id ?? null, driverName: driver?.fullName ?? "", truckId: truck?.id ?? null, unitNumber: truck?.unitNumber ?? "" }, trucks, owners);
}
