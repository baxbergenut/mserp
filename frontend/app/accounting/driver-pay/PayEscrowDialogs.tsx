"use client";

import { useEffect, useState } from "react";
import { Modal, controlClass } from "@/app/components/management/ManagementUI";
import { changeDriverPayMembership, fetchDriverPayCandidates, fetchDriverPayEscrow, saveDriverPayEscrow } from "@/app/lib/api";
import type { DriverPayCandidate, DriverPayEscrowEditor, PayrollEscrowDraft } from "@/app/lib/types";
import { decimalDisplay, hundredths } from "@/app/gross-board/board";
import { useDebouncedValue } from "@/app/lib/useDebouncedValue";

const money = (amount: string) => decimalDisplay(hundredths(amount), true);
const cents = (amount: string) => amount === "" ? BigInt(0) : /^\d{1,10}(?:\.\d{1,2})?$/.test(amount) ? hundredths(amount) : null;

export function AddPayDriverDialog({ week, onClose, onAdded }: { week: string; onClose: () => void; onAdded: (id: string) => void }) {
  const [search, setSearch] = useState("");
  const query = useDebouncedValue(search, 200);
  const [drivers, setDrivers] = useState<DriverPayCandidate[]>([]);
  const [driver, setDriver] = useState("");
  const [loaded, setLoaded] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    void fetchDriverPayCandidates(week, query, controller.signal).then(items => {
      if (controller.signal.aborted) return;
      setDrivers(items ?? []);
      setLoaded(query);
    }).catch(reason => { if (!controller.signal.aborted) setError(reason.message); });
    return () => controller.abort();
  }, [week, query]);
  const ready = loaded === search && drivers.some(item => item.id === driver);
  return <Modal title="Add driver" description={`Add a statement for the week of ${week}.`} onClose={onClose} isSaving={saving} submitLabel="Add to payroll" submitDisabled={!ready}
    onSubmit={async event => {
      event.preventDefault();
      if (!ready || saving) return;
      setError(""); setSaving(true);
      try { await changeDriverPayMembership(week, driver); onAdded(driver); }
      catch (reason) { setError(reason instanceof Error ? reason.message : "Could not add driver"); setSaving(false); }
    }}>
    <div className="space-y-3">
      <label className="block text-sm">Search drivers<input autoFocus className={controlClass} value={search} onChange={event => { setSearch(event.target.value); setDriver(""); setError(""); }} disabled={saving} /></label>
      {loaded === search && drivers.length === 0 && <p role="status" className="text-sm text-zinc-400">No eligible drivers match this search.</p>}
      <div className="max-h-64 overflow-y-auto">
        {loaded === search && drivers.map(item => <label key={item.id} className="flex items-center gap-3 border-b border-zinc-800 py-2 text-sm">
          <input type="radio" name="pay-driver" checked={driver === item.id} onChange={() => setDriver(item.id)} disabled={saving} />
          {item.name} <span className="text-xs text-zinc-400">{item.active ? "Active" : "Inactive"}</span>
        </label>)}
      </div>
      {error && <p role="alert" className="text-sm text-red-300">{error}</p>}
    </div>
  </Modal>;
}

const emptyDraft: PayrollEscrowDraft = { escrowId: "", version: 0, returnAmount: "0", expenses: {} };

export function PayEscrowDialog({ week, driver, name, onClose, onSaved }: {
  week: string; driver: string; name: string; onClose: () => void; onSaved: () => void;
}) {
  const [data, setData] = useState<DriverPayEscrowEditor | null>(null);
  const [draft, setDraft] = useState<PayrollEscrowDraft>(emptyDraft);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    void fetchDriverPayEscrow(week, driver, controller.signal).then(result => {
      if (controller.signal.aborted) return;
      const account = result.accounts.find(item => item.id === result.draft.escrowId) ?? result.accounts[0];
      setDraft(result.draft.escrowId ? { ...result.draft, version: account?.id === result.draft.escrowId ? account.version : 0 } :
        { ...emptyDraft, escrowId: account?.id ?? "", version: account?.version ?? 0, stopCollection: result.suggestStop && account && !account.stopped ? true : undefined });
      setData(result);
    }).catch(reason => { if (!controller.signal.aborted) setError(reason.message); });
    return () => controller.abort();
  }, [week, driver]);
  const account = data?.accounts.find(item => item.id === draft.escrowId);
  const applied = Object.values(draft.expenses).reduce((total, value) => total + (cents(value) ?? BigInt(0)), BigInt(0));
  const returned = cents(draft.returnAmount);
  const used = applied + (returned ?? BigInt(0));
  const invalidExpenses = Object.entries(draft.expenses).some(([id, value]) => {
    const expense = data?.expenses.find(item => item.expenseId === id);
    const amount = cents(value);
    return amount === null || (amount > BigInt(0) && (!expense || amount > hundredths(expense.available)));
  });
  const valid = !!account && returned !== null && !invalidExpenses && used <= hundredths(account.available) && (used > BigInt(0) || draft.stopCollection !== undefined);
  async function save(clear = false) {
    if (!data || saving || (!clear && !valid)) return;
    setError(""); setSaving(true);
    try {
      const expenses = Object.fromEntries(Object.entries(draft.expenses).filter(([, value]) => (cents(value) ?? BigInt(0)) > BigInt(0)));
      await saveDriverPayEscrow(week, driver, data.editVersion, data.expenseRevision, clear ? emptyDraft : { ...draft, expenses, returnAmount: draft.returnAmount || "0" });
      onSaved();
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Could not save escrow"); setSaving(false); }
  }
  return <Modal title="Manage escrow" description={`${name} · Week of ${week}. These instructions post when the statement is finalized.`}
    onClose={onClose} isSaving={saving} submitLabel="Save escrow draft" submitDisabled={!valid}
    footerStart={data?.draft.escrowId && <button type="button" className="ui-button" disabled={saving} onClick={() => void save(true)}>Clear escrow draft</button>}
    onSubmit={event => { event.preventDefault(); void save(); }}>
    <div className="space-y-4">
      {!data && !error && <p role="status">Loading escrow…</p>}
      {data && data.accounts.length === 0 && <p role="status">There is no escrow account available for this week.</p>}
      {data && data.accounts.length > 1 && <label className="block text-sm">Escrow account
        <select value={draft.escrowId} className={controlClass} disabled={saving} onChange={event => {
          const selected = data.accounts.find(item => item.id === event.target.value);
          if (selected) setDraft({ ...emptyDraft, escrowId: selected.id, version: selected.version });
        }}>{data.accounts.map((item, index) => <option key={item.id} value={item.id}>Account {index + 1} · {money(item.held)} held</option>)}</select>
      </label>}
      {account && <>
        <dl className="grid gap-3 text-sm sm:grid-cols-3">
          <div><dt className="text-zinc-400">Held after commitments</dt><dd>{money(account.held)}</dd></div>
          <div><dt className="text-zinc-400">Available for this week</dt><dd>{money(account.available)}</dd></div>
          <div><dt className="text-zinc-400">Projected held balance</dt><dd>{decimalDisplay(hundredths(account.held) - used, true)}</dd></div>
        </dl>
        <label className="block text-sm">Return to driver<input className={controlClass} value={draft.returnAmount} inputMode="decimal" disabled={saving} onChange={event => setDraft({ ...draft, returnAmount: event.target.value })} /></label>
        <label className="block text-sm">Escrow collection
          <select className={controlClass} disabled={saving} value={draft.stopCollection === undefined ? "keep" : draft.stopCollection ? "stop" : "resume"} onChange={event => setDraft({ ...draft, stopCollection: event.target.value === "keep" ? undefined : event.target.value === "stop" })}>
            <option value="keep">{account.stopped ? "Keep collection stopped" : "Maintain escrow collection"}</option>
            <option value="stop">Stop collection from this week</option>
            <option value="resume">Resume collection next week</option>
          </select>
        </label>
        <fieldset className="space-y-2" disabled={saving}>
          <legend className="text-sm font-medium">Pay ordinary expenses from escrow</legend>
          {data?.expenses.length === 0 && <p className="text-xs text-zinc-400">No eligible unpaid expenses.</p>}
          {data?.expenses.map(expense => <label key={expense.expenseId} className="flex items-center justify-between gap-4 text-sm">
            <span>{expense.name} · {expense.expenseDate} · {money(expense.available)} available</span>
            <input aria-label={`Escrow for ${expense.name}`} className={`${controlClass} !w-28`} inputMode="decimal" value={draft.expenses[expense.expenseId] ?? ""} onChange={event => setDraft({ ...draft, expenses: { ...draft.expenses, [expense.expenseId]: event.target.value } })} />
          </label>)}
          {Object.keys(draft.expenses).filter(id => !data?.expenses.some(expense => expense.expenseId === id)).map(id => <p key={id} className="text-sm text-amber-300">An allocated expense is no longer available. <button type="button" className="underline" onClick={() => {
            const expenses = { ...draft.expenses }; delete expenses[id]; setDraft({ ...draft, expenses });
          }}>Remove allocation</button></p>)}
        </fieldset>
        {(invalidExpenses || returned === null || used > hundredths(account.available)) && <p role="status" className="text-sm text-amber-300">Enter valid amounts within the available escrow and expense balances.</p>}
      </>}
      {error && <p role="alert" className="text-sm text-red-300">{error}</p>}
    </div>
  </Modal>;
}
