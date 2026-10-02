/** Canonical phone values are optional, ten ASCII digits, without a country code. */
export function normalizePhone(value: string): string | null {
  const trimmed = value.trim();
  if (!trimmed) return "";
  if (/[^0-9+(). -]/.test(trimmed)) return null;
  let digits = trimmed.replace(/[^0-9]/g, "");
  if (digits.length === 11 && digits.startsWith("1")) digits = digits.slice(1);
  return /^[0-9]{10}$/.test(digits) ? digits : null;
}

export function formatPhone(value: string | null | undefined): string {
  const digits = normalizePhone(value ?? "");
  if (!digits) return "";
  return `+1 (${digits.slice(0, 3)}) ${digits.slice(3, 6)}-${digits.slice(6)}`;
}

/** Fail before sending malformed numbers even when callers bypass native forms. */
export function withPhone<T extends { phone: string }>(input: T): T {
  const phone = normalizePhone(input.phone);
  if (phone === null) throw new Error("Phone must contain exactly 10 digits.");
  return { ...input, phone };
}
