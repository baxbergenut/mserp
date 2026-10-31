"use client";

import { ExpenseBalanceAction } from "./ExpenseBalanceEditor";
import { useDebouncedValue } from "@/app/lib/useDebouncedValue";
import { PaymentHistoryPanel } from "@/app/components/PaymentHistoryPanel";

import { useViewState } from "@/app/lib/viewMemory";

import { useCallback, useEffect, useId, useRef, useState, type ReactNode } from "react";
import Link from "@/app/components/AccessLink";
import { Filter, WalletCards } from "lucide-react";
import { fetchExpensesPage } from "@/app/lib/api";
import type { Expense, ExpensePage } from "@/app/lib/types";
import {
  EmptyState,
  ErrorBanner,
  ManagementSearch,
  controlClass,
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

function HeaderFilter({ label, summary, children, onClear }: { label: string; summary: string; children: ReactNode; onClear: () => void }) {
  const id = useId();
  const panel = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  return <div>
    <button type="button" aria-label={`Filter ${label}`} aria-expanded={open} popoverTarget={id} className={`inline-flex items-center gap-1.5 py-1 ${summary ? "text-blue-400" : "text-zinc-500 hover:text-zinc-300"}`} onClick={event => {
      const rect = event.currentTarget.getBoundingClientRect();
      if (panel.current) { panel.current.style.left = `${Math.max(8, Math.min(rect.left, innerWidth - 264))}px`; panel.current.style.top = `${rect.bottom + 4}px`; panel.current.style.maxHeight = `${Math.max(100, innerHeight - rect.bottom - 16)}px`; }
    }}>{label}<Filter className="h-3 w-3" /></button>
    {summary && <span className="block max-w-36 truncate text-[11px] font-normal text-blue-400" title={summary}>{summary}</span>}
    <div id={id} ref={panel} popover="auto" role="group" aria-label={`${label} filter controls`} onToggle={event => setOpen(event.newState === "open")} className="fixed m-0 w-64 max-w-[calc(100vw-16px)] space-y-3 overflow-y-auto rounded-lg border border-zinc-700 bg-zinc-900 p-3 text-left text-xs font-normal text-zinc-300 ui-popover">
      {children}
      <div className="flex justify-between gap-2"><button type="button" className="text-blue-400" onClick={onClear}>Clear</button><button type="button" className="ui-button" popoverTarget={id} popoverTargetAction="hide">Done</button></div>
    </div>
  </div>;
}

export function RelatedExpenses({
  truckId,
  driverId,
  scope,
  generalOnly = false,
}: {
  generalOnly?: boolean;
  truckId?: string;
  driverId?: string;
  scope?: "personal" | "non_personal";
}) {
  const [search, setSearch] = useState("");
  const [filters, setFilters] = useState({dateFrom:"",dateTo:"",categoryId:"",expenseType:"",description:"",coveredBy:"",amountMin:"",amountMax:"",paidMin:"",paidMax:"",remainingMin:"",remainingMax:""});
  const [categories, setCategories] = useState<ExpensePage["options"]["categories"]>([]);
  const debouncedSearch = useDebouncedValue(search);
  const debouncedFilters = useDebouncedValue(filters);
  const request = useRef<AbortController | null>(null);


  const [selected, setSelected] = useState<Expense | null>(null);
  const [expenses, setExpenses] = useState<Expense[]>([]);
  const [page, setPage] = useViewState(`RelatedExpenses:${scope ?? "linked"}:page`, 1);
  const [pageSize, setPageSize] = useViewState(`RelatedExpenses:${scope ?? "linked"}:pageSize`, 10);
  const [total, setTotal] = useState(0);
  const [totalPages, setTotalPages] = useState(1);
  const [amount, setAmount] = useState("0");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const filter = (key: keyof typeof filters, value: string) => { setFilters(old => ({...old,[key]:value})); setPage(1); };
  const textFilter = (key: keyof typeof filters, label: string, type = "text") => <label className="block">{label}<input aria-label={label} type={type} step={type === "number" ? "0.01" : undefined} className={controlClass + " mt-1 font-normal"} value={filters[key]} onChange={e => filter(key,e.target.value)} /></label>;
  const moneyFilter = (min: keyof typeof filters, max: keyof typeof filters, label: string) => <HeaderFilter label={label} summary={[filters[min] && `≥ ${filters[min]}`, filters[max] && `≤ ${filters[max]}`].filter(Boolean).join(" · ")} onClear={() => { filter(min, ""); filter(max, ""); }}>{textFilter(min, label+" minimum", "number")}{textFilter(max,label+" maximum", "number")}</HeaderFilter>;

  const load = useCallback(async () => {
    request.current?.abort();
    const controller = new AbortController(); request.current = controller;
    setLoading(true);
    try {
      const response = await fetchExpensesPage({...debouncedFilters, search:debouncedSearch, scope:generalOnly ? "general":undefined, page, pageSize, truckId, driverId: scope === "personal" ? undefined : driverId, chargeDriverId: scope === "personal" ? driverId : undefined, responsibility: scope === "non_personal" ? scope : undefined }, controller.signal);
      if (controller.signal.aborted) return;
      setCategories(response.options.categories);
      setExpenses(response.items);
      setPage(response.page);
      setTotal(response.total);
      setTotalPages(response.totalPages);
      setAmount(response.summary.amount);
      setError("");
    } catch (reason) {
      if (controller.signal.aborted) return;
      setError(reason instanceof Error ? reason.message : "Failed to load expenses");
    } finally {
      if (!controller.signal.aborted) setLoading(false);
    }
  }, [setPage, driverId, page, pageSize, truckId, scope, generalOnly, debouncedSearch, debouncedFilters]);

  useEffect(() => {
    const timeout = window.setTimeout(() => void load(), 0);
    return () => { window.clearTimeout(timeout); request.current?.abort(); };
  }, [load]);

  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <WalletCards className="h-4 w-4 text-zinc-500" />
            <h2 className="text-sm font-semibold text-zinc-100">{scope === "personal" ? "Personal expense charges" : scope === "non_personal" ? "Company & other-party expenses" : "Expenses"}</h2>
            <span className="rounded-full bg-zinc-800/60 px-2 py-0.5 text-[11px] text-zinc-400">{total}</span>
          </div>
          <p className="mt-1 text-[12px] text-zinc-500">{scope === "personal" ? "Total charged" : "Linked total"}: {formatMoney(amount)}</p>
        </div>
        <Link href="/expenses" className="text-[12px] font-medium text-blue-400 transition hover:text-blue-300">Open Expenses</Link>
      </div>
      <div className="flex gap-2 items-center"><ManagementSearch value={search} onChange={value => { setSearch(value); setPage(1); }} placeholder="Search charges…" />{(search || Object.values(filters).some(Boolean)) && <button className="ui-button" onClick={() => { setSearch(""); setFilters({dateFrom:"",dateTo:"",categoryId:"",expenseType:"",description:"",coveredBy:"",amountMin:"",amountMax:"",paidMin:"",paidMax:"",remainingMin:"",remainingMax:""}); setPage(1); }}>Clear filters</button>}</div>
      {error && <ErrorBanner message={error} />}
      <TableShell>
        <table className="w-full min-w-[820px] text-left text-[13px]">
            <thead>
              <tr className="border-b border-zinc-800/50 text-zinc-500">
                <th className="px-4 py-3 font-medium"><HeaderFilter label="Date" summary={[filters.dateFrom, filters.dateTo].filter(Boolean).join(" → ")} onClear={() => { filter("dateFrom", ""); filter("dateTo", ""); }}>{textFilter("dateFrom","Charge date from","date")}{textFilter("dateTo","Charge date to","date")}</HeaderFilter></th>
                <th className="px-4 py-3 font-medium"><HeaderFilter label="Category" summary={categories.find(c => c.id === filters.categoryId)?.name ?? ""} onClear={() => filter("categoryId", "")}><label>Category<select aria-label="Charge category" className={controlClass + " mt-1"} value={filters.categoryId} onChange={e => filter("categoryId",e.target.value)}><option value="">All categories</option>{categories.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}</select></label></HeaderFilter></th>
                <th className="px-4 py-3 font-medium"><HeaderFilter label="Type" summary={filters.expenseType} onClear={() => filter("expenseType", "")}>{textFilter("expenseType","Filter type")}</HeaderFilter></th>
                <th className="px-4 py-3 font-medium"><HeaderFilter label="Description" summary={filters.description} onClear={() => filter("description", "")}>{textFilter("description","Filter description")}</HeaderFilter></th>
                <th className="px-4 py-3 font-medium"><HeaderFilter label="Responsibility" summary={filters.coveredBy} onClear={() => filter("coveredBy", "")}><label>Responsibility<select aria-label="Charge responsibility" className={controlClass+" mt-1"} value={filters.coveredBy} onChange={e => filter("coveredBy",e.target.value)}><option value="">All responsibility</option><option>Driver</option><option>Truck Owner</option><option>Company</option></select></label></HeaderFilter></th>
                <th className="px-4 py-3 text-right font-medium">{moneyFilter("amountMin","amountMax","Total")}</th>
                {scope === "personal" && <><th className="px-4 py-3 text-right font-medium">{moneyFilter("paidMin","paidMax","Paid")}</th><th className="px-4 py-3 text-right font-medium">{moneyFilter("remainingMin","remainingMax","Remaining")}</th></>}
              </tr>
            </thead>
            <tbody>
              {loading ? <tr><td colSpan={scope === "personal" ? 8 : 6} className="p-4 text-center text-zinc-500">Loading charges…</td></tr> : expenses.length === 0 ? <tr><td colSpan={scope === "personal" ? 8 : 6}><EmptyState message="No expenses match these filters." /></td></tr> : expenses.map((expense) => (
                <tr key={expense.id} className="border-b border-zinc-900/70 text-zinc-300 last:border-0">
                  <td className="px-4 py-3 font-mono tabular-nums">{formatDate(expense.expenseDate)}<span className="mt-1 block font-sans text-[11px] text-zinc-600">{expense.createdByName ? `Added by ${expense.createdByName}` : "Added before tracking"}</span></td>
                  <td className="px-4 py-3">{expense.category}</td>
                  <td className="px-4 py-3 text-zinc-400">{expense.expenseType || "—"}</td>
                  <td className="max-w-[330px] truncate px-4 py-3 text-zinc-400" title={expense.description ?? undefined}>{expense.description || "—"}{scope === "personal" && <button className="mt-1 block text-xs text-blue-400 hover:text-blue-300" onClick={() => setSelected(expense)}>Payment history</button>}</td>
                  <td className="px-4 py-3">{expense.coveredBy || "Unspecified"}{expense.ownerName && <span className="block text-xs text-zinc-500">{expense.ownerName}</span>}</td>
                  <td className={`px-4 py-3 text-right font-mono font-medium tabular-nums ${expense.amount === null ? "text-amber-400" : "text-zinc-100"}`}>{formatMoney(expense.amount)}</td>
                  {scope === "personal" && <><td className="px-4 py-3 text-right font-mono">{formatMoney(expense.paidAmount)}</td><td className="px-4 py-3 text-right font-mono">{formatMoney(expense.remainingAmount)}<ExpenseBalanceAction expense={expense} onSaved={load} /></td></>}
                </tr>
              ))}
            </tbody>
          </table>
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
      {selected && <PaymentHistoryPanel title={selected.description || selected.category} paid={selected.paidAmount ?? "0"} remaining={selected.remainingAmount ?? "0"} previouslyPaid={selected.driverSettled ? selected.amount ?? "0" : "0"} payments={selected.payments} driverId={driverId} onClose={() => setSelected(null)} />}
    </section>
  );
}
