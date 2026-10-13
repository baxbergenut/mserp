import { decimalDisplay, hundredths } from "@/app/gross-board/board";
import { expenseFee } from "./fees";

export function ExpenseFeePreview({ amount, paymentType }: { amount: string; paymentType: string }) {
  const fee = expenseFee(amount, paymentType);
  if (fee === null) return null;
  return <p className="mt-1 text-xs text-zinc-500" role="status">EFS fee: {decimalDisplay(fee, true)} · Total: {decimalDisplay(hundredths(amount) + fee, true)}</p>;
}
