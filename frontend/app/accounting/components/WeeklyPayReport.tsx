"use client";

import { useEffect, useState } from "react";
import { Banknote, CircleDollarSign, Headset, Pencil, Plus, Route, UsersRound } from "lucide-react";
import { PageHeader } from "@/app/components/PageHeader";
import { MetricCard } from "@/app/components/MetricCard";
import { Modal, controlClass } from "@/app/components/management/ManagementUI";
import { usePermissions } from "@/app/lib/access";
import { changeDispatcherPayStatement, fetchDispatcherCommissionRules, fetchDispatcherPay, saveDispatcherCommissionRules, saveDispatcherPayInput } from "@/app/lib/api";
import type { DispatcherCommissionReport, DispatcherCommissionRow, DispatcherCommissionRules, DispatcherCommissionRuleVersion, DispatcherCommissionStatement } from "@/app/lib/types";

const money = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", minimumFractionDigits: 2 });
const number = new Intl.NumberFormat("en-US", { maximumFractionDigits: 2 });
const button = "inline-flex min-h-8 items-center justify-center gap-2 rounded-lg border border-zinc-800 px-3 text-[13px] text-zinc-200 hover:bg-zinc-800/60 disabled:opacity-50";
function weekLabel(week: string) { const start = new Date(week + "T12:00:00"); const end = new Date(start); end.setDate(end.getDate() + 6); return `${start.toLocaleDateString("en-US", { month: "short", day: "numeric" })} – ${end.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" })}`; }
function message(error: unknown) { return error instanceof Error ? error.message : "The request could not be completed."; }
function rpmTier(row: DispatcherCommissionRow, rule: DispatcherCommissionRuleVersion | null) {
  const brackets = rule?.rules.tiers.find(tier => tier.minGross === row.selectedTier)?.brackets;
  if (!brackets?.length) return "No tier";
  const index = brackets.findLastIndex(bracket => row.rpm >= bracket.minRPM);
  if (index < 0) return `Below $${brackets[0].minRPM.toFixed(2)}`;
  const lower = `$${brackets[index].minRPM.toFixed(2)}`;
  return index + 1 < brackets.length ? `${lower}–<$${brackets[index + 1].minRPM.toFixed(2)}` : `${lower}+`;
}

export function WeeklyPayReport() {
  const permissions = usePermissions();
  const [report, setReport] = useState<DispatcherCommissionReport | null>(null);
  const [rules, setRules] = useState<DispatcherCommissionRuleVersion[]>([]);
  const [week, setWeek] = useState("");
  const [tab, setTab] = useState("pay");
  const [dispatcher, setDispatcher] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [editing, setEditing] = useState<DispatcherCommissionRow | null>(null);
  const [homeDays, setHomeDays] = useState("0");
  const [note, setNote] = useState("");
  const [ruleDraft, setRuleDraft] = useState<DispatcherCommissionRules | null>(null);
  const [effectiveWeek, setEffectiveWeek] = useState("");
  const [ruleRevision, setRuleRevision] = useState(0);
  const [statementAction, setStatementAction] = useState<{ action: "finalize" | "reopen"; statement: DispatcherCommissionStatement } | null>(null);
  const [reason, setReason] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    fetchDispatcherPay({ weekStart: week || undefined }, controller.signal).then(value => {
      setReport(value); setError("");
    }).catch(e => { if (!controller.signal.aborted) { setReport(null); setError(message(e)); } }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [week]);
  useEffect(() => {
    if (tab !== "rules") return;
    let active = true;
    fetchDispatcherCommissionRules().then(value => { if (active) setRules(value); }).catch(e => { if (active) setError(message(e)); });
    return () => { active = false; };
  }, [tab]);
  useEffect(() => { if (!notice) return; const timeout = setTimeout(() => setNotice(""), 5000); return () => clearTimeout(timeout); }, [notice]);

  async function mutate(action: () => Promise<unknown>, close: () => void) {
    setBusy(true); setError("");
    try {
      await action(); close(); setNotice("Saved");
      const value = await fetchDispatcherPay({ weekStart: report?.weekStart }); setReport(value);
      if (tab === "rules") setRules(await fetchDispatcherCommissionRules());
    } catch (e) { setError(message(e)); } finally { setBusy(false); }
  }
  function editRule(version: DispatcherCommissionRuleVersion) {
    setRuleDraft(structuredClone(version.rules)); setEffectiveWeek(version.effectiveWeek);
    setRuleRevision(Math.max(0, ...rules.map(v => v.id))); setError("");
  }
  function changeTier(index: number, value: number) { setRuleDraft(old => old && ({ ...old, tiers: old.tiers.map((tier, i) => i === index ? { ...tier, minGross: value } : tier) })); }
  function changeBracket(tier: number, bracket: number, field: "minRPM" | "percent", value: number) {
    setRuleDraft(old => old && ({ ...old, tiers: old.tiers.map((t, i) => i === tier ? { ...t, brackets: t.brackets.map((b, j) => j === bracket ? { ...b, [field]: value } : b) } : t) }));
  }
  const statements = report?.statements ?? [];
  const visible = statements.filter(s => !dispatcher || s.dispatcherId === dispatcher);
  const currentWeek = report?.weekStart ?? week;
  const lockedDrivers = new Set(statements.filter(s => s.finalized).flatMap(s => s.rows.map(row => row.driverId)));
  const selectedRules = rules.filter((version, index) => rules.findIndex(v => v.effectiveWeek === version.effectiveWeek) === index);
  const total = (key: "pay" | "gross" | "loadCount") => statements.reduce((sum, s) => sum + s[key], 0);
  const modalError = error && <p role="alert" className="rounded-lg border border-red-500/30 p-3 text-red-300">{error}</p>;

  return <div className="space-y-4">
    <PageHeader><div className="flex items-center gap-2"><Headset className="h-5 w-5 text-violet-400" /><h1 className="font-semibold">Dispatcher pay</h1></div></PageHeader>
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <MetricCard label="Dispatcher commission" value={money.format(total("pay"))} icon={Banknote} loading={loading} />
      <MetricCard label="Gross Board credited gross" value={money.format(total("gross"))} icon={CircleDollarSign} loading={loading} />
      <MetricCard label="Dispatchers" value={String(statements.filter(s => s.dispatcherId).length)} icon={UsersRound} loading={loading} />
      <MetricCard label="Loads" value={number.format(total("loadCount"))} icon={Route} loading={loading} />
    </div>
    <div className="flex flex-wrap items-center gap-2">
      <div role="tablist" aria-label="Dispatcher pay views" className="flex gap-4">
        <button role="tab" aria-selected={tab === "pay"} onClick={() => setTab("pay")}>Weekly pay</button>
        <button role="tab" aria-selected={tab === "rules"} onClick={() => setTab("rules")}>Commission rules</button>
      </div>
      <label className="ml-auto flex items-center gap-2 whitespace-nowrap text-xs">Week starting<input type="date" aria-label="Week starting" className={controlClass} value={week || currentWeek} disabled={busy || loading} onChange={e => { if (e.target.value) { setLoading(true); setWeek(e.target.value); } }} /></label>
      {tab === "pay" && <select aria-label="Dispatcher" className={`${controlClass} max-w-64`} value={dispatcher} onChange={e => setDispatcher(e.target.value)}><option value="">All dispatchers</option>{statements.filter(s => s.dispatcherId).map(s => <option key={s.dispatcherId} value={s.dispatcherId}>{s.dispatcherName}</option>)}</select>}
    </div>
    {notice && <p role="status" className="text-emerald-400">{notice}</p>}
    {error && !editing && !ruleDraft && !statementAction && <div className="flex items-center gap-3">{modalError}<button className={button} onClick={() => { setLoading(true); void fetchDispatcherPay({ weekStart: week || undefined }).then(value => { setReport(value); setError(""); }).catch(e => setError(message(e))).finally(() => setLoading(false)); }}>Retry</button></div>}
    {loading && <div aria-label="Loading dispatcher pay" className="h-40 animate-pulse rounded-lg bg-zinc-800/40" />}
    {!loading && report && tab === "pay" && <>
      <div className="flex flex-wrap items-center gap-3 text-xs text-zinc-400"><span>{weekLabel(report.weekStart)}</span><span>{report.rule ? `Rules effective ${report.rule.effectiveWeek}` : "No rules apply to this week"}</span>{permissions.includes("board.read") && <a className="text-blue-400 hover:underline" href="/gross-board">Open Gross Board</a>}</div>
      {statements.some(s => s.rows.some(row => row.issues.length > 0)) && <p role="status" className="rounded-lg border border-amber-500/20 bg-amber-500/5 p-3 text-amber-400">Some rows need review. Commission totals include calculated rows only; resolve the flagged rows before finalizing.</p>}
      {visible.length === 0 && <div className="ui-card py-10 text-center text-zinc-400">No Gross Board loads in this week.</div>}
      {visible.map(s => <section key={s.dispatcherId || "unassigned"} className="overflow-hidden rounded-lg border border-zinc-800">
        <div className="flex flex-wrap items-center gap-3 bg-card p-4">
          <h2 className="font-semibold">{s.dispatcherName}</h2><span className={s.finalized ? "text-emerald-400" : "text-zinc-400"}>{s.finalized ? "Finalized" : "Draft"}</span>
          <span className="text-xs text-zinc-400">Gross tiers: {[...new Set(s.rows.map(row => row.selectedTier ? money.format(row.selectedTier) : "No tier"))].join(", ")}</span>
          <span className="text-xs text-zinc-400">RPM tiers: {[...new Set(s.rows.map(row => rpmTier(row, s.rule)))].join(", ")}</span>
          <span className="text-xs text-zinc-400">{s.rule ? `Rules from ${s.rule.effectiveWeek}` : "No commission rules"}{s.finalizedAt ? ` · ${new Date(s.finalizedAt).toLocaleString()}` : ""}</span>
          <strong className="ml-auto font-mono tabular-nums">{money.format(s.pay)}</strong>
          {s.dispatcherId && permissions.includes(s.finalized ? "dispatcher_pay.reopen" : "dispatcher_pay.finalize") && <button className={button} disabled={busy || (!s.finalized && s.rows.some(row => row.issues.length > 0))} onClick={() => { setStatementAction({ action: s.finalized ? "reopen" : "finalize", statement: s }); setReason(""); setError(""); }}>{s.finalized ? "Reopen" : "Finalize"}</button>}
        </div>
        {s.sourceChanged && <p className="border-t border-amber-500/20 bg-amber-500/5 px-4 py-2 text-amber-400">Gross Board, weekly inputs or rules have changed. This finalized statement retains its saved amounts.</p>}
        {!s.dispatcherId && <p className="px-4 py-2 text-amber-400">Assign dispatcher credit in Gross Board to calculate pay for these loads.</p>}
        <div className="overflow-x-auto"><table className="w-full min-w-[1250px] text-left text-[13px]">
          <thead className="bg-zinc-900/60 text-xs text-zinc-400"><tr>{["Driver / truck", "Weekly gross", "Miles", "RPM", "Home days", "Adjusted gross", "Actual gross tier", "Selected gross tier", "RPM tier", "Rate", "Credited gross", "Commission", ""].map((label, i) => <th key={i} className={`px-3 py-2 font-medium ${i > 0 && i < 12 ? "text-right" : ""}`}>{label}</th>)}</tr></thead>
          <tbody>{s.rows.map(row => <tr key={row.driverId} className="border-t border-zinc-800/60 align-top">
            <td className="px-3 py-2"><div>{row.driverName}</div><div className="text-xs text-zinc-400">{row.truckUnits.join(", ") || "No assigned truck"} · {row.loadCount} loads</div>{row.note && <div className="max-w-64 whitespace-pre-wrap text-xs text-zinc-400">{row.note}</div>}{row.issues.map(issue => <div key={issue} className="text-xs text-amber-400">{issue}</div>)}</td>
            {[money.format(row.gross), number.format(row.miles), `$${row.rpm.toFixed(3)}`, row.homeDays, money.format(row.adjustedGross), row.actualTier ? money.format(row.actualTier) : "No tier", row.selectedTier ? money.format(row.selectedTier) : "No tier", rpmTier(row, s.rule), `${number.format(row.rate)}%`, money.format(row.creditedGross), row.issues.length ? "Review" : money.format(row.pay)].map((value, i) => <td key={i} className="whitespace-nowrap px-3 py-2 text-right font-mono tabular-nums">{value}</td>)}
            <td className="px-3 py-2">{permissions.includes("dispatcher_pay.edit") && <button aria-label={`Edit home days for ${row.driverName}`} className={button} disabled={busy || lockedDrivers.has(row.driverId)} title={lockedDrivers.has(row.driverId) ? "Reopen this driver's finalized dispatcher statements first" : "Edit weekly home days and comment"} onClick={() => { setEditing(row); setHomeDays(String(row.homeDays)); setNote(row.note); setError(""); }}><Pencil className="h-4 w-4" /></button>}</td>
          </tr>)}</tbody>
        </table></div>
      </section>)}
      <p className="text-xs text-zinc-400">Gross and miles use counted Gross Board entries. Weekly driver gross ÷ weekly miles determines RPM; dated dispatcher credit determines payable gross. Home-day credit affects the tier only and can reduce commission. Commission equals actual credited gross × rate.</p>
    </>}
    {tab === "rules" && <div className="space-y-4">
      <p className="text-xs text-zinc-400">The latest revision with an effective starting week on or before the report week applies. New revisions recalculate draft statements; finalized statements retain their saved rules.</p>
      {rules.length === 0 && <p className="text-zinc-400">Loading commission rules…</p>}
      {selectedRules.map(version => <section key={version.id} className="ui-card space-y-3">
        <div className="flex flex-wrap items-center gap-3"><h2 className="font-semibold">Effective {version.effectiveWeek}</h2><span className="text-xs text-zinc-400">Revision {version.id} · saved {new Date(version.createdAt).toLocaleString()}</span>{permissions.includes("dispatcher_pay.rules") && <button className={`${button} ml-auto`} disabled={busy} onClick={() => editRule(version)}><Pencil className="h-4 w-4" />Edit / schedule rules</button>}</div>
        <p className="text-xs text-zinc-400">Home-day credit: {money.format(version.rules.homeDayCredit)} per day. Maximum promotion: {version.rules.maxTierPromotion} tier(s).</p>
        <div className="grid gap-4 md:grid-cols-3">{version.rules.tiers.map((tier, i) => <div key={i} className="rounded-lg border border-zinc-800 p-3"><h3 className="mb-2 font-medium">Gross {money.format(tier.minGross)}+</h3><table className="w-full"><thead><tr className="text-xs text-zinc-400"><th className="text-left font-normal">RPM at least</th><th className="text-right font-normal">Commission</th></tr></thead><tbody>{tier.brackets.map((b, j) => <tr key={j}><td>${b.minRPM.toFixed(2)}</td><td className="text-right">{number.format(b.percent)}%</td></tr>)}</tbody></table></div>)}</div>
      </section>)}
      {rules.length > selectedRules.length && <details className="ui-card"><summary className="cursor-pointer">Earlier revisions</summary>{rules.filter(v => !selectedRules.includes(v)).map(v => <p key={v.id} className="mt-2 text-xs text-zinc-400">Revision {v.id} · effective {v.effectiveWeek} · saved {new Date(v.createdAt).toLocaleString()}</p>)}</details>}
    </div>}
    {editing && <Modal title={`Weekly inputs · ${editing.driverName}`} isSaving={busy} submitLabel="Save" onClose={() => setEditing(null)} onSubmit={e => { e.preventDefault(); void mutate(() => saveDispatcherPayInput({ weekStart: currentWeek, driverId: editing.driverId, homeDays: Number(homeDays), note, version: editing.version }), () => setEditing(null)); }}>
      <div className="space-y-4">{modalError}<label className="block text-xs">Home days<input className={controlClass} type="number" min="0" max="7" step="1" required value={homeDays} onChange={e => setHomeDays(e.target.value)} /></label><label className="block text-xs">Comment<textarea className={`${controlClass} min-h-20`} maxLength={1000} value={note} onChange={e => setNote(e.target.value)} /></label><p className="text-xs text-zinc-400">Applies to this driver’s whole week, including all credited dispatchers. Home days affect tier qualification only.</p></div>
    </Modal>}
    {statementAction && <Modal title={`${statementAction.action === "finalize" ? "Finalize" : "Reopen"} · ${statementAction.statement.dispatcherName}`} isSaving={busy} submitLabel={statementAction.action === "finalize" ? "Finalize" : "Reopen"} onClose={() => setStatementAction(null)} onSubmit={e => { e.preventDefault(); void mutate(() => changeDispatcherPayStatement(statementAction.action, { weekStart: currentWeek, dispatcherId: statementAction.statement.dispatcherId, revision: statementAction.statement.revision, version: statementAction.statement.version, reason }), () => setStatementAction(null)); }}>
      <div className="space-y-4">{modalError}<p>{weekLabel(currentWeek)} · {money.format(statementAction.statement.pay)}</p><p className="text-zinc-400">{statementAction.action === "finalize" ? "Save the reviewed inputs, commission rules and calculated amounts as a finalized statement." : "Reopening recalculates this statement using current Gross Board values, weekly inputs and effective rules."}</p>{statementAction.action === "reopen" && <label className="block text-xs">Reason<textarea className={controlClass} required maxLength={1000} value={reason} onChange={e => setReason(e.target.value)} /></label>}</div>
    </Modal>}
    {ruleDraft && <Modal title="Commission rules" wide isSaving={busy} submitLabel="Save revision" onClose={() => setRuleDraft(null)} onSubmit={e => { e.preventDefault(); void mutate(() => saveDispatcherCommissionRules({ expectedRevision: ruleRevision, effectiveWeek, rules: ruleDraft }), () => setRuleDraft(null)); }}>
      <div className="space-y-4">{modalError}<div className="grid gap-3 sm:grid-cols-3">
        <label className="text-xs">Effective starting week<input className={controlClass} type="date" required value={effectiveWeek} onChange={e => setEffectiveWeek(e.target.value)} /></label>
        <label className="text-xs">Home-day credit ($)<input className={controlClass} type="number" required min="0" max="1000000" step="0.01" value={ruleDraft.homeDayCredit} onChange={e => setRuleDraft({ ...ruleDraft, homeDayCredit: Number(e.target.value) })} /></label>
        <label className="text-xs">Maximum tier promotion<input className={controlClass} type="number" required min="0" max={ruleDraft.tiers.length} step="1" value={ruleDraft.maxTierPromotion} onChange={e => setRuleDraft({ ...ruleDraft, maxTierPromotion: Number(e.target.value) })} /></label>
      </div><p className="text-xs text-zinc-400">Choose a Monday. A revision replaces the rules from that starting week until the next scheduled rule set. Finalized statements stay frozen.</p>
      {ruleDraft.tiers.map((tier, i) => <div key={i} className="space-y-3 rounded-lg border border-zinc-800 p-3">
        <div className="flex items-end gap-3"><label className="flex-1 text-xs">Tier {i + 1} minimum gross ($)<input className={controlClass} type="number" required min="0" max="100000000" step="0.01" value={tier.minGross} onChange={e => changeTier(i, Number(e.target.value))} /></label><button type="button" className={button} disabled={ruleDraft.tiers.length === 1} onClick={() => setRuleDraft({ ...ruleDraft, tiers: ruleDraft.tiers.filter((_, j) => j !== i) })}>Remove tier</button></div>
        {tier.brackets.map((bracket, j) => <div key={j} className="flex items-end gap-3"><label className="flex-1 text-xs">Tier {i + 1} RPM {j + 1}<input className={controlClass} type="number" required min="0" max="1000" step="0.01" value={bracket.minRPM} onChange={e => changeBracket(i, j, "minRPM", Number(e.target.value))} /></label><label className="flex-1 text-xs">Tier {i + 1} commission {j + 1} (%)<input className={controlClass} type="number" required min="0" max="100" step="0.01" value={bracket.percent} onChange={e => changeBracket(i, j, "percent", Number(e.target.value))} /></label><button type="button" className={button} disabled={tier.brackets.length === 1} onClick={() => setRuleDraft({ ...ruleDraft, tiers: ruleDraft.tiers.map((t, k) => k === i ? { ...t, brackets: t.brackets.filter((_, k) => k !== j) } : t) })}>Remove</button></div>)}
        <button type="button" className={button} disabled={tier.brackets.length >= 20} onClick={() => setRuleDraft({ ...ruleDraft, tiers: ruleDraft.tiers.map((t, j) => j === i ? { ...t, brackets: [...t.brackets, { minRPM: Math.round((t.brackets.at(-1)!.minRPM + 0.1) * 100) / 100, percent: t.brackets.at(-1)!.percent }] } : t) })}><Plus className="h-4 w-4" />Add RPM bracket</button>
      </div>)}
      <button type="button" className={button} disabled={ruleDraft.tiers.length >= 10} onClick={() => setRuleDraft({ ...ruleDraft, tiers: [...ruleDraft.tiers, { minGross: ruleDraft.tiers.at(-1)!.minGross + 1000, brackets: [{ minRPM: 2.5, percent: 1 }] }] })}><Plus className="h-4 w-4" />Add gross tier</button></div>
    </Modal>}
  </div>;
}
