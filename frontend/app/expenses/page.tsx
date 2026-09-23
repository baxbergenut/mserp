"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { Check, WalletCards, X } from "lucide-react";
import {
  createExpense,
  deleteExpense,
  fetchExpensesPage,
  fetchDrivers,
  fetchTrucks,
  updateExpense,
} from "../lib/api";
import type { Driver, Expense, ExpenseInput, Truck } from "../lib/types";
import { useDebouncedValue } from "../lib/useDebouncedValue";
import {
  ConfirmDialog,
  EmptyState,
  ErrorBanner,
  LoadingTable,
  ManagementHeader,
  ManagementSearch,
  Modal,
  RowActions,
  TablePagination,
  TableShell,
} from "../components/management/ManagementUI";
import {
  emptyExpenseInput,
  EXPENSE_CATEGORIES,
  ExpenseForm,
  expenseToInput,
} from "./ExpenseForm";

const filterClass =
  "rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-1.5 text-[13px] text-zinc-300 outline-none transition-colors focus:border-zinc-600";

const emptyOptions = {
  companies: [] as string[],
  paymentTypes: [] as string[],
  expenseTypes: [] as string[],
  paidBy: [] as string[],
  coveredBy: [] as string[],
};

function formatMoney(value: string | null) {
  if (value === null) return "Missing";
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
  }).format(Number(value));
}

function formatDate(value: string | null) {
  if (!value) return "Missing date";
  return new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
  }).format(new Date(`${value}T00:00:00`));
}

function formatWeek(value: string | null) {
  if (!value) return "Week unavailable";
  const start = new Date(`${value}T00:00:00`);
  const end = new Date(start);
  end.setDate(start.getDate() + 6);
  const short = (date: Date) => date.toLocaleDateString("en-US", { month: "short", day: "numeric" });
  return `${short(start)} – ${short(end)}`;
}

function Verification({ checked, label }: { checked: boolean; label: string }) {
  return (
    <span className={`inline-flex items-center gap-1 text-[11px] ${checked ? "text-emerald-400" : "text-zinc-600"}`}>
      {checked ? <Check className="h-3 w-3" /> : <X className="h-3 w-3" />}
      {label}
    </span>
  );
}

export default function ExpensesPage() {
  const [expenses, setExpenses] = useState<Expense[]>([]);
  const [search, setSearch] = useState("");
  const [category, setCategory] = useState("");
  const [company, setCompany] = useState("");
  const [dateFrom, setDateFrom] = useState("");
  const [dateTo, setDateTo] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);
  const [total, setTotal] = useState(0);
  const [totalPages, setTotalPages] = useState(1);
  const [summaryAmount, setSummaryAmount] = useState("0");
  const [incompleteCount, setIncompleteCount] = useState(0);
  const [options, setOptions] = useState(emptyOptions);
  const [drivers, setDrivers] = useState<Driver[]>([]);
  const [trucks, setTrucks] = useState<Truck[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Expense | null | undefined>(undefined);
  const [form, setForm] = useState<ExpenseInput>(emptyExpenseInput);
  const [isSaving, setIsSaving] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<Expense | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);
  const debouncedSearch = useDebouncedValue(search);

  const loadData = useCallback(async () => {
    setIsLoading(true);
    try {
      const response = await fetchExpensesPage({
        page, pageSize, search: debouncedSearch, category, company, dateFrom, dateTo,
      });
      setExpenses(response.items);
      setPage(response.page);
      setTotal(response.total);
      setTotalPages(response.totalPages);
      setSummaryAmount(response.summary.amount);
      setIncompleteCount(response.summary.incompleteCount);
      setOptions({
        companies: response.options.companies,
        paymentTypes: response.options.paymentTypes,
        expenseTypes: response.options.expenseTypes,
        paidBy: response.options.paidBy,
        coveredBy: response.options.coveredBy,
      });
      setError("");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to load expenses");
    } finally {
      setIsLoading(false);
    }
  }, [category, company, dateFrom, dateTo, debouncedSearch, page, pageSize]);

  useEffect(() => {
    const timeout = window.setTimeout(() => void loadData(), 0);
    return () => window.clearTimeout(timeout);
  }, [loadData]);

  useEffect(() => {
    let cancelled = false;
    Promise.all([fetchDrivers(), fetchTrucks()])
      .then(([driverValues, truckValues]) => {
        if (!cancelled) {
          setDrivers(driverValues);
          setTrucks(truckValues);
        }
      })
      .catch((reason) => {
        if (!cancelled) {
          setError(reason instanceof Error ? reason.message : "Failed to load fleet assignments");
        }
      });
    return () => { cancelled = true; };
  }, []);

  const hasFilters = Boolean(search || category || company || dateFrom || dateTo);

  function openCreate() {
    setForm({ ...emptyExpenseInput, expenseDate: new Date().toISOString().slice(0, 10) });
    setEditing(null);
  }

  function openEdit(expense: Expense) {
    setForm(expenseToInput(expense));
    setEditing(expense);
  }

  async function save() {
    setIsSaving(true);
    setError("");
    try {
      if (editing) await updateExpense(editing.id, form);
      else await createExpense(form);
      setEditing(undefined);
      await loadData();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to save expense");
    } finally {
      setIsSaving(false);
    }
  }

  async function remove() {
    if (!pendingDelete) return;
    setIsDeleting(true);
    setError("");
    try {
      await deleteExpense(pendingDelete.id);
      setPendingDelete(null);
      await loadData();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to delete expense");
      setPendingDelete(null);
    } finally {
      setIsDeleting(false);
    }
  }

  function clearFilters() {
    setSearch("");
    setCategory("");
    setCompany("");
    setDateFrom("");
    setDateTo("");
    setPage(1);
  }

  return (
    <div className="space-y-5 animate-fade-in">
      <ManagementHeader
        icon={WalletCards}
        title="Expenses"
        description="Record, verify, and review company expenses across every department."
        count={total}
        actionLabel="Add expense"
        onAction={openCreate}
      />

      {error && <ErrorBanner message={error} />}

      <div className="grid gap-3 sm:grid-cols-3">
        <div className="rounded-xl border border-zinc-800/60 bg-zinc-900/30 p-4">
          <p className="text-[11px] font-medium uppercase tracking-wider text-zinc-600">Filtered total</p>
          <p className="mt-1 font-mono text-xl font-semibold tabular-nums text-zinc-100">{formatMoney(summaryAmount)}</p>
        </div>
        <div className="rounded-xl border border-zinc-800/60 bg-zinc-900/30 p-4">
          <p className="text-[11px] font-medium uppercase tracking-wider text-zinc-600">Records</p>
          <p className="mt-1 font-mono text-xl font-semibold tabular-nums text-zinc-100">{total.toLocaleString()}</p>
        </div>
        <div className="rounded-xl border border-zinc-800/60 bg-zinc-900/30 p-4">
          <p className="text-[11px] font-medium uppercase tracking-wider text-zinc-600">Needs date or amount</p>
          <p className={`mt-1 font-mono text-xl font-semibold tabular-nums ${incompleteCount ? "text-amber-400" : "text-zinc-100"}`}>{incompleteCount.toLocaleString()}</p>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <ManagementSearch value={search} onChange={(value) => { setSearch(value); setPage(1); }} placeholder="Search expenses…" />
        <select value={category} onChange={(event) => { setCategory(event.target.value); setPage(1); }} className={filterClass}>
          <option value="">All categories</option>
          {EXPENSE_CATEGORIES.map((value) => <option key={value}>{value}</option>)}
        </select>
        <select value={company} onChange={(event) => { setCompany(event.target.value); setPage(1); }} className={filterClass}>
          <option value="">All companies</option>
          {options.companies.map((value) => <option key={value}>{value}</option>)}
        </select>
        <div className="flex items-center gap-1.5">
          <input aria-label="Expense date from" type="date" value={dateFrom} onChange={(event) => { setDateFrom(event.target.value); setPage(1); }} className={filterClass} />
          <span className="text-[13px] text-zinc-700">–</span>
          <input aria-label="Expense date to" type="date" value={dateTo} onChange={(event) => { setDateTo(event.target.value); setPage(1); }} className={filterClass} />
        </div>
        {hasFilters && (
          <button type="button" onClick={clearFilters} className="inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-[13px] text-zinc-500 transition hover:bg-zinc-800/50 hover:text-zinc-300">
            <X className="h-3 w-3" /> Clear
          </button>
        )}
      </div>

      <TableShell>
        {isLoading ? (
          <LoadingTable columns={8} />
        ) : expenses.length === 0 ? (
          <EmptyState message={hasFilters ? "No expenses match these filters." : "No expenses yet. Add the first expense."} />
        ) : (
          <table className="w-full min-w-[1380px] text-left text-[13px]">
            <thead>
              <tr className="border-b border-zinc-800/50 text-zinc-500">
                <th className="px-4 py-3 font-medium">Date / week</th>
                <th className="px-4 py-3 font-medium">Category / company</th>
                <th className="px-4 py-3 font-medium">Unit / driver</th>
                <th className="px-4 py-3 font-medium">Expense</th>
                <th className="px-4 py-3 font-medium">Payment</th>
                <th className="px-4 py-3 font-medium">Responsibility</th>
                <th className="px-4 py-3 text-right font-medium">Amount</th>
                <th className="px-4 py-3 text-right font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {expenses.map((expense) => (
                <tr key={expense.id} className="border-b border-zinc-900/70 text-zinc-300 transition last:border-0 hover:bg-zinc-800/15">
                  <td className="px-4 py-3">
                    <div className={expense.expenseDate ? "font-mono tabular-nums text-zinc-200" : "text-amber-400"}>{formatDate(expense.expenseDate)}</div>
                    <div className="mt-0.5 text-[11px] text-zinc-600">{formatWeek(expense.weekStart)}</div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="font-medium text-zinc-200">{expense.category}</div>
                    <div className="mt-0.5 text-[11px] text-zinc-600">{expense.company}</div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="font-mono text-zinc-300">
                      {expense.truckId ? (
                        <Link href={`/trucks/detail?id=${expense.truckId}`} className="transition hover:text-blue-400">{expense.unitNumber || "—"}</Link>
                      ) : expense.unitNumber || "—"}
                    </div>
                    <div className="mt-0.5 text-[11px] text-zinc-600">
                      {expense.driverId ? (
                        <Link href={`/drivers/detail?id=${expense.driverId}`} className="transition hover:text-blue-400">{expense.driverName || "No driver"}</Link>
                      ) : expense.driverName || "No driver"}
                    </div>
                  </td>
                  <td className="max-w-[310px] px-4 py-3">
                    <div className="text-zinc-300">{expense.expenseType || "Uncategorized"}</div>
                    <div className="mt-0.5 truncate text-[11px] text-zinc-600" title={expense.description ?? undefined}>{expense.description || expense.referenceNumber || "No description"}</div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="text-zinc-300">{expense.paymentType || "—"}</div>
                    <div className="mt-0.5 text-[11px] text-zinc-600">Paid by {expense.paidBy || "—"}</div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="text-zinc-300">{expense.coveredBy || "—"}</div>
                    <div className="mt-1 flex gap-2">
                      <Verification checked={expense.managerVerified} label="Manager" />
                      <Verification checked={expense.accountingVerified} label="Accounting" />
                    </div>
                  </td>
                  <td className={`px-4 py-3 text-right font-mono font-medium tabular-nums ${expense.amount === null ? "text-amber-400" : "text-zinc-100"}`}>
                    {formatMoney(expense.amount)}
                  </td>
                  <td className="px-4 py-3"><RowActions onEdit={() => openEdit(expense)} onDelete={() => setPendingDelete(expense)} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </TableShell>

      {!isLoading && (
        <TablePagination
          page={page}
          pageSize={pageSize}
          totalItems={total}
          totalPages={totalPages}
          onPageChange={setPage}
          onPageSizeChange={(value) => { setPageSize(value); setPage(1); }}
        />
      )}

      {editing !== undefined && (
        <Modal
          title={editing ? "Edit expense" : "Add expense"}
          description={editing?.sourceSheet ? `Imported from ${editing.sourceSheet}, row ${editing.sourceRow}.` : "Record an expense and its review status."}
          isSaving={isSaving}
          submitLabel={editing ? "Save changes" : "Create expense"}
          onClose={() => setEditing(undefined)}
          onSubmit={(event) => { event.preventDefault(); void save(); }}
        >
          {error && <div className="mb-4"><ErrorBanner message={error} /></div>}
          <ExpenseForm value={form} options={options} drivers={drivers} trucks={trucks} onChange={setForm} />
        </Modal>
      )}

      {pendingDelete && (
        <ConfirmDialog
          title="Delete expense?"
          message={`This permanently deletes the ${formatMoney(pendingDelete.amount)} ${pendingDelete.category.toLowerCase()} expense${pendingDelete.description ? ` for ${pendingDelete.description}` : ""}.`}
          isDeleting={isDeleting}
          onCancel={() => setPendingDelete(null)}
          onConfirm={() => void remove()}
        />
      )}
    </div>
  );
}
