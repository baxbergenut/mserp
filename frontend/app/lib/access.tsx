"use client";

import { createContext, useContext } from "react";
import type { ExpenseCategoryAccess } from "./types";

export const PermissionsContext = createContext<string[]>([]);
export const usePermissions = () => useContext(PermissionsContext);
export const ExpenseCategoryAccessContext = createContext<ExpenseCategoryAccess[]>([]);
export const useExpenseCategoryAccess = () => useContext(ExpenseCategoryAccessContext);

export function pagePermission(path: string): string {
  if (path.startsWith("/settings")) return "access.manage";
  if (path.startsWith("/expenses/settings")) return "expense_settings.manage";
  if (path.startsWith("/accounting/escrow")) return "escrow.read";
  if (path.startsWith("/accounting/driver-charges")) return "charges.read";
  if (path.startsWith("/accounting")) return "payroll.read";
  if (["/drivers", "/trucks", "/dispatchers", "/investors"].some(p => path === p || path.startsWith(p + "/"))) return "fleet.read";
  const resource = path.split("/")[1];
  return ({ loads: "loads.read", "gross-board": "board.read", "driver-board": "driver_board.read", fuel: "fuel.read", tolls: "tolls.read", expenses: "expenses.read", tasks: "tasks.read" } as Record<string,string>)[resource] ?? "";
}

export function firstAllowedPage(permissions: string[]): string {
  return ["/gross-board", "/driver-board", "/loads", "/drivers", "/expenses", "/expenses/settings", "/accounting/driver-pay", "/accounting/escrow", "/accounting/driver-charges", "/fuel", "/tolls", "/tasks", "/settings"].find(p => permissions.includes(pagePermission(p))) ?? "/access-denied";
}
