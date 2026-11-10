"use client";
import { ExpenseCreationInfo } from "./ExpenseCreationInfo";
import type { ExpenseDriverChoice, ExpenseTruckChoice, ExpenseOwnerChoice } from "@/app/lib/types";

import { applyExpenseCategoryRules } from "./catalog";


import { TableFilters } from "@/app/components/management/TableFilters";
import { useViewState } from "@/app/lib/viewMemory";
import { useQuickCreate } from "@/app/lib/topNavigation";

import { useCallback, useEffect, useId, useRef, useState, type DragEvent, type SelectHTMLAttributes } from "react";
import { createPortal } from "react-dom";
import Link from "@/app/components/AccessLink";

import { FileText, Check, ChevronDown, Ellipsis, FileUp, LoaderCircle, Settings, Trash2, WalletCards, X } from "lucide-react";
import {
  createExpense,
  uploadExpenseDocument,
  deleteExpense,
  fetchExpensesPage,
  fetchExpense,

  fetchAuthSession,
  fetchExpenseFormChoices,
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
import { ExpenseAIImport, expenseAttachmentError } from "./ExpenseAIImport";
import { ReceiptUpload } from "../components/ReceiptUpload";
import { ExpenseBalanceAction } from "@/app/components/expenses/ExpenseBalanceEditor";
import { ExternalSettlements } from "./ExternalSettlements";
import { ExpenseDocuments } from "./ExpenseDocuments";
import { defaultExpenseResponsibility, expenseAssignment } from "./assignments";
import { useExpenseCategoryAccess, usePermissions } from "../lib/access";

const filterClass =
  "rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-1.5 text-[13px] text-zinc-300 outline-none transition-colors focus:border-zinc-600";

function ExpenseFilterSelect({ wide = false, children, ...props }: SelectHTMLAttributes<HTMLSelectElement> & { wide?: boolean }) {
  return <span className={`relative inline-block max-w-full ${wide ? "w-full" : ""}`}>
    <select {...props} className={`${filterClass} appearance-none !pr-9 ${wide ? "w-full" : ""}`}>{children}</select>
    <ChevronDown aria-hidden="true" className="pointer-events-none absolute right-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-zinc-500" />
  </span>;
}

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
  const role = "Accountant";
  return role + ": " + (verification.userName || "Verifier not recorded");
}

function Verification({ expense, onClick }: { expense: Expense; onClick: () => void }) {
  const label = (expense.verifications ?? []).map(verificationLabel).join("; ") || (expense.accountingVerified ? "Accountant: verifier not recorded" : "");
  return label ? <button type="button" onClick={onClick} title={label} aria-label={"View verification: " + label} className="inline-flex h-8 min-w-8 items-center justify-center rounded hover:bg-zinc-800/50 focus-visible:outline-2 focus-visible:outline-blue-500">
    <span aria-label={label} role="img" className="relative inline-block h-5 w-[26px] shrink-0">
      {expense.accountingVerified && <Check strokeWidth={3} className="absolute left-0 top-0 h-5 w-5 text-blue-500" />}
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
  const [truckId, setTruckId] = useState("");

  const [unitNumber, setUnitNumber] = useViewState("page:unitNumber", "");
  const [driverName, setDriverName] = useViewState("page:driverName", "");
  const [paidBy, setPaidBy] = useViewState("page:paidBy", "");
  const [paymentType, setPaymentType] = useViewState("page:paymentType", "");
  const [coveredBy, setCoveredBy] = useViewState("page:coveredBy", "");
  const [dateFrom, setDateFrom] = useViewState("page:dateFrom", "");
  const [accountantVerified, setAccountantVerified] = useViewState("page:accountantVerified", "");
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
  const [choicesLoaded,setChoicesLoaded]=useState(false);
  const [drivers, setDrivers] = useState<ExpenseDriverChoice[]>([]);
  const [owners, setOwners] = useState<ExpenseOwnerChoice[]>([]);
  const [trucks, setTrucks] = useState<ExpenseTruckChoice[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Expense | null | undefined>(undefined);
  const [form, setForm] = useState<ExpenseInput>(emptyExpenseInput);
  const [review,setReview]=useState<{entries:{form:ExpenseInput;source:AIExpenseDraft}[];index:number;file:File|null}|null>(null);
  const [aiBusy,setAIBusy]=useState(false);
  const [attachment, setAttachment] = useState<File | null>(null);
  const [pageDropFile, setPageDropFile] = useState<File | null>(null);
  const [pageDropActive, setPageDropActive] = useState(false);
  const [pageDropReading, setPageDropReading] = useState(false);
  const pageDragDepth = useRef(0);
  const [uploadError, setUploadError] = useState("");
  const [pendingUploadId, setPendingUploadId] = useState<string | null>(null);
  const saveLock=useRef(false);
  const accessorialRequest=useRef<{ key: string; body: string } | null>(null);
  const [currentUser, setCurrentUser] = useState("");
  const [currentUserId, setCurrentUserId] = useState("");
  const [administrator, setAdministrator] = useState(false);
  const [showHistory,setShowHistory]=useState(false);
  const [ownerReview,setOwnerReview]=useState(false);
  const actionsId = useId();
  const actionsPanel = useRef<HTMLDivElement>(null);
  const [verificationDetails, setVerificationDetails] = useState<Expense | null>(null);
  const verificationUndo = useRef<{ expenseId: string; verificationId: string }[]>([]);
  const verificationBusy = useRef(false);
  const [verificationNotice, setVerificationNotice] = useState("");
  const [verificationRole, setVerificationRole] = useState<"accountant" | "">("");
  const [verifying, setVerifying] = useState<string | null>(null);
  const [isSaving, setIsSaving] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<Expense | null>(null);
  const [isDeleting, setIsDeleting] = useState(false);
  const debouncedSearch = useDebouncedValue(search);

  const loadData = useCallback(async (showLoading = true) => {
    if (showLoading) setIsLoading(true);
    try {
      const [response, session] = await Promise.all([fetchExpensesPage({
        history:showHistory || ownerReview?"1":"",ownerReimbursementReview:ownerReview?"1":"",page, pageSize, search: debouncedSearch, categoryId: category, truckId, dateFrom, dateTo, accountantVerified, unitNumber, driverName, paymentType, paidBy, coveredBy,
      }), fetchAuthSession()]);
      setCurrentUser(session.user.username);
      setCurrentUserId(session.user.id);
      setVerificationRole(session.user.expenseVerificationRole ?? "");
      setAdministrator(session.user.administrator);
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
  }, [showHistory,ownerReview,setPage, truckId, category, dateFrom, dateTo, debouncedSearch, page, pageSize, accountantVerified, unitNumber, driverName, paymentType, paidBy, coveredBy]);

  useEffect(() => {
    const timeout = window.setTimeout(() => void loadData(), 0);
    return () => window.clearTimeout(timeout);
  }, [loadData]);

  const expenseOpen = editing !== undefined;
  useEffect(() => {
    if (!expenseOpen || !canLoadFleetSelectors) return;
    let request: AbortController | undefined;
    const refresh = () => {
      request?.abort();
      const controller = new AbortController();
      request = controller;
      void fetchExpenseFormChoices(controller.signal).then(response => {
        if (controller.signal.aborted) return;
        setOptions(current => ({ ...response.options, filters: current.filters }));
        setDrivers(response.drivers); setTrucks(response.trucks); setOwners(response.owners); setChoicesLoaded(true);
      }).catch(reason => {
        if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : "Failed to refresh driver choices");
      });
    };
    const onVisible = () => { if (document.visibilityState === "visible") refresh(); };
    refresh();
    window.addEventListener("focus", refresh);
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      request?.abort();
      window.removeEventListener("focus", refresh);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [expenseOpen, canLoadFleetSelectors]);

  function showReview(queue: NonNullable<typeof review>,index:number) {
    setPageDropFile(null);
    const entry=queue.entries[index];setReview({...queue,index});setError("");setAttachment(queue.file);setPendingUploadId(null);setUploadError("");
    setForm(entry.form);setEditing(null);
  }
  function completeEntry() {setEditing(undefined);setAttachment(null);setPendingUploadId(null);setUploadError("");if(review && review.index+1<review.entries.length)showReview(review,review.index+1);else setReview(null);}
  function closeEntry(){setEditing(undefined);setReview(null);setPageDropFile(null);setError("");setAttachment(null);setPendingUploadId(null);setUploadError("");}
  function changeForm(next:ExpenseInput){ setForm(next); }
  const hasFilters = Boolean(ownerReview || truckId || unitNumber || driverName || paymentType || paidBy || coveredBy || search || category || dateFrom || dateTo || accountantVerified);
  const hiddenFilterCount = [showHistory, ownerReview, paymentType, paidBy, coveredBy, accountantVerified].filter(Boolean).length;

  function openCreate() {
    accessorialRequest.current=null;
    const categoryId=createCategoryIds.includes(category) ? category : createCategoryIds[0] ?? "";
    const value={...emptyExpenseInput,paidBy:currentUser,categoryId,expenseDate:new Intl.DateTimeFormat("en-CA",{timeZone:"America/New_York",year:"numeric",month:"2-digit",day:"2-digit"}).format(new Date())};
    setReview(null);setPageDropFile(null);setError("");setForm(value);setAttachment(null);setPendingUploadId(null);setUploadError("");
    setEditing(null);
  }
  function openCreateFromDrop(file: File) {
    openCreate();
    setPageDropFile(file);
  }
  useEffect(() => {
    const query = new URLSearchParams(window.location.search);
    const id = query.get("expenseId");
    let active = true;
    void Promise.resolve().then(async () => {
      if (!active) return;
      if (query.get("categoryId")) setCategory(query.get("categoryId")!);
      if (query.get("truckId")) setTruckId(query.get("truckId")!);
      if (query.get("search")) setSearch(query.get("search")!);
      if (id) {
        const expense = await fetchExpense(id);
        if (active) { setForm(expenseToInput(expense)); setEditing(expense); }
      }
    }).catch(reason => { if (active) setError(reason.message); });
    return () => { active = false; };
  }, [setCategory, setSearch]);
  useQuickCreate(openCreate, !isLoading && createCategoryIds.length > 0);
  function openEdit(expense: Expense) {
    setReview(null);setPageDropFile(null);setError("");setAttachment(null);setPendingUploadId(null);setUploadError("");
    setForm(expenseToInput(expense));setEditing(expense);
  }
  async function save() {
    if(saveLock.current || aiBusy)return;
    saveLock.current=true;setIsSaving(true);setError("");
    try {
      let id = pendingUploadId;
      if (!id) {
        const input = applyExpenseCategoryRules(form, options.settings);
        let requestId: string | undefined;
        if (!editing && input.paymentType === "Driver payroll") {
          const body=JSON.stringify(input);
          if (accessorialRequest.current?.body !== body) accessorialRequest.current={body,key:crypto.randomUUID()};
          requestId=accessorialRequest.current.key;
        }
        const saved = editing ? await updateExpense(editing.id, input) : await createExpense(input,requestId);
        id = saved.id;
      }
      if (attachment) {
        try { await uploadExpenseDocument(id, attachment); }
        catch (reason) {
          setPendingUploadId(id);
          setUploadError(`Entry saved. Receipt upload failed: ${reason instanceof Error ? reason.message : "Try again."}`);
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
      if (accountantVerified) await loadData(false);
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
      if (accountantVerified) await loadData(false);
      setVerificationDetails(current => current?.id === updated.id ? updated : current);
      verificationUndo.current = verificationUndo.current.filter(item => item.verificationId !== verificationId);
      setVerificationNotice("Verification removed.");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not remove verification");
    } finally { setVerifying(null); verificationBusy.current = false; }
  }, [accountantVerified, loadData]);

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
    return administrator || (stamp.userId === currentUserId && verificationRole === "accountant" && stamp.role === "accountant");
  }

  function clearFilters() {
    setSearch("");
    setCategory("");
    setTruckId("");
    setUnitNumber(""); setDriverName(""); setPaymentType(""); setPaidBy(""); setCoveredBy("");

    setDateFrom("");
    setDateTo("");
    setAccountantVerified("");
    setPage(1);
  }

  const pageDropEnabled = !isLoading && createCategoryIds.length > 0 && editing === undefined && !verificationDetails && !pendingDelete && !aiBusy && !pageDropReading;

  function isFileDrag(event: DragEvent<HTMLElement>) {
    return event.dataTransfer.types.includes("Files");
  }

  function handlePageDragEnter(event: DragEvent<HTMLDivElement>) {
    if (!pageDropEnabled || !isFileDrag(event)) return;
    event.preventDefault();
    pageDragDepth.current += 1;
    setPageDropActive(true);
  }

  function handlePageDragOver(event: DragEvent<HTMLDivElement>) {
    if (!pageDropEnabled || !isFileDrag(event)) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = "copy";
  }

  function handlePageDragLeave(event: DragEvent<HTMLDivElement>) {
    if (!isFileDrag(event)) return;
    pageDragDepth.current = Math.max(0, pageDragDepth.current - 1);
    if (pageDragDepth.current === 0) setPageDropActive(false);
  }

  async function handlePageDrop(event: DragEvent<HTMLDivElement>) {
    if (!pageDropEnabled || !isFileDrag(event)) return;
    event.preventDefault();
    pageDragDepth.current = 0;
    setPageDropActive(false);
    const droppedFiles = Array.from(event.dataTransfer.files);
    if (droppedFiles.length === 0) return;
    if (droppedFiles.length > 1) {
      setError("Attach one file at a time. A single file can still contain several transactions.");
      return;
    }
    const droppedFile = droppedFiles[0];
    const validationError = expenseAttachmentError(droppedFile);
    if (validationError) {
      setError(validationError);
      return;
    }
    setPageDropReading(true);
    setError("");
    try {
      const data = await droppedFile.arrayBuffer();
      const stableFile = new File([data], droppedFile.name, {
        type: droppedFile.type,
        lastModified: droppedFile.lastModified,
      });
      const stableValidationError = expenseAttachmentError(stableFile);
      if (stableValidationError) {
        setError(stableValidationError);
        return;
      }
      openCreateFromDrop(stableFile);
    } catch {
      setError("The dropped file could not be read. Try dropping it again or use Browse in Add entry.");
    } finally {
      setPageDropReading(false);
    }
  }

  return (
    <div data-testid="expenses-drop-target" className="min-h-[calc(100dvh-7rem)] space-y-5 animate-fade-in" onDragEnter={handlePageDragEnter} onDragOver={handlePageDragOver} onDragLeave={handlePageDragLeave} onDrop={handlePageDrop}>

      {(pageDropActive || pageDropReading) && createPortal(<div className="pointer-events-none fixed inset-3 z-[70] flex items-center justify-center rounded-2xl border-2 border-dashed border-blue-400 bg-zinc-950/90 backdrop-blur-sm" role="status" aria-live="polite">
        <div className="flex flex-col items-center gap-3 text-center">
          <span className="flex h-12 w-12 items-center justify-center rounded-full bg-blue-500/15 text-blue-400">{pageDropReading ? <LoaderCircle className="h-6 w-6 animate-spin" aria-hidden="true" /> : <FileUp className="h-6 w-6" aria-hidden="true" />}</span>
          <div><p className="text-base font-semibold text-zinc-100">{pageDropReading ? "Reading expense file…" : "Drop to add expense"}</p><p className="mt-1 text-xs text-zinc-400">We’ll analyze the file and open a draft for review.</p></div>
        </div>
      </div>, document.body)}


      {error && <ErrorBanner message={error} />}

      <div className="grid gap-3 sm:grid-cols-3">
        <div className="rounded-xl border border-zinc-800/60 bg-zinc-900/30 p-4">
          <p className="text-[11px] font-medium text-zinc-600">Filtered total</p>
          <p className="mt-1 font-mono text-xl font-semibold tabular-nums text-zinc-100">{formatMoney(summaryAmount)}</p>
        </div>
        <div className="rounded-xl border border-zinc-800/60 bg-zinc-900/30 p-4">
          <p className="text-[11px] font-medium text-zinc-600">Records</p>
          <p className="mt-1 font-mono text-xl font-semibold tabular-nums text-zinc-100">{total.toLocaleString()}</p>
        </div>
        <div className="rounded-xl border border-zinc-800/60 bg-zinc-900/30 p-4">
          <p className="text-[11px] font-medium text-zinc-600">Needs date or amount</p>
          <p className={`mt-1 font-mono text-xl font-semibold tabular-nums ${incompleteCount ? "text-amber-400" : "text-zinc-100"}`}>{incompleteCount.toLocaleString()}</p>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2 xl:flex-nowrap">
        <ManagementSearch value={search} onChange={(value) => { setSearch(value); setPage(1); }} placeholder="Search expenses by text or amount" />
        <TableFilters>
          <label className="block space-y-1"><input aria-label="Expense date from" type="date" value={dateFrom} onChange={(event) => { setDateFrom(event.target.value); setPage(1); }} className={filterClass} /></label><span aria-hidden="true">→</span><label><input aria-label="Expense date to" type="date" value={dateTo} onChange={(event) => { setDateTo(event.target.value); setPage(1); }} className={filterClass} /></label>
          <ExpenseFilterSelect aria-label="Expense truck" value={unitNumber} onChange={event => { setUnitNumber(event.target.value); setPage(1); }}><option value="">All trucks</option>{(options.filters?.trucks ?? []).map(value => <option key={value} value={value}>{value}</option>)}</ExpenseFilterSelect><ExpenseFilterSelect aria-label="Expense driver" value={driverName} onChange={event => { setDriverName(event.target.value); setPage(1); }}><option value="">All drivers</option>{(options.filters?.drivers ?? []).map(value => <option key={value} value={value}>{value}</option>)}</ExpenseFilterSelect>
          <ExpenseFilterSelect aria-label="Expense category" value={category} onChange={(event) => { setCategory(event.target.value); setPage(1); }}>
          <option value="">All categories</option>
          {options.categories.map((value) => <option key={value.id} value={value.id}>{value.name}</option>)}
        </ExpenseFilterSelect>
        </TableFilters>



        {hasFilters && (
          <button type="button" onClick={() => { clearFilters(); setOwnerReview(false); }} className="inline-flex items-center gap-1 rounded-lg px-2 py-1.5 text-[13px] text-zinc-500 transition hover:bg-zinc-800/50 hover:text-zinc-300">
            <X className="h-3 w-3" /> Clear
          </button>
        )}
      <div className="relative ml-auto flex max-w-full shrink-0 flex-nowrap items-center gap-2 [&>div]:flex-nowrap [&>div>div]:flex-nowrap"><span role="status" title={verificationNotice} className="pointer-events-none absolute right-full mr-2 h-8 w-52 truncate text-right text-xs leading-8 text-zinc-400">{isSaving ? "Saving…" : verifying ? "Saving verification…" : verificationNotice}</span><ManagementHeader
        icon={WalletCards}
        title="Expenses"

        count={total}
        actionLabel={createCategoryIds.length ? "Add entry" : undefined}
        onAction={createCategoryIds.length ? openCreate : undefined}
        secondaryAction={<>
          <button type="button" className="ui-button relative !px-2" aria-label={hiddenFilterCount ? `More filters and actions, ${hiddenFilterCount} active` : "More filters and actions"} title="More filters and actions" popoverTarget={actionsId} onClick={event => {
            const panel = actionsPanel.current;
            if (!panel) return;
            const rect = event.currentTarget.getBoundingClientRect();
            panel.style.left = Math.max(8, rect.right - 288) + "px";
            panel.style.top = Math.max(8, rect.bottom + 4) + "px";
            requestAnimationFrame(() => {
              const height = panel.getBoundingClientRect().height;
              if (height) panel.style.top = Math.max(8, Math.min(rect.bottom + 4, window.innerHeight - height - 8)) + "px";
            });
          }}>
            <Ellipsis className="h-4 w-4" aria-hidden="true" />
            {hiddenFilterCount > 0 && <span className="absolute right-1 top-1 h-1.5 w-1.5 rounded-full bg-blue-500" aria-hidden="true" />}
          </button>
          <div ref={actionsPanel} id={actionsId} popover="auto" role="group" aria-label="Expense filters and actions" className="fixed m-0 max-h-[calc(100dvh-16px)] w-72 max-w-[calc(100vw-16px)] overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-900 text-xs text-zinc-200 ui-popover">
            <div className="space-y-3 p-3">
              <p className="font-medium text-zinc-300">More filters</p>
              <label className="flex items-center gap-2 text-zinc-300"><input type="checkbox" checked={showHistory} onChange={event => { setShowHistory(event.target.checked); setPage(1); }} />Show historical</label>
              <label className="flex items-center gap-2 text-zinc-300"><input type="checkbox" checked={ownerReview} onChange={event => { setOwnerReview(event.target.checked); setPage(1); }} />Owner reimbursement review</label>
              <label className="block space-y-1.5"><span className="text-zinc-500">Payment type</span><ExpenseFilterSelect wide aria-label="Expense payment type" value={paymentType} onChange={event => { setPaymentType(event.target.value); setPage(1); }}><option value="">All payment types</option>{(options.filters?.paymentTypes ?? []).map(value => <option key={value} value={value}>{value}</option>)}</ExpenseFilterSelect></label>
              <label className="block space-y-1.5"><span className="text-zinc-500">Paid by</span><ExpenseFilterSelect wide aria-label="Expense paid by" value={paidBy} onChange={event => { setPaidBy(event.target.value); setPage(1); }}><option value="">All payers</option>{(options.filters?.paidBy ?? []).map(value => <option key={value} value={value}>{value}</option>)}</ExpenseFilterSelect></label>
              <label className="block space-y-1.5"><span className="text-zinc-500">Responsibility</span><ExpenseFilterSelect wide aria-label="Expense responsibility" value={coveredBy} onChange={event => { setCoveredBy(event.target.value); setPage(1); }}><option value="">All responsibilities</option>{(options.filters?.coveredBy ?? []).map(value => <option key={value} value={value}>{value}</option>)}</ExpenseFilterSelect></label>
              <label className="block space-y-1.5"><span className="text-zinc-500">Accountant status</span><ExpenseFilterSelect wide aria-label="Accountant verification" value={accountantVerified} onChange={event => { setAccountantVerified(event.target.value); setPage(1); }}><option value="">Any accountant status</option><option value="false">Unverified by accountant</option><option value="true">Verified by accountant</option></ExpenseFilterSelect></label>
            </div>
            <div className="flex items-center justify-between gap-2 border-t border-zinc-800 p-1">
              {permissions.includes("expense_settings.manage") ? <Link href="/expenses/settings" className="inline-flex items-center gap-2 rounded px-2 py-2 text-zinc-300 hover:bg-zinc-800"><Settings className="h-3.5 w-3.5" />Expenses settings</Link> : <span />}
              <button type="button" className="rounded px-3 py-2 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200" popoverTarget={actionsId} popoverTargetAction="hide">Done</button>
            </div>
          </div>
        </>}
      /></div></div>


      <TableShell>
        <table className="w-full min-w-[960px] text-left text-[13px]">
            <thead>
              <tr className="border-b border-zinc-800/50 text-zinc-500">
                <th className="px-4 py-3 font-medium">Date</th>
                <th className="px-4 py-3 font-medium">Unit / driver</th>
                <th className="px-4 py-3 font-medium">Expense</th>
                <th className="px-4 py-3 font-medium">Payment</th>
                <th className="px-4 py-3 font-medium">Responsibility</th>
                <th className="px-4 py-3 text-right font-medium">Amount</th>
                <th className="px-4 py-3 text-center font-medium">Invoice / receipt</th>
                <th className="px-4 py-3 text-right font-medium">Verification</th>
              </tr>
            </thead>
            <tbody>{isLoading ? <tr><td colSpan={8} className="p-8 text-center text-zinc-500" role="status">Loading…</td></tr> : expenses.length === 0 ? <tr><td colSpan={8}>{(
          <EmptyState message={hasFilters ? "No entries match these filters." : createCategoryIds.length ? "No entries yet. Add the first entry." : "No entries are available in your categories."} />
        )}</td></tr> : <>
              {expenses.map((expense) => (
                <tr key={expense.id} tabIndex={0} aria-label={"Open " + (expense.workOrderDescription?.trim() || expense.description?.trim() || expense.expenseType || expense.category)}
                  onClick={event => { if (!ownerReview && !(event.target as HTMLElement).closest("a,button")) openEdit(expense); }}
                  onKeyDown={event => { if (!ownerReview && event.target === event.currentTarget && (event.key === "Enter" || event.key === " ")) { event.preventDefault(); openEdit(expense); } }}
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
                    <div className="mt-0.5 text-[11px] text-zinc-500">{expense.category}{expense.expenseType && ` · ${expense.expenseType}`}</div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="text-zinc-300">{expense.paymentType || "—"}</div>
                    <div className="mt-0.5 text-[11px] text-zinc-600">{expense.managedInSheets?"Managed in Sheets":`Paid by ${expense.paidBy || "—"}`}</div>
                  </td>
                  <td className="px-4 py-3">
                    <div className="text-zinc-300">{expense.coveredBy || "—"}{expense.coveredBy === "Broker" && <span className="block text-xs text-zinc-500">{expense.brokerLoadNumber ? `Load ${expense.brokerLoadNumber}` : "Load number needed"}</span>}{expense.ownerName && <span className="block text-xs text-zinc-500">{expense.ownerName}</span>}</div>
                  </td>
                  <td className={`px-4 py-3 text-right font-mono font-medium tabular-nums ${expense.amount === null ? "text-amber-400" : "text-zinc-100"}`}>
                    <span tabIndex={0} title={(expense.paymentFee && expense.paymentFee !== "0.00" ? "Base: " + formatMoney(expense.baseAmount ?? expense.amount) + "\nEFS fee: " + formatMoney(expense.paymentFee) + "\n" : "") + "Paid: " + (expense.paidAmount == null ? "—" : formatMoney(expense.paidAmount)) + "\nRemaining: " + (expense.remainingAmount == null ? "—" : formatMoney(expense.remainingAmount)) + (expense.driverSettled ? "\nExisting expense: assumed fully paid" : "")} className="cursor-help border-b border-dotted border-zinc-600">{formatMoney(expense.amount)}</span>
                    {expense.ownerCollectionRemaining != null && <div className="mt-1 text-xs text-zinc-400">Owner remaining: {formatMoney(expense.ownerCollectionRemaining)}</div>}
                    {expense.remainingAmount != null && <div className="mt-1 text-xs text-zinc-400">{expense.driverPayoutRemaining != null ? "Driver remaining" : "Remaining"}: {formatMoney(expense.remainingAmount)}{!ownerReview && <ExpenseBalanceAction expense={expense} onSaved={() => loadData(false)} />}</div>}
                  </td>
                  <td className="px-4 py-3 text-center"><button type="button" onClick={() => openEdit(expense)} aria-label={expense.hasDocuments ? "View invoice or receipt" : "No invoice or receipt attached"} title={expense.hasDocuments ? "Invoice / receipt attached" : "No invoice / receipt attached"} className={`inline-flex rounded p-2 ${expense.hasDocuments ? "text-blue-400 hover:bg-blue-500/10" : "text-zinc-500 hover:bg-zinc-800"}`}><FileText className="h-5 w-5" /><span className="sr-only">{expense.hasDocuments ? "Attached" : "Not attached"}</span></button></td>
                  <td className="px-4 py-3">
                    <div className="flex items-center justify-end gap-2">
                      {!ownerReview && <Verification expense={expense} onClick={() => { setError(""); setVerificationDetails(expense); }} />}
                      {!ownerReview && verificationRole && !(expense.accountingVerified) && <button type="button" className="ui-button" disabled={verifying !== null} onClick={() => void verify(expense)}>{verifying === expense.id ? "Verifying…" : "Verify"}</button>}
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
          sidePanel={editing ? <ExpenseDocuments key={editing.id} id={editing.id} /> : undefined}
          submitDisabled={!choicesLoaded || !!editing && (editing.managedInSheets || !(editing.canEdit ?? categoryAccess.some(item => item.categoryId === editing.categoryId && item.canEdit)))}
          footerStart={editing && !editing.managedInSheets && categoryAccess.some(item => item.categoryId === editing.categoryId && item.canDelete) ? <button type="button" disabled={isSaving || isDeleting} className="ui-button text-red-400" onClick={() => setPendingDelete(editing)}><Trash2 className="h-4 w-4" />Delete entry</button> : undefined}
          isSaving={isSaving || aiBusy}
          submitLabel={pendingUploadId ? "Retry receipt upload" : editing ? "Save changes" : review && review.index+1<review.entries.length ? "Save & review next" : "Create entry"}
          onClose={closeEntry}
          onSubmit={(event) => { event.preventDefault(); void save(); }}
        >
          <ExpenseCreationInfo expense={editing} pendingSaved={!!pendingUploadId} />
          {(uploadError || error) && <div className="mb-4"><ErrorBanner message={uploadError || error} /></div>}
          {!editing && !review && !pendingUploadId && (
            <div className="mb-5">
              <ExpenseAIImport
                initialFile={pageDropFile}
                analyzeInitialFile={!!pageDropFile}
                autoAnalyzeReady={choicesLoaded}
                analysisSource={pageDropFile ? "page-drop" : "modal"}
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
          <fieldset disabled={!choicesLoaded || isSaving || aiBusy || !!pendingUploadId || !!editing && (editing.managedInSheets || !(editing.canEdit ?? categoryAccess.some(item => item.categoryId === editing.categoryId && item.canEdit)))}><ExpenseForm originalCategory={editing ? { id: editing.categoryId, name: editing.category } : undefined} allowedCategoryIds={editing ? Array.from(new Set([...createCategoryIds, editing.categoryId])) : createCategoryIds} owners={owners} value={form} options={options} drivers={drivers} trucks={trucks} onChange={changeForm} /></fieldset>
          <ReceiptUpload file={attachment} disabled={isSaving || aiBusy || !!editing && !(editing.canEdit ?? categoryAccess.some(item => item.categoryId === editing.categoryId && item.canEdit))} onChange={setAttachment} onError={setError} />
          {editing && <ExternalSettlements expense={editing} onSaved={async()=>{const updated=await fetchExpense(editing.id);setEditing(updated);setForm(expenseToInput(updated));await loadData(false)}}/>}
        </Modal>
      )}

      {verificationDetails && <Modal title="Verification" isSaving={!!verifying} submitLabel="Done" hideSubmit cancelLabel="Done" onClose={() => setVerificationDetails(null)} onSubmit={event => event.preventDefault()}>
        {error && <ErrorBanner message={error} />}
        <div className="space-y-3">
          {(verificationDetails.verifications ?? []).map(stamp => <div key={stamp.id} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-zinc-800 p-3">
            <div><p className="flex items-center gap-2 text-sm"><Check className={"h-4 w-4 text-blue-500"} />{verificationLabel(stamp)}</p>{stamp.verifiedAt && <p className="mt-1 text-xs text-zinc-500">{new Date(stamp.verifiedAt).toLocaleString()}</p>}</div>
            {canCancelVerification(stamp) && <button type="button" className="ui-button text-red-400" disabled={!!verifying} onClick={() => void cancelVerification(verificationDetails.id, stamp.id)}>Remove Accountant verification</button>}
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
