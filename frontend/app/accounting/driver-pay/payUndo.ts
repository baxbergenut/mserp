import type { DriverPayEdits } from "@/app/lib/types";

// Restore only editable values. Keep the latest statement, expense and schedule
// versions so the existing save endpoint still rejects concurrent changes.
export function restorePayEdit(current: DriverPayEdits, before: DriverPayEdits, after: DriverPayEdits): DriverPayEdits {
  const restored = { ...current };
  for (const key of ["notes", "fuelOverride", "tollOverride", "comments", "adjustments"] as const) {
    if (JSON.stringify(before[key]) !== JSON.stringify(after[key])) Object.assign(restored, { [key]: before[key] });
  }
  restored.generatedCharges = current.generatedCharges?.map(row => {
    const previous = before.generatedCharges?.find(item => item.scheduleId === row.scheduleId);
    const next = after.generatedCharges?.find(item => item.scheduleId === row.scheduleId);
    if (!previous || !next || JSON.stringify(previous) === JSON.stringify(next)) return row;
    return { ...row, name: previous.name, amount: previous.amount, overridden: previous.overridden,
      waiveRemainder: previous.waiveRemainder, reset: !previous.overridden };
  });
  restored.expenseDeductions = current.expenseDeductions?.map(row => {
    const previous = before.expenseDeductions?.find(item => item.expenseId === row.expenseId);
    const next = after.expenseDeductions?.find(item => item.expenseId === row.expenseId);
    return previous && next && previous.amount !== next.amount ? { ...row, amount: previous.amount, apply: true } : row;
  });
  return restored;
}
