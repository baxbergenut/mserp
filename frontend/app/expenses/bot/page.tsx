"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { Bot, CheckCircle2, CircleAlert, Clock3, RefreshCw, SearchX } from "lucide-react";
import {
  fetchDrivers,
  fetchExpensesPage,
  fetchTelegramExpenseActivity,
  fetchTrucks,
  resolveTelegramExpenseUpdate,
  retryTelegramExpenseUpdate,
} from "@/app/lib/api";
import type {
  Driver,
  ExpenseCategory,
  ExpenseInput,
  TelegramExpenseActivity,
  TelegramExpenseStatus,
  Truck,
} from "@/app/lib/types";
import { useDebouncedValue } from "@/app/lib/useDebouncedValue";
import {
  EmptyState,
  ErrorBanner,
  LoadingTable,
  ManagementHeader,
  ManagementSearch,
  Modal,
  TablePagination,
  TableShell,
} from "@/app/components/management/ManagementUI";
import {
  emptyExpenseInput,
  EXPENSE_CATEGORIES,
  ExpenseForm,
} from "../ExpenseForm";

const filterClass =
  "rounded-lg border border-zinc-800 bg-zinc-950 px-2.5 py-1.5 text-[13px] text-zinc-300 outline-none transition-colors focus:border-zinc-600";

const statusLabels: Record<TelegramExpenseStatus, string> = {
  queued: "Queued",
  processing: "Processing",
  retry: "Retrying",
  completed: "Completed",
  ignored: "Not an expense",
  needs_review: "Needs review",
  failed: "Failed",
};

const statusClasses: Record<TelegramExpenseStatus, string> = {
  queued: "border-blue-500/20 bg-blue-500/10 text-blue-300",
  processing: "border-blue-500/20 bg-blue-500/10 text-blue-300",
  retry: "border-amber-500/20 bg-amber-500/10 text-amber-300",
  completed: "border-emerald-500/20 bg-emerald-500/10 text-emerald-300",
  ignored: "border-zinc-700 bg-zinc-800/50 text-zinc-400",
  needs_review: "border-orange-500/20 bg-orange-500/10 text-orange-300",
  failed: "border-red-500/20 bg-red-500/10 text-red-300",
};

const emptySummary = {
  received24Hours: 0,
  completed: 0,
  inProgress: 0,
  needsReview: 0,
  ignored: 0,
  failed: 0,
  unmatched: 0,
  missingExpense: 0,
  lastCompletedAt: null as string | null,
};

const emptyOptions = {
  companies: [] as string[],
  paymentTypes: [] as string[],
  expenseTypes: [] as string[],
  paidBy: [] as string[],
  coveredBy: [] as string[],
};

function formatDateTime(value: string | null) {
  if (!value) return "Never";
  return new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  }).format(new Date(value));
}

function formatMoney(value: string | null) {
  if (!value) return "—";
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
  }).format(Number(value));
}

function StatusBadge({ status }: { status: TelegramExpenseStatus }) {
  return (
    <span className={`inline-flex rounded-full border px-2 py-1 text-[11px] font-medium ${statusClasses[status]}`}>
      {statusLabels[status]}
    </span>
  );
}

function Metric({
  label,
  value,
  tone = "text-zinc-100",
}: {
  label: string;
  value: string | number;
  tone?: string;
}) {
  return (
    <div className="rounded-xl border border-zinc-800/60 bg-zinc-900/30 p-4">
      <p className="text-[11px] font-medium uppercase tracking-wider text-zinc-600">{label}</p>
      <p className={`mt-1 font-mono text-xl font-semibold tabular-nums ${tone}`}>{value}</p>
    </div>
  );
}

function sourceSummary(item: TelegramExpenseActivity) {
  return item.fileName || item.messageText || "Message without visible text";
}

function normalized(value: string | null | undefined) {
  return (value || "").trim().toLocaleLowerCase();
}

function primaryExtraction(item: TelegramExpenseActivity) {
  const extraction = item.extractedData;
  return extraction?.expenses?.[0] || extraction;
}

export default function TelegramExpenseActivityPage() {
  const [items, setItems] = useState<TelegramExpenseActivity[]>([]);
  const [summary, setSummary] = useState(emptySummary);
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);
  const [total, setTotal] = useState(0);
  const [totalPages, setTotalPages] = useState(1);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState("");
  const [retrying, setRetrying] = useState<number | null>(null);
  const [reviewing, setReviewing] = useState<TelegramExpenseActivity | null>(null);
  const [reviewForms, setReviewForms] = useState<ExpenseInput[]>([]);
  const [reviewIndex, setReviewIndex] = useState(0);
  const [drivers, setDrivers] = useState<Driver[]>([]);
  const [trucks, setTrucks] = useState<Truck[]>([]);
  const [options, setOptions] = useState(emptyOptions);
  const [isSaving, setIsSaving] = useState(false);
  const debouncedSearch = useDebouncedValue(search);

  const loadData = useCallback(async (quiet = false) => {
    if (!quiet) setIsLoading(true);
    try {
      const response = await fetchTelegramExpenseActivity({
        page,
        pageSize,
        search: debouncedSearch,
        status,
      });
      setItems(response.items);
      setSummary(response.summary);
      setPage(response.page);
      setTotal(response.total);
      setTotalPages(response.totalPages);
      setError("");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to load bot activity");
    } finally {
      if (!quiet) setIsLoading(false);
    }
  }, [debouncedSearch, page, pageSize, status]);

  useEffect(() => {
    const timeout = window.setTimeout(() => void loadData(), 0);
    return () => window.clearTimeout(timeout);
  }, [loadData]);

  useEffect(() => {
    let cancelled = false;
    Promise.all([
      fetchDrivers(),
      fetchTrucks(),
      fetchExpensesPage({ page: 1, pageSize: 1 }),
    ]).then(([driverValues, truckValues, expensePage]) => {
      if (cancelled) return;
      setDrivers(driverValues);
      setTrucks(truckValues);
      setOptions({
        companies: expensePage.options.companies,
        paymentTypes: expensePage.options.paymentTypes,
        expenseTypes: expensePage.options.expenseTypes,
        paidBy: expensePage.options.paidBy,
        coveredBy: expensePage.options.coveredBy,
      });
    }).catch((reason) => {
      if (!cancelled) {
        setError(reason instanceof Error ? reason.message : "Failed to load expense form options");
      }
    });
    return () => { cancelled = true; };
  }, []);

  useEffect(() => {
    const interval = window.setInterval(() => void loadData(true), 15_000);
    return () => window.clearInterval(interval);
  }, [loadData]);

  async function retry(item: TelegramExpenseActivity) {
    setRetrying(item.updateId);
    setError("");
    try {
      await retryTelegramExpenseUpdate(item.updateId);
      await loadData(true);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to retry Telegram message");
    } finally {
      setRetrying(null);
    }
  }

  function openReview(item: TelegramExpenseActivity) {
    const candidates = item.extractedData?.expenses?.length
      ? item.extractedData.expenses
      : [primaryExtraction(item)];
    setReviewForms(candidates.map((extraction, index) => {
      const unitNumber = extraction?.unitNumber || (index === 0 ? item.unitNumber : "") || "";
      const driverName = extraction?.driverName || (index === 0 ? item.driverName : "") || "";
      const truck = trucks.find((value) => normalized(value.unitNumber) === normalized(unitNumber));
      const driver = drivers.find((value) => normalized(value.fullName) === normalized(driverName));
      const category = EXPENSE_CATEGORIES.includes(extraction?.category as ExpenseCategory)
        ? extraction?.category as ExpenseCategory
        : "Other";
      return {
        ...emptyExpenseInput,
        company: extraction?.company || (index === 0 ? item.company : null) || "MS Express",
        category,
        expenseDate: extraction?.expenseDate || (index === 0 ? item.expenseDate : null) || item.createdAt.slice(0, 10),
        truckId: truck?.id || null,
        driverId: driver?.id || null,
        unitNumber,
        driverName,
        amount: extraction?.amount || (index === 0 ? item.amount : null) || "",
        paymentType: extraction?.paymentType || "",
        expenseType: extraction?.expenseType || (index === 0 ? item.expenseType : null) || "",
        referenceNumber: extraction?.referenceNumber || "",
        description: extraction?.description || (index === 0 ? item.description : null) || item.messageText || "",
        coveredBy: extraction?.coveredBy || "Company",
        paidBy: extraction?.paidBy || "",
      };
    }));
    setReviewIndex(0);
    setReviewing(item);
  }

  async function saveReview() {
    if (!reviewing) return;
    setIsSaving(true);
    setError("");
    try {
      await resolveTelegramExpenseUpdate(reviewing.updateId, reviewForms);
      setReviewing(null);
      await loadData(true);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to create reviewed expense");
    } finally {
      setIsSaving(false);
    }
  }

  const attention = summary.needsReview + summary.failed + summary.unmatched + summary.missingExpense;

  return (
    <div className="space-y-5 animate-fade-in">
      <ManagementHeader
        icon={Bot}
        title="Bot Activity"
        description="Trace every accepted Telegram message from delivery through expense creation. Refreshes every 15 seconds."
        count={total}
        actionLabel="Refresh"
        actionIcon={RefreshCw}
        onAction={() => void loadData()}
      />

      {error && <ErrorBanner message={error} />}

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <Metric label="Received · 24h" value={summary.received24Hours} />
        <Metric label="Completed" value={summary.completed} tone="text-emerald-400" />
        <Metric label="In progress" value={summary.inProgress} tone={summary.inProgress ? "text-blue-400" : "text-zinc-100"} />
        <Metric label="Needs attention" value={attention} tone={attention ? "text-amber-400" : "text-zinc-100"} />
        <Metric label="Last completed" value={formatDateTime(summary.lastCompletedAt)} />
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <ManagementSearch
          value={search}
          onChange={(value) => { setSearch(value); setPage(1); }}
          placeholder="Search sender, file, truck, driver, error…"
        />
        <select
          value={status}
          onChange={(event) => { setStatus(event.target.value); setPage(1); }}
          className={filterClass}
          aria-label="Bot activity status"
        >
          <option value="">All statuses</option>
          {(Object.keys(statusLabels) as TelegramExpenseStatus[]).map((value) => (
            <option key={value} value={value}>{statusLabels[value]}</option>
          ))}
        </select>
        <Link
          href="/expenses"
          className="rounded-lg px-2.5 py-1.5 text-[13px] text-zinc-500 transition hover:bg-zinc-800/50 hover:text-zinc-200"
        >
          View expense ledger
        </Link>
      </div>

      <TableShell>
        {isLoading ? (
          <LoadingTable columns={7} />
        ) : items.length === 0 ? (
          <EmptyState message="No Telegram activity matches these filters." />
        ) : (
          <table className="w-full min-w-[1440px] text-left text-[13px]">
            <thead>
              <tr className="border-b border-zinc-800/50 text-zinc-500">
                <th className="px-4 py-3 font-medium">Received</th>
                <th className="px-4 py-3 font-medium">Source</th>
                <th className="px-4 py-3 font-medium">Status</th>
                <th className="px-4 py-3 font-medium">Expense</th>
                <th className="px-4 py-3 font-medium">Truck / driver</th>
                <th className="px-4 py-3 font-medium">Result</th>
                <th className="px-4 py-3 text-right font-medium">Action</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => {
                const extraction = primaryExtraction(item);
                const expenseCount = item.expenseCount || (item.expenseId ? 1 : 0);
                const isMissingExpense = item.status === "completed" && expenseCount === 0;
                const canRetry = isMissingExpense || ["retry", "ignored", "failed"].includes(item.status);
                const unitNumber = item.unitNumber || extraction?.unitNumber;
                const driverName = item.driverName || extraction?.driverName;
                const amount = item.amount || extraction?.amount;
                return (
                  <tr key={item.updateId} className="border-b border-zinc-900/70 align-top text-zinc-300 last:border-0 hover:bg-zinc-800/15">
                    <td className="whitespace-nowrap px-4 py-3">
                      <div className="font-mono text-zinc-300">{formatDateTime(item.createdAt)}</div>
                      <div className="mt-1 text-[11px] text-zinc-600">Message {item.messageId}</div>
                    </td>
                    <td className="max-w-[300px] px-4 py-3">
                      <div className="truncate font-medium text-zinc-200" title={sourceSummary(item)}>{sourceSummary(item)}</div>
                      <div className="mt-1 truncate text-[11px] text-zinc-600">
                        {item.senderName || "Unknown sender"} · {item.chatTitle || item.chatType}
                      </div>
                      {item.mediaGroupId && <div className="mt-1 text-[11px] text-amber-500/80">Media album</div>}
                    </td>
                    <td className="px-4 py-3">
                      {isMissingExpense ? (
                        <span className="inline-flex rounded-full border border-amber-500/20 bg-amber-500/10 px-2 py-1 text-[11px] font-medium text-amber-300">
                          Expense missing
                        </span>
                      ) : <StatusBadge status={item.status} />}
                      <div className="mt-1 text-[11px] text-zinc-600">{item.attempts} attempt{item.attempts === 1 ? "" : "s"}</div>
                    </td>
                    <td className="px-4 py-3">
                      <div className="font-mono font-medium text-zinc-100">{formatMoney(amount ?? null)}</div>
                      {expenseCount > 1 && <div className="mt-1 text-[11px] font-medium text-blue-400">+ {expenseCount - 1} more expense{expenseCount === 2 ? "" : "s"}</div>}
                      <div className="mt-1 text-[11px] text-zinc-500">{item.category || extraction?.category || "Unclassified"} · {item.expenseType || extraction?.expenseType || "Unknown type"}</div>
                    </td>
                    <td className="px-4 py-3">
                      <div className={unitNumber && !item.truckId && item.status === "completed" ? "text-amber-400" : "text-zinc-200"}>
                        {item.truckId ? <Link href={`/trucks/detail?id=${item.truckId}`} className="hover:text-blue-400">Unit {unitNumber || "—"}</Link> : `Unit ${unitNumber || "—"}`}
                      </div>
                      <div className={`mt-1 text-[11px] ${driverName && !item.driverId && item.status === "completed" ? "text-amber-400" : "text-zinc-600"}`}>
                        {item.driverId ? <Link href={`/drivers/detail?id=${item.driverId}`} className="hover:text-blue-400">{driverName || "No driver"}</Link> : driverName || "No driver"}
                      </div>
                    </td>
                    <td className="max-w-[360px] px-4 py-3">
                      {isMissingExpense ? (
                        <div className="flex items-start gap-2 text-amber-300">
                          <CircleAlert className="mt-0.5 h-4 w-4 shrink-0" />
                          <span>The linked expense was deleted or is unavailable.</span>
                        </div>
                      ) : item.status === "completed" ? (
                        <div className="flex items-start gap-2 text-emerald-400">
                          <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />
                          <span>{expenseCount > 1 ? `${expenseCount} expenses created` : item.description || extraction?.description || "Expense created"}</span>
                        </div>
                      ) : item.lastError ? (
                        <div className="flex items-start gap-2 text-amber-300">
                          <CircleAlert className="mt-0.5 h-4 w-4 shrink-0" />
                          <span className="line-clamp-3" title={item.lastError}>{item.lastError}</span>
                        </div>
                      ) : (
                        <div className="flex items-center gap-2 text-zinc-500">
                          <Clock3 className="h-4 w-4" /> Waiting for the worker
                        </div>
                      )}
                    </td>
                    <td className="px-4 py-3 text-right">
                      {item.status === "needs_review" ? (
                        <button
                          type="button"
                          onClick={() => openReview(item)}
                          className="inline-flex items-center gap-1.5 rounded-lg bg-blue-600 px-2.5 py-1.5 text-[12px] font-medium text-white transition hover:bg-blue-500"
                        >
                          Review
                        </button>
                      ) : canRetry ? (
                        <button
                          type="button"
                          disabled={retrying === item.updateId}
                          onClick={() => void retry(item)}
                          className="inline-flex items-center gap-1.5 rounded-lg border border-zinc-700 bg-zinc-900 px-2.5 py-1.5 text-[12px] text-zinc-300 transition hover:border-zinc-600 hover:text-white disabled:cursor-wait disabled:opacity-50"
                        >
                          <RefreshCw className={`h-3.5 w-3.5 ${retrying === item.updateId ? "animate-spin" : ""}`} />
                          Retry
                        </button>
                      ) : item.status === "completed" ? (
                        <div className="text-right">
                          <div className="font-mono text-[11px] text-zinc-600" title={item.expenseId || undefined}>{item.expenseId?.slice(0, 8)}</div>
                          {expenseCount > 1 && <div className="mt-1 text-[11px] text-blue-400">{expenseCount} linked</div>}
                        </div>
                      ) : (
                        <SearchX className="ml-auto h-4 w-4 text-zinc-700" />
                      )}
                    </td>
                  </tr>
                );
              })}
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

      {reviewing && (
        <Modal
          title="Review Telegram expense"
          description={`Confirm or correct the extracted fields from message ${reviewing.messageId}. Saving creates ${reviewForms.length === 1 ? "the expense" : `all ${reviewForms.length} expenses`} and closes this review item.`}
          isSaving={isSaving}
          submitLabel={reviewForms.length === 1 ? "Create expense" : `Create ${reviewForms.length} expenses`}
          onClose={() => setReviewing(null)}
          onSubmit={(event) => { event.preventDefault(); void saveReview(); }}
        >
          {error && <div className="mb-4"><ErrorBanner message={error} /></div>}
          {reviewForms.length > 1 && (
            <div className="mb-4 flex flex-wrap gap-2" aria-label="Extracted expenses">
              {reviewForms.map((value, index) => (
                <button
                  key={`${index}-${value.amount}-${value.expenseType}`}
                  type="button"
                  onClick={() => setReviewIndex(index)}
                  className={`rounded-lg border px-3 py-1.5 text-[12px] transition ${reviewIndex === index ? "border-blue-500/40 bg-blue-500/10 text-blue-300" : "border-zinc-800 text-zinc-500 hover:text-zinc-200"}`}
                >
                  Expense {index + 1}{value.amount ? ` · ${formatMoney(value.amount)}` : ""}
                </button>
              ))}
            </div>
          )}
          <ExpenseForm
            value={reviewForms[reviewIndex] || emptyExpenseInput}
            options={options}
            drivers={drivers}
            trucks={trucks}
            onChange={(value) => setReviewForms((current) => current.map((form, index) => index === reviewIndex ? value : form))}
          />
        </Modal>
      )}
    </div>
  );
}
