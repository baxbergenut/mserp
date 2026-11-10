import type { Expense } from "@/app/lib/types";

type Props = {
  expense?: Pick<Expense, "createdByName" | "createdAt" | "sourceSpreadsheetId" | "sourceSheet"> | null;
  pendingSaved?: boolean;
};

export function ExpenseCreationInfo({ expense, pendingSaved = false }: Props) {
  if (!expense) return <p className="mb-4 text-xs text-zinc-500" data-testid="expense-creation-info">
    {pendingSaved ? "Entry saved. Reopen its details to view the creator and time added." : "Not saved yet. Creator and time added are recorded when you save."}
  </p>;
  const timestamp = new Date(expense.createdAt);
  const validTime = Number.isFinite(timestamp.getTime());
  const imported = !!(expense.sourceSpreadsheetId || expense.sourceSheet);
  return <div className="mb-4 space-y-1 text-xs text-zinc-500" data-testid="expense-creation-info">
    <p>
      Added by <span className="text-zinc-300">{expense.createdByName?.trim() || "Not recorded"}</span>{" - "}
      <span className="text-zinc-300">{validTime ? <time dateTime={timestamp.toISOString()}>{new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "long" }).format(timestamp)}</time> : "Time not recorded"}</span>
    </p>
    {imported && <p>Imported record. This is the time added to Clustie, not necessarily when the original expense was entered.</p>}
  </div>;
}

