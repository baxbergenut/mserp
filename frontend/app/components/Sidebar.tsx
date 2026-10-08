"use client";

import { useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  Banknote,
  ChevronDown,
  Users,
  PanelLeftClose,
  PanelLeftOpen,
  Truck,
  Package,
  Headset,
  Receipt,
  Fuel,
  Landmark,
  WalletCards,
  CalendarRange,
  ListChecks,
  Settings,
} from "lucide-react";
import { usePermissions, pagePermission } from "@/app/lib/access";

const NAV_ITEMS = [
  { href: "/driver-board", label: "Status Board", icon: Truck },
  { href: "/gross-board", label: "Gross Board", icon: CalendarRange },
  { href: "/loads", label: "Loads", icon: Package },
  { href: "/fuel", label: "Fuel", icon: Fuel },
  { href: "/tasks", label: "Tasks", icon: ListChecks },
  { href: "/tolls", label: "Tolls", icon: Receipt },
  { href: "/expenses", label: "Expenses & Charges", icon: WalletCards },
  { href: "/expenses/settings", label: "Expenses & Charges settings", icon: Settings },
  { href: "/accounting", label: "Accounting", icon: Landmark, children: [
    { href: "/accounting/driver-pay", label: "Driver Pay", icon: Banknote },
    { href: "/accounting/investor-pay", label: "Investor Pay", icon: Banknote },
    { href: "/accounting/escrow", label: "Escrow", icon: WalletCards },
    { href: "/accounting/driver-charges", label: "Recurring Charges", icon: Receipt },
    { href: "/accounting/dispatcher-pay", label: "Dispatcher Pay", icon: Headset },
  ] },
  { href: "/drivers", label: "Drivers", icon: Users },
  { href: "/investors", label: "Investors", icon: Landmark },
  { href: "/trucks", label: "Trucks", icon: Truck },
  { href: "/dispatchers", label: "Dispatchers and updaters", icon: Headset },
  { href: "/settings", label: "Settings", icon: Settings },
] as const;

export function Sidebar() {
  const permissions = usePermissions();
  const pathname = usePathname();
  const [collapsed, setCollapsed] = useState(false);
  const [accountingOpen, setAccountingOpen] = useState(
    pathname.startsWith("/accounting"),
  );
  return (
    <aside
      className={`
        flex flex-col border-r border-zinc-800/60 bg-sidebar-bg
        transition-[width] duration-200 ease-in-out
        ${collapsed ? "w-16" : "w-60"}
      `}
    >
      {/* ── Brand ── */}
      <div className="flex h-14 items-center gap-2.5 border-b border-zinc-800/40 px-4">
        <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-accent/10">
          <span className="text-xs font-bold text-accent">M</span>
        </div>
        {!collapsed && (
          <span className="text-sm font-semibold tracking-wide text-zinc-100">
            MSERP
          </span>
        )}
      </div>

      {/* ── Navigation ── */}
      <nav className="mt-4 flex flex-1 flex-col gap-1 px-2">
        {NAV_ITEMS.map((item) => {
		  if (item.href === "/expenses/settings" && permissions.includes("expenses.read")) return null;
          const children = "children" in item ? item.children.filter(child => permissions.includes(pagePermission(child.href))) : [];
          if ("children" in item ? children.length === 0 : pagePermission(item.href) && !permissions.includes(pagePermission(item.href))) return null;
          const active =
            pathname === item.href ||
            (!("exact" in item) && pathname.startsWith(item.href + "/"));
          const Icon = item.icon;

          if ("children" in item) {
            if (collapsed) {
              return (
                <Link
                  key={item.href}
                  href={children[0].href}
                  className={`group flex items-center justify-center rounded-lg py-2 text-[13px] font-medium transition-all duration-150 ${
                    active
                      ? "bg-accent/10 text-accent"
                      : "text-zinc-500 hover:bg-zinc-800/40 hover:text-zinc-200"
                  }`}
                  title={item.label}
                >
                  <Icon className={`h-[18px] w-[18px] ${active ? "text-accent" : "text-zinc-500 group-hover:text-zinc-300"}`} />
                </Link>
              );
            }

            const accountingExpanded = accountingOpen || active;

            return (
              <div key={item.href}>
                <button
                  type="button"
                  onClick={() => setAccountingOpen((open) => !open)}
                  className={`group flex w-full items-center gap-3 rounded-lg px-3 py-2 text-[13px] font-medium transition-all duration-150 ${
                    active
                      ? "text-accent"
                      : "text-zinc-500 hover:bg-zinc-800/40 hover:text-zinc-200"
                  }`}
                  aria-expanded={accountingExpanded}
                >
                  <Icon className={`h-[18px] w-[18px] shrink-0 ${active ? "text-accent" : "text-zinc-500 group-hover:text-zinc-300"}`} />
                  <span className="flex-1 text-left">{item.label}</span>
                  <ChevronDown className={`h-3.5 w-3.5 transition-transform ${accountingExpanded ? "rotate-180" : ""}`} />
                </button>
                {accountingExpanded && (
                  <div className="mt-1 space-y-1 pl-5">
                    {children.map((child) => {
                      const childActive = pathname === child.href;
                      const ChildIcon = child.icon;
                      return (
                        <Link
                          key={child.href}
                          href={child.href}
                          className={`group flex items-center gap-2.5 rounded-lg px-3 py-1.5 text-[12px] font-medium transition-colors ${
                            childActive
                              ? "bg-accent/10 text-accent"
                              : "text-zinc-600 hover:bg-zinc-800/40 hover:text-zinc-300"
                          }`}
                        >
                          <ChildIcon className="h-3.5 w-3.5 shrink-0" />
                          <span>{child.label}</span>
                        </Link>
                      );
                    })}
                  </div>
                )}
              </div>
            );
          }

          return (
            <Link
              key={item.href}
              href={item.href}
              className={`
                group flex items-center gap-3 rounded-lg px-3 py-2 text-[13px] font-medium
                transition-all duration-150
                ${
                  active
                    ? "bg-accent/10 text-accent"
                    : "text-zinc-500 hover:bg-zinc-800/40 hover:text-zinc-200"
                }
                ${collapsed ? "justify-center px-0" : ""}
              `}
              title={collapsed ? item.label : undefined}
            >
              <Icon
                className={`h-[18px] w-[18px] shrink-0 ${
                  active
                    ? "text-accent"
                    : "text-zinc-500 group-hover:text-zinc-300"
                }`}
              />
              {!collapsed && <span>{item.label}</span>}
            </Link>
          );
        })}
      </nav>

      <div className="border-t border-zinc-800/40 p-2">
        <button
          onClick={() => setCollapsed((c) => !c)}
          className={`
            flex w-full items-center gap-3 rounded-lg px-3 py-2 text-[13px]
            text-zinc-600 transition-colors hover:bg-zinc-800/40 hover:text-zinc-300
            ${collapsed ? "justify-center px-0" : ""}
          `}
          aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
        >
          {collapsed ? (
            <PanelLeftOpen className="h-[18px] w-[18px]" />
          ) : (
            <>
              <PanelLeftClose className="h-[18px] w-[18px]" />
              <span>Collapse</span>
            </>
          )}
        </button>
      </div>
    </aside>
  );
}
