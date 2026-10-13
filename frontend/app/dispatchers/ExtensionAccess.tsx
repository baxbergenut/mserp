"use client";

import { useEffect, useState } from "react";
import { fetchDispatcherExtension, saveDispatcherExtension } from "@/app/lib/api";
import type { DispatcherExtensionAccess } from "@/app/lib/types";
import { ErrorBanner } from "@/app/components/management/ManagementUI";

export function ExtensionAccess({ dispatcherId, active }: { dispatcherId: string; active: boolean }) {
  const [access, setAccess] = useState<DispatcherExtensionAccess | null>(null);
  const [label, setLabel] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [reload, setReload] = useState(0);
  useEffect(() => {
    let cancelled = false;
    fetchDispatcherExtension(dispatcherId).then(a => { if (!cancelled) { setAccess(a); setLabel(a.accountLabel); setError(""); } }).catch(e => { if (!cancelled) setError(e.message); });
    return () => { cancelled = true; };
  }, [dispatcherId, reload]);
  async function save(enabled: boolean) {
    if (!access) return;
    setBusy(true); setError(""); setCode("");
    try {
      const result = await saveDispatcherExtension(dispatcherId, { enabled, version: access.version, accountLabel: label });
      setCode(result.activationCode);
      setReload(n => n + 1);
    } catch (e) { setError(e instanceof Error ? e.message : "Could not update access"); }
    finally { setBusy(false); }
  }
  return <section className="ui-card space-y-3">
    <div className="flex flex-wrap items-center justify-between gap-2"><h2>Dispatcher Chrome extension</h2><span className="text-xs text-zinc-400">{!active ? "Dispatcher inactive" : access?.enabled ? "Access enabled" : "Access disabled"}</span></div>
    {error && <><ErrorBanner message={error} /><button className="ui-button" onClick={() => setReload(n => n + 1)}>Reload access</button></>}
    {!access ? <p className="text-zinc-400">Loading extension access…</p> : <>
      <label className="block max-w-md text-xs text-zinc-400">J.B. Hunt account reference (optional)
        <input className="mt-1 block w-full rounded-lg border border-zinc-700 bg-zinc-950 px-3 py-1.5 text-[13px] text-zinc-100" maxLength={254} value={label} onChange={e => setLabel(e.target.value)} disabled={busy} />
      </label>
      <p className="text-xs text-zinc-400">Reference only; this does not verify a J.B. Hunt login. Each code activates one browser for 30 days. A replacement code revokes the previous browser immediately.</p>
      <div className="flex flex-wrap gap-2">
        <button className="ui-button ui-button-primary" disabled={busy || !active} onClick={() => void save(true)}>{busy ? "Saving…" : access.enabled ? "Replace activation code" : "Enable and create code"}</button>
        {access.enabled && <button className="ui-button" disabled={busy} onClick={() => void save(false)}>Revoke access</button>}
      </div>
      {code && <div className="space-y-2 rounded-lg border border-blue-500/40 p-3" role="status"><p>Activation code — use within 10 minutes. Copy it now; it is shown only here.</p><input aria-label="Extension activation code" className="w-full rounded border border-zinc-700 bg-zinc-950 p-2 font-mono text-xs" readOnly value={code} onFocus={e => e.target.select()} /><p className="text-xs text-zinc-400">Paste into the extension’s Activate field on the dispatcher’s computer.</p></div>}
      {access.activatedAt && <p className="text-xs text-zinc-400">Activated {new Date(access.activatedAt).toLocaleString()} · Expires {access.tokenExpiresAt ? new Date(access.tokenExpiresAt).toLocaleDateString() : "—"}</p>}
    </>}
  </section>;
}
