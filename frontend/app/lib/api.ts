import type {
  Dispatcher,
  DispatcherInput,
  CDLFileUploadResult,
  Driver,
  DriverInput,
  FuelTransaction,
  FuelTransactionPage,
  Load,
  LoadPage,
  PaginatedResponse,
  IRPFileUploadResult,
  SyncLoadsResult,
  SyncFuelResult,
  Toll,
  TollPage,
  TollImportResult,
  Truck,
  TruckInput,
} from "./types";

type PageQuery = {
  page: number;
  pageSize: number;
  search?: string;
};

function withQuery(path: string, values: Record<string, unknown>) {
  const query = new URLSearchParams();
  Object.entries(values).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== "") {
      query.set(key, String(value));
    }
  });
  return `${path}?${query.toString()}`;
}

async function paginatedRequest<T extends PaginatedResponse<unknown>>(
  path: string,
): Promise<T> {
  const value = await apiRequest<unknown>(path);
  if (
    typeof value === "object" &&
    value !== null &&
    Array.isArray((value as PaginatedResponse<unknown>).items) &&
    typeof (value as PaginatedResponse<unknown>).total === "number" &&
    typeof (value as PaginatedResponse<unknown>).page === "number" &&
    typeof (value as PaginatedResponse<unknown>).pageSize === "number" &&
    typeof (value as PaginatedResponse<unknown>).totalPages === "number"
  ) {
    return value as T;
  }
  throw new Error(
    "The API server is running an older build. Restart the backend to enable pagination.",
  );
}

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

export const fetchLoadsPage = (query: PageQuery & {
  status?: string;
  customer?: string;
  dispatcher?: string;
  driver?: string;
  pickupFrom?: string;
  pickupTo?: string;
  sort?: string;
  direction?: string;
}) => paginatedRequest<LoadPage>(withQuery("/loads", query));

export const syncLoads = () =>
  apiRequest<SyncLoadsResult>("/jobs/sync-loads", { method: "POST" });

export const fetchFuelTransactions = () =>
  apiRequest<FuelTransaction[]>("/fuel-transactions");
export const fetchFuelTransactionsPage = (query: PageQuery & {
  driver?: string;
  state?: string;
  category?: string;
  dateFrom?: string;
  dateTo?: string;
}) => paginatedRequest<FuelTransactionPage>(withQuery("/fuel-transactions", query));
export const syncFuelTransactions = () =>
  apiRequest<SyncFuelResult>("/jobs/sync-fuel", { method: "POST" });

async function apiRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    cache: "no-store",
    ...init,
    headers: {
      ...(typeof init?.body === "string"
        ? { "Content-Type": "application/json" }
        : {}),
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
export const fetchDriversPage = (query: PageQuery & { includeInactive?: boolean }) =>
  paginatedRequest<PaginatedResponse<Driver>>(withQuery("/drivers", query));
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

export const uploadCDLFile = (file: File, renderedPages: Blob[] = []) => {
  const form = new FormData();
  form.append("file", file);
  renderedPages.forEach((page, index) => {
    form.append("page", page, `page-${index + 1}.jpg`);
  });
  return apiRequest<CDLFileUploadResult>("/cdl-files", {
    method: "POST",
    body: form,
  });
};

export const fetchTrucks = () => apiRequest<Truck[]>("/trucks");
export const fetchTrucksPage = (query: PageQuery) =>
  paginatedRequest<PaginatedResponse<Truck>>(withQuery("/trucks", query));
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

export const uploadIRPFile = (file: File, renderedPages: Blob[] = []) => {
  const form = new FormData();
  form.append("file", file);
  renderedPages.forEach((page, index) => {
    form.append("page", page, `page-${index + 1}.jpg`);
  });
  return apiRequest<IRPFileUploadResult>("/irp-files", {
    method: "POST",
    body: form,
  });
};

export const fileDownloadUrl = (id: string) =>
  `${API_BASE}/files/${encodeURIComponent(id)}`;

export const fetchDispatchers = () =>
  apiRequest<Dispatcher[]>("/dispatchers");
export const fetchDispatchersPage = (query: PageQuery) =>
  paginatedRequest<PaginatedResponse<Dispatcher>>(withQuery("/dispatchers", query));
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

export const fetchTolls = () => apiRequest<Toll[]>("/tolls");
export const fetchTollsPage = (query: PageQuery & {
  unit?: string;
  agency?: string;
  postFrom?: string;
  postTo?: string;
}) => paginatedRequest<TollPage>(withQuery("/tolls", query));
export const uploadTollReport = (file: File) => {
  const form = new FormData();
  form.append("file", file);
  return apiRequest<TollImportResult>("/toll-reports", {
    method: "POST",
    body: form,
  });
};
