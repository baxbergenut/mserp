"use client";

import { useEffect, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import type { AuthSession } from "@/app/lib/types";
import { fetchAuthSession } from "@/app/lib/api";
import { Sidebar } from "./Sidebar";

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const router = useRouter();
  const [session, setSession] = useState<AuthSession | null>(null);
  const [authenticatedPath, setAuthenticatedPath] = useState<string | null>(null);
  const isLogin = pathname === "/login";
  const query = searchParams.toString();

  useEffect(() => {
    let active = true;
    if (isLogin) {
      return () => {
        active = false;
      };
    }

    fetchAuthSession()
      .then((value) => {
        if (active) {
          setSession(value);
          setAuthenticatedPath(pathname);
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
  }, [isLogin, pathname, query, router]);

  if (isLogin) return <>{children}</>;

  if (!session || authenticatedPath !== pathname) {
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
      <main className="flex-1 overflow-auto">
        <div className="mx-auto max-w-7xl px-6 py-6">{children}</div>
      </main>
    </div>
  );
}
