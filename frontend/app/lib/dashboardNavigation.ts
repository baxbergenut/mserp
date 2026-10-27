// Dashboard links start with clean filters. API validation remains authoritative.
export function dashboardView<T>(name: string, initial: T, search?: string): T | undefined {
  if (!search) return undefined;
  const query = new URLSearchParams(search);
  if (query.get("dashboard") !== "1") return undefined;
  if (name === "drivers:filters") return { employment: "active", availability: query.get("availability") ?? "", assignment: query.get("assignment") ?? "" } as T;
  const keys: Record<string, string> = {
    "page:truckStatus": "status", "page:trailerStatus": "status", "page:truckGPSAlert": "gpsAlert",
    "page:dateFrom": "dateFrom", "page:dateTo": "dateTo", "page:postFrom": "dateFrom", "page:postTo": "dateTo",
    "page:activeTab": "tab", "wmt:tab": "tab", "page:category": "category", "page:categoryId": "categoryId",
    "wmt:transactions:from": "dateFrom", "wmt:transactions:to": "dateTo", "tasks:status": "status", "programs:status": "status",
  };
  if (name === "page:unlinked") return (query.get("unlinked") === "true") as T;
  if (name === "page:productionOnly") return (query.get("productionOnly") === "true") as T;
  if (keys[name]) return (query.get(keys[name]) ?? initial) as T;
  if (/^(page:|wmt:|tasks:|programs:)/.test(name) && !name.endsWith("pageSize")) return initial;
  return undefined;
}
