"use client";

import { createContext, useContext, useMemo } from "react";
import type { ExpenseCategoryAccess, PermissionDefinition } from "./types";

export const CurrentUserIDContext = createContext("");
export const AdministratorContext = createContext(false);
export const useAdministrator = () => useContext(AdministratorContext);
export const PermissionsContext = createContext<string[]>([]);
export const PermissionCatalogContext = createContext<PermissionDefinition[]>([]);
export const usePermissions = () => {
 const permissions=useContext(PermissionsContext), catalog=useContext(PermissionCatalogContext);
 return useMemo(()=>catalog.length ? permissions.filter(key=>{const definition=catalog.find(p=>p.key===key);return definition ? definition.requires.every(required=>permissions.includes(required)) : key==="expenses.read" || key==="expenses.write";}) : permissions,[permissions,catalog]);
};
export const ExpenseCategoryAccessContext = createContext<ExpenseCategoryAccess[]>([]);
export const useExpenseCategoryAccess = () => useContext(ExpenseCategoryAccessContext);

export function pagePermission(path: string): string {
  if (path.startsWith("/fleet/weekly-pti")) return "pti.read";
  if (path.startsWith("/fleet/inventory")) return "inventory.read";
  if (path.startsWith("/fleet/trailers")) return "trailers.read";
  if (path === "/settings") return "";
  if (path === "/weighmytruck") return "weighmytruck.read";
  if (path.startsWith("/settings/")) return "access.manage";
  if (path.startsWith("/expenses/settings")) return "expense_settings.manage";
  if (path.startsWith("/accounting/escrow")) return "escrow.read";
  if (path.startsWith("/accounting/driver-charges")) return "charges.read";
  if (path.startsWith("/accounting/driver-pay")) return "driver_pay.read";
  if (path.startsWith("/accounting/investor-pay")) return "investor_pay.read";
  if (path.startsWith("/accounting/dispatcher-pay")) return "dispatcher_pay.read";
  if (["drivers", "trucks", "dispatchers", "updaters", "investors"].includes(path.split("/")[1])) return `${path.split("/")[1]}.read`;
  const resource = path.split("/")[1];
  return ({ loads: "loads.read", "gross-board": "board.read", "driver-board": "driver_board.read", fuel: "fuel.read", tolls: "tolls.read", expenses: "expenses.read", maintenance: "expenses.read", fleet: "expenses.read", tasks: "tasks.read" } as Record<string,string>)[resource] ?? "";
}

export function firstAllowedPage(permissions: string[], administrator = false): string {
  if (administrator) return "/dashboard";
  return ["/gross-board", "/driver-board", "/loads", "/drivers", "/trucks", "/fleet/inventory", "/fleet/trailers", "/dispatchers", "/updaters", "/investors", "/accounting/investor-pay", "/accounting/dispatcher-pay", "/weighmytruck", "/expenses", "/expenses/settings", "/accounting/driver-pay", "/accounting/escrow", "/accounting/driver-charges", "/fuel", "/tolls", "/tasks", "/settings"].find(p => canVisitPage(permissions,p)) ?? "/access-denied";
}

export function canVisitPage(permissions: string[], path: string, administrator = false) { if (path === "/dashboard" || path.startsWith("/dashboard/")) return administrator; const required=pagePermission(path); return !required || permissions.includes(required); }
export function canCreateDriver(permissions: string[]) { return ["drivers.create","drivers.private.edit","driver_compensation.edit","escrow.create"].every(key=>permissions.includes(key)); }
export function pageActionPermission(path:string, action:"create"|"edit"|"delete") { const area=path.split("/")[1]; return ["drivers","trucks","dispatchers","updaters","investors","tasks"].includes(area) ? area+"."+action : ""; }
