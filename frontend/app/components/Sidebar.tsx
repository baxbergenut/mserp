"use client";

import { useState, useSyncExternalStore } from "react";
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
  LayoutDashboard,
  MessageSquare,
} from "lucide-react";
import { useMaintenanceAccess } from "@/app/lib/maintenanceAccess";
import { usePermissions, canVisitPage, useAdministrator } from "@/app/lib/access";
import { useTaskUpdates } from "./TaskUpdatesProvider";
import { BrandLogo } from "./BrandLogo";

const NAV_ITEMS = [
  { href: "/dashboard", label: "Dashboard", icon: LayoutDashboard },
  { href: "/driver-board", label: "Status Board", icon: Truck },
  { href: "/gross-board", label: "Gross Board", icon: CalendarRange },
  { href: "/tasks", label: "Tasks", icon: ListChecks },
  { href: "/expenses", label: "Expenses", icon: WalletCards },
  { href: "/expenses/settings", label: "Expenses settings", icon: Settings },
  { href: "/loads", label: "Loads", icon: Package },
  { href: "/operating-costs", label: "Operating Costs", icon: Receipt, children: [
    { href: "/fuel", label: "Fuel", icon: Fuel },
    { href: "/tolls", label: "Tolls", icon: Receipt },
    { href: "/weighmytruck", label: "Scale", icon: Scale },
  ] },
  { href: "/fleet", label: "Fleet", icon: Truck, children: [
    { href: "/trucks", label: "Trucks", icon: Truck },
    { href: "/fleet/trailers", label: "Trailers", icon: Truck },
    { href: "/fleet/inventory", label: "Inventory", icon: Package },
    { href: "/fleet/weekly-pti", label: "Weekly PTI", icon: ListChecks },
    { href: "/fleet/service-programs", label: "Service programs", icon: CalendarRange },
  ] },
  { href: "/accounting", label: "Accounting", icon: Landmark, children: [
    { href: "/accounting/driver-pay", label: "Driver Pay", icon: Banknote },
    { href: "/accounting/investor-pay", label: "Investor Pay", icon: Banknote },
    { href: "/accounting/escrow", label: "Escrow", icon: WalletCards },
    { href: "/accounting/driver-charges", label: "Recurring Charges", icon: Receipt },
    { href: "/accounting/dispatcher-pay", label: "Dispatcher Pay", icon: Headset },
  ] },
  { href: "/hr", label: "HR", icon: Users, children: [
    { href: "/drivers", label: "Drivers", icon: Users },
    { href: "/dispatchers", label: "Dispatchers", icon: Headset },
    { href: "/updaters", label: "Updaters", icon: Headset },
    { href: "/investors", label: "Investors", icon: Landmark },
  ] },
  { href: "/feedback", label: "Feedback", icon: MessageSquare },
  { href: "/settings", label: "Settings", icon: Settings },
] as const;

type Badge = { count: number; label: string };

function NavBadge({ badge, collapsed = false }: { badge?: Badge; collapsed?: boolean }) {
  if (!badge || !(badge.count > 0)) return null;
  return <span aria-label={badge.label} title={badge.label}
    className={`rounded-full bg-red-600 px-1.5 text-[11px] font-semibold tabular-nums text-white ${collapsed ? "absolute -right-1 -top-1" : "ml-auto"}`}>{badge.count > 99 ? "99+" : badge.count}</span>;
}

function subscribeNarrowViewport(update: () => void) {
  const media = window.matchMedia("(max-width: 639px)");
  media.addEventListener("change", update);
  return () => media.removeEventListener("change", update);
}
const narrowViewport = () => window.matchMedia("(max-width: 639px)").matches;
const serverViewport = () => false;

export function Sidebar() {
  const { count, feedbackCount } = useTaskUpdates();
  const permissions = usePermissions();
  const administrator = useAdministrator();
  const maintenanceAccess = useMaintenanceAccess();
  const pathname = usePathname();
  const narrow = useSyncExternalStore(subscribeNarrowViewport, narrowViewport, serverViewport);
  const [collapsePreference, setCollapsed] = useState<boolean | null>(null);
  const collapsed = collapsePreference ?? narrow;
  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>({});
  const badgeFor = (href: string): Badge | undefined => !administrator && href === "/tasks" ? { count, label: feedbackCount ? `${count - feedbackCount} assigned tasks and ${feedbackCount} unread feedback updates` : `${count} assigned tasks` }
    : undefined;
  return (
    <aside
      className={`
        flex min-h-0 shrink-0 flex-col overflow-hidden border-r border-zinc-800/60 bg-sidebar-bg
        transition-[width] duration-200 ease-in-out
        ${collapsed ? "w-16" : "w-60"}
      `}
    >
      {/* ── Brand ── */}
      <div className="flex h-14 shrink-0 items-center overflow-hidden border-b border-zinc-800/40 px-4">
        <BrandLogo compact={collapsed} />
      </div>

      {/* ── Navigation ── */}
      <nav className="mt-4 flex min-h-0 flex-1 flex-col gap-1 overflow-x-hidden overflow-y-auto overscroll-contain px-2 pb-2 *:shrink-0">
        {NAV_ITEMS.map((item) => {
		  if (item.href === "/expenses/settings" && permissions.includes("expenses.read")) return null;
          const children = "children" in item ? item.children.filter(child => canVisitPage(permissions,child.href) && (child.href === "/fleet/service-programs" ? maintenanceAccess?.canView : true)) : [];
          if ("children" in item ? children.length === 0 : !canVisitPage(permissions,item.href,administrator)) return null;
          const active = children.some(child => pathname === child.href || pathname.startsWith(child.href+"/")) ||
            pathname === item.href ||
            (!("exact" in item) && pathname.startsWith(item.href + "/"));
          const Icon = item.icon;
          const childBadges = children.flatMap(child => {
            const childBadge = badgeFor(child.href);
            return childBadge && childBadge.count > 0 ? [childBadge] : [];
          });
          const badge = "children" in item ? (childBadges.length ? {
            count: childBadges.reduce((sum, childBadge) => sum + childBadge.count, 0),
            label: `${item.label}: ${childBadges.map(childBadge => childBadge.label).join("; ")}`,
          } : undefined) : badgeFor(item.href);

          if ("children" in item) {
            if (collapsed) {
              return (
                <Link
                  key={item.href}
                  href={children[0].href}
                  className={`group relative flex items-center justify-center rounded-lg py-2 text-[13px] font-medium transition-all duration-150 ${
                    active
                      ? "bg-accent/10 text-accent"
                      : "text-zinc-500 hover:bg-zinc-800/40 hover:text-zinc-200"
                  }`}
                  title={item.label}
                  aria-label={badge?.label ?? item.label}
                >
                  <Icon className={`h-[18px] w-[18px] ${active ? "text-accent" : "text-zinc-500 group-hover:text-zinc-300"}`} />
                  <NavBadge badge={badge} collapsed />
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
                  aria-label={badge?.label ?? item.label}
                >
                  <Icon className={`h-[18px] w-[18px] shrink-0 ${active ? "text-accent" : "text-zinc-500 group-hover:text-zinc-300"}`} />
                  <span className="flex-1 text-left">{item.label}</span>
                  <NavBadge badge={badge} />
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
                          <NavBadge badge={badgeFor(child.href)} />
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
              <NavBadge badge={badge} collapsed={collapsed} />
            </Link>
          );
        })}
      </nav>

      <div className="shrink-0 border-t border-zinc-800/40 p-2">
        <button
          onClick={() => setCollapsed(!collapsed)}
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
