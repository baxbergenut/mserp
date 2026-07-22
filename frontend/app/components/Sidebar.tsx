"use client";

import { useState } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  LayoutDashboard,
  Users,
  PanelLeftClose,
  PanelLeftOpen,
  Truck,
  Package,
  Headset,
  Receipt,
  Fuel,
} from "lucide-react";

const NAV_ITEMS = [
  { href: "/dashboard", label: "Dashboard", icon: LayoutDashboard },
  { href: "/loads", label: "Loads", icon: Package },
  { href: "/fuel", label: "Fuel", icon: Fuel },
  { href: "/tolls", label: "Tolls", icon: Receipt },
  { href: "/drivers", label: "Drivers", icon: Users },
  { href: "/trucks", label: "Trucks", icon: Truck },
  { href: "/dispatchers", label: "Dispatchers", icon: Headset },
] as const;

export function Sidebar() {
  const pathname = usePathname();
  const [collapsed, setCollapsed] = useState(false);

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
          const active =
            pathname === item.href || pathname.startsWith(item.href + "/");
          const Icon = item.icon;

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

      {/* ── Collapse toggle ── */}
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
