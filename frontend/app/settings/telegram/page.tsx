"use client";

import { useCallback, useEffect, useState } from "react";
import { Bot, ExternalLink, Link2, RefreshCw, ShieldCheck, Unlink } from "lucide-react";
import {
  createTelegramLink,
  fetchAssistantAudit,
  fetchTelegramManagers,
  revokeTelegramManagerLink,
  setTelegramManagerApproval,
} from "@/app/lib/api";
import type { AssistantAuditEntry, TelegramManager } from "@/app/lib/types";

function formatDate(value: string | null) {
  return value ? new Date(value).toLocaleString() : "—";
}

export default function TelegramSettingsPage() {
  const [managers, setManagers] = useState<TelegramManager[]>([]);
  const [audit, setAudit] = useState<AssistantAuditEntry[]>([]);
  const [link, setLink] = useState<{ url: string; expiresAt: string } | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [managerRows, auditRows] = await Promise.all([
        fetchTelegramManagers(), fetchAssistantAudit(),
      ]);
      setManagers(managerRows);
      setAudit(auditRows);
      setError("");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Telegram settings could not be loaded");
    } finally { setLoading(false); }
  }, []);

  useEffect(() => {
    const timeout = window.setTimeout(() => void load(), 0);
    return () => window.clearTimeout(timeout);
  }, [load]);

  async function changeApproval(manager: TelegramManager) {
    setBusy(manager.userId); setError("");
    try { await setTelegramManagerApproval(manager.userId, !manager.approved); await load(); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "Approval could not be changed"); }
    finally { setBusy(""); }
  }

  async function revokeLink(manager: TelegramManager) {
    setBusy(`link-${manager.userId}`); setError("");
    try { await revokeTelegramManagerLink(manager.userId); await load(); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "Link could not be revoked"); }
    finally { setBusy(""); }
  }

  async function generateLink() {
    setBusy("generate"); setError("");
    try { setLink(await createTelegramLink()); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "Link could not be generated"); }
    finally { setBusy(""); }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-2"><Bot className="h-5 w-5 text-accent" /><h1 className="text-xl font-semibold text-zinc-100">Telegram Bot</h1></div>
          <p className="mt-1 text-sm text-zinc-500">Approve managers, link private Telegram accounts, and review assistant activity.</p>
        </div>
        <button onClick={() => void generateLink()} disabled={busy === "generate"} className="flex items-center gap-2 rounded-lg bg-accent px-3 py-2 text-sm font-medium text-white disabled:opacity-50">
          <Link2 className="h-4 w-4" />{busy === "generate" ? "Generating…" : "Link my Telegram"}
        </button>
      </div>

      {error && <div className="rounded-lg border border-red-900/60 bg-red-950/30 px-4 py-3 text-sm text-red-300">{error}</div>}
      {link && <div className="rounded-xl border border-blue-800/50 bg-blue-950/20 p-4">
        <p className="text-sm font-medium text-zinc-100">One-time link generated</p>
        <p className="mt-1 text-xs text-zinc-500">Expires {formatDate(link.expiresAt)} and becomes invalid after first use.</p>
        <a href={link.url} target="_blank" rel="noreferrer" className="mt-3 inline-flex items-center gap-2 rounded-lg border border-blue-700/60 px-3 py-2 text-sm text-blue-300 hover:bg-blue-900/20">Open Telegram <ExternalLink className="h-4 w-4" /></a>
      </div>}

      <section className="overflow-hidden rounded-xl border border-zinc-800 bg-zinc-950/40">
        <div className="flex items-center justify-between border-b border-zinc-800 px-4 py-3"><div className="flex items-center gap-2 text-sm font-medium text-zinc-200"><ShieldCheck className="h-4 w-4 text-accent" />Manager access</div><button onClick={() => void load()} className="text-zinc-500 hover:text-zinc-200" aria-label="Refresh"><RefreshCw className="h-4 w-4" /></button></div>
        <div className="overflow-x-auto"><table className="w-full min-w-[760px] text-left text-sm"><thead className="bg-zinc-900/60 text-xs text-zinc-500"><tr><th className="px-4 py-3">ERP user</th><th className="px-4 py-3">Approval</th><th className="px-4 py-3">Telegram</th><th className="px-4 py-3">Expires</th><th className="px-4 py-3 text-right">Actions</th></tr></thead><tbody className="divide-y divide-zinc-800/70">
          {loading ? <tr><td colSpan={5} className="px-4 py-8 text-center text-zinc-500">Loading…</td></tr> : managers.map((manager) => <tr key={manager.userId}>
            <td className="px-4 py-3 text-zinc-200">{manager.username}{!manager.active && <span className="ml-2 text-xs text-red-400">inactive</span>}</td>
            <td className="px-4 py-3"><span className={manager.approved ? "text-emerald-400" : "text-zinc-600"}>{manager.approved ? "Approved" : "Not approved"}</span></td>
            <td className="px-4 py-3 text-zinc-400">{manager.telegramUsername ? `@${manager.telegramUsername}` : manager.displayName ?? "Not linked"}</td>
            <td className="px-4 py-3 text-zinc-500">{formatDate(manager.linkExpiresAt)}</td>
            <td className="px-4 py-3"><div className="flex justify-end gap-2"><button disabled={busy === manager.userId} onClick={() => void changeApproval(manager)} className="rounded-md border border-zinc-700 px-2.5 py-1.5 text-xs text-zinc-300 disabled:opacity-50">{manager.approved ? "Revoke access" : "Approve"}</button>{manager.telegramUserId && <button disabled={busy === `link-${manager.userId}`} onClick={() => void revokeLink(manager)} className="rounded-md border border-zinc-700 p-1.5 text-zinc-400 hover:text-red-300" title="Unlink Telegram"><Unlink className="h-4 w-4" /></button>}</div></td>
          </tr>)}</tbody></table></div>
      </section>

      <section className="overflow-hidden rounded-xl border border-zinc-800 bg-zinc-950/40">
        <div className="border-b border-zinc-800 px-4 py-3 text-sm font-medium text-zinc-200">Recent assistant activity</div>
        <div className="divide-y divide-zinc-800/70">{audit.length === 0 ? <p className="px-4 py-8 text-center text-sm text-zinc-600">No assistant activity yet.</p> : audit.map((entry) => <div key={entry.id} className="grid gap-2 px-4 py-3 md:grid-cols-[160px_140px_1fr]"><div className="text-xs text-zinc-600">{formatDate(entry.createdAt)}</div><div className="text-xs text-zinc-400">{entry.username ?? "Removed user"}<span className={`ml-2 ${entry.outcome === "success" ? "text-emerald-500" : "text-red-400"}`}>{entry.outcome}</span></div><div className="truncate text-sm text-zinc-300" title={entry.prompt ?? entry.eventType}>{entry.prompt ?? entry.eventType}{entry.errorMessage && <span className="ml-2 text-red-400">{entry.errorMessage}</span>}</div></div>)}</div>
      </section>
    </div>
  );
}
