"use client";

import { FormEvent, useState } from "react";
import { useRouter } from "next/navigation";
import { changePassword } from "@/app/lib/api";
import { Field, controlClass } from "@/app/components/management/ManagementUI";
import { PageHeader } from "@/app/components/PageHeader";

export default function AccountPage() {
  const router = useRouter();
  const [current, setCurrent] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(event: FormEvent) {
    event.preventDefault(); setError("");
    if (password !== confirm) { setError("Passwords do not match."); return; }
    setBusy(true);
    try { await changePassword(current, password); router.replace("/login"); }
    catch (e) { setError(e instanceof Error ? e.message : "Could not change password"); }
    finally { setBusy(false); }
  }
  return <div className="clear-both pt-4">
    <PageHeader><h1 className="text-lg font-semibold text-zinc-100">Change password</h1></PageHeader>
    <form className="max-w-lg space-y-5 rounded-xl border border-zinc-800 bg-zinc-900/30 p-5" onSubmit={submit}>
      <p className="text-sm text-zinc-400">Use at least 12 characters (maximum 72 bytes). Changing your password signs you out on every device, including this one.</p>
      <fieldset disabled={busy} className="grid gap-4">
        <Field label="Current password"><input className={controlClass} type="password" autoComplete="current-password" required value={current} onChange={e => setCurrent(e.target.value)} /></Field>
        <Field label="New password"><input className={controlClass} type="password" autoComplete="new-password" minLength={12} maxLength={72} required value={password} onChange={e => setPassword(e.target.value)} /></Field>
        <Field label="Confirm new password"><input className={controlClass} type="password" autoComplete="new-password" required value={confirm} onChange={e => setConfirm(e.target.value)} /></Field>
      </fieldset>
      {error && <p role="alert" className="text-sm text-red-300">{error}</p>}
      <button disabled={busy} className="rounded-lg bg-blue-600 px-4 py-2 text-sm text-white hover:bg-blue-500 disabled:opacity-50">{busy ? "Saving…" : "Change password"}</button>
    </form>
  </div>;
}
