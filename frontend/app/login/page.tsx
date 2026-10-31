"use client";

import { FormEvent, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { BrandLogo } from "@/app/components/BrandLogo";
import { login } from "@/app/lib/api";
import { firstAllowedPage, canVisitPage } from "@/app/lib/access";
import { controlClass } from "@/app/components/management/ManagementUI";

export default function LoginPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setError(""); setSubmitting(true);
    try {
      const session = await login(email, password);
      const next = searchParams.get("next");
      const allowed = next?.startsWith("/") && !next.startsWith("//") && !next.includes("\\") && next.split("?")[0] !== "/login" && canVisitPage(session.user.permissions, next.split("?")[0], session.user.administrator);
      router.replace(allowed ? next! : firstAllowedPage(session.user.permissions,session.user.administrator));
      router.refresh();
    } catch (caught) { setError(caught instanceof Error ? caught.message : "Login failed"); }
    finally { setSubmitting(false); }
  }

  return <main className="mserp-ui flex min-h-full items-center justify-center px-6 py-12">
    <div className="w-full max-w-sm">
      <div className="mb-8 text-center">
        <div className="flex justify-center"><BrandLogo className="w-56" /></div>
      </div>
      <form onSubmit={handleSubmit} className="space-y-4 rounded-lg border border-zinc-800 bg-card p-6">
        <fieldset disabled={submitting} className="space-y-4 disabled:opacity-60">
          <input name="email" aria-label="Email" placeholder="Email" autoComplete="username" autoFocus required maxLength={254} value={email} onChange={e => setEmail(e.target.value)} className={controlClass} />
          <input name="password" aria-label="Password" placeholder="Password" type="password" autoComplete="current-password" required maxLength={72} value={password} onChange={e => setPassword(e.target.value)} className={controlClass} />
        </fieldset>
        {error && <p className="rounded-lg border border-red-500/20 bg-red-500/10 px-3 py-2.5 text-sm text-red-300" role="alert">{error}</p>}
        <button type="submit" disabled={submitting} className="ui-button ui-button-primary h-11 w-full">{submitting ? "Signing in…" : "Sign in"}</button>
      </form>
    </div>
  </main>;
}
