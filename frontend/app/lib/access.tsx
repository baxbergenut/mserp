"use client";

import { createContext, useContext } from "react";

export const PermissionsContext = createContext<string[]>([]);
export const usePermissions = () => useContext(PermissionsContext);

export function pagePermission(path: string): string {
  if (path.startsWith("/settings")) return "access.manage";
  if (path.startsWith("/accounting/driver-charges")) return "charges.read";
  if (path.startsWith("/accounting")) return "payroll.read";
  if (["/drivers", "/trucks", "/dispatchers", "/investors"].some(p => path === p || path.startsWith(p + "/"))) return "fleet.read";
  const resource = path.split("/")[1];
  return ({ loads: "loads.read", "gross-board": "board.read", fuel: "fuel.read", tolls: "tolls.read", expenses: "expenses.read", tasks: "tasks.read" } as Record<string,string>)[resource] ?? "";
}

export function firstAllowedPage(permissions: string[]): string {
  return ["/gross-board", "/loads", "/drivers", "/expenses", "/accounting/driver-pay", "/accounting/driver-charges", "/fuel", "/tolls", "/tasks", "/settings"].find(p => permissions.includes(pagePermission(p))) ?? "/access-denied";
}
