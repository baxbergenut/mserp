"use client";

import { useEffect, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import type { AuthSession } from "@/app/lib/types";
import { fetchAuthSession } from "@/app/lib/api";
import { PageNavigation } from "./PageNavigation";
import { Sidebar } from "./Sidebar";
import { TopBar } from "./TopBar";

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  return pathname === "/login" ? <>{children}</> : <AuthenticatedShell>{children}</AuthenticatedShell>;
}

function AuthenticatedShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const router = useRouter();
  const [session, setSession] = useState<AuthSession | null>(null);
  const query = searchParams.toString();

  useEffect(() => {
    let active = true;
    fetchAuthSession()
      .then((value) => {
        if (active) {
          setSession(value);
        }
      })
      .catch(() => {
        if (!active) return;
        const next = query ? `${pathname}?${query}` : pathname;
        router.replace(`/login?next=${encodeURIComponent(next)}`);
      });

    return () => {
      active = false;
    };
  }, [pathname, query, router]);

  if (!session) {
    return (
      <div className="flex h-full items-center justify-center" role="status">
        <div className="flex items-center gap-3 text-sm text-zinc-500">
          <span className="h-4 w-4 animate-spin rounded-full border-2 border-zinc-700 border-t-accent" />
          Verifying session…
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-full">
      <Sidebar username={session.user.username} />
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <TopBar key={pathname + query} username={session.user.username} />
        <PageNavigation key={`${pathname}?${query}`} userId={session.user.id} url={query ? `${pathname}?${query}` : pathname}>{children}</PageNavigation>
      </div>
    </div>
  );
}
