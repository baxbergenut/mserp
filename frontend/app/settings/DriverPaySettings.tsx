"use client";

import { useEffect, useState } from "react";
import { fetchDriverPaySettings, saveDriverPaySettings } from "@/app/lib/api";
import type { DriverPaySettings as Settings } from "@/app/lib/types";
import { Field, controlClass } from "@/app/components/management/ManagementUI";

export function DriverPaySettings() {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [amount, setAmount] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  async function load() {
    setBusy(true); setError(""); setNotice("");
    try { const result = await fetchDriverPaySettings(); setSettings(result); setAmount(result.layoverDailyAmount); }
    catch (e) { setError(e instanceof Error ? e.message : "Could not load Driver Pay settings"); }
    finally { setBusy(false); }
  }
  useEffect(() => {
    let active = true;
    fetchDriverPaySettings().then(result => { if (active) { setSettings(result); setAmount(result.layoverDailyAmount); } }).catch(e => { if (active) setError(e instanceof Error ? e.message : "Could not load Driver Pay settings"); });
    return () => { active = false; };
  }, []);
  return <form className="ui-card max-w-lg space-y-4 p-4" onSubmit={async event => {
    event.preventDefault(); if (!settings || busy) return;
    setBusy(true); setError(""); setNotice("");
    try { const result = await saveDriverPaySettings({ ...settings, layoverDailyAmount: amount }); setSettings(result); setAmount(result.layoverDailyAmount); setNotice("Driver Pay settings saved."); }
    catch (e) { setError(e instanceof Error ? e.message : "Could not save Driver Pay settings"); }
    finally { setBusy(false); }
  }}>
    <h2 className="font-medium text-zinc-200">Shop layover bonus</h2>
    <p className="text-xs text-zinc-500">Added to Driver Pay for each day marked Shop in Gross Board. Applies to open weeks; finalized statements keep their saved amounts. Set to $0 to disable.</p>
    <Field label="Daily layover amount ($)"><input className={controlClass} aria-label="Daily layover amount ($)" type="number" min="0" max="999999999.99" step="0.01" required disabled={!settings || busy} value={amount} onChange={event => { setAmount(event.target.value); setNotice(""); }} /></Field>
    {!settings && !error && <p role="status" className="text-xs text-zinc-500">Loading Driver Pay settings…</p>}
    {error && <p role="alert" className="text-xs text-red-300">{error}</p>}
    {notice && <p role="status" className="text-xs text-emerald-300">{notice}</p>}
    <div className="flex gap-2"><button type="submit" className="ui-button-primary" disabled={!settings || busy || amount === settings.layoverDailyAmount}>{busy ? "Saving…" : "Save"}</button><button type="button" className="ui-button" disabled={busy} onClick={() => void load()}>Reload</button></div>
  </form>;
}
