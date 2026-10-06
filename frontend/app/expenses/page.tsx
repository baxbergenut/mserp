"use client";

import { useViewState } from "@/app/lib/viewMemory";
import { useQuickCreate } from "@/app/lib/topNavigation";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { Check, Settings, WalletCards, X } from "lucide-react";
import {
  createExpense,
  createExpenses,
  deleteExpense,
  fetchExpensesPage,
  fetchDrivers,
  fetchInvestors,
  fetchTrucks,
  updateExpense,
} from "../lib/api";
import type { Driver, Expense, ExpenseInput, ExpensePage, Investor, Truck } from "../lib/types";
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
  ExpenseForm,
  expenseToInput,
} from "./ExpenseForm";
import { ExpenseAIImport, ExpenseBatchEditor } from "./ExpenseAIImport";
import { useExpenseCategoryAccess, usePermissions } from "../lib/access";

const filterClass =
  "rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-1.5 text-[13px] text-zinc-300 outline-none transition-colors focus:border-zinc-600";

const emptyOptions: ExpensePage["options"] = {
  settings: [],
  categories: [],
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
	const categoryAccess = useExpenseCategoryAccess();
	const permissions = usePermissions();
	const canLoadFleetSelectors = categoryAccess.some(item => item.canCreate || item.canEdit);
  const [expenses, setExpenses] = useState<Expense[]>([]);
  const [search, setSearch] = useViewState("page:search", "");
  const [category, setCategory] = useViewState("page:categoryId", "");
  const [company, setCompany] = useViewState("page:company", "");
  const [dateFrom, setDateFrom] = useViewState("page:dateFrom", "");
  const [dateTo, setDateTo] = useViewState("page:dateTo", "");
  const [page, setPage] = useViewState("page:page", 1);
  const [pageSize, setPageSize] = useViewState("page:pageSize", 25);
  const [total, setTotal] = useState(0);
  const [totalPages, setTotalPages] = useState(1);
  const [summaryAmount, setSummaryAmount] = useState("0");
  const [incompleteCount, setIncompleteCount] = useState(0);
  const [options, setOptions] = useState(emptyOptions);
  const createCategoryIds = options.settings
    .filter(item => item.kind === "category" && item.active && categoryAccess.some(access => access.categoryId === item.id && access.canCreate))
    .map(item => item.id);
  const [drivers, setDrivers] = useState<Driver[]>([]);
  const [owners, setOwners] = useState<Investor[]>([]);
  const [trucks, setTrucks] = useState<Truck[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Expense | null | undefined>(undefined);
  const [form, setForm] = useState<ExpenseInput>(emptyExpenseInput);
  const [batchForms, setBatchForms] = useState<ExpenseInput[]>([]);
  const [aiMessage, setAIMessage] = useState("");
  const [isSaving, setIsSaving] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<Expense | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);
  const debouncedSearch = useDebouncedValue(search);

  const loadData = useCallback(async () => {
    setIsLoading(true);
    try {
      const response = await fetchExpensesPage({
        page, pageSize, search: debouncedSearch, categoryId: category, company, dateFrom, dateTo,
      });
      setExpenses(response.items);
      setPage(response.page);
      setTotal(response.total);
      setTotalPages(response.totalPages);
      setSummaryAmount(response.summary.amount);
      setIncompleteCount(response.summary.incompleteCount);
      setOptions(response.options);
      setError("");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to load entries");
    } finally {
      setIsLoading(false);
    }
  }, [setPage, category, company, dateFrom, dateTo, debouncedSearch, page, pageSize]);

  useEffect(() => {
    const timeout = window.setTimeout(() => void loadData(), 0);
    return () => window.clearTimeout(timeout);
  }, [loadData]);

  useEffect(() => {
    if (!canLoadFleetSelectors) return;
    let cancelled = false;
    Promise.all([fetchDrivers(), fetchTrucks(), fetchInvestors()])
      .then(([driverValues, truckValues, ownerValues]) => {
        if (!cancelled) {
          setDrivers(driverValues);
          setTrucks(truckValues);
          setOwners(ownerValues);
        }
      })
      .catch((reason) => {
        if (!cancelled) {
          setError(reason instanceof Error ? reason.message : "Failed to load fleet assignments");
        }
      });
    return () => { cancelled = true; };
  }, [canLoadFleetSelectors]);

  const hasFilters = Boolean(search || category || company || dateFrom || dateTo);

  function openCreate() {
    setForm({ ...emptyExpenseInput, categoryId: options.settings.find(item => item.kind === "category" && item.active && item.name === "Maintenance" && createCategoryIds.includes(item.id))?.id ?? options.settings.find(item => item.kind === "category" && item.active && createCategoryIds.includes(item.id))?.id ?? "", expenseDate: new Date().toISOString().slice(0, 10) });
    setBatchForms([]);
    setAIMessage("");
    setError("");
    setEditing(null);
  }

  useQuickCreate(openCreate, !isLoading && createCategoryIds.length > 0);

  function openEdit(expense: Expense) {
    setForm(expenseToInput(expense));
    setBatchForms([]);
    setAIMessage("");
    setEditing(expense);
  }

  async function save() {
    setIsSaving(true);
    setError("");
    try {
      if (editing) await updateExpense(editing.id, form);
      else if (batchForms.length > 1) await createExpenses(batchForms);
      else if (batchForms.length === 1) await createExpense(batchForms[0]);
      else await createExpense(form);
      setEditing(undefined);
      await loadData();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to save entry");
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
      setError(reason instanceof Error ? reason.message : "Failed to delete entry");
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
        title="Expenses & Charges"
        description="Record and review the costs and charges assigned to your categories."
        count={total}
        actionLabel={createCategoryIds.length ? "Add entry" : undefined}
        onAction={createCategoryIds.length ? openCreate : undefined}
        secondaryAction={permissions.includes("expense_settings.manage") ? <Link href="/expenses/settings" className="inline-flex items-center gap-2 rounded-lg border border-zinc-800 px-3 py-2 text-xs text-zinc-400 hover:bg-zinc-800"><Settings className="h-3.5 w-3.5" />Expenses & Charges settings</Link> : undefined}
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
        <ManagementSearch value={search} onChange={(value) => { setSearch(value); setPage(1); }} placeholder="Search expenses and charges…" />
        <select value={category} onChange={(event) => { setCategory(event.target.value); setPage(1); }} className={filterClass}>
          <option value="">All categories</option>
          {options.categories.map((value) => <option key={value.id} value={value.id}>{value.name}</option>)}
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
          <EmptyState message={hasFilters ? "No entries match these filters." : createCategoryIds.length ? "No entries yet. Add the first entry." : "No entries are available in your categories."} />
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
                <th className="px-4 py-3 text-right font-medium">Total amount</th>
                <th className="px-4 py-3 text-right font-medium">Paid</th><th className="px-4 py-3 text-right font-medium">Remaining</th>
                <th className="px-4 py-3 text-right font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {expenses.map((expense) => (
                <tr key={expense.id} className="border-b border-zinc-900/70 text-zinc-300 transition last:border-0 hover:bg-zinc-800/15">
                  <td className="px-4 py-3">
                    <div className={expense.expenseDate ? "font-mono tabular-nums text-zinc-200" : "text-amber-400"}>{formatDate(expense.expenseDate)}</div>
                    <div className="mt-0.5 text-[11px] text-zinc-600">{formatWeek(expense.weekStart)}</div>
					<div className="mt-0.5 text-[11px] text-zinc-600">{expense.createdByName ? `Added by ${expense.createdByName}` : "Added before tracking"}</div>
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
                    <div className="text-zinc-300">{expense.coveredBy || "—"}{expense.ownerName && <span className="block text-xs text-zinc-500">{expense.ownerName}</span>}</div>
                    <div className="mt-1 flex gap-2">
                      <Verification checked={expense.managerVerified} label="Manager" />
                      <Verification checked={expense.accountingVerified} label="Accounting" />
                    </div>
                  </td>
                  <td className={`px-4 py-3 text-right font-mono font-medium tabular-nums ${expense.amount === null ? "text-amber-400" : "text-zinc-100"}`}>
                    {formatMoney(expense.amount)}
                  </td>
                  <td className="px-4 py-3 text-right font-mono tabular-nums" title={expense.driverSettled ? "Existing driver expense: assumed fully paid" : "Saved Driver Pay deductions"}>{expense.paidAmount == null ? "—" : formatMoney(expense.paidAmount)}</td>
                  <td className="px-4 py-3 text-right font-mono tabular-nums">{expense.remainingAmount == null ? "—" : formatMoney(expense.remainingAmount)}</td>
                  <td className="px-4 py-3"><RowActions onEdit={categoryAccess.some(item => item.categoryId === expense.categoryId && item.canEdit) ? () => openEdit(expense) : undefined} onDelete={categoryAccess.some(item => item.categoryId === expense.categoryId && item.canDelete) ? () => setPendingDelete(expense) : undefined} /></td>
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
          title={editing ? "Edit entry" : "Add entry"}
          description={editing?.sourceSheet ? `Imported from ${editing.sourceSheet}, row ${editing.sourceRow}.` : batchForms.length > 0 ? "Review every AI suggestion before creating these entries." : "Enter an expense or charge manually, or use AI to fill the details."}
          isSaving={isSaving}
          submitLabel={editing ? "Save changes" : batchForms.length > 1 ? `Create ${batchForms.length} entries` : "Create entry"}
          wide={batchForms.length > 0}
          onClose={() => setEditing(undefined)}
          onSubmit={(event) => { event.preventDefault(); void save(); }}
        >
          {error && <div className="mb-4"><ErrorBanner message={error} /></div>}
          {!editing && (
            <div className="mb-5">
              <ExpenseAIImport
                onError={setError}
                onExtracted={(drafts) => {
                  setError("");
                  if (drafts.length === 1) {
                    setForm(drafts[0]);
                    setBatchForms([]);
                    setAIMessage("AI found one transaction and filled the form below.");
                  } else {
                    setBatchForms(drafts);
                    setAIMessage(`AI found ${drafts.length} transactions. Review and edit them in the table below.`);
                  }
                }}
              />
              {aiMessage && (
                <div className="mt-3 flex items-center gap-2 rounded-lg border border-emerald-500/20 bg-emerald-500/5 px-3 py-2 text-[12px] text-emerald-300">
                  <Check className="h-4 w-4 shrink-0" /> {aiMessage}
                </div>
              )}
            </div>
          )}
          {batchForms.length > 0 && !editing ? (
            <ExpenseBatchEditor settings={options.settings} allowedCategoryIds={createCategoryIds} owners={owners} values={batchForms} drivers={drivers} trucks={trucks} onChange={setBatchForms} />
          ) : (
            <ExpenseForm originalCategory={editing ? { id: editing.categoryId, name: editing.category } : undefined} allowedCategoryIds={editing ? Array.from(new Set([...createCategoryIds, editing.categoryId])) : createCategoryIds} owners={owners} value={form} options={options} drivers={drivers} trucks={trucks} onChange={setForm} />
          )}
        </Modal>
      )}

      {pendingDelete && (
        <ConfirmDialog
          title="Delete entry?"
          message={`This permanently deletes the ${formatMoney(pendingDelete.amount)} ${pendingDelete.category.toLowerCase()} entry${pendingDelete.description ? ` for ${pendingDelete.description}` : ""}.`}
          isDeleting={isDeleting}
          onCancel={() => setPendingDelete(null)}
          onConfirm={() => void remove()}
        />
      )}
    </div>
  );
}
