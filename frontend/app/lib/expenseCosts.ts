import type { ExpenseInput } from "./types";

export function expenseCents(value: string): bigint | null {
  const match = /^(-?)(\d{1,12})(?:\.(\d{1,2}))?$/.exec(value.trim());
  if (!match) return null;
  return (BigInt(match[2]) * BigInt(100) + BigInt((match[3] ?? "").padEnd(2, "0"))) * (match[1] ? -BigInt(1) : BigInt(1));
}

export function costBreakdownError(value: ExpenseInput): string {
  const rows = value.costBreakdown ?? [];
  if (!rows.length) return "";
  if (rows.length > 100) return "Cost breakdown may contain at most 100 rows.";
  let sum = BigInt(0);
  for (const row of rows) {
    if (!row.name.trim() || Array.from(row.name.trim()).length > 100) return "Each cost breakdown needs a name of 1–100 characters.";
    const cents = expenseCents(row.amount);
    if (cents === null) return "Each cost breakdown needs an amount with at most two decimal places.";
    sum += cents;
  }
  return expenseCents(value.amount) === sum ? "" : "Cost breakdown total must exactly equal the entered amount.";
}

export function expensePayload(value: ExpenseInput) {
  const error = costBreakdownError(value);
  if (error) throw new Error(error);
  return { ...value, baseAmount: value.amount, costBreakdown: value.costBreakdown?.map(row => ({ name: row.name.trim(), amount: row.amount.trim() })) };
}

export function centsLabel(cents: bigint) {
  const abs = cents < BigInt(0) ? -cents : cents;
  return `${cents < BigInt(0) ? "-" : ""}$${abs / BigInt(100)}.${String(abs % BigInt(100)).padStart(2, "0")}`;
}
