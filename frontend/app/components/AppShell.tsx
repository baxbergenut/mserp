"use client";

import { useEffect, useRef, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import type { AuthSession } from "@/app/lib/types";
import { APIError, fetchAuthSession } from "@/app/lib/api";
import { PageNavigation } from "./PageNavigation";
import { Sidebar } from "./Sidebar";
import { TaskUpdatesProvider } from "./TaskUpdatesProvider";
import { TopBar } from "./TopBar";
import { ThemeProvider } from "./ThemeProvider";
import { PageHeaderProvider } from "./PageHeader";
import { BirthdayGreeting } from "./BirthdayGreeting";
import Link from "@/app/components/AccessLink";
import { AdministratorContext, PermissionCatalogContext, CurrentUserIDContext, ExpenseCategoryAccessContext, PermissionsContext, canVisitPage, firstAllowedPage } from "@/app/lib/access";

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
  const priorAccess = useRef("");
  const [sessionError, setSessionError] = useState("");
  const [accessNotice,setAccessNotice]=useState("");
  const accessKey=session ? JSON.stringify([session.user.id,session.user.administrator,session.user.accessRevision,session.user.permissions,session.user.expenseCategoryAccess]) : "";

  useEffect(() => {
    let active=true, running=false;
    const refresh=async()=>{
      if(running)return;running=true;
      try { const value=await fetchAuthSession(); if(active){
        const signature=JSON.stringify([value.user.id,value.user.administrator,value.user.accessRevision,value.user.permissions,value.user.expenseCategoryAccess]);
        if(priorAccess.current && priorAccess.current!==signature)setAccessNotice("Your access changed. This view was reloaded and unsaved edits were discarded.");
        priorAccess.current=signature;setSession(value);setSessionError("");
      } } catch(error){
        if(active && error instanceof APIError && error.status===401){setSession(null);const next=query ? pathname+"?"+query : pathname;router.replace("/login?next="+encodeURIComponent(next));}
      if(active)setSessionError("Could not refresh access. Retrying when you return to this window.");
      } finally {running=false;}
    };
    void refresh();
    const visible=()=>{if(document.visibilityState==="visible")void refresh();};
    const timer=setInterval(visible,30000);
    window.addEventListener("focus",visible);document.addEventListener("visibilitychange",visible);window.addEventListener("mserp:access-invalidated",visible);
    return ()=>{active=false;clearInterval(timer);window.removeEventListener("focus",visible);document.removeEventListener("visibilitychange",visible);window.removeEventListener("mserp:access-invalidated",visible);};
  },[pathname,query,router]);

  if (!session) {
    return (
      <div className="flex h-full items-center justify-center" role="status">
        <div className="flex items-center gap-3 text-sm text-zinc-500">
          <span className="h-4 w-4 animate-spin rounded-full border-2 border-zinc-700 border-t-accent" />
          {sessionError || "Verifying session…"}
        </div>
      </div>
    );
  }

  return (
    <ThemeProvider key={session.user.id} initialTheme={session.user.theme}>
    <AdministratorContext.Provider value={session.user.administrator === true}>
    <CurrentUserIDContext.Provider value={session.user.id}>
    <PermissionCatalogContext.Provider value={session.permissionCatalog ?? []}>
    <PermissionsContext.Provider value={session.user.permissions}>
    <ExpenseCategoryAccessContext.Provider value={session.user.expenseCategoryAccess ?? []}>
    <TaskUpdatesProvider key={accessKey}>
    <div className="mserp-ui flex h-full">
      <Sidebar />
      <BirthdayGreeting key={session.user.id} userId={session.user.id} />
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <PageHeaderProvider key={`${pathname}?${query}`}>
        <TopBar key={pathname + query} username={session.user.username} />
        {accessNotice && <p role="status" className="px-6 py-2 text-xs text-amber-300">{accessNotice}<button className="ml-3 underline" onClick={()=>setAccessNotice("")}>Dismiss</button></p>}
        <PageNavigation key={`${accessKey}:${pathname}?${query}`} userId={session.user.id} url={query ? `${pathname}?${query}` : pathname}>{canVisitPage(session.user.permissions,pathname,session.user.administrator) ? children : <div className="p-8"><h1 className="text-lg font-semibold">Access restricted</h1><p className="mt-2 text-sm text-zinc-400">Your role does not have access to this page.</p><Link className="mt-4 inline-block text-blue-400" href={firstAllowedPage(session.user.permissions,session.user.administrator)}>Open an available page</Link></div>}</PageNavigation>
        </PageHeaderProvider>
      </div>
    </div>
    </TaskUpdatesProvider>
    </ExpenseCategoryAccessContext.Provider>
    </PermissionsContext.Provider>
    </PermissionCatalogContext.Provider>
    </CurrentUserIDContext.Provider>
    </AdministratorContext.Provider>
    </ThemeProvider>
  );
}
