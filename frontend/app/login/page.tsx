"use client";

import { FormEvent, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { LockKeyhole } from "lucide-react";
import { login } from "@/app/lib/api";
import { firstAllowedPage, pagePermission } from "@/app/lib/access";
import { controlClass } from "@/app/components/management/ManagementUI";

export default function LoginPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [trust, setTrust] = useState(false);
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setError(""); setSubmitting(true);
    try {
      const session = await login(email, password, trust);
      const next = searchParams.get("next");
      const allowed = next?.startsWith("/") && !next.startsWith("//") && !next.includes("\\") && next !== "/login" && session.user.permissions.includes(pagePermission(next.split("?")[0]));
      router.replace(allowed ? next! : firstAllowedPage(session.user.permissions));
      router.refresh();
    } catch (caught) { setError(caught instanceof Error ? caught.message : "Login failed"); }
    finally { setSubmitting(false); }
  }

  return <main className="relative flex min-h-full items-center justify-center overflow-hidden px-6 py-12">
    <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_50%_20%,rgba(59,130,246,0.12),transparent_38%)]" />
    <div className="relative w-full max-w-sm animate-fade-in">
      <div className="mb-8 text-center">
        <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-xl border border-blue-500/20 bg-blue-500/10"><LockKeyhole className="h-5 w-5 text-accent" /></div>
        <h1 className="text-2xl font-semibold tracking-tight text-zinc-50">Sign in to MSERP</h1>
        <p className="mt-2 text-sm text-zinc-500">Use your work email and password.</p>
      </div>
      <form onSubmit={handleSubmit} className="space-y-5 rounded-2xl border border-zinc-800 bg-card p-6 shadow-2xl shadow-black/25">
        <fieldset disabled={submitting} className="space-y-5 disabled:opacity-60">
          <label className="block space-y-2 text-sm text-zinc-300">Email or existing username<input name="email" autoComplete="username" autoFocus required maxLength={254} value={email} onChange={e => setEmail(e.target.value)} className={controlClass} /></label>
          <label className="block space-y-2 text-sm text-zinc-300">Password<input name="password" type="password" autoComplete="current-password" required maxLength={72} value={password} onChange={e => setPassword(e.target.value)} className={controlClass} /></label>
          <label className="flex items-start gap-3 text-sm text-zinc-300"><input type="checkbox" checked={trust} onChange={e => setTrust(e.target.checked)} className="mt-1 accent-blue-500" /><span>Trust this device for 30 days<span className="mt-1 block text-xs text-zinc-500">Keep me signed in on this browser. Use only on a device you control.</span></span></label>
        </fieldset>
        {error && <p className="rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2.5 text-sm text-red-300" role="alert">{error}</p>}
        <button type="submit" disabled={submitting} className="flex h-11 w-full items-center justify-center rounded-lg bg-accent text-sm font-semibold text-white hover:bg-accent-hover disabled:opacity-60">{submitting ? "Signing in…" : "Sign in"}</button>
        <p className="text-center text-xs leading-5 text-zinc-500">Accounts are created by your administrator. Contact them for access or a password reset.</p>
      </form>
    </div>
  </main>;
}
