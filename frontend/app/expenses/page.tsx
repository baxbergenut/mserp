"use client";


import { ColumnFilter } from "@/app/components/management/ColumnFilter";
import { useViewState } from "@/app/lib/viewMemory";
import { useQuickCreate } from "@/app/lib/topNavigation";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { Check, Settings, Trash2, WalletCards, X } from "lucide-react";
import {
  createExpense,
  createExpenses,
  deleteExpense,
  fetchExpensesPage,
  fetchAuthSession,
  fetchDrivers,
  fetchInvestors,
  fetchTrucks,
  updateExpense,
  verifyExpense,
  unverifyExpense,
} from "../lib/api";
import type { Driver, Expense, ExpenseInput, ExpensePage, ExpenseVerification, Investor, Truck } from "../lib/types";
import { useDebouncedValue } from "../lib/useDebouncedValue";
import {
  ConfirmDialog,
  EmptyState,
  ErrorBanner,
  ManagementHeader,
  ManagementSearch,
  Modal,
  TablePagination,
  TableShell,
} from "../components/management/ManagementUI";
import {
  emptyExpenseInput,
  ExpenseForm,
  expenseToInput,
} from "./ExpenseForm";
import { ExpenseAIImport, ExpenseBatchEditor } from "./ExpenseAIImport";
import { defaultExpenseResponsibility, expenseAssignment } from "./assignments";
import { useExpenseCategoryAccess, usePermissions } from "../lib/access";

const filterClass =
  "rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-1.5 text-[13px] text-zinc-300 outline-none transition-colors focus:border-zinc-600";

const emptyOptions: ExpensePage["options"] = {
  settings: [],
  categories: [],
  filters: { drivers: [], trucks: [], paymentTypes: [], paidBy: [], coveredBy: [] },
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

function verificationLabel(verification: ExpenseVerification) {
  const role = verification.role === "admin" ? "Administrator" : "Accountant";
  return role + ": " + (verification.userName || "Verifier not recorded");
}

function Verification({ expense, onClick }: { expense: Expense; onClick: () => void }) {
  const label = (expense.verifications ?? []).map(verificationLabel).join("; ") || [expense.accountingVerified && "Accountant: verifier not recorded", expense.managerVerified && "Administrator: verifier not recorded"].filter(Boolean).join("; ");
  return label ? <button type="button" onClick={onClick} title={label} aria-label={"View verification: " + label} className="inline-flex h-8 min-w-8 items-center justify-center rounded hover:bg-zinc-800/50 focus-visible:outline-2 focus-visible:outline-blue-500">
    <span aria-label={label} role="img" className="relative inline-block h-5 w-[26px] shrink-0">
      {expense.accountingVerified && <Check strokeWidth={3} className="absolute left-0 top-0 h-5 w-5 text-blue-500" />}
      {expense.managerVerified && <Check strokeWidth={3} className={"absolute h-5 w-5 text-emerald-500 " + (expense.accountingVerified ? "left-1.5 top-0" : "left-0 top-0")} />}
    </span>
  </button> : null;
}

export default function ExpensesPage() {
	const categoryAccess = useExpenseCategoryAccess();
	const permissions = usePermissions();
	const canLoadFleetSelectors = categoryAccess.some(item => item.canCreate || item.canEdit);
  const [expenses, setExpenses] = useState<Expense[]>([]);
  const [search, setSearch] = useViewState("page:search", "");
  const [category, setCategory] = useViewState("page:categoryId", "");

  const [unitNumber, setUnitNumber] = useViewState("page:unitNumber", "");
  const [driverName, setDriverName] = useViewState("page:driverName", "");
  const [paidBy, setPaidBy] = useViewState("page:paidBy", "");
  const [paymentType, setPaymentType] = useViewState("page:paymentType", "");
  const [coveredBy, setCoveredBy] = useViewState("page:coveredBy", "");
  const [dateFrom, setDateFrom] = useViewState("page:dateFrom", "");
  const [accountantVerified, setAccountantVerified] = useViewState("page:accountantVerified", "");
  const [adminVerified, setAdminVerified] = useViewState("page:adminVerified", "");
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
  const [currentUser, setCurrentUser] = useState("");
  const [currentUserId, setCurrentUserId] = useState("");
  const [verificationDetails, setVerificationDetails] = useState<Expense | null>(null);
  const verificationUndo = useRef<{ expenseId: string; verificationId: string }[]>([]);
  const verificationBusy = useRef(false);
  const [verificationNotice, setVerificationNotice] = useState("");
  const [verificationRole, setVerificationRole] = useState<"admin" | "accountant" | "">("");
  const [verifying, setVerifying] = useState<string | null>(null);
  const [isSaving, setIsSaving] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<Expense | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);
  const debouncedSearch = useDebouncedValue(search);

  const loadData = useCallback(async (showLoading = true) => {
    if (showLoading) setIsLoading(true);
    try {
      const [response, session] = await Promise.all([fetchExpensesPage({
        page, pageSize, search: debouncedSearch, categoryId: category, dateFrom, dateTo, accountantVerified, adminVerified, unitNumber, driverName, paymentType, paidBy, coveredBy,
      }), fetchAuthSession()]);
      setCurrentUser(session.user.username);
      setCurrentUserId(session.user.id);
      setVerificationRole(session.user.expenseVerificationRole ?? "");
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
  }, [setPage, category, dateFrom, dateTo, debouncedSearch, page, pageSize, accountantVerified, adminVerified, unitNumber, driverName, paymentType, paidBy, coveredBy]);

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

  const hasFilters = Boolean(unitNumber || driverName || paymentType || paidBy || coveredBy || search || category || dateFrom || dateTo || accountantVerified || adminVerified);

  function openCreate() {
    setForm({ ...emptyExpenseInput, paidBy: currentUser, categoryId: options.settings.find(item => item.kind === "category" && item.active && item.name === "Maintenance" && createCategoryIds.includes(item.id))?.id ?? options.settings.find(item => item.kind === "category" && item.active && createCategoryIds.includes(item.id))?.id ?? "", expenseDate: new Intl.DateTimeFormat("en-CA", { timeZone: "America/New_York", year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date()) });
    setBatchForms([]);
    setError("");
    setEditing(null);
  }

  useQuickCreate(openCreate, !isLoading && createCategoryIds.length > 0);

  function openEdit(expense: Expense) {
    setForm(expenseToInput(expense));
    setBatchForms([]);
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
      setVerificationNotice("Saved.");
      await loadData(false);
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
      setEditing(undefined);
      await loadData();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to delete entry");
      setPendingDelete(null);
    } finally {
      setIsDeleting(false);
    }
  }

  async function verify(expense: Expense) {
    if (verificationBusy.current) return;
    verificationBusy.current = true;
    setVerifying(expense.id);
    setError("");
    try {
      const updated = await verifyExpense(expense.id, expense.updatedAt);
      const stamp = updated.verifications.find(item => item.role === verificationRole && item.userId === currentUserId);
      if (stamp) verificationUndo.current.push({ expenseId: expense.id, verificationId: stamp.id });
      setVerificationNotice("Verified. Press Ctrl+Z to undo.");
      setExpenses(items => items.map(item => item.id === updated.id ? updated : item));
      if (accountantVerified || adminVerified) await loadData(false);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not verify entry");
    } finally { setVerifying(null); verificationBusy.current = false; }
  }

  const cancelVerification = useCallback(async (expenseId: string, verificationId: string) => {
    if (verificationBusy.current) return;
    verificationBusy.current = true;
    setVerifying(expenseId);
    setError("");
    try {
      const updated = await unverifyExpense(expenseId, verificationId);
      setExpenses(items => items.map(item => item.id === updated.id ? updated : item));
      if (accountantVerified || adminVerified) await loadData(false);
      setVerificationDetails(current => current?.id === updated.id ? updated : current);
      verificationUndo.current = verificationUndo.current.filter(item => item.verificationId !== verificationId);
      setVerificationNotice("Verification removed.");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not remove verification");
    } finally { setVerifying(null); verificationBusy.current = false; }
  }, [accountantVerified, adminVerified, loadData]);

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.defaultPrevented || event.shiftKey || !(event.ctrlKey || event.metaKey) || event.key.toLowerCase() !== "z" || editing !== undefined || verificationDetails) return;
      if ((event.target as HTMLElement)?.closest("input,textarea,select,[contenteditable='true']")) return;
      const last = verificationUndo.current.at(-1);
      if (!last) return;
      event.preventDefault();
      if (!verificationBusy.current) void cancelVerification(last.expenseId, last.verificationId);
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [cancelVerification, editing, verificationDetails]);

  useEffect(() => {
    if (!verificationNotice) return;
    const timeout = window.setTimeout(() => setVerificationNotice(""), 5000);
    return () => window.clearTimeout(timeout);
  }, [verificationNotice]);

  function canCancelVerification(stamp: ExpenseVerification) {
    return stamp.userId === currentUserId || verificationRole === "admin" || verificationRole === stamp.role;
  }

  function clearFilters() {
    setSearch("");
    setCategory("");
    setUnitNumber(""); setDriverName(""); setPaymentType(""); setPaidBy(""); setCoveredBy("");

    setDateFrom("");
    setDateTo("");
    setAccountantVerified("");
    setAdminVerified("");
    setPage(1);
  }

  return (
    <div className="space-y-5 animate-fade-in">


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



        {hasFilters && (
          <button type="button" onClick={clearFilters} className="inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-[13px] text-zinc-500 transition hover:bg-zinc-800/50 hover:text-zinc-300">
            <X className="h-3 w-3" /> Clear
          </button>
        )}
      <div className="ml-auto flex max-w-full flex-wrap items-center gap-2"><span role="status" title={verificationNotice} className="h-8 w-52 shrink-0 truncate text-xs leading-8 text-zinc-400">{isSaving ? "Saving…" : verifying ? "Saving verification…" : verificationNotice}</span><ManagementHeader
        icon={WalletCards}
        title="Expenses"

        count={total}
        actionLabel={createCategoryIds.length ? "Add entry" : undefined}
        onAction={createCategoryIds.length ? openCreate : undefined}
        secondaryAction={permissions.includes("expense_settings.manage") ? <Link href="/expenses/settings" className="inline-flex items-center gap-2 rounded-lg border border-zinc-800 px-3 py-2 text-xs text-zinc-400 hover:bg-zinc-800"><Settings className="h-3.5 w-3.5" />Expenses settings</Link> : undefined}
      /></div></div>


      <TableShell>
        <table className="w-full min-w-[960px] text-left text-[13px]">
            <thead>
              <tr className="border-b border-zinc-800/50 text-zinc-500">
                <th className="px-4 py-3 font-medium"><ColumnFilter label="Date" active={!!(dateFrom || dateTo)}><label className="block space-y-1">From<input aria-label="Expense date from" type="date" value={dateFrom} onChange={(event) => { setDateFrom(event.target.value); setPage(1); }} className={filterClass} /></label><label className="block space-y-1">To<input aria-label="Expense date to" type="date" value={dateTo} onChange={(event) => { setDateTo(event.target.value); setPage(1); }} className={filterClass} /></label></ColumnFilter></th>
                <th className="px-4 py-3 font-medium"><ColumnFilter label="Unit / driver" active={!!(unitNumber || driverName)}><select aria-label="Expense truck" value={unitNumber} onChange={event => { setUnitNumber(event.target.value); setPage(1); }} className={filterClass}><option value="">All trucks</option>{(options.filters?.trucks ?? []).map(value => <option key={value} value={value}>{value}</option>)}</select><select aria-label="Expense driver" value={driverName} onChange={event => { setDriverName(event.target.value); setPage(1); }} className={filterClass}><option value="">All drivers</option>{(options.filters?.drivers ?? []).map(value => <option key={value} value={value}>{value}</option>)}</select></ColumnFilter></th>
                <th className="px-4 py-3 font-medium"><ColumnFilter label="Expense" active={!!category}><label className="block space-y-1">Category<select aria-label="Expense category" value={category} onChange={(event) => { setCategory(event.target.value); setPage(1); }} className={filterClass}>
          <option value="">All categories</option>
          {options.categories.map((value) => <option key={value.id} value={value.id}>{value.name}</option>)}
        </select></label></ColumnFilter></th>
                <th className="px-4 py-3 font-medium"><ColumnFilter label="Payment" active={!!(paymentType || paidBy)}><select aria-label="Expense payment type" value={paymentType} onChange={event => { setPaymentType(event.target.value); setPage(1); }} className={filterClass}><option value="">All payment types</option>{(options.filters?.paymentTypes ?? []).map(value => <option key={value} value={value}>{value}</option>)}</select><select aria-label="Expense paid by" value={paidBy} onChange={event => { setPaidBy(event.target.value); setPage(1); }} className={filterClass}><option value="">All payers</option>{(options.filters?.paidBy ?? []).map(value => <option key={value} value={value}>{value}</option>)}</select></ColumnFilter></th>
                <th className="px-4 py-3 font-medium"><ColumnFilter label="Responsibility" active={!!coveredBy}><select aria-label="Expense responsibility" value={coveredBy} onChange={event => { setCoveredBy(event.target.value); setPage(1); }} className={filterClass}><option value="">All responsibilities</option>{(options.filters?.coveredBy ?? []).map(value => <option key={value} value={value}>{value}</option>)}</select></ColumnFilter></th>
                <th className="px-4 py-3 text-right font-medium">Amount</th>
                <th className="px-4 py-3 text-right font-medium"><ColumnFilter label="Verification" active={!!(accountantVerified || adminVerified)}><label className="block space-y-1">Accountant<select aria-label="Accountant verification" value={accountantVerified} onChange={e => { setAccountantVerified(e.target.value); setPage(1); }} className={filterClass}><option value="">Any accountant status</option><option value="false">Unverified by accountant</option><option value="true">Verified by accountant</option></select></label><label className="block space-y-1">Admin<select aria-label="Admin verification" value={adminVerified} onChange={e => { setAdminVerified(e.target.value); setPage(1); }} className={filterClass}><option value="">Any admin status</option><option value="false">Unverified by admin</option><option value="true">Verified by admin</option></select></label></ColumnFilter></th>
              </tr>
            </thead>
            <tbody>{isLoading ? <tr><td colSpan={7} className="p-8 text-center text-zinc-500" role="status">Loading…</td></tr> : expenses.length === 0 ? <tr><td colSpan={7}>{(
          <EmptyState message={hasFilters ? "No entries match these filters." : createCategoryIds.length ? "No entries yet. Add the first entry." : "No entries are available in your categories."} />
        )}</td></tr> : <>
              {expenses.map((expense) => (
                <tr key={expense.id} tabIndex={0} aria-label={"Open " + (expense.expenseType || expense.category)}
                  onClick={event => { if (!(event.target as HTMLElement).closest("a,button")) openEdit(expense); }}
                  onKeyDown={event => { if (event.target === event.currentTarget && (event.key === "Enter" || event.key === " ")) { event.preventDefault(); openEdit(expense); } }}
                  className="cursor-pointer border-b border-zinc-900/70 text-zinc-300 transition last:border-0 hover:bg-zinc-800/25 focus-visible:outline-2 focus-visible:outline-blue-500">
                  <td className="px-4 py-3">
                    <div className={expense.expenseDate ? "font-mono tabular-nums text-zinc-200" : "text-amber-400"}>{formatDate(expense.expenseDate)}</div>
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
                    <div className="text-zinc-300">{expense.expenseType || "Unnamed expense"}</div>
                    <div className="mt-0.5 text-[11px] text-zinc-500">{expense.category}</div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="text-zinc-300">{expense.paymentType || "—"}</div>
                    <div className="mt-0.5 text-[11px] text-zinc-600">Paid by {expense.paidBy || "—"}</div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="text-zinc-300">{expense.coveredBy || "—"}{expense.ownerName && <span className="block text-xs text-zinc-500">{expense.ownerName}</span>}</div>
                  </td>
                  <td className={`px-4 py-3 text-right font-mono font-medium tabular-nums ${expense.amount === null ? "text-amber-400" : "text-zinc-100"}`}>
                    <span tabIndex={0} title={(expense.paymentFee && expense.paymentFee !== "0.00" ? "Base: " + formatMoney(expense.baseAmount ?? expense.amount) + "\nEFS fee: " + formatMoney(expense.paymentFee) + "\n" : "") + "Paid: " + (expense.paidAmount == null ? "—" : formatMoney(expense.paidAmount)) + "\nRemaining: " + (expense.remainingAmount == null ? "—" : formatMoney(expense.remainingAmount)) + (expense.driverSettled ? "\nExisting expense: assumed fully paid" : "")} className="cursor-help border-b border-dotted border-zinc-600">{formatMoney(expense.amount)}</span>
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center justify-end gap-2">
                      <Verification expense={expense} onClick={() => { setError(""); setVerificationDetails(expense); }} />
                      {verificationRole && !(verificationRole === "admin" ? expense.managerVerified : expense.accountingVerified) && <button type="button" className="ui-button" disabled={verifying !== null} onClick={() => void verify(expense)}>{verifying === expense.id ? "Verifying…" : "Verify"}</button>}
                    </div>
                  </td>
                </tr>
              ))}
            </>}</tbody>
          </table>
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
          submitDisabled={!!editing && !categoryAccess.some(item => item.categoryId === editing.categoryId && item.canEdit)}
          footerStart={editing && categoryAccess.some(item => item.categoryId === editing.categoryId && item.canDelete) ? <button type="button" disabled={isSaving || isDeleting} className="ui-button text-red-400" onClick={() => setPendingDelete(editing)}><Trash2 className="h-4 w-4" />Delete entry</button> : undefined}
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
                  drafts = drafts.map(draft => {
                    const value = { ...draft, paidBy: currentUser };
                    return value.truckId && !value.driverId && !value.driverName ? expenseAssignment(value, "truck", value.truckId, drivers, trucks, owners)
                      : value.driverId && !value.truckId && !value.unitNumber ? expenseAssignment(value, "driver", value.driverId, drivers, trucks, owners)
                      : defaultExpenseResponsibility(value, trucks, owners);
                  });
                  if (drafts.length === 1) {
                    setForm(drafts[0]);
                    setBatchForms([]);
                  } else {
                    setBatchForms(drafts);
                  }
                }}
              />
            </div>
          )}
          {batchForms.length > 0 && !editing ? (
            <ExpenseBatchEditor settings={options.settings} users={options.paidBy} allowedCategoryIds={createCategoryIds} owners={owners} values={batchForms} drivers={drivers} trucks={trucks} onChange={setBatchForms} />
          ) : (
            <fieldset disabled={!!editing && !categoryAccess.some(item => item.categoryId === editing.categoryId && item.canEdit)}><ExpenseForm originalCategory={editing ? { id: editing.categoryId, name: editing.category } : undefined} allowedCategoryIds={editing ? Array.from(new Set([...createCategoryIds, editing.categoryId])) : createCategoryIds} owners={owners} value={form} options={options} drivers={drivers} trucks={trucks} onChange={setForm} /></fieldset>
          )}
        </Modal>
      )}

      {verificationDetails && <Modal title="Verification" isSaving={!!verifying} submitLabel="Done" hideSubmit cancelLabel="Done" onClose={() => setVerificationDetails(null)} onSubmit={event => event.preventDefault()}>
        {error && <ErrorBanner message={error} />}
        <div className="space-y-3">
          {(verificationDetails.verifications ?? []).map(stamp => <div key={stamp.id} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-zinc-800 p-3">
            <div><p className="flex items-center gap-2 text-sm"><Check className={stamp.role === "admin" ? "h-4 w-4 text-emerald-500" : "h-4 w-4 text-blue-500"} />{verificationLabel(stamp)}</p>{stamp.verifiedAt && <p className="mt-1 text-xs text-zinc-500">{new Date(stamp.verifiedAt).toLocaleString()}</p>}</div>
            {canCancelVerification(stamp) && <button type="button" className="ui-button text-red-400" disabled={!!verifying} onClick={() => void cancelVerification(verificationDetails.id, stamp.id)}>Remove {stamp.role === "admin" ? "Administrator" : "Accountant"} verification</button>}
          </div>)}
          {!verificationDetails.verifications?.length && <p className="text-sm text-zinc-400">No active verifications.</p>}
        </div>
      </Modal>}

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
