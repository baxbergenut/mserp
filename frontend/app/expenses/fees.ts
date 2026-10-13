import { hundredths, validDecimal } from "@/app/gross-board/board";

export function expenseFee(amount: string, paymentType: string): bigint | null {
  if (paymentType.trim().toUpperCase() !== "EFS" || !amount.trim() || !validDecimal(amount)) return null;
  const cents = hundredths(amount);
  if (cents < BigInt(0)) return null;
  return ((cents + BigInt(49999)) / BigInt(50000)) * BigInt(300);
}
