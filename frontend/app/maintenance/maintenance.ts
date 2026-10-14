import type { ExpenseInput, MaintenanceAIDraft, MaintenanceInput, MaintenanceOptions, MaintenanceRecord, MaintenanceWork } from "../lib/types";
import { emptyExpenseInput, expenseToInput } from "../expenses/ExpenseForm";
import { costBreakdownError } from "../lib/expenseCosts";

export const today = () => new Intl.DateTimeFormat("en-CA", { timeZone: "America/New_York", year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date());
export const serviceTypeForName = (name: string) => ({ "pm service": "preventive", "oil change": "preventive", "electrical issue": "electrical", "electrical repair": "electrical", "brake issue": "brakes", "tire issue": "tires", "tire replacement": "tires", leakage: "fluids", "body work": "body", towing: "recovery", "truck wash": "cleaning", detailing: "cleaning" } as Record<string, string>)[name.trim().toLowerCase()] ?? "other";
export function emptyMaintenance(options: MaintenanceOptions, truckId?: string): MaintenanceInput {
  const value: MaintenanceInput = { id: crypto.randomUUID(), version: 0, truckId: truckId ?? null, vendorId: null, vendorName: "", vendorAddress: "", vendorPhone: "", vendorContact: "", serviceDate: today(), odometer: null, location: "shop", status: "completed", workOrder: "", notes: "", services: [{ typeCode: options.types.find(t=>t.active&&t.code==="preventive")?.code ?? options.types.find(t=>t.active)?.code ?? "other", name: "", amount: null, trackingCode: "" }], expense: { ...emptyExpenseInput, categoryId: options.categoryId, paidBy: options.currentUser, expenseDate: today(), costBreakdown: [] } };
  return assignMaintenanceTruck(value, truckId ?? "", options);
}
export function assignMaintenanceTruck(value: MaintenanceInput, id: string, options: MaintenanceOptions): MaintenanceInput {
  const truck = options.trucks.find(t => t.id === id);
  const driverOwner = truck?.ownerDriverId && truck.ownerDriverId === truck.driverId;
  const coveredBy = truck?.isCompanyOwned ? "Company" : driverOwner ? "Driver" : truck?.ownerId ? "Truck Owner" : "Company";
  return { ...value, savedOwnerName:truck?.ownerName,truckId: id || null, expense: value.expense ? { ...value.expense, truckId: id || null, unitNumber: truck?.unitNumber ?? "", driverId: truck?.driverId ?? null, driverName: truck?.driverName ?? "", coveredBy, ownerId: coveredBy === "Truck Owner" ? truck?.ownerId ?? null : null } : null };
}
export function maintenanceToInput(record: MaintenanceRecord): MaintenanceInput {
  return { savedOwnerName:record.expense?.ownerName ?? undefined,legacyPaymentTotal: record.expense?.baseAmount == null && record.expense?.paymentType?.toLowerCase() === "efs" ? record.expense?.amount ?? undefined : undefined, id: record.id, version: record.version, truckId: record.truckId, vendorId: record.vendorId, vendorName: record.vendorName, vendorAddress: record.vendorAddress, vendorPhone: record.vendorPhone, vendorContact: record.vendorContact, serviceDate: record.serviceDate ?? "", odometer: record.odometer, location: record.location, status: record.status === "completed" ? "completed" : "needs_review", workOrder: record.workOrder, notes: record.notes, services: record.services, expense: record.expense ? expenseToInput(record.expense) : null };
}
export function maintenanceDraft(draft: MaintenanceAIDraft, options: MaintenanceOptions): MaintenanceInput {
  const base = emptyMaintenance(options);
  const meta = draft.maintenance;
  let services: MaintenanceWork[] = meta?.services?.length ? meta.services : draft.costBreakdown?.length ? draft.costBreakdown.map(s => ({ ...s, typeCode: serviceTypeForName(s.name), trackingCode: "" })) : [{ typeCode: serviceTypeForName(draft.expenseType), name: draft.expenseType || "", amount: null, trackingCode: "" }];
  services = services.map(s => ({ ...s, typeCode: options.types.some(t => t.active && t.code === s.typeCode) ? s.typeCode : options.types.find(t=>t.active&&t.code==="other")?.code ?? options.types.find(t=>t.active)?.code ?? "other", trackingCode: s.typeCode === "preventive" ? s.trackingCode : "" }));
  const matched = assignMaintenanceTruck(base, draft.truckId ?? "", options);
  const vendor = options.vendors.filter(v => v.active && v.name.toLowerCase() === meta?.vendorName?.trim().toLowerCase());
  return { ...matched, status: "needs_review", serviceDate: meta?.serviceDate ?? "", odometer: meta?.odometer ?? null, location: meta?.location ?? "unknown", vendorId: vendor.length === 1 ? vendor[0].id : null, vendorName: meta?.vendorName ?? "", vendorAddress: meta?.vendorAddress ?? "", vendorPhone: meta?.vendorPhone ?? "", vendorContact: meta?.vendorContact ?? "", workOrder: meta?.workOrder ?? "", notes: draft.description ?? "", services, expense: meta?.hasInvoice === false ? null : { ...matched.expense!, categoryId:options.categoryId,expenseDate:draft.expenseDate,amount:draft.amount,paymentType:draft.paymentType,expenseType:draft.expenseType,referenceNumber:draft.referenceNumber,description:draft.description,unitNumber:draft.unitNumber || matched.expense?.unitNumber || "",driverId:draft.driverId ?? matched.expense?.driverId ?? null,driverName:draft.driverName || matched.expense?.driverName || "",paidBy: options.currentUser, ownerId: matched.expense?.ownerId ?? null, coveredBy: matched.expense?.coveredBy ?? "Company", managerVerified: false, accountingVerified: false } };
}
export function maintenanceError(value: MaintenanceInput): string {
  if (value.status === "completed" && (!value.truckId || !value.serviceDate)) return "Completed work requires a truck and service date.";
  if (!value.services.length || value.services.some(s => !s.name.trim() || !s.typeCode)) return "Each work item needs a service type and name.";
  const priced = value.services.filter(s => s.amount !== null);
  if (priced.length && priced.length !== value.services.length) return "Price every work item or leave all costs unallocated.";
  const input: ExpenseInput = { ...(value.expense ?? emptyExpenseInput), amount: value.expense?.amount ?? "0", costBreakdown: priced.map(s => ({ name: s.name, amount: s.amount! })) };
  return costBreakdownError(input);
}
