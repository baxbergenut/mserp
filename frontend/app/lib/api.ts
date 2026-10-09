import { expandBoard } from "./compactBoard";
import { ReadRequests } from "./readRequests";
import type { Updater, UpdaterInput } from "./types";
import type { DriverStatusChange, PaySourceAcceptance, PayPageQuery } from "./types";
import type { ProfileNote, InvestorStatementWeek } from "./types";

export const fetchProfileNotes = (kind: "drivers" | "trucks", id: string) => apiRequest<ProfileNote[]>(`/${kind}/${id}/notes`);
export const addProfileNote = (kind: "drivers" | "trucks", id: string, note: { id: string; body: string }) => apiRequest<ProfileNote>(`/${kind}/${id}/notes`, { method: "POST", body: JSON.stringify(note) });
export const fetchTruckAssignments = (id: string) => apiRequest<AssignmentHistoryEntry[]>(`/trucks/${id}/assignments`);
export const fetchInvestor = (id: string) => apiRequest<Investor>(`/investors/${id}`);
export const fetchDispatcher = (id: string) => apiRequest<Dispatcher>(`/dispatchers/${id}`);
export const fetchInvestorHistory = (investorId: string, page: number, signal?: AbortSignal) => paginatedRequest<PaginatedResponse<InvestorStatementWeek>>(withQuery("/investor-pay/history", { investorId, page, pageSize: 10 }), signal);
import type { DriverPayHistoryRow, SettlementEvent } from "./types";
import { withPhone } from "./phone";
import type {
  DriverBoard, DriverBoardEntry, DriverBoardHistory, BoardLoads, BoardLoadAction,
  AccessData, AccessRole, ManagedUser, SystemTaskAssignment, TaskUser,
 ChargeCell, ChargeData, ChargeType, ChargeCreate, ChargeBulk, ChargeOccurrence, ChargeEvent,
  Investor,
  InvestorInput,
  DriverPayWeek,
  DriverPayEdits,
  CustomTask,
  TaskStatus,
  CustomTaskInput,
  RelayIdentityTask,
  GrossBoard,
  GrossBoardEntry,
  GrossBoardBalanceLine,
  GrossBoardLoad,
  Dispatcher,
  DispatcherInput,
  CDLFileUploadResult,
  Driver,
  AssignmentHistoryEntry,
  DriverInput,
  DriverSetupDefaults,
  DriverIntake,
  DriverDirectoryEntry,
  FuelDashboard,
  FinancialDashboard,
  FuelTransaction,
  FuelTransactionPage,
  Load,
  LoadPage,
  PaginatedResponse,
  IRPFileUploadResult,
  SyncLoadsResult,
  SyncFiveELDResult,
  SyncFuelResult,
  SyncTollsResult,
  Toll,
  TollPage,
  TollDashboard,
  Truck,
  FleetLocation,
  TruckInput,
  AuthSession,
  Expense,
  ExpenseInput,
  ExpenseSetting,
  DriverEscrowSetting,
  EscrowPage,
  EscrowReleaseInput,
  ExpensePage,
  ExpenseExtraction,
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
  path: string, signal?: AbortSignal,
): Promise<T> {
  const value = await apiRequest<unknown>(path, { signal });
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

let csrfToken = "";

export const fetchTasks = (query: PageQuery & { status: TaskStatus | "all" }) =>
  paginatedRequest<PaginatedResponse<CustomTask>>(withQuery("/tasks", query));
export const fetchTaskCount = () => apiRequest<{ count: number }>("/tasks/count");
export function subscribeTaskEvents(onChange: () => void, onUnauthorized: () => void) {
  const events = new EventSource(`${API_BASE}/tasks/events`, { withCredentials: true });
  events.addEventListener("changed", onChange);
  events.addEventListener("unauthorized", () => { events.close(); onUnauthorized(); });
  return () => events.close();
}
export const confirmOffboarding = (id: string, checklist: { equipment: boolean; access: boolean; settlement: boolean }) =>
  apiRequest<void>(`/tasks/offboarding/${id}/confirm`, { method: "POST", body: JSON.stringify(checklist) });

export const fetchCustomTasks = (query: PageQuery & { status: TaskStatus | "all" }) =>
  paginatedRequest<PaginatedResponse<CustomTask>>(withQuery("/tasks/custom", query));
export const createCustomTask = (input: CustomTaskInput) =>
  apiRequest<CustomTask>("/tasks/custom", { method: "POST", body: JSON.stringify(input) });
export const updateCustomTask = (id: string, input: CustomTaskInput) =>
  apiRequest<CustomTask>(`/tasks/custom/${id}`, { method: "PUT", body: JSON.stringify(input) });
export const setTaskStatus = (id: string, status: TaskStatus) =>
  apiRequest<CustomTask>(`/tasks/custom/${id}`, { method: "PATCH", body: JSON.stringify({ status }) });
export const setCustomTaskCompleted = (id: string, completed: boolean) =>
  apiRequest<CustomTask>(`/tasks/custom/${id}`, { method: "PATCH", body: JSON.stringify({ completed }) });
export const deleteCustomTask = (id: string) =>
  apiRequest<void>(`/tasks/custom/${id}`, { method: "DELETE" });

export const fetchRelayIdentityTasks = (query: PageQuery) =>
  paginatedRequest<PaginatedResponse<RelayIdentityTask>>(withQuery("/tasks/relay-identities", query));
export const reviewRelayIdentity = (id: string, driverId: string, action: "link" | "reject") =>
  apiRequest<{ transactionsLinked: number }>(`/tasks/relay-identities/${id}/review`, {
    method: "POST", body: JSON.stringify({ driverId, action }),
  });

export const fetchGrossBoard = (weekStart: string) =>
  apiRequest<GrossBoard>(withQuery("/gross-board", { weekStart }));
export const fetchDriverBoard = (weekStart: string) =>
  apiRequest<DriverBoard | import("./types").CompactDriverBoard>(withQuery("/driver-board", { weekStart })).then(expandBoard);
export const saveDriverBoard = (entries: DriverBoardEntry[]) =>
  apiRequest<DriverBoardEntry[]>("/driver-board", { method: "PUT", body: JSON.stringify({ entries }) });
export const fetchDriverBoardHistory = (driverIds: string[], before = 0) =>
  apiRequest<DriverBoardHistory>(withQuery("/driver-board/history", { driverIds: driverIds.join(","), before }));
// The server clamps the optional upcoming cutoff to today in New York; older unfinished plans are returned separately.
// Includes all weekly match candidates separately from the today-forward queue.
export const fetchBoardLoads = (driverId: string, from?: string) => apiRequest<BoardLoads>(withQuery(`/driver-board/loads/${driverId}`, { from }));
export const changeBoardLoads = (entry: DriverBoardEntry, view: BoardLoads, action: BoardLoadAction) =>
  apiRequest<{ entry: DriverBoardEntry; loads: BoardLoads }>(`/driver-board/loads/${entry.driverId}`, { method: "POST", body: JSON.stringify({ ...action, version: entry.version, homeVersion: entry.homeVersion, revision: view.revision, fromDate: view.fromDate }) });
export const undoDriverBoardEvent = (id: number, entry: DriverBoardEntry, personal = false) =>
  apiRequest<DriverBoardEntry>(`/driver-board/history/${id}/undo`, { method: "POST", body: JSON.stringify({ driverId: entry.driverId, version: entry.version, homeVersion: entry.homeVersion, personal }) });
export const fetchGrossBoardBalance = (driverId: string, weekStart: string) =>
  apiRequest<GrossBoardBalanceLine[]>(withQuery("/gross-board/balance", { driverId, weekStart }));
export const searchGrossBoardLoads = (search: string) =>
  apiRequest<GrossBoardLoad[]>(withQuery("/gross-board/loads", { search }));
export const saveGrossBoard = (weekStart: string, entries: GrossBoardEntry[]) =>
  apiRequest<GrossBoardEntry[]>("/gross-board", { method: "PUT", body: JSON.stringify({ weekStart, entries }) });

export async function fetchLoads(): Promise<Load[]> {
	const json = await apiRequest<unknown>("/loads");

	// Defensive: handle either a raw array or a { data: [...] } wrapper.
	if (Array.isArray(json)) return json as Load[];
	if (
		typeof json === "object" &&
		json !== null &&
		"data" in json &&
		Array.isArray(json.data)
	) {
		return json.data as Load[];
	}

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
}, signal?: AbortSignal) => paginatedRequest<LoadPage>(withQuery("/loads", query), signal);

export const syncLoads = () =>
  apiRequest<SyncLoadsResult>("/jobs/sync-loads", { method: "POST" });

export const syncFiveELD = () =>
  apiRequest<SyncFiveELDResult>("/jobs/sync-eld", { method: "POST" });

export const fetchFuelTransactions = () =>
  apiRequest<FuelTransaction[]>("/fuel-transactions");
export const fetchFuelTransactionsPage = (query: PageQuery & {
  driver?: string;
  state?: string;
  responsibility?: "non_personal";
  chargeDriverId?: string;
  category?: string;
  dateFrom?: string;
  dateTo?: string;
}) => paginatedRequest<FuelTransactionPage>(withQuery("/fuel-transactions", query));
export const fetchFuelDashboard = (query: {
  dateFrom?: string;
  dateTo?: string;
}) => apiRequest<FuelDashboard>(withQuery("/fuel-dashboard", query));
export const fetchFinancialDashboard = (query: { weekStart?: string }) =>
  apiRequest<FinancialDashboard>(withQuery("/financial-dashboard", query));
export const syncFuelTransactions = () =>
  apiRequest<SyncFuelResult>("/jobs/sync-fuel", { method: "POST" });

const reads = new ReadRequests();
function apiRequest<T>(path: string, init?: RequestInit): Promise<T> {
  if ((init?.method ?? "GET").toUpperCase() === "GET" && !init?.headers) {
    return reads.read(path, signal => performRequest<T>(path, { ...init, signal }), init?.signal);
  }
  reads.clear();
  return performRequest<T>(path, init).finally(() => reads.clear());
}
async function performRequest<T>(path: string, init?: RequestInit): Promise<T> {
	const method = (init?.method ?? "GET").toUpperCase();
	const needsCSRF = !["GET", "HEAD", "OPTIONS"].includes(method);
	const response = await fetch(`${API_BASE}${path}`, {
		cache: "no-store",
		credentials: "include",
		...init,
    headers: {
      ...(typeof init?.body === "string"
        ? { "Content-Type": "application/json" }
			: {}),
		...(needsCSRF && csrfToken ? { "X-CSRF-Token": csrfToken } : {}),
		...init?.headers,
    },
  });

	if (!response.ok) {
		const body = await response.json().catch(() => null);
		if (
			response.status === 401 &&
			!path.startsWith("/auth/") &&
			typeof window !== "undefined"
		) {
			const next = `${window.location.pathname}${window.location.search}`;
			window.location.assign(`/login?next=${encodeURIComponent(next)}`);
		}
    throw new Error(
      body?.error ??
        `Request failed (${response.status} ${response.statusText})`,
    );
  }

  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}

export async function fetchAuthSession(): Promise<AuthSession> {
	const session = await apiRequest<AuthSession>("/auth/session");
	csrfToken = session.csrfToken;
	return session;
}

export async function login(email: string, password: string, trustDevice = false): Promise<AuthSession> {
	const session = await apiRequest<AuthSession>("/auth/login", {
		method: "POST",
		body: JSON.stringify({ email, password, trustDevice }),
	});
	csrfToken = session.csrfToken;
	return session;
}

export const changePassword = (currentPassword: string, newPassword: string) => apiRequest<void>("/auth/password", { method: "POST", body: JSON.stringify({ currentPassword, newPassword }) });
export const fetchAccess = () => apiRequest<AccessData>("/settings/access");
export const saveTheme = (theme: string) => apiRequest<void>("/auth/theme", { method: "PUT", body: JSON.stringify({ theme }) });
export const fetchSystemTaskAssignments = () => apiRequest<SystemTaskAssignment[]>("/settings/system-tasks");
export const saveSystemTaskAssignment = (input: SystemTaskAssignment) => apiRequest<void>(`/settings/system-tasks/${input.kind}`, { method: "PUT", body: JSON.stringify(input) });
export const fetchTaskUsers = () => apiRequest<TaskUser[]>("/tasks/users");
export const saveUser = (user: ManagedUser) => apiRequest<{id: string}>(`/settings/users${user.id ? `/${user.id}` : ""}`, { method: user.id ? "PUT" : "POST", body: JSON.stringify(user) });
export const saveRole = (role: AccessRole) => apiRequest<void>(`/settings/roles${role.id ? `/${role.id}` : ""}`, { method: role.id ? "PUT" : "POST", body: JSON.stringify(role) });
export const revokeUserAccess = (id: string) => apiRequest<void>(`/settings/users/${id}/revoke`, { method: "POST" });

export async function logout(): Promise<void> {
	try {
		await apiRequest<void>("/auth/logout", { method: "POST" });
	} finally {
		csrfToken = "";
    reads.clear(true);
	}
}

export const fetchDrivers = () => apiRequest<Driver[]>("/drivers");
export const fetchDriverSetupDefaults = () => apiRequest<DriverSetupDefaults>("/drivers/setup-defaults");
export const fetchDriverDirectory = (query: PageQuery & { includeInactive?: boolean }) =>
  paginatedRequest<PaginatedResponse<DriverDirectoryEntry>>(withQuery("/driver-directory", query));
export const fetchDriverIntakeById = (id: string) => apiRequest<DriverIntake>(`/driver-intake/${id}`);
export const fetchDriverIntake = (query: PageQuery) =>
  paginatedRequest<PaginatedResponse<DriverIntake>>(withQuery("/driver-intake", query));
export const completeDriverIntake = (id: string, input:
  { driver: DriverInput; separateConfirmed: boolean } | { linkDriverId: string }) =>
  apiRequest<Driver>(`/driver-intake/${id}/complete`, { method: "POST", body: JSON.stringify("driver" in input ? { ...input, driver: withPhone(input.driver) } : input) });
export const fetchDriver = (id: string) => apiRequest<Driver>(`/drivers/${id}`);
export const fetchDriverAssignments = (id: string) => apiRequest<AssignmentHistoryEntry[]>(`/drivers/${id}/assignments`);
export const fetchDriversPage = (query: PageQuery & { includeInactive?: boolean }, signal?: AbortSignal) =>
  paginatedRequest<PaginatedResponse<Driver>>(withQuery("/drivers", query), signal);
export const createDriver = (input: DriverInput) =>
  apiRequest<Driver>("/drivers", {
    method: "POST",
    body: JSON.stringify(withPhone(input)),
  });
export const updateDriver = (id: string, input: DriverInput) =>
  apiRequest<Driver>(`/drivers/${id}`, {
    method: "PUT",
    body: JSON.stringify(withPhone(input)),
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
export const fetchTruck = (id: string) => apiRequest<Truck>(`/trucks/${id}`);
export const fetchTruckLocation = (id: string) => apiRequest<FleetLocation>(`/trucks/${id}/location`);
export const fetchDriverTruckLocation = (id: string) => apiRequest<FleetLocation>(`/drivers/${id}/location`);
export const fetchTrucksPage = (query: PageQuery, signal?: AbortSignal) =>
  paginatedRequest<PaginatedResponse<Truck>>(withQuery("/trucks", query), signal);
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
    body: JSON.stringify(withPhone(input)),
  });
export const updateDispatcher = (id: string, input: DispatcherInput) =>
  apiRequest<Dispatcher>(`/dispatchers/${id}`, {
    method: "PUT",
    body: JSON.stringify(withPhone(input)),
  });
export const deleteDispatcher = (id: string) =>
  apiRequest<void>(`/dispatchers/${id}`, { method: "DELETE" });

export const fetchTolls = () => apiRequest<Toll[]>("/tolls");
export const fetchTollDashboard = (query: { dateFrom: string; dateTo: string }) =>
  apiRequest<TollDashboard>(withQuery("/toll-dashboard", query));
export const fetchTollsPage = (query: PageQuery & {
  unit?: string;
  agency?: string;
  postFrom?: string;
  postTo?: string;
}) => paginatedRequest<TollPage>(withQuery("/tolls", query));
export const syncTolls = () =>
  apiRequest<SyncTollsResult>("/jobs/sync-tolls", { method: "POST" });

export const fetchExpensesPage = (query: PageQuery & {
  responsibility?: "non_personal";
  chargeDriverId?: string;
  categoryId?: string;
  company?: string;
  dateFrom?: string;
  dateTo?: string;
  truckId?: string;
  driverId?: string;
}, signal?: AbortSignal) => paginatedRequest<ExpensePage>(withQuery("/expenses", query), signal);
export const createExpense = (input: ExpenseInput) =>
  apiRequest<Expense>("/expenses", {
    method: "POST",
    body: JSON.stringify(input),
  });
export const createExpenses = (expenses: ExpenseInput[]) =>
  apiRequest<Expense[]>("/expenses/bulk", {
    method: "POST",
    body: JSON.stringify({ expenses }),
  });
export const extractExpenses = (text: string, file: File | null) => {
  const form = new FormData();
  if (text.trim()) form.append("text", text.trim());
  if (file) form.append("file", file);
  return apiRequest<ExpenseExtraction>("/expenses/extract", {
    method: "POST",
    body: form,
  });
};
export const updateExpense = (id: string, input: ExpenseInput) =>
  apiRequest<Expense>(`/expenses/${id}`, {
    method: "PUT",
    body: JSON.stringify(input),
  });
export const deleteExpense = (id: string) =>
  apiRequest<void>(`/expenses/${id}`, { method: "DELETE" });

export const fetchDriverPay = (weekStart: string, signal?: AbortSignal, query?: PayPageQuery) => apiRequest<DriverPayWeek>(withQuery("/driver-pay", { weekStart, ...query }), { signal });
export const acceptPaySystemValues = (investor: boolean, input: PaySourceAcceptance) => apiRequest<void>(`/${investor ? "investor-pay" : "driver-pay"}/accept-system`, { method: "POST", body: JSON.stringify(input) });
export const changeDriverStatus = (id: string, input: DriverStatusChange) => apiRequest<Driver>(`/drivers/${id}/status`, { method: "PATCH", body: JSON.stringify(input) });
export const saveDriverPay = (edits: DriverPayEdits) => apiRequest<DriverPayEdits>("/driver-pay", { method: "PUT", body: JSON.stringify(edits) });
export const refreshDriverPayLoads = (weekStart: string) => apiRequest<DriverPayWeek>("/driver-pay/refresh-loads", { method: "POST", body: JSON.stringify({ weekStart }) });

export const fetchInvestors = () => apiRequest<Investor[]>("/investors");
export const fetchInvestorsPage = (query: PageQuery & { includeCompany?: boolean }, signal?: AbortSignal) =>
  paginatedRequest<PaginatedResponse<Investor>>(withQuery("/investors", query), signal);
export const createInvestor = (input: InvestorInput) =>
  apiRequest<Investor>("/investors", { method: "POST", body: JSON.stringify(withPhone(input)) });
export const updateInvestor = (id: string, input: InvestorInput) =>
  apiRequest<Investor>(`/investors/${id}`, { method: "PUT", body: JSON.stringify(withPhone(input)) });

export const fetchDriverCharges = (driverId?: string) => apiRequest<ChargeData>(withQuery("/driver-charges", { driverId }));
export const saveChargeType = (input: ChargeType) => apiRequest<ChargeType>("/driver-charges/types", { method: "POST", body: JSON.stringify(input) });
export const deleteChargeType = (input: ChargeType) => apiRequest<void>(`/driver-charges/types/${input.id}`, { method: "DELETE", body: JSON.stringify({ version: input.version }) });
export const previewNewCharges = (input: ChargeCreate) => apiRequest<ChargeOccurrence[]>("/driver-charges/schedules/preview", { method: "POST", body: JSON.stringify(input) });
export const createDriverCharges = (input: ChargeCreate) => apiRequest<string[]>("/driver-charges/schedules", { method: "POST", body: JSON.stringify(input) });
export const bulkDriverCharges = (input: ChargeBulk) => apiRequest<void>("/driver-charges/bulk", { method: "POST", body: JSON.stringify(input) });
export const previewDriverCharge = (id: string) => apiRequest<ChargeOccurrence[]>(`/driver-charges/schedules/${id}/preview`);
export const fetchChargeHistory = (id: string) => apiRequest<ChargeEvent[]>(`/driver-charges/schedules/${id}/history`);
export const confirmDriverCharges = (driverId: string, weekStart: string, rows: ChargeOccurrence[], reason?: string) =>
  apiRequest<void>(`/driver-charges/${reason === undefined ? "confirm" : "reopen"}`, { method: "POST", body: JSON.stringify({ driverId, weekStart, rows, reason: reason ?? "" }) });

export const saveRecurringCharge = (input: ChargeCell) => apiRequest<void>("/driver-charges/recurring", { method: "PUT", body: JSON.stringify(input) });

export const fetchDriverPayHistory = (id: string, page: number, pageSize = 25, signal?: AbortSignal) => apiRequest<PaginatedResponse<DriverPayHistoryRow>>(withQuery(`/drivers/${id}/pay-history`, { page, pageSize }), { signal });
export const fetchSettlementHistory = (id: string, weekStart: string) => apiRequest<SettlementEvent[]>(withQuery(`/drivers/${id}/settlement-history`, { weekStart }));
export const settleDriverPay = (weekStart: string, revision: string, driverId: string | undefined, reopen: boolean, reason: string) => apiRequest<DriverPayWeek>(`/driver-pay/${reopen ? "reopen" : "finalize"}`, { method: "POST", body: JSON.stringify({weekStart, revision, driverId, reason}) });

export const fetchExpenseSettings = () => apiRequest<ExpenseSetting[]>("/expense-settings");
export const saveExpenseSetting = (input: Omit<ExpenseSetting, "id"> & { id?: string }) => apiRequest<ExpenseSetting>(input.id ? `/expense-settings/${input.id}` : "/expense-settings", { method: input.id ? "PUT" : "POST", body: JSON.stringify(input) });
export const fetchDriverEscrowSetting = () => apiRequest<DriverEscrowSetting>("/escrows/settings");
export const saveDriverEscrowSetting = (input: DriverEscrowSetting) => apiRequest<DriverEscrowSetting>("/escrows/settings", { method: "PUT", body: JSON.stringify(input) });

export const fetchInvestorPay = (weekStart: string, truckId?: string, signal?: AbortSignal, query?: PayPageQuery) => apiRequest<DriverPayWeek>(withQuery("/investor-pay", { weekStart, truckId, ...query }), { signal });
export const saveInvestorPay = (input: DriverPayEdits) => apiRequest<DriverPayEdits>("/investor-pay", { method: "PUT", body: JSON.stringify(input) });
export const settleInvestorPay = (weekStart: string, revision: string, driverId: string | undefined, reopen: boolean, reason: string) => apiRequest<DriverPayWeek>(`/investor-pay/${reopen ? "reopen" : "finalize"}`, { method: "POST", body: JSON.stringify({ weekStart, revision, driverId, reason }) });
export const fetchTruckCharges = () => apiRequest<import("./types").TruckChargeData>("/truck-charges");
export const saveTruckTerm = (input: import("./types").TruckTerm) => apiRequest<void>("/truck-charges/terms", { method: "PUT", body: JSON.stringify(input) });
export const saveTruckCharge = (input: import("./types").TruckChargePhase) => apiRequest<void>("/truck-charges/recurring", { method: "PUT", body: JSON.stringify(input) });

export const fetchUpdaters = () => apiRequest<Updater[]>("/updaters");
export const createUpdater = (value: UpdaterInput) => apiRequest<Updater>("/updaters", { method: "POST", body: JSON.stringify(value) });
export const updateUpdater = (id: string, value: UpdaterInput) => apiRequest<Updater>(`/updaters/${id}`, { method: "PUT", body: JSON.stringify(value) });
export const deleteUpdater = (id: string) => apiRequest<void>(`/updaters/${id}`, { method: "DELETE" });

export const fetchEscrows = (query: PageQuery & { driverId?: string; status?: string; group?: string; includeInactive?: boolean }) => apiRequest<EscrowPage>(withQuery("/escrows", query));

export const saveEscrowRelease = (escrowId: string, input: EscrowReleaseInput) => apiRequest<{ saved: boolean }>(`/escrows/${escrowId}/releases${input.version ? `/${input.id}` : ""}`, { method: input.version ? "PUT" : "POST", body: JSON.stringify(input) });

export const fetchEscrowTask = (id: string) => apiRequest<import("./types").EscrowTaskDetail>(`/tasks/escrow/${id}`);
export const completeEscrowTask = (id: string, input: import("./types").EscrowTaskDecision) => apiRequest<void>(`/tasks/escrow/${id}/complete`, { method: "POST", body: JSON.stringify(input) });
export const saveEscrowOpening = (id: string, input: { openingPaid: string; version: number; reason: string }) => apiRequest<void>(`/escrows/${id}/opening`, { method: "PUT", body: JSON.stringify(input) });
