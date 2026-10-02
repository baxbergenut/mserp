"use client";

import { useEffect, useRef, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { Search, Plus, UserRound, X, LogOut, ChevronDown } from "lucide-react";
import { IntentLink } from "./IntentLink";
import { fetchDriversPage, fetchTrucksPage, fetchLoadsPage, fetchExpensesPage, fetchInvestorsPage, logout } from "@/app/lib/api";

type Result = { label: string; detail: string; href: string };
type Group = { name: string; results: Result[]; failed?: boolean };
const quickActions = [{ label: "Expense", path: "/expenses" }, { label: "Driver", path: "/drivers" }, { label: "Truck", path: "/trucks" }, { label: "Task", path: "/tasks" }];
const itemClass = "block rounded-md px-3 py-2 text-sm text-zinc-300 hover:bg-zinc-800 focus-visible:outline-2 focus-visible:outline-blue-400";

export function TopBar({ username }: { username: string }) {
  const pathname = usePathname();
  const router = useRouter();
  const [menu, setMenu] = useState<"search" | "new" | "account" | null>(null);
  const [createToken, setCreateToken] = useState(0);
  const [query, setQuery] = useState("");
  const [data, setData] = useState<{ query: string; groups: Group[] } | null>(null);
  const [signingOut, setSigningOut] = useState(false);
  const [error, setError] = useState("");
  const root = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const term = query.trim();
  const close = () => { setMenu(null); if (menu === "search") trigger.current?.focus(); };

  useEffect(() => {
    const key = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault(); setMenu("search");
      }
      if (event.key === "Escape") { setMenu(null); trigger.current?.focus(); }
    };
    const outside = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setMenu(null);
    };
    document.addEventListener("keydown", key); document.addEventListener("pointerdown", outside);
    return () => { document.removeEventListener("keydown", key); document.removeEventListener("pointerdown", outside); };
  }, []);
  useEffect(() => { if (menu === "search") input.current?.focus(); }, [menu]);
  useEffect(() => {
    if (menu !== "search" || term.length < 2) return;
    let cancelled = false;
    const timer = setTimeout(async () => {
      const page = { page: 1, pageSize: 5, search: term };
      const requests: Array<{ name: string; request: Promise<Result[]> }> = [
        { name: "Drivers", request: fetchDriversPage({ ...page, includeInactive: true }).then(r => r.items.map(d => ({ label: d.fullName, detail: [d.truckUnit && `Truck ${d.truckUnit}`, d.active ? "Active" : "Inactive"].filter(Boolean).join(" · "), href: `/drivers/detail?id=${encodeURIComponent(d.id)}` }))) },
        { name: "Trucks", request: fetchTrucksPage(page).then(r => r.items.map(t => ({ label: `Truck ${t.unitNumber}`, detail: t.driverName ?? t.status, href: `/trucks/detail?id=${encodeURIComponent(t.id)}` }))) },
        { name: "Loads", request: fetchLoadsPage(page).then(r => r.items.map(l => ({ label: l.LoadID || `DataTruck #${l.ID}`, detail: [l.DriverName, (l.PickupTime || l.PickupAppointmentTime)?.slice(0, 10)].filter(Boolean).join(" · "), href: `/loads?search=${encodeURIComponent(l.LoadID || String(l.ID))}` }))) },
        { name: "Expenses", request: fetchExpensesPage(page).then(r => r.items.map(e => ({ label: e.expenseType || e.category, detail: [e.expenseDate, e.referenceNumber, e.driverName].filter(Boolean).join(" · "), href: `/expenses?search=${encodeURIComponent(term)}` }))) },
        { name: "Investors", request: fetchInvestorsPage(page).then(r => r.items.map(i => ({ label: i.fullName, detail: `${i.trucks.length} trucks`, href: i.driverId ? `/drivers/detail?id=${encodeURIComponent(i.driverId)}` : `/investors?search=${encodeURIComponent(i.fullName)}` }))) },
      ];
      const responses = await Promise.allSettled(requests.map(r => r.request));
      if (!cancelled) setData({ query: term, groups: responses.map((r, i) => ({ name: requests[i].name, results: r.status === "fulfilled" ? r.value : [], failed: r.status === "rejected" })) });
    }, 250);
    return () => { cancelled = true; clearTimeout(timer); };
  }, [term, menu]);

  const title = pathname.split("/").filter(Boolean).map(part => part.split("-").map(word => word[0]?.toUpperCase() + word.slice(1)).join(" ")).join(" / ") || "Dashboard";
  const groups = data?.query === term ? data.groups : null;

  return <div ref={root} className="relative z-40 shrink-0 border-b border-zinc-800/60 bg-zinc-950">
    <header className="flex h-14 items-center gap-3 px-4 sm:px-6 xl:px-8" aria-label="Global navigation">
      <span className="hidden min-w-0 flex-1 truncate text-sm text-zinc-400 lg:block">{title}</span>
      <button ref={trigger} onClick={() => setMenu(menu === "search" ? null : "search")} aria-expanded={menu === "search"} aria-controls="global-search" className="flex min-w-0 flex-1 items-center gap-2 rounded-lg border border-zinc-800 bg-zinc-900/60 px-3 py-2 text-sm text-zinc-500 hover:border-zinc-600 lg:max-w-md"><Search className="h-4 w-4 shrink-0" /><span className="truncate">Search MSERP…</span><kbd className="ml-auto hidden whitespace-nowrap text-[11px] sm:block">Ctrl / ⌘ K</kbd></button>
      <button onClick={() => { setCreateToken(Date.now()); setMenu(menu === "new" ? null : "new"); }} aria-expanded={menu === "new"} aria-controls="quick-create" className="flex items-center gap-1 rounded-lg bg-blue-600 px-3 py-2 text-sm text-white hover:bg-blue-500"><Plus className="h-4 w-4" /><span className="hidden sm:inline">New</span></button>
      <button onClick={() => setMenu(menu === "account" ? null : "account")} aria-label={`Account: ${username}`} aria-expanded={menu === "account"} aria-controls="account-menu" className="flex items-center gap-2 rounded-lg p-2 text-zinc-400 hover:bg-zinc-800"><UserRound className="h-4 w-4" /><span className="hidden max-w-32 truncate text-sm xl:block">{username}</span><ChevronDown className="h-3 w-3" /></button>
    </header>
    {menu === "new" && <nav id="quick-create" aria-label="Create a record" className="absolute right-14 top-12 w-48 rounded-xl border border-zinc-800 bg-zinc-950 p-2 shadow-xl">{quickActions.map(action => <IntentLink key={action.path} href={`${action.path}?new=${createToken}`} onClick={close} className={itemClass}>New {action.label.toLowerCase()}</IntentLink>)}</nav>}
    {menu === "account" && <div id="account-menu" className="absolute right-4 top-12 w-56 rounded-xl border border-zinc-800 bg-zinc-950 p-2 shadow-xl"><p className="truncate px-3 py-2 text-sm text-zinc-400">Signed in as <span className="text-zinc-100">{username}</span></p><button disabled={signingOut} className={`${itemClass} flex w-full items-center gap-2`} onClick={async () => { setSigningOut(true); setError(""); try { await logout(); router.replace("/login"); } catch { setError("Could not sign out. Please try again."); } finally { setSigningOut(false); } }}><LogOut className="h-4 w-4" />{signingOut ? "Signing out…" : "Sign out"}</button>{error && <p role="alert" className="px-3 py-2 text-xs text-red-400">{error}</p>}</div>}
    {menu === "search" && <section id="global-search" aria-label="Global search" className="absolute left-2 right-2 top-12 mx-auto max-w-2xl overflow-hidden rounded-xl border border-zinc-700 bg-zinc-950 shadow-2xl">
      <div className="flex items-center gap-3 border-b border-zinc-800 px-4 py-3"><Search className="h-4 w-4 text-zinc-500" /><input ref={input} aria-label="Search records" value={query} onChange={e => setQuery(e.target.value)} placeholder="Driver, truck, load, expense or investor…" className="min-w-0 flex-1 bg-transparent text-sm text-zinc-100 outline-none" onKeyDown={e => { if (e.key === "ArrowDown") { e.preventDefault(); root.current?.querySelector<HTMLAnchorElement>("#global-search a")?.focus(); } }} /><button aria-label="Close search" onClick={close} className="p-1 text-zinc-400"><X className="h-4 w-4" /></button></div>
      <div className="max-h-[65dvh] overflow-auto p-3" aria-live="polite">
        {term.length < 2 ? <p className="p-3 text-sm text-zinc-500">Type at least two characters to search records.</p> : !groups ? <p role="status" className="p-3 text-sm text-zinc-500">Searching…</p> : <>
          {groups.map(group => <div key={group.name}>{(group.results.length > 0 || group.failed) && <h2 className="px-3 pb-1 pt-3 text-xs font-medium text-zinc-500">{group.name}</h2>}{group.failed && <p className="px-3 py-2 text-xs text-amber-400">Could not search {group.name.toLowerCase()}. Try closing and reopening search.</p>}{group.results.map((result, index) => <IntentLink key={`${result.href}:${index}`} href={result.href} onClick={close} className={itemClass}><span className="block text-zinc-200">{result.label}</span><span className="block text-xs text-zinc-500">{result.detail}</span></IntentLink>)}</div>)}
          {groups.every(g => !g.results.length && !g.failed) && <p className="p-3 text-sm text-zinc-500">No records found for “{term}”.</p>}
          <p className="px-3 pt-3 text-xs text-zinc-600">Up to five matches per category. Loads, expenses and independent investors open in their searchable lists.</p>
        </>}
      </div>
    </section>}
  </div>;
}
