"use client";
import type { DriverOption, TruckOption, InvestorOption } from "@/app/lib/types";

import { applyExpenseCategoryRules } from "./catalog";


import { ColumnFilter } from "@/app/components/management/ColumnFilter";
import { useViewState } from "@/app/lib/viewMemory";
import { useQuickCreate } from "@/app/lib/topNavigation";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "@/app/components/AccessLink";
import { MaintenanceWorkspace } from "../maintenance/MaintenanceWorkspace";
import { Check, Settings, Trash2, WalletCards, X } from "lucide-react";
import {
  createExpense,
  uploadExpenseDocument,
  deleteExpense,
  fetchExpensesPage,
  fetchMaintenancePage,
  fetchAuthSession,
  fetchDriverOptions,
  fetchInvestorOptions,
  fetchTruckOptions,
  updateExpense,
  verifyExpense,
  unverifyExpense,
} from "../lib/api";
import type { AIExpenseDraft, Expense, ExpenseInput, ExpensePage, ExpenseVerification } from "../lib/types";
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
import { ExpenseAIImport } from "./ExpenseAIImport";
import { ReceiptUpload } from "../components/ReceiptUpload";
import { ExpenseDocuments } from "./ExpenseDocuments";
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
  const [drivers, setDrivers] = useState<DriverOption[]>([]);
  const [owners, setOwners] = useState<InvestorOption[]>([]);
  const [trucks, setTrucks] = useState<TruckOption[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Expense | null | undefined>(undefined);
  const [form, setForm] = useState<ExpenseInput>(emptyExpenseInput);
  const [review,setReview]=useState<{entries:{form:ExpenseInput;source:AIExpenseDraft}[];index:number;file:File|null}|null>(null);
  const [workOrder,setWorkOrder]=useState<{key:string;recordId?:string;expense?:ExpenseInput;draft?:AIExpenseDraft;file?:File|null}|null>(null);
  const [aiBusy,setAIBusy]=useState(false);
  const [attachment, setAttachment] = useState<File | null>(null);
  const [pendingUploadId, setPendingUploadId] = useState<string | null>(null);
  const saveLock=useRef(false);
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
    Promise.all([fetchDriverOptions(), fetchTruckOptions(), fetchInvestorOptions()])
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

  const workCategory=options.workOrdersCategoryId ?? "";
  function showReview(queue: NonNullable<typeof review>,index:number) {
    const entry=queue.entries[index];setReview({...queue,index});setError("");setAttachment(queue.file);setPendingUploadId(null);
    if(workCategory && entry.form.categoryId===workCategory){setEditing(undefined);setWorkOrder({key:crypto.randomUUID(),draft:{...entry.source,...entry.form},file:queue.file});}
    else {setWorkOrder(null);setForm(entry.form);setEditing(null);}
  }
  function completeEntry() {setWorkOrder(null);setEditing(undefined);setAttachment(null);setPendingUploadId(null);if(review && review.index+1<review.entries.length)showReview(review,review.index+1);else setReview(null);}
  function closeEntry(){setWorkOrder(null);setEditing(undefined);setReview(null);setError("");setAttachment(null);setPendingUploadId(null);}
  function changeForm(next:ExpenseInput){
    if(!editing && workCategory && next.categoryId===workCategory){setEditing(undefined);setWorkOrder({key:crypto.randomUUID(),file:attachment,...(review ? {draft:{...review.entries[review.index].source,...next}} : {expense:next})});}
    else setForm(next);
  }
  const hasFilters = Boolean(unitNumber || driverName || paymentType || paidBy || coveredBy || search || category || dateFrom || dateTo || accountantVerified || adminVerified);

  function openCreate() {
    const categoryId=createCategoryIds.includes(category) ? category : createCategoryIds.find(id=>id!==workCategory) ?? createCategoryIds[0] ?? "";
    const value={...emptyExpenseInput,paidBy:currentUser,categoryId,expenseDate:new Intl.DateTimeFormat("en-CA",{timeZone:"America/New_York",year:"numeric",month:"2-digit",day:"2-digit"}).format(new Date())};
    setReview(null);setError("");setForm(value);setAttachment(null);setPendingUploadId(null);
    if(workCategory && categoryId===workCategory){setEditing(undefined);setWorkOrder({key:crypto.randomUUID(),expense:value});}else{setWorkOrder(null);setEditing(null);}
  }
  useQuickCreate(openCreate, !isLoading && createCategoryIds.length > 0);
  function openEdit(expense: Expense) {
    setReview(null);setError("");setAttachment(null);setPendingUploadId(null);
    if(workCategory && expense.categoryId===workCategory){void fetchMaintenancePage({expenseId:expense.id,pageSize:1}).then(result=>{if(!result.items[0])throw new Error("No work order was found for this charge.");setWorkOrder({key:crypto.randomUUID(),recordId:result.items[0].id});}).catch(e=>setError(e.message));return;}
    setForm(expenseToInput(expense));setEditing(expense);
  }
  async function save() {
    if(saveLock.current || aiBusy)return;
    saveLock.current=true;setIsSaving(true);setError("");
    try {
      let id = pendingUploadId;
      if (!id) {
        const input = applyExpenseCategoryRules(form, options.settings);
        const saved = editing ? await updateExpense(editing.id, input) : await createExpense(input);
        id = saved.id;
      }
      if (attachment) {
        try { await uploadExpenseDocument(id, attachment); }
        catch (reason) {
          setPendingUploadId(id);
          setError(`Entry saved. Receipt upload failed: ${reason instanceof Error ? reason.message : "Try again."}`);
          await loadData(false); return;
        }
      }
      completeEntry();setVerificationNotice("Saved.");await loadData(false);
    }
    catch(reason){setError(reason instanceof Error ? reason.message : "Failed to save entry");}
    finally{setIsSaving(false);saveLock.current=false;}
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
      if (event.defaultPrevented || event.shiftKey || !(event.ctrlKey || event.metaKey) || event.key.toLowerCase() !== "z" || editing !== undefined || workOrder || verificationDetails) return;
      if ((event.target as HTMLElement)?.closest("input,textarea,select,[contenteditable='true']")) return;
      const last = verificationUndo.current.at(-1);
      if (!last) return;
      event.preventDefault();
      if (!verificationBusy.current) void cancelVerification(last.expenseId, last.verificationId);
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [cancelVerification, editing, workOrder, verificationDetails]);

  useEffect(() => {
    if (!verificationNotice) return;
    const timeout = window.setTimeout(() => setVerificationNotice(""), 5000);
    return () => window.clearTimeout(timeout);
  }, [verificationNotice]);

  function canCancelVerification(stamp: ExpenseVerification) {
    return verificationRole === "admin" || (stamp.userId === currentUserId && verificationRole === "accountant" && stamp.role === "accountant");
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
                <tr key={expense.id} tabIndex={0} aria-label={"Open " + (expense.workOrderDescription?.trim() || expense.description?.trim() || expense.expenseType || expense.category)}
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
                    <div className="truncate text-zinc-300" title={expense.workOrderDescription?.trim() || expense.description?.trim() || expense.expenseType || undefined}>{expense.workOrderDescription?.trim() || expense.description?.trim() || expense.expenseType || expense.category}</div>
                    <div className="mt-0.5 text-[11px] text-zinc-500">{expense.categoryId===workCategory ? "Work orders" : expense.category}{expense.expenseType && ` · ${expense.expenseType}`}</div>
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
          isSaving={isSaving || aiBusy}
          submitLabel={pendingUploadId ? "Retry receipt upload" : editing ? "Save changes" : review && review.index+1<review.entries.length ? "Save & review next" : "Create entry"}
          onClose={closeEntry}
          onSubmit={(event) => { event.preventDefault(); void save(); }}
        >
          {error && <div className="mb-4"><ErrorBanner message={error} /></div>}
          {!editing && !review && !pendingUploadId && (
            <div className="mb-5">
              <ExpenseAIImport
                onError={setError}
                onBusyChange={setAIBusy}
                onExtracted={(drafts,file,source) => {
                  const forms=drafts.map(draft=>{const value={...draft,paidBy:currentUser};return value.truckId && !value.driverId && !value.driverName ? expenseAssignment(value,"truck",value.truckId,drivers,trucks,owners) : value.driverId && !value.truckId && !value.unitNumber ? expenseAssignment(value,"driver",value.driverId,drivers,trucks,owners) : defaultExpenseResponsibility(value,trucks,owners);});
                  showReview({entries:forms.map((form,i)=>({form,source:source[i]})),index:0,file:file ?? attachment},0);
                }}
              />
            </div>
          )}
          {review && <p className="mb-4 text-xs text-zinc-500">Review entry {review.index+1} of {review.entries.length}. Each item is saved separately.</p>}
          <fieldset disabled={isSaving || aiBusy || !!pendingUploadId || !!editing && !categoryAccess.some(item => item.categoryId === editing.categoryId && item.canEdit)}><ExpenseForm originalCategory={editing ? { id: editing.categoryId, name: editing.category } : undefined} allowedCategoryIds={editing ? Array.from(new Set([...createCategoryIds, editing.categoryId])) : createCategoryIds} owners={owners} value={form} options={options} drivers={drivers} trucks={trucks} onChange={changeForm} /></fieldset>
          <ReceiptUpload file={attachment} disabled={isSaving || aiBusy || !!editing && !categoryAccess.some(item => item.categoryId === editing.categoryId && item.canEdit)} onChange={setAttachment} onError={setError} />
          {editing && <ExpenseDocuments key={editing.id} id={editing.id} />}
        </Modal>
      )}

      {workOrder && <MaintenanceWorkspace key={workOrder.key} modalOnly recordId={workOrder.recordId} initialExpense={workOrder.expense} initialAIDraft={workOrder.draft} initialAttachment={workOrder.file} reviewHasNext={!!review && review.index+1<review.entries.length} reviewLabel={review ? `Review entry ${review.index+1} of ${review.entries.length}` : undefined} onRecordClose={closeEntry} onComplete={completeEntry} onRecordSaved={()=>void loadData(false)} onUseExpense={workOrder.recordId ? undefined : (value,file)=>{setWorkOrder(null);setForm({...value,categoryId:""});setAttachment(file ?? null);setEditing(null);}} />}
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
