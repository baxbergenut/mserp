"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { ArrowLeft, Banknote, ChevronDown, CircleDollarSign, Headset, Pencil, Plus, Settings } from "lucide-react";
import { useRouter } from "next/navigation";
import { IntentLink } from "@/app/components/IntentLink";
import { useViewState } from "@/app/lib/viewMemory";
import { latestPayrollWeek, payrollWeekGroups, payrollWeekLabel } from "../payrollWeek";
import { useLatestPayrollWeek } from "./useLatestPayrollWeek";
import { TierMedal } from "./TierMedal";
import { PageHeader } from "@/app/components/PageHeader";
import { FinalizationCard } from "./FinalizationCard";
import { MetricCard } from "@/app/components/MetricCard";
import { DispatcherPaySkeleton } from "@/app/components/WeeklyTableSkeleton";
import { StatementMenu } from "../driver-pay/StatementMenu";
import { ManagementSearch, Modal, controlClass } from "@/app/components/management/ManagementUI";
import { usePermissions } from "@/app/lib/access";
import { changeDispatcherPayStatement, fetchDispatcherCommissionRules, fetchDispatcherPay, saveDispatcherCommissionRules, saveDispatcherPayInput } from "@/app/lib/api";
import type { DispatcherCommissionReport, DispatcherCommissionRow, DispatcherCommissionRules, DispatcherCommissionRuleVersion, DispatcherCommissionStatement } from "@/app/lib/types";

const money = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", minimumFractionDigits: 2 });
const number = new Intl.NumberFormat("en-US", { maximumFractionDigits: 2 });
const button = "inline-flex min-h-8 items-center justify-center gap-2 rounded-lg border border-zinc-700/70 px-3 py-1 text-xs text-zinc-300 hover:bg-zinc-800 focus-visible:outline-2 focus-visible:outline-blue-500 disabled:cursor-not-allowed disabled:opacity-40";
function weekLabel(week: string) { const start = new Date(week + "T12:00:00"); const end = new Date(start); end.setDate(end.getDate() + 6); return `${start.toLocaleDateString("en-US", { month: "short", day: "numeric" })} – ${end.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" })}`; }
function message(error: unknown) { return error instanceof Error ? error.message : "The request could not be completed."; }
export function WeeklyPayReport() {
  const permissions = usePermissions();
  const router = useRouter();
  const [report, setReport] = useState<DispatcherCommissionReport | null>(null);
  const [rules, setRules] = useState<DispatcherCommissionRuleVersion[]>([]);
  const latestWeek = useLatestPayrollWeek();
  const [storedWeek, setWeek] = useViewState("page:week", () => latestPayrollWeek());
  const week = storedWeek > latestWeek ? latestWeek : storedWeek;
  const [availableWeeks, setAvailableWeeks] = useState([week]);
  const weekOptions = useMemo(() => payrollWeekGroups([...availableWeeks, week, latestWeek]), [availableWeeks, week, latestWeek]);
  const [opened, setOpened] = useViewState<Set<string>>("page:opened", new Set());
  const [tab, setTab] = useState("pay");
  const [search, setSearch] = useViewState("page:search", "");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [editing, setEditing] = useState<DispatcherCommissionRow | null>(null);
  const [homeDays, setHomeDays] = useState("0");
  const [autoHomeDays, setAutoHomeDays] = useState(true);
  const [note, setNote] = useState("");
  const [ruleDraft, setRuleDraft] = useState<DispatcherCommissionRules | null>(null);
  const [effectiveWeek, setEffectiveWeek] = useState("");
  const [ruleRevision, setRuleRevision] = useState(0);
  const [statementAction, setStatementAction] = useState<{ action: "finalize" | "reopen"; statement: DispatcherCommissionStatement } | null>(null);
  const [statementMenu, setStatementMenu] = useState<{ statement: DispatcherCommissionStatement; x: number; y: number } | null>(null);
  const closeStatementMenu = useCallback(() => setStatementMenu(null), []);

  useEffect(() => {
    const controller = new AbortController();
    fetchDispatcherPay({ weekStart: week || undefined }, controller.signal).then(value => {
      setReport(value); setAvailableWeeks(value.availableWeeks); setError("");
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
      const value = await fetchDispatcherPay({ weekStart: report?.weekStart }); setReport(value); setAvailableWeeks(value.availableWeeks);
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
  function switchWeek(next: string) {
    if (next === week || busy || loading) return;
    setLoading(true); setReport(null); setError(""); setNotice(""); setOpened(new Set()); setWeek(next);
  }
  const statements = report?.statements ?? [];
  const normalizedSearch = search.trim().toLocaleLowerCase();
  const visible = statements.filter(s => !normalizedSearch || [s.dispatcherName, ...s.rows.flatMap(row => [row.driverName, ...row.truckUnits])].some(value => value.toLocaleLowerCase().includes(normalizedSearch))).sort((a, b) => Number(a.finalized) - Number(b.finalized));
  const currentWeek = report?.weekStart ?? week;
  const lockedDrivers = new Set(statements.filter(s => s.finalized).flatMap(s => s.rows.map(row => row.driverId)));
  const selectedRules = rules.filter((version, index) => rules.findIndex(v => v.effectiveWeek === version.effectiveWeek) === index);
  const total = (key: "pay" | "gross" | "loadCount") => statements.reduce((sum, s) => sum + s[key], 0);
  const modalError = error && <p role="alert" className="rounded-lg border border-red-500/30 p-3 text-red-300">{error}</p>;

  return <div className="space-y-4">
    <PageHeader><div className="flex items-center gap-3"><Headset className="h-5 w-5 text-zinc-500" /><h1 className="text-lg font-semibold text-zinc-100">Dispatcher pay</h1><span className="rounded-full bg-zinc-800/60 px-2.5 py-0.5 text-xs text-zinc-400">{loading ? "—" : statements.length}</span></div></PageHeader>
    <div className="grid gap-4 md:grid-cols-3">
      <MetricCard compact label="Dispatcher commission" value={money.format(total("pay"))} icon={Banknote} loading={loading} />
      <MetricCard compact label="Gross" value={money.format(total("gross"))} icon={CircleDollarSign} loading={loading} />
      <FinalizationCard loading={loading} finalized={statements.filter(s => s.dispatcherId && s.finalized).length} total={statements.filter(s => s.dispatcherId).length} />
    </div>
    <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-2">
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative shrink-0"><select aria-label="Payroll week" className={`${controlClass} appearance-none !h-8 !w-auto !py-0 !pr-9 font-mono`} value={week} disabled={busy || loading} onChange={event => switchWeek(event.target.value)}>
          {weekOptions.map(([year, options]) => <optgroup key={year} label={String(year)}>{options.map(({ date, label }) => <option key={date} value={date}>{label}</option>)}</optgroup>)}
        </select><ChevronDown aria-hidden="true" className="pointer-events-none absolute right-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-zinc-500" /></div>
        <span className="text-xs tabular-nums text-zinc-400">{payrollWeekLabel(week).year}</span>
        <button type="button" className={button} disabled={busy || loading || week === latestWeek} onClick={() => switchWeek(latestWeek)}>Last week</button>
      </div>
      <div className="flex min-w-0 flex-[1_1_22rem] items-center gap-2">
        {tab === "pay" && <div className="min-w-28 flex-1 [&_input]:h-8 [&_input]:py-0"><ManagementSearch value={search} onChange={setSearch} placeholder="Dispatcher, driver, or truck…" /></div>}
        <button type="button" className={`${button} ml-auto shrink-0`} onClick={() => { setTab(tab === "pay" ? "rules" : "pay"); setError(""); }}>
          {tab === "pay" ? <Settings aria-hidden="true" className="h-3.5 w-3.5" /> : <ArrowLeft aria-hidden="true" className="h-3.5 w-3.5" />}{tab === "pay" ? "Commission rules" : "Back to weekly pay"}
        </button>
      </div>
    </div>
    {notice && <p role="status" className="text-emerald-400">{notice}</p>}
    {error && !editing && !ruleDraft && !statementAction && <div className="flex items-center gap-3">{modalError}<button className={button} onClick={() => { setLoading(true); void fetchDispatcherPay({ weekStart: week || undefined }).then(value => { setReport(value); setAvailableWeeks(value.availableWeeks); setError(""); }).catch(e => setError(message(e))).finally(() => setLoading(false)); }}>Retry</button></div>}
    {loading && tab === "pay" && <DispatcherPaySkeleton />}
    {!loading && report && tab === "pay" && <>
      {statements.some(s => s.rows.some(row => row.issues.length > 0)) && <p role="status" className="rounded-lg border border-amber-500/20 bg-amber-500/5 p-3 text-amber-400">Review flagged rows before finalizing.</p>}
      <div className="overflow-x-auto rounded-lg border border-zinc-800"><table className="w-full min-w-[960px] table-fixed border-separate border-spacing-0 text-left text-[13px]" aria-label="Weekly dispatcher pay">
        <colgroup><col style={{ width: "28%" }} /><col style={{ width: "9%" }} /><col style={{ width: "9%" }} /><col style={{ width: "18%" }} /><col style={{ width: "17%" }} /><col style={{ width: "19%" }} /></colgroup>
        <thead><tr className="h-8 bg-zinc-900 text-[11px] text-zinc-500">{["Dispatcher", "Drivers", "Loads", "Gross", "Commission", "Finalized by"].map((label, index) => <th key={index} className={`border-b border-zinc-800 px-3 font-medium ${index >= 1 ? "text-right" : ""}`}>{label}</th>)}</tr></thead>
        {visible.length === 0 && <tbody><tr><td colSpan={6} className="p-8 text-center text-zinc-500">{normalizedSearch ? "No matching dispatchers." : "No Gross Board loads in this week."}</td></tr></tbody>}
        {visible.map(s => { const key = s.dispatcherId || "unassigned", open = opened.has(key), canReopen = !!s.dispatcherId && s.finalized && permissions.includes("dispatcher_pay.reopen"); const toggleStatement = () => setOpened(current => { const next = new Set(current); if (next.has(key)) next.delete(key); else next.add(key); return next; }); return <tbody key={key} role="region" aria-label={s.dispatcherName}>
          <tr className={`h-9 cursor-pointer text-xs text-zinc-300 ${open ? "bg-zinc-950" : "bg-zinc-900 hover:bg-zinc-800/70"}`} onClick={toggleStatement} onContextMenu={event => { if (canReopen) { event.preventDefault(); setStatementMenu({ statement: s, x: event.clientX, y: event.clientY }); } }} onKeyDown={event => { if (canReopen && (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10"))) { event.preventDefault(); const rect = event.currentTarget.getBoundingClientRect(); setStatementMenu({ statement: s, x: rect.left + 12, y: rect.bottom }); } }}>
            <th scope="row" className="border-b border-zinc-800 px-3 py-0 text-left font-medium"><div className="flex h-9 w-full items-center gap-2 text-left text-zinc-100"><button type="button" aria-label={`${open ? "Collapse" : "Expand"} ${s.dispatcherName}`} aria-expanded={open} aria-controls={`dispatcher-${key}`} className="shrink-0 rounded focus-visible:outline-2 focus-visible:outline-blue-500"><ChevronDown className={`h-3.5 w-3.5 shrink-0 text-zinc-500 ${open ? "rotate-180" : ""}`} /></button><span className="truncate">{s.dispatcherName}</span>{s.finalized && <span className="ml-auto text-[10px] text-emerald-400" title={s.finalizedAt ? new Date(s.finalizedAt).toLocaleString() : undefined}>Finalized</span>}</div></th>
            <td className="border-b border-zinc-800 px-3 text-right font-mono tabular-nums">{s.rows.length}</td><td className="border-b border-zinc-800 px-3 text-right font-mono tabular-nums">{s.loadCount}</td><td className="border-b border-zinc-800 px-3 text-right font-mono tabular-nums">{money.format(s.gross)}</td><td className="border-b border-zinc-800 px-3 text-right font-mono font-semibold tabular-nums">{money.format(s.pay)}</td><td className="border-b border-zinc-800 px-3 text-right text-xs text-zinc-300">{s.finalized ? s.finalizedBy || "Unknown" : s.dispatcherId && permissions.includes("dispatcher_pay.finalize") ? <button className={button} disabled={busy || s.rows.some(row => row.issues.length > 0)} onClick={event => { event.stopPropagation(); setStatementAction({ action: "finalize", statement: s }); setError(""); }}>Finalize</button> : "—"}</td>
          </tr>
          {s.sourceChanged && <tr><td colSpan={6} className="border-b border-amber-500/20 bg-amber-500/5 px-3 py-2 text-amber-400">Source values changed. Reopen to recalculate.</td></tr>}
          {open && <tr id={`dispatcher-${key}`} className="bg-zinc-950"><td colSpan={6} className="border-b border-zinc-700 bg-zinc-950 p-0">
        {!s.dispatcherId && <p className="px-3 py-2 text-amber-400">Assign a dispatcher in Gross Board.</p>}
        <div className="overflow-x-auto bg-zinc-950"><table className="w-full min-w-[960px] text-left text-[13px]">
          <thead className="bg-zinc-950 text-xs text-zinc-400"><tr>{["Driver / truck", "Gross", "Miles", "RPM", "Home days", "Gross tier", "RPM tier", "Rate", "Commission", ""].map((label, i) => <th key={i} className={`!bg-zinc-950 px-3 py-2 font-medium ${i > 0 && i < 9 ? "text-right" : ""}`}>{label}</th>)}</tr></thead>
          <tbody>{s.rows.map(row => { const href = `/gross-board?${new URLSearchParams({ driverId: row.driverId, weekStart: currentWeek })}`; const miles = row.creditedMiles ?? (row.gross === row.creditedGross ? row.miles : null); return <tr key={row.driverId} className={`border-t border-zinc-800/60 align-top ${permissions.includes("board.read") ? "cursor-pointer hover:bg-zinc-800/30" : ""}`} onClick={event => { if (!permissions.includes("board.read") || (event.target instanceof Element && event.target.closest("a,button,input,textarea,select,[tabindex]"))) return; if (event.ctrlKey || event.metaKey) window.open(href, "_blank", "noopener"); else router.push(href); }}>
            <td className="px-3 py-2"><div>{permissions.includes("board.read") ? <IntentLink href={href} className="hover:text-blue-300 hover:underline">{row.driverName}</IntentLink> : row.driverName}</div><div className="text-xs text-zinc-400">{row.truckUnits.join(", ") || "—"} · {row.loadCount} {row.loadCount === 1 ? "load" : "loads"}</div>{row.note && <div className="max-w-64 whitespace-pre-wrap text-xs text-zinc-400">{row.note}</div>}{row.issues.map(issue => <div key={issue} className="text-xs text-amber-400">{issue}</div>)}</td>
            <td className="whitespace-nowrap px-3 py-2 text-right font-mono tabular-nums">{money.format(row.creditedGross)}</td><td className="px-3 py-2 text-right font-mono tabular-nums">{miles === null ? "—" : number.format(miles)}</td><td className="px-3 py-2 text-right font-mono tabular-nums">{miles && miles > 0 ? `$${(row.creditedGross / miles).toFixed(3)}` : "—"}</td><td className="px-3 py-2 text-right font-mono tabular-nums">{row.homeDays}</td><td className="px-3 py-2 text-right"><TierMedal kind="gross" row={row} rule={s.rule} /></td><td className="px-3 py-2 text-right"><TierMedal kind="rpm" row={row} rule={s.rule} /></td><td className="px-3 py-2 text-right font-mono tabular-nums">{number.format(row.rate)}%</td><td className="px-3 py-2 text-right font-mono tabular-nums">{row.issues.length ? "Review" : money.format(row.pay)}</td>
            <td className="px-3 py-2">{permissions.includes("dispatcher_pay.edit") && <button aria-label={`Edit home days for ${row.driverName}`} className={button} disabled={busy || lockedDrivers.has(row.driverId)} title={lockedDrivers.has(row.driverId) ? "Reopen this driver's finalized dispatcher statements first" : row.autoHomeDays ? "Home days from Gross Board; edit settings or comment" : "Manual home days; edit or use Gross Board"} onClick={() => { setEditing(row); setHomeDays(String(row.homeDays)); setAutoHomeDays(!!row.autoHomeDays); setNote(row.note); setError(""); }}><Pencil className="h-4 w-4" /></button>}</td>
          </tr>; })}</tbody>
        </table></div>
          </td></tr>}
        </tbody>; })}
      </table></div>
    </>}
    {tab === "rules" && <div className="space-y-4">
      {rules.length === 0 && <p className="text-zinc-400">Loading commission rules…</p>}
      {selectedRules.map(version => <section key={version.id} className="ui-card space-y-3">
        <div className="flex flex-wrap items-center gap-3"><h2 className="font-semibold">Effective {version.effectiveWeek}</h2><span className="text-xs text-zinc-400">Revision {version.id} · saved {new Date(version.createdAt).toLocaleString()}</span>{permissions.includes("dispatcher_pay.rules") && <button className={`${button} ml-auto`} disabled={busy} onClick={() => editRule(version)}><Pencil className="h-4 w-4" />Edit / schedule rules</button>}</div>
        <p className="text-xs text-zinc-400">Home-day credit: {money.format(version.rules.homeDayCredit)} per day. Maximum promotion: {version.rules.maxTierPromotion} tier(s).</p>
        <div className="grid gap-4 md:grid-cols-3">{version.rules.tiers.map((tier, i) => <div key={i} className="rounded-lg border border-zinc-800 p-3"><h3 className="mb-2 font-medium">Gross {money.format(tier.minGross)}+</h3><table className="w-full"><thead><tr className="text-xs text-zinc-400"><th className="text-left font-normal">RPM at least</th><th className="text-right font-normal">Commission</th></tr></thead><tbody>{tier.brackets.map((b, j) => <tr key={j}><td>${b.minRPM.toFixed(2)}</td><td className="text-right">{number.format(b.percent)}%</td></tr>)}</tbody></table></div>)}</div>
      </section>)}
      {rules.length > selectedRules.length && <details className="ui-card"><summary className="cursor-pointer">Earlier revisions</summary>{rules.filter(v => !selectedRules.includes(v)).map(v => <p key={v.id} className="mt-2 text-xs text-zinc-400">Revision {v.id} · effective {v.effectiveWeek} · saved {new Date(v.createdAt).toLocaleString()}</p>)}</details>}
    </div>}
    {editing && <Modal title={`Weekly inputs · ${editing.driverName}`} isSaving={busy} submitLabel="Save" onClose={() => setEditing(null)} onSubmit={e => { e.preventDefault(); void mutate(() => saveDispatcherPayInput({ weekStart: currentWeek, driverId: editing.driverId, homeDays: autoHomeDays ? editing.boardHomeDays ?? 0 : Number(homeDays), autoHomeDays, note, version: editing.version }), () => setEditing(null)); }}>
      <div className="space-y-4">{modalError}<label className="flex items-center gap-2 text-xs"><input type="checkbox" checked={autoHomeDays} onChange={event => setAutoHomeDays(event.target.checked)} />Use Gross Board home days</label><label className="block text-xs">Home days<input className={controlClass} type="number" min="0" max="7" step="1" required disabled={autoHomeDays} value={autoHomeDays ? editing.boardHomeDays ?? 0 : homeDays} onChange={e => setHomeDays(e.target.value)} /></label><label className="block text-xs">Comment<textarea className={`${controlClass} min-h-20`} maxLength={1000} value={note} onChange={e => setNote(e.target.value)} /></label></div>
    </Modal>}
    {statementAction && <Modal title={`${statementAction.action === "finalize" ? "Finalize" : "Reopen"} · ${statementAction.statement.dispatcherName}`} isSaving={busy} submitLabel={statementAction.action === "finalize" ? "Finalize" : "Reopen"} onClose={() => setStatementAction(null)} onSubmit={e => { e.preventDefault(); void mutate(() => changeDispatcherPayStatement(statementAction.action, { weekStart: currentWeek, dispatcherId: statementAction.statement.dispatcherId, revision: statementAction.statement.revision, version: statementAction.statement.version }), () => setStatementAction(null)); }}>
      <div className="space-y-4">{modalError}<p>{weekLabel(currentWeek)} · {money.format(statementAction.statement.pay)}</p><p className="text-zinc-400">{statementAction.action === "finalize" ? "Save the reviewed inputs, commission rules and calculated amounts as a finalized statement." : "Reopening recalculates this statement using current Gross Board values, weekly inputs and effective rules."}</p></div>
    </Modal>}
    {statementMenu && <StatementMenu x={statementMenu.x} y={statementMenu.y} label="Reopen dispatcher" disabled={busy} onClose={closeStatementMenu} onSelect={() => { setStatementAction({ action: "reopen", statement: statementMenu.statement }); setError(""); }} />}
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
