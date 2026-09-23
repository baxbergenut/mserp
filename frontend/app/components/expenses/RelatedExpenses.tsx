"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { WalletCards } from "lucide-react";
import { fetchExpensesPage } from "@/app/lib/api";
import type { Expense } from "@/app/lib/types";
import {
  EmptyState,
  ErrorBanner,
  LoadingTable,
  TablePagination,
  TableShell,
} from "@/app/components/management/ManagementUI";

function formatMoney(value: string | null) {
  if (value === null) return "Missing";
  return new Intl.NumberFormat("en-US", { style: "currency", currency: "USD" }).format(Number(value));
}

function formatDate(value: string | null) {
  if (!value) return "Missing date";
  return new Date(`${value}T00:00:00`).toLocaleDateString("en-US", {
    month: "short", day: "numeric", year: "numeric",
  });
}

export function RelatedExpenses({
  truckId,
  driverId,
}: {
  truckId?: string;
  driverId?: string;
}) {
  const [expenses, setExpenses] = useState<Expense[]>([]);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [total, setTotal] = useState(0);
  const [totalPages, setTotalPages] = useState(1);
  const [amount, setAmount] = useState("0");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const response = await fetchExpensesPage({ page, pageSize, truckId, driverId });
      setExpenses(response.items);
      setPage(response.page);
      setTotal(response.total);
      setTotalPages(response.totalPages);
      setAmount(response.summary.amount);
      setError("");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to load expenses");
    } finally {
      setLoading(false);
    }
  }, [driverId, page, pageSize, truckId]);

  useEffect(() => {
    const timeout = window.setTimeout(() => void load(), 0);
    return () => window.clearTimeout(timeout);
  }, [load]);

  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <WalletCards className="h-4 w-4 text-zinc-500" />
            <h2 className="text-sm font-semibold text-zinc-100">Expenses</h2>
            <span className="rounded-full bg-zinc-800/60 px-2 py-0.5 text-[11px] text-zinc-400">{total}</span>
          </div>
          <p className="mt-1 text-[12px] text-zinc-500">Linked total: {formatMoney(amount)}</p>
        </div>
        <Link href="/expenses" className="text-[12px] font-medium text-blue-400 transition hover:text-blue-300">Open expense manager</Link>
      </div>
      {error && <ErrorBanner message={error} />}
      <TableShell>
        {loading ? (
          <LoadingTable columns={5} />
        ) : expenses.length === 0 ? (
          <EmptyState message="No expenses are linked to this record." />
        ) : (
          <table className="w-full min-w-[820px] text-left text-[13px]">
            <thead>
              <tr className="border-b border-zinc-800/50 text-zinc-500">
                <th className="px-4 py-3 font-medium">Date</th>
                <th className="px-4 py-3 font-medium">Category</th>
                <th className="px-4 py-3 font-medium">Type</th>
                <th className="px-4 py-3 font-medium">Description</th>
                <th className="px-4 py-3 text-right font-medium">Amount</th>
              </tr>
            </thead>
            <tbody>
              {expenses.map((expense) => (
                <tr key={expense.id} className="border-b border-zinc-900/70 text-zinc-300 last:border-0">
                  <td className="px-4 py-3 font-mono tabular-nums">{formatDate(expense.expenseDate)}</td>
                  <td className="px-4 py-3">{expense.category}</td>
                  <td className="px-4 py-3 text-zinc-400">{expense.expenseType || "—"}</td>
                  <td className="max-w-[330px] truncate px-4 py-3 text-zinc-400" title={expense.description ?? undefined}>{expense.description || "—"}</td>
                  <td className={`px-4 py-3 text-right font-mono font-medium tabular-nums ${expense.amount === null ? "text-amber-400" : "text-zinc-100"}`}>{formatMoney(expense.amount)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </TableShell>
      {!loading && total > 0 && (
        <TablePagination
          page={page}
          pageSize={pageSize}
          totalItems={total}
          totalPages={totalPages}
          onPageChange={setPage}
          onPageSizeChange={(value) => { setPageSize(value); setPage(1); }}
        />
      )}
    </section>
  );
}
