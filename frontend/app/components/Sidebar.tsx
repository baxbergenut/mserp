"use client";

import { useState } from "react";
import Link from "@/app/components/AccessLink";
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
  Scale,
  Wrench,
} from "lucide-react";
import { useMaintenanceAccess } from "@/app/lib/maintenanceAccess";
import { usePermissions, pagePermission, canVisitPage } from "@/app/lib/access";
import { useTaskUpdates } from "./TaskUpdatesProvider";
import { useSidebarCounts } from "./useSidebarCounts";

const NAV_ITEMS = [
  { href: "/driver-board", label: "Status Board", icon: Truck },
  { href: "/gross-board", label: "Gross Board", icon: CalendarRange },
  { href: "/tasks", label: "Tasks", icon: ListChecks },
  { href: "/loads", label: "Loads", icon: Package },
  { href: "/fuel", label: "Fuel", icon: Fuel },
  { href: "/tolls", label: "Tolls", icon: Receipt },
  { href: "/weighmytruck", label: "Scale", icon: Scale },
  { href: "/fleet", label: "Fleet", icon: Truck, children: [
    { href: "/trucks", label: "Trucks", icon: Truck },
    { href: "/fleet/trailers", label: "Trailers", icon: Truck },
    { href: "/fleet/inventory", label: "Inventory", icon: Package },
    { href: "/maintenance", label: "Work orders", icon: Wrench },
    { href: "/fleet/service-programs", label: "Service programs", icon: CalendarRange },
  ] },
  { href: "/expenses", label: "Expenses", icon: WalletCards },
  { href: "/expenses/settings", label: "Expenses settings", icon: Settings },
  { href: "/accounting", label: "Accounting", icon: Landmark, children: [
    { href: "/accounting/driver-pay", label: "Driver Pay", icon: Banknote },
    { href: "/accounting/investor-pay", label: "Investor Pay", icon: Banknote },
    { href: "/accounting/escrow", label: "Escrow", icon: WalletCards },
    { href: "/accounting/driver-charges", label: "Recurring Charges", icon: Receipt },
    { href: "/accounting/dispatcher-pay", label: "Dispatcher Pay", icon: Headset },
  ] },
  { href: "/drivers", label: "Drivers", icon: Users },
  { href: "/investors", label: "Investors", icon: Landmark },
  { href: "/dispatchers", label: "Dispatchers and updaters", icon: Headset },
  { href: "/settings", label: "Settings", icon: Settings },
] as const;

export function Sidebar() {
  const { count } = useTaskUpdates();
  const alerts = useSidebarCounts();
  const permissions = usePermissions();
  const maintenanceAccess = useMaintenanceAccess();
  const pathname = usePathname();
  const [collapsed, setCollapsed] = useState(false);
  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>({});
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
          const children = "children" in item ? item.children.filter(child => canVisitPage(permissions,child.href) && (child.href === "/maintenance" ? maintenanceAccess?.canView || maintenanceAccess?.canCreate : child.href === "/fleet/service-programs" ? maintenanceAccess?.canView : true)) : [];
          if ("children" in item ? children.length === 0 : pagePermission(item.href) && !canVisitPage(permissions,item.href)) return null;
          const active = children.some(child => pathname === child.href || pathname.startsWith(child.href+"/")) ||
            pathname === item.href ||
            (!("exact" in item) && pathname.startsWith(item.href + "/"));
          const Icon = item.icon;
          const badge = item.href === "/tasks" ? { count, label: `${count} open tasks` }
            : item.href === "/fuel" ? { count: alerts.fuel, label: `${alerts.fuel} terminated drivers still in Relay` }
            : item.href === "/weighmytruck" ? { count: alerts.weighMyTruck, label: `${alerts.weighMyTruck} terminated drivers with confirmed or unresolved WeighMyTruck access` }
            : item.href === "/tolls" ? { count: alerts.tolls, label: `${alerts.tolls} toll transactions without a linked truck, posted since ${alerts.tollsFrom}` }
            : undefined;

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

            const groupExpanded = openGroups[item.href] ?? active;

            return (
              <div key={item.href}>
                <button
                  type="button"
                  onClick={() => setOpenGroups(groups => ({ ...groups, [item.href]: !groupExpanded }))}
                  className={`group flex w-full items-center gap-3 rounded-lg px-3 py-2 text-[13px] font-medium transition-all duration-150 ${
                    active
                      ? "text-accent"
                      : "text-zinc-500 hover:bg-zinc-800/40 hover:text-zinc-200"
                  }`}
                  aria-expanded={groupExpanded}
                >
                  <Icon className={`h-[18px] w-[18px] shrink-0 ${active ? "text-accent" : "text-zinc-500 group-hover:text-zinc-300"}`} />
                  <span className="flex-1 text-left">{item.label}</span>
                  <ChevronDown className={`h-3.5 w-3.5 transition-transform ${groupExpanded ? "rotate-180" : ""}`} />
                </button>
                {groupExpanded && (
                  <div className="mt-1 space-y-1 pl-5">
                    {children.map((child) => {
                      const childActive = pathname === child.href || pathname.startsWith(child.href + "/");
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
                group relative flex items-center gap-3 rounded-lg px-3 py-2 text-[13px] font-medium
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
              {badge && badge.count > 0 && <span aria-label={badge.label} title={badge.label}
                className={`rounded-full bg-red-600 px-1.5 text-[11px] font-semibold tabular-nums text-white ${collapsed ? "absolute -right-1 -top-1" : "ml-auto"}`}>{badge.count > 99 ? "99+" : badge.count}</span>}
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
