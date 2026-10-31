"use client";

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { CircleDollarSign, LoaderCircle, X } from "lucide-react";
import { correctExpenseBalance, fetchExpense, fetchExpenseBalanceHistory } from "@/app/lib/api";
import { usePermissions } from "@/app/lib/access";
import type { Expense, ExpenseBalanceCorrection } from "@/app/lib/types";
import { controlClass, ErrorBanner } from "@/app/components/management/ManagementUI";

export function canCorrectExpenseBalance(expense: Expense) {
  return expense.remainingAmount != null && !expense.managedInSheets && (expense.paymentType ?? "").trim().toLowerCase() !== "reimburse" && !!(expense.chargeDriverId || expense.ownerId);
}

export function ExpenseBalanceAction({ expense, onSaved }: { expense: Expense; onSaved: () => void | Promise<void> }) {
  const allowed = usePermissions().includes("expenses.settle");
  const [open, setOpen] = useState(false);
  if (!allowed || !canCorrectExpenseBalance(expense)) return null;
  return <><button type="button" className="ml-auto block w-max text-xs font-sans text-blue-400 hover:text-blue-300" onClick={() => setOpen(true)} aria-label={`Edit balance: ${expense.expenseType || expense.description || expense.category}`}>Edit balance</button>{open && <ExpenseBalanceEditor id={expense.id} onClose={() => setOpen(false)} onSaved={onSaved} />}</>;
}

function ExpenseBalanceEditor({ id, onClose, onSaved }: { id: string; onClose: () => void; onSaved: () => void | Promise<void> }) {
  const [expense, setExpense] = useState<Expense | null>(null);
  const [history, setHistory] = useState<ExpenseBalanceCorrection[]>([]);
  const [remaining, setRemaining] = useState("");
  const [reason, setReason] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const lock = useRef(false);
  const dialog = useRef<HTMLDialogElement>(null);
  const request = useRef<{ fingerprint: string; id: string } | null>(null);
  useEffect(() => {
    const element = dialog.current;
    const previous = document.activeElement as HTMLElement | null;
    element?.showModal();
    return () => { element?.close(); previous?.focus(); };
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    Promise.all([fetchExpense(id, controller.signal), fetchExpenseBalanceHistory(id, controller.signal)]).then(([value, entries]) => {
      if (controller.signal.aborted) return;
      setExpense(value); setHistory(entries); setRemaining(value.remainingAmount ?? ""); setError("");
    }).catch(e => { if (!controller.signal.aborted) setError(e.message); });
    return () => controller.abort();
  }, [id, attempt]);
  async function save() {
    if (!expense || lock.current) return;
    lock.current = true; setBusy(true); setError("");
    const values = { remaining, reason, version: expense.balanceVersion };
    const fingerprint = JSON.stringify(values);
    const receipt = request.current?.fingerprint === fingerprint ? request.current.id : crypto.randomUUID();
    request.current = { fingerprint, id: receipt };
    try { await correctExpenseBalance(id, { ...values, id: receipt }); await onSaved(); onClose(); }
    catch (e) { setError(e instanceof Error ? e.message : "Could not save balance"); }
    finally { lock.current = false; setBusy(false); }
  }
  if (typeof document === "undefined") return null;
  const submitDisabled = !expense || !/^\d{1,10}(\.\d{1,2})?$/.test(remaining) || !reason.trim() || Number(remaining) === Number(expense.remainingAmount);
  return createPortal(<dialog ref={dialog} aria-label="Edit charge balance"
    onCancel={event => { event.preventDefault(); if (!lock.current) onClose(); }}
    onClick={event => {
      if (event.target !== event.currentTarget || lock.current) return;
      const bounds = event.currentTarget.getBoundingClientRect();
      if (event.clientX < bounds.left || event.clientX > bounds.right) onClose();
    }}
    className="mserp-ui fixed inset-0 m-0 ml-auto h-dvh max-h-none w-full max-w-lg border-l border-zinc-800 bg-zinc-950 p-0 text-zinc-300 ui-popover backdrop:bg-black/50">
    <form className="flex h-full flex-col" onSubmit={event => { event.preventDefault(); void save(); }}>
      <header className="flex shrink-0 items-start gap-3 border-b border-zinc-800 p-5">
        <CircleDollarSign className="mt-0.5 h-5 w-5 text-zinc-500" />
        <h2 className="min-w-0 flex-1 font-semibold text-zinc-100">Edit charge balance</h2>
        <button type="button" autoFocus aria-label="Close balance editor" disabled={busy} onClick={onClose} className="rounded-lg p-1.5 text-zinc-500 hover:bg-zinc-800 hover:text-zinc-200 disabled:opacity-50"><X className="h-5 w-5" /></button>
      </header>
      <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-5">
    {error && <><div role="alert"><ErrorBanner message={error} /></div><button type="button" disabled={busy} className="text-blue-400 text-xs" onClick={() => { setExpense(null); setAttempt(v => v + 1); }}>Reload current balance</button></>}
    {!expense ? <p className="text-sm text-zinc-400">Loading balance…</p> : <div className="space-y-4">
      <p className="text-sm text-zinc-400">{expense.expenseType || expense.category} · Current remaining: ${Number(expense.remainingAmount).toFixed(2)}</p>
      <p className="text-xs text-zinc-500">This records a balance correction. The original expense and payment history are preserved. Saved payroll deductions remain allocated; finalized statements must be reopened first.</p>
      <label className="block text-xs text-zinc-400">Remaining balance<input required aria-label="Remaining balance" inputMode="decimal" className={controlClass} disabled={busy} value={remaining} onChange={e => setRemaining(e.target.value)} /></label>
      <label className="block text-xs text-zinc-400">Reason<textarea required aria-label="Balance correction reason" maxLength={2000} className={controlClass} disabled={busy} value={reason} onChange={e => setReason(e.target.value)} /></label>
      {history.length > 0 && <div className="space-y-2 border-t border-zinc-800 pt-3"><h3 className="text-sm font-medium">Balance history</h3>{history.map(entry => <div key={entry.id} className="text-xs text-zinc-400"><p>${entry.previous} → ${entry.remaining} · {entry.actor} · {entry.createdAt ? new Date(entry.createdAt).toLocaleString() : ""}</p><p className="whitespace-pre-wrap break-words">{entry.reason}</p></div>)}</div>}
    </div>}
      </div>
      <footer className="flex shrink-0 justify-end gap-2 border-t border-zinc-800 p-5">
        <button type="button" disabled={busy} onClick={onClose} className="ui-button">Cancel</button>
        <button type="submit" disabled={busy || submitDisabled} className="ui-button ui-button-primary inline-flex items-center gap-2">{busy && <LoaderCircle className="h-4 w-4 animate-spin" />}Save balance</button>
      </footer>
    </form>
  </dialog>, document.body);
}
