"use client";

import { useEffect, useRef, useState } from "react";
import { correctExpenseBalance, fetchExpense, fetchExpenseBalanceHistory } from "@/app/lib/api";
import { usePermissions } from "@/app/lib/access";
import type { Expense, ExpenseBalanceCorrection } from "@/app/lib/types";
import { controlClass, ErrorBanner, Modal } from "@/app/components/management/ManagementUI";

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
  const request = useRef<{ fingerprint: string; id: string } | null>(null);
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
  return <Modal title="Edit charge balance" isSaving={busy} submitLabel="Save balance" submitDisabled={!expense || !/^\d{1,10}(\.\d{1,2})?$/.test(remaining) || !reason.trim() || Number(remaining) === Number(expense.remainingAmount)} onClose={onClose} onSubmit={e => { e.preventDefault(); void save(); }}>
    {error && <><div role="alert"><ErrorBanner message={error} /></div><button type="button" disabled={busy} className="text-blue-400 text-xs" onClick={() => { setExpense(null); setAttempt(v => v + 1); }}>Reload current balance</button></>}
    {!expense ? <p className="text-sm text-zinc-400">Loading balance…</p> : <div className="space-y-4">
      <p className="text-sm text-zinc-400">{expense.expenseType || expense.category} · Current remaining: ${Number(expense.remainingAmount).toFixed(2)}</p>
      <p className="text-xs text-zinc-500">This records a balance correction. The original expense and payment history are preserved. Saved payroll deductions remain allocated; finalized statements must be reopened first.</p>
      <label className="block text-xs text-zinc-400">Remaining balance<input autoFocus required aria-label="Remaining balance" inputMode="decimal" className={controlClass} disabled={busy} value={remaining} onChange={e => setRemaining(e.target.value)} /></label>
      <label className="block text-xs text-zinc-400">Reason<textarea required aria-label="Balance correction reason" maxLength={2000} className={controlClass} disabled={busy} value={reason} onChange={e => setReason(e.target.value)} /></label>
      {history.length > 0 && <div className="space-y-2 border-t border-zinc-800 pt-3"><h3 className="text-sm font-medium">Balance history</h3>{history.map(entry => <div key={entry.id} className="text-xs text-zinc-400"><p>${entry.previous} → ${entry.remaining} · {entry.actor} · {entry.createdAt ? new Date(entry.createdAt).toLocaleString() : ""}</p><p className="whitespace-pre-wrap break-words">{entry.reason}</p></div>)}</div>}
    </div>}
  </Modal>;
}
