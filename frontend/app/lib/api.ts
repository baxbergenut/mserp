import type {
  Dispatcher,
  DispatcherInput,
  Driver,
  DriverInput,
  Load,
  SyncLoadsResult,
  Truck,
  TruckInput,
} from "./types";

const API_BASE =
  process.env.NEXT_PUBLIC_API_URL ??
  process.env.NEXT_PUBLIC_LOADS_API_URL ??
  "http://localhost:8080";

export async function fetchLoads(): Promise<Load[]> {
  const res = await fetch(`${API_BASE}/loads`, { cache: "no-store" });

  if (!res.ok) {
    throw new Error(`Failed to fetch loads (${res.status} ${res.statusText})`);
  }

  const json = await res.json();

  // Defensive: handle either a raw array or a { data: [...] } wrapper.
  if (Array.isArray(json)) return json as Load[];
  if (Array.isArray(json?.data)) return json.data as Load[];

  return [];
}

export const syncLoads = () =>
  apiRequest<SyncLoadsResult>("/jobs/sync-loads", { method: "POST" });

async function apiRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    cache: "no-store",
    ...init,
    headers: {
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...init?.headers,
    },
  });

  if (!response.ok) {
    const body = await response.json().catch(() => null);
    throw new Error(
      body?.error ??
        `Request failed (${response.status} ${response.statusText})`,
    );
  }

  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}

export const fetchDrivers = () => apiRequest<Driver[]>("/drivers");
export const createDriver = (input: DriverInput) =>
  apiRequest<Driver>("/drivers", {
    method: "POST",
    body: JSON.stringify(input),
  });
export const updateDriver = (id: string, input: DriverInput) =>
  apiRequest<Driver>(`/drivers/${id}`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
export const deleteDriver = (id: string) =>
  apiRequest<void>(`/drivers/${id}`, { method: "DELETE" });

export const fetchTrucks = () => apiRequest<Truck[]>("/trucks");
export const createTruck = (input: TruckInput) =>
  apiRequest<Truck>("/trucks", {
    method: "POST",
    body: JSON.stringify(input),
  });
export const updateTruck = (id: string, input: TruckInput) =>
  apiRequest<Truck>(`/trucks/${id}`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
export const deleteTruck = (id: string) =>
  apiRequest<void>(`/trucks/${id}`, { method: "DELETE" });

export const fetchDispatchers = () =>
  apiRequest<Dispatcher[]>("/dispatchers");
export const createDispatcher = (input: DispatcherInput) =>
  apiRequest<Dispatcher>("/dispatchers", {
    method: "POST",
    body: JSON.stringify(input),
  });
export const updateDispatcher = (id: string, input: DispatcherInput) =>
  apiRequest<Dispatcher>(`/dispatchers/${id}`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
export const deleteDispatcher = (id: string) =>
  apiRequest<void>(`/dispatchers/${id}`, { method: "DELETE" });
