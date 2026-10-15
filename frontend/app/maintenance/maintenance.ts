import type { ExpenseInput, MaintenanceAIDraft, MaintenanceInput, MaintenanceOptions, MaintenanceRecord, MaintenanceWork } from "../lib/types";
import { emptyExpenseInput, expenseToInput } from "../expenses/ExpenseForm";
import { costBreakdownError } from "../lib/expenseCosts";

export const today = () => new Intl.DateTimeFormat("en-CA", { timeZone: "America/New_York", year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
export const serviceTypeForName = (name: string) => ({ "pm service": "preventive", "oil change": "oil", "electrical issue": "electrical", "electrical repair": "electrical", "brake issue": "brakes", "tire issue": "tires", "tire replacement": "tires", leakage: "fluids", "body work": "body", towing: "recovery", "truck wash": "cleaning", detailing: "cleaning" } as Record<string, string>)[name.trim().toLowerCase()] ?? "other";
export const trackingForType = (code: string): MaintenanceWork["trackingCode"] => code === "preventive" ? "pm" : code === "oil" ? "oil" : "";
export function serviceWork(code: string, options: MaintenanceOptions, amount: string | null = null): MaintenanceWork {
  return { typeCode: code, name: options.types.find(t => t.code === code)?.name ?? code, amount, trackingCode: trackingForType(code) };
}
export function normalizeMaintenance(value: MaintenanceInput, options: MaintenanceOptions): MaintenanceInput {
  const services = value.services.map(s => serviceWork(s.typeCode, options, s.amount));
  if (services.length === 1 && value.expense?.amount && value.legacyPaymentTotal !== value.expense.amount) services[0].amount = value.expense.amount;
  return { ...value, services, expense: value.expense ? { ...value.expense, driverId: value.driverId, driverName: value.driverName, expenseType: [...new Set(services.map(s => s.name))].join(", ").slice(0, 100), expenseDate: value.serviceDate || value.expense.expenseDate } : null };
}
export function emptyMaintenance(options: MaintenanceOptions, truckId?: string): MaintenanceInput {
  const code = options.types.find(t => t.active && t.code === "preventive")?.code ?? options.types.find(t => t.active)?.code ?? "other";
  return assignMaintenanceTruck({ id: crypto.randomUUID(), version: 0, truckId: null, driverId: null, driverName: "", vendorId: null, vendorName: "", vendorAddress: "", vendorPhone: "", vendorContact: "", serviceDate: today(), odometer: null, location: "shop", status: "completed", workOrder: "", notes: "", services: [serviceWork(code, options)], expense: { ...emptyExpenseInput, categoryId: options.categoryId, paidBy: options.currentUser, expenseDate: today(), costBreakdown: [] } }, truckId ?? "", options);
}
export function assignMaintenanceTruck(value: MaintenanceInput, id: string, options: MaintenanceOptions): MaintenanceInput {
  const truck = options.trucks.find(t => t.id === id);
  const driverOwner = truck?.ownerDriverId && truck.ownerDriverId === truck.driverId;
  const coveredBy = truck?.isCompanyOwned ? "Company" : driverOwner ? "Driver" : truck?.ownerId ? "Truck Owner" : "Company";
  return { ...value, odometer: null, savedOwnerName: truck?.ownerName, truckId: id || null, driverId: truck?.driverId ?? null, driverName: truck?.driverName ?? "", expense: value.expense ? { ...value.expense, truckId: id || null, unitNumber: truck?.unitNumber ?? "", driverId: truck?.driverId ?? null, driverName: truck?.driverName ?? "", coveredBy, ownerId: coveredBy === "Truck Owner" ? truck?.ownerId ?? null : null } : null };
}
export function maintenanceToInput(record: MaintenanceRecord): MaintenanceInput {
  return { savedOwnerName: record.expense?.ownerName ?? undefined, legacyPaymentTotal: record.expense?.baseAmount == null && record.expense?.paymentType?.toLowerCase() === "efs" ? record.expense?.amount ?? undefined : undefined, id: record.id, version: record.version, truckId: record.truckId, driverId: record.driverId ?? record.expense?.driverId ?? null, driverName: record.driverName || record.expense?.driverName || "", vendorId: record.vendorId, vendorName: record.vendorName, vendorAddress: record.vendorAddress, vendorPhone: record.vendorPhone, vendorContact: record.vendorContact, serviceDate: record.serviceDate ?? "", odometer: record.odometer, location: record.location, status: record.status === "completed" ? "completed" : "needs_review", workOrder: record.workOrder, notes: record.notes, services: record.services, expense: record.expense ? expenseToInput(record.expense) : null };
}
export function maintenanceDraft(draft: MaintenanceAIDraft, options: MaintenanceOptions): MaintenanceInput {
  const meta = draft.maintenance;
  const matched = assignMaintenanceTruck(emptyMaintenance(options), draft.truckId ?? "", options);
  const source = meta?.services?.length ? meta.services : draft.costBreakdown?.length ? draft.costBreakdown.map(s => ({ ...s, typeCode: serviceTypeForName(s.name), trackingCode: "" })) : [{ typeCode: serviceTypeForName(draft.expenseType), name: draft.expenseType || "", amount: null, trackingCode: "" }];
  const services = source.map(s => {
    let code = s.typeCode === "preventive" && (s.trackingCode === "oil" || serviceTypeForName(s.name) === "oil") ? "oil" : s.typeCode;
    if (!options.types.some(t => t.active && t.code === code)) code = options.types.find(t => t.active && t.code === "other")?.code ?? options.types.find(t => t.active)?.code ?? "other";
    return serviceWork(code, options, s.amount);
  });
  const vendor = options.vendors.filter(v => v.active && v.name.toLowerCase() === meta?.vendorName?.trim().toLowerCase());
  return normalizeMaintenance({ ...matched, driverId: draft.driverId ?? matched.driverId, driverName: draft.driverName || matched.driverName, status: "needs_review", serviceDate: meta?.serviceDate ?? "", odometer: meta?.odometer ?? null, location: meta?.location ?? "unknown", vendorId: vendor.length === 1 ? vendor[0].id : null, vendorName: meta?.vendorName ?? "", vendorAddress: meta?.vendorAddress ?? "", vendorPhone: meta?.vendorPhone ?? "", vendorContact: meta?.vendorContact ?? "", workOrder: meta?.workOrder ?? "", notes: draft.description ?? "", services, expense: meta?.hasInvoice === false ? null : { ...matched.expense!, amount: draft.amount, paymentType: draft.paymentType, referenceNumber: draft.referenceNumber, description: draft.description, expenseDate: draft.expenseDate } }, options);
}
export function maintenanceError(value: MaintenanceInput): string {
  if (value.status === "completed" && (!value.truckId || !value.serviceDate)) return "Completed work requires a truck and service date.";
  if (!value.services.length || value.services.some(s => !s.typeCode)) return "Choose a type for each service.";
  const priced = value.services.filter(s => s.amount !== null);
  if (priced.length && priced.length !== value.services.length) return "Price every service or leave all costs unallocated.";
  const input: ExpenseInput = { ...(value.expense ?? emptyExpenseInput), amount: value.expense?.amount ?? "0", costBreakdown: priced.map(s => ({ name: s.name, amount: s.amount! })) };
  return costBreakdownError(input);
}
