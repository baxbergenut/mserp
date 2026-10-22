import { expensePayload } from "./expenseCosts";
export const fetchTruckDataTruck = (id: string, signal?: AbortSignal) => apiRequest<import("./types").DataTruckEnrollment>(`/trucks/${id}/datatruck`, {signal});
export const retryTruckDataTruck = (id: string, version: number) => apiRequest<void>(`/trucks/${id}/datatruck`, {method:"POST",body:JSON.stringify({version})});
import type { InventoryCollection, InventoryData, InventoryRecord, InventoryEvent, InventoryMovement, InventorySnapshot, InventoryImport } from "./inventoryTypes";
export const fetchInventory = (signal?: AbortSignal) => apiRequest<InventoryData>("/inventory", { signal });
export const fetchTrailers = (signal?: AbortSignal) => apiRequest<InventoryRecord[]>("/trailers", { signal });
export const saveInventoryRecord = (collection: InventoryCollection, record: InventoryRecord, requestId: string) => apiRequest<InventoryRecord>(collection === "trailers" ? `/trailers/${record.id}` : `/inventory/${collection}/${record.id}`, { method: "PUT", body: JSON.stringify({ requestId, record }) });
export const fetchInventoryHistory = (collection = "", id = "") => apiRequest<InventoryEvent[]>(collection === "trailers" ? `/trailers/${encodeURIComponent(id)}/history` : `/inventory/history?${new URLSearchParams({ collection, id })}`);
export const moveInventoryStock = (movement: InventoryMovement) => apiRequest<void>("/inventory/movements", { method: "POST", body: JSON.stringify(movement) });
export const fetchInventoryImports = () => apiRequest<InventoryImport[]>("/inventory/imports");
export const previewInventoryImport = (snapshot: InventorySnapshot) => apiRequest<InventoryImport>("/inventory/imports/preview", { method: "POST", body: JSON.stringify(snapshot) });
export const reviewInventoryImport = (batch: InventoryImport) => apiRequest<InventoryImport>(`/inventory/imports/${batch.id}`, { method: "PUT", body: JSON.stringify({ version: batch.version, rows: batch.rows }) });
export const commitInventoryImport = (batch: InventoryImport) => apiRequest<InventoryImport>(`/inventory/imports/${batch.id}/commit`, { method: "POST", body: JSON.stringify({ version: batch.version }) });
import { expandBoard } from "./compactBoard";
import type { PrePassFleetChange, PrePassFleetList } from "./types";
export const fetchPrePassFleet = (signal?: AbortSignal) => apiRequest<PrePassFleetList>("/prepass-fleet", { signal });
export const refreshPrePassFleet = () => apiRequest<void>("/prepass-fleet/refresh", { method: "POST" });
export const changePrePassFleet = (input: PrePassFleetChange) => apiRequest<void>(input.action === "link" ? "/prepass-fleet/link" : "/prepass-fleet/change", { method: "POST", body: JSON.stringify(input) });
export const previewPrePassFleetLink = (input: PrePassFleetChange) => apiRequest<import("./types").PrePassTruckPreview>("/prepass-fleet/link/preview", { method: "POST", body: JSON.stringify(input) });
export const verifyPrePassFleetOperation = (id: string) => apiRequest<void>(`/prepass-fleet/operations/${encodeURIComponent(id)}/verify`, { method: "POST", body: JSON.stringify({ reviewed: true }) });
import type { DriverTeam, DriverTeamInput, DriverTeamTruck } from "./types";
export const fetchDriverTeamTrucks = (weekStart: string, signal?: AbortSignal) => apiRequest<DriverTeamTruck[]>(`/drivers/team-trucks?${new URLSearchParams({ weekStart })}`, { signal });
export const fetchDriverTeam = (id: string) => apiRequest<DriverTeam>(`/drivers/${id}/team`);
export const saveDriverTeam = (id: string, input: DriverTeamInput) => apiRequest<DriverTeam>(`/drivers/${id}/team`, { method: "PUT", body: JSON.stringify(input) });
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
import type { DriverPayHistoryRow, PayrollSettlementInput, SettlementEvent } from "./types";
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
export const fetchSidebarCounts = (signal?: AbortSignal) => apiRequest<import("./types").SidebarCounts>("/sidebar-counts", { signal });
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
export const fetchGrossPresence = (weekStart: string) => apiRequest<import("./types").GrossBoardPresence[]>(withQuery("/gross-board/presence", { weekStart }));
export const updateGrossPresence = (clientId: string, cell: import("./types").GrossBoardCell) => performRequest<void>("/gross-board/presence", { method: "PUT", body: JSON.stringify({ clientId, ...cell }) });
export const clearGrossPresence = (clientId: string) => performRequest<void>(`/gross-board/presence/${clientId}`, { method: "DELETE", keepalive: true });
export function subscribeGrossEvents(weekStart: string, changed: (board?: GrossBoard) => void, presence: (peers?: import("./types").GrossBoardPresence[]) => void, connection: (connected: boolean) => void, unauthorized: () => void) {
  const stream = new EventSource(`${API_BASE}${withQuery("/gross-board/events", { weekStart })}`, { withCredentials: true });
  stream.addEventListener("open", () => connection(true));
  stream.addEventListener("error", () => connection(false));
  stream.addEventListener("changed", () => changed());
  stream.addEventListener("presence", () => presence());
  stream.addEventListener("grossBoard", event => { try { changed(JSON.parse(event.data)); } catch { changed(); } });
  stream.addEventListener("peers", event => { try { presence(JSON.parse(event.data)); } catch { presence(); } });
  stream.addEventListener("unauthorized", () => { stream.close(); connection(false); unauthorized(); });
  return () => stream.close();
}
export const fetchDriverBoard = (weekStart: string) =>
  apiRequest<DriverBoard | import("./types").CompactDriverBoard>(withQuery("/driver-board", { weekStart })).then(expandBoard);
export const saveDriverBoard = (entries: DriverBoardEntry[]) =>
  apiRequest<DriverBoardEntry[]>("/driver-board", { method: "PUT", body: JSON.stringify({ entries }) });
export const reorderBoardDrivers = (action: import("./types").DriverBoardOrderAction) =>
  apiRequest<import("./types").DriverBoardOrder>("/driver-board/order", { method: "POST", body: JSON.stringify(action) });
export const reorderBoardDispatchers = (action: import("./types").DispatcherBoardOrderAction) =>
  apiRequest<import("./types").DispatcherBoardOrder>("/driver-board/dispatcher-order", { method: "POST", body: JSON.stringify(action) });
export const fetchBoardPresence = () => apiRequest<import("./types").BoardPresence[]>("/driver-board/presence");
export const updateBoardPresence = (clientId: string, driverId: string, column: string) =>
  performRequest<void>("/driver-board/presence", { method: "PUT", body: JSON.stringify({ clientId, driverId, column }) });
export const clearBoardPresence = (clientId: string) =>
  performRequest<void>(`/driver-board/presence/${clientId}`, { method: "DELETE", keepalive: true });
export function subscribeBoardEvents(changed: (board?: DriverBoard) => void, presence: (peers?: import("./types").BoardPresence[]) => void, connection: (connected: boolean) => void, unauthorized: () => void) {
  const stream = new EventSource(`${API_BASE}/driver-board/events`, { withCredentials: true });
  stream.addEventListener("open", () => connection(true));
  stream.addEventListener("error", () => connection(false));
  stream.addEventListener("changed", () => changed());
  stream.addEventListener("presence", () => presence());
  stream.addEventListener("board", event => { try { changed(JSON.parse(event.data)); } catch { changed(); } });
  stream.addEventListener("peers", event => { try { presence(JSON.parse(event.data)); } catch { presence(); } });
  stream.addEventListener("unauthorized", () => { stream.close(); connection(false); unauthorized(); });
  return () => stream.close();
}
export const fetchDriverBoardHistory = (driverIds: string[], before = 0, field = "") =>
  apiRequest<DriverBoardHistory>(withQuery("/driver-board/history", { driverIds: driverIds.join(","), before, field }));
// The server clamps the optional upcoming cutoff to today in New York; older unfinished plans are returned separately.
// Includes all weekly match candidates separately from the today-forward queue.
export const fetchBoardLoads = (driverId: string, from?: string) => apiRequest<BoardLoads>(withQuery(`/driver-board/loads/${driverId}`, { from }));
export const changeBoardLoads = (entry: DriverBoardEntry, view: BoardLoads, action: BoardLoadAction) =>
  apiRequest<{ entry: DriverBoardEntry; loads: BoardLoads }>(`/driver-board/loads/${entry.driverId}`, { method: "POST", body: JSON.stringify({ ...action, version: entry.version, revision: view.revision, fromDate: view.fromDate }) });
export const undoDriverBoardEvent = (id: number, entry: DriverBoardEntry, personal = false) =>
  apiRequest<DriverBoardEntry>(`/driver-board/history/${id}/undo`, { method: "POST", body: JSON.stringify({ driverId: entry.driverId, version: entry.version, personal }) });
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
let accessGeneration = 0;
let accessFingerprint = "";
let currentPermissions: string[] = [];
export class APIError extends Error {
  constructor(message: string, public status: number, public code: string) { super(message); this.name = "APIError"; }
}
export function resetAccessRequests() { accessGeneration++; reads.clear(true); }
function acceptSession(session: AuthSession) {
  const fingerprint = JSON.stringify([session.user.id, session.user.accessRevision, session.user.permissions, session.user.expenseCategoryAccess]);
  if (fingerprint !== accessFingerprint) {
    resetAccessRequests(); accessFingerprint = fingerprint; currentPermissions = session.user.permissions;
  }
}
function profilePayload(path: string, body: BodyInit | null | undefined) {
  if (typeof body !== "string" || !/^\/(drivers|trucks|dispatchers)(\/[^/]+)?$/.test(path)) return body;
  const data = JSON.parse(body) as Record<string, unknown>;
  const omit = (permission: string, fields: string[]) => { if (!currentPermissions.includes(permission)) for (const key of fields) delete data[key]; };
  if (path.startsWith("/drivers")) {
    omit("driver_compensation.edit", ["payType", "payRate"]);
    omit("drivers.private.edit", ["address", "city", "state", "postalCode", "emergencyContact", "licenseNumber", "licenseState", "licenseExpires"]);
    omit("drivers.documents.edit", ["cdlFileId"]); omit(path === "/drivers" ? "escrow.create" : "escrow.edit", ["escrowAmount"]);
  }
  if (path.startsWith("/trucks")) omit("trucks.documents.edit", ["irpFileId"]);
  if (path.startsWith("/dispatchers")) omit("dispatcher_compensation.edit", ["payPercentage"]);
  return JSON.stringify(data);
}
function apiRequest<T>(path: string, init?: RequestInit): Promise<T> {
  if ((init?.method ?? "GET").toUpperCase() === "GET" && !init?.headers) {
    return reads.read(path, signal => performRequest<T>(path, { ...init, signal }), init?.signal);
  }
  reads.clear();
  return performRequest<T>(path, init).finally(() => {
    reads.clear();
    if (typeof window !== "undefined" && /^\/(relay-drivers|weighmytruck|tolls|drivers|trucks|tasks\/relay-identities|jobs\/sync-tolls)(\/|$)/.test(path)) {
      window.dispatchEvent(new Event("mserp:sidebar-counts-changed"));
    }
  });
}
async function performRequest<T>(path: string, init?: RequestInit): Promise<T> {
	const generation = accessGeneration;
	const method = (init?.method ?? "GET").toUpperCase();
	const needsCSRF = !["GET", "HEAD", "OPTIONS"].includes(method);
	const response = await fetch(`${API_BASE}${path}`, {
		cache: "no-store",
		credentials: "include",
		...init,
    body: profilePayload(path, init?.body),
    headers: {
      ...(typeof init?.body === "string"
        ? { "Content-Type": "application/json" }
			: {}),
		...(needsCSRF && csrfToken ? { "X-CSRF-Token": csrfToken } : {}),
		...init?.headers,
    },
  }).catch(error => {
    if (error instanceof Error && error.name === "AbortError") throw error;
    throw new APIError("Could not reach the server. Check your connection and try again.", 0, "network_error");
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
    if (response.status === 403 && body?.code === "permission_denied" && typeof window !== "undefined") window.dispatchEvent(new Event("mserp:access-invalidated"));
    throw new APIError(
      body?.error ??
        `Request failed (${response.status} ${response.statusText})`,
      response.status, body?.code ?? "request_failed",
    );
  }

  if (generation !== accessGeneration && !path.startsWith("/auth/")) throw new APIError("Access changed; refresh this view", 403, "access_changed");
  if (response.status === 204) return undefined as T;
  const value = await response.json();
  if (generation !== accessGeneration && !path.startsWith("/auth/")) throw new APIError("Access changed; refresh this view", 403, "access_changed");
  return value as T;
}

export async function fetchAuthSession(): Promise<AuthSession> {
	const session = await apiRequest<AuthSession>("/auth/session");
	csrfToken = session.csrfToken;
	acceptSession(session);
	return session;
}

export async function login(email: string, password: string, trustDevice = false): Promise<AuthSession> {
	const session = await apiRequest<AuthSession>("/auth/login", {
		method: "POST",
		body: JSON.stringify({ email, password, trustDevice }),
	});
	csrfToken = session.csrfToken;
	acceptSession(session);
	return session;
}

export const changePassword = (currentPassword: string, newPassword: string) => apiRequest<void>("/auth/password", { method: "POST", body: JSON.stringify({ currentPassword, newPassword }) });
export const fetchDriverPaySettings = () => apiRequest<import("./types").DriverPaySettings>("/settings/driver-pay");
export const saveDriverPaySettings = (settings: import("./types").DriverPaySettings) => apiRequest<import("./types").DriverPaySettings>("/settings/driver-pay", { method: "PUT", body: JSON.stringify(settings) });
export const fetchAccess = () => apiRequest<AccessData>("/settings/access");
export const saveTheme = (theme: string) => apiRequest<void>("/auth/theme", { method: "PUT", body: JSON.stringify({ theme }) });
export const fetchSystemTaskAssignments = () => apiRequest<SystemTaskAssignment[]>("/settings/system-tasks");
export const saveSystemTaskAssignment = (input: SystemTaskAssignment) => apiRequest<void>(`/settings/system-tasks/${input.kind}`, { method: "PUT", body: JSON.stringify(input) });
export const fetchTaskUsers = () => apiRequest<TaskUser[]>("/tasks/users");
export const saveUser = (user: ManagedUser) => apiRequest<{id: string}>(`/settings/users${user.id ? `/${user.id}` : ""}`, { method: user.id ? "PUT" : "POST", body: JSON.stringify({ id: user.id, username: user.username, email: user.email, roleId: user.roleId, active: user.active, version: user.version, password: user.password }) });
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
  ({ driver: DriverInput; separateConfirmed: boolean } | { linkDriverId: string }) & { addWeighMyTruck?: boolean; services?: import("./types").DriverServiceIntent[] }) =>
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
  scope?: "general";
  accountantVerified?: string;
  adminVerified?: string;
  responsibility?: "non_personal";
  chargeDriverId?: string;
  categoryId?: string;
  paymentType?: string;
  paidBy?: string;
  coveredBy?: string;
  unitNumber?: string;
  driverName?: string;
  dateFrom?: string;
  dateTo?: string;
  truckId?: string;
  driverId?: string;
}, signal?: AbortSignal) => paginatedRequest<ExpensePage>(withQuery("/expenses", query), signal);
// Expense reads include workOrderDescription for display; writes retain the saved expense description.
// Expense and work-order responses include per-record canEdit; input types omit it.
export const fetchExpense = (id: string, signal?: AbortSignal) => apiRequest<Expense>(`/expenses/${id}`, { signal });
export const fetchExpenseDocuments = (id: string, signal?: AbortSignal) => apiRequest<import("./types").ExpenseDocument[]>(`/expenses/${id}/documents`, { signal });
export const uploadExpenseDocument = (id: string, file: File) => {
  const body = new FormData(); body.append("file", file);
  return apiRequest<import("./types").ExpenseDocument>(`/expenses/${id}/documents`, { method: "POST", body });
};
export const expenseDocumentUrl = (id: string) => `${API_BASE}/expenses/documents/${encodeURIComponent(id)}`;

export const createExpense = (input: ExpenseInput) =>
  apiRequest<Expense>("/expenses", {
    method: "POST",
    body: JSON.stringify(expensePayload(input)),
  });
export const createExpenses = (expenses: ExpenseInput[]) =>
  apiRequest<Expense[]>("/expenses/bulk", {
    method: "POST",
    body: JSON.stringify({ expenses: expenses.map(expensePayload) }),
  });
export const extractExpenses = (text: string, file: File | null) => {
  const form = new FormData();
  form.append("includeWorkOrders","true");
  if (text.trim()) form.append("text", text.trim());
  if (file) form.append("file", file);
  return apiRequest<ExpenseExtraction>("/expenses/extract", {
    method: "POST",
    body: form,
  });
};
export const unverifyExpense = (id: string, verificationId: string) =>
  apiRequest<Expense>(`/expenses/${id}/unverify`, { method: "POST", body: JSON.stringify({ verificationId }) });

export const verifyExpense = (id: string, updatedAt: string) =>
  apiRequest<Expense>(`/expenses/${id}/verify`, { method: "POST", body: JSON.stringify({ updatedAt }) });

export const updateExpense = (id: string, input: ExpenseInput) =>
  apiRequest<Expense>(`/expenses/${id}`, {
    method: "PUT",
    body: JSON.stringify(expensePayload(input)),
  });
export const deleteExpense = (id: string) =>
  apiRequest<void>(`/expenses/${id}`, { method: "DELETE" });

// detailIds hydrates remembered expanded rows in the same paginated snapshot.
export const fetchDriverPay = (weekStart: string, signal?: AbortSignal, query?: PayPageQuery) => apiRequest<DriverPayWeek>(withQuery("/driver-pay", { weekStart, ...query }), { signal });
export const acceptPaySystemValues = (investor: boolean, input: PaySourceAcceptance) => apiRequest<import("./types").PaySourceReceipt>(`/${investor ? "investor-pay" : "driver-pay"}/accept-system?undoReceipt=1`, { method: "POST", body: JSON.stringify(input) });
export const undoPaySystemValues = (investor: boolean, id: string) => apiRequest<void>(`/${investor ? "investor-pay" : "driver-pay"}/undo-system`, { method: "POST", body: JSON.stringify({ id }) });
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
export const settleDriverPay = (weekStart: string, revision: string, driverId: string | undefined, reopen: boolean, reason: string, override = false) => apiRequest<DriverPayWeek>(`/driver-pay/${reopen ? "reopen" : "finalize"}`, { method: "POST", body: JSON.stringify({weekStart, revision, driverId, reason, override} satisfies PayrollSettlementInput) });

export const fetchExpenseSettings = () => apiRequest<ExpenseSetting[]>("/expense-settings");
export const saveExpenseSetting = (input: Omit<ExpenseSetting, "id" | "driverOnly"> & { id?: string }) => apiRequest<ExpenseSetting>(input.id ? `/expense-settings/${input.id}` : "/expense-settings", { method: input.id ? "PUT" : "POST", body: JSON.stringify(input) });
export const fetchDriverEscrowSetting = () => apiRequest<DriverEscrowSetting>("/escrows/settings");
export const saveDriverEscrowSetting = (input: DriverEscrowSetting) => apiRequest<DriverEscrowSetting>("/escrows/settings", { method: "PUT", body: JSON.stringify(input) });

export const fetchInvestorPay = (weekStart: string, truckId?: string, signal?: AbortSignal, query?: PayPageQuery) => apiRequest<DriverPayWeek>(withQuery("/investor-pay", { weekStart, truckId, ...query }), { signal });
export const saveInvestorPay = (input: DriverPayEdits) => apiRequest<DriverPayEdits>("/investor-pay", { method: "PUT", body: JSON.stringify(input) });
export const settleInvestorPay = (weekStart: string, revision: string, driverId: string | undefined, reopen: boolean, reason: string, override = false) => apiRequest<DriverPayWeek>(`/investor-pay/${reopen ? "reopen" : "finalize"}`, { method: "POST", body: JSON.stringify({ weekStart, revision, driverId, reason, override } satisfies PayrollSettlementInput) });
export const fetchTruckCharges = (weekStart?: string) => apiRequest<import("./types").TruckChargeData>(`/truck-charges${weekStart ? `?weekStart=${encodeURIComponent(weekStart)}` : ""}`);
export const saveTruckTerm = (input: import("./types").TruckTerm) => apiRequest<void>("/truck-charges/terms", { method: "PUT", body: JSON.stringify(input) });
export const saveTruckCharge = (input: import("./types").TruckChargePhase) => apiRequest<void>("/truck-charges/recurring", { method: "PUT", body: JSON.stringify(input) });
export const saveTruckCharges = (updates: import("./types").TruckChargePhase[]) => apiRequest<import("./types").TruckChargeBulkResult[]>("/truck-charges/recurring/bulk", { method: "PUT", body: JSON.stringify({ updates }) });

export const fetchUpdaters = () => apiRequest<Updater[]>("/updaters");
export const createUpdater = (value: UpdaterInput) => apiRequest<Updater>("/updaters", { method: "POST", body: JSON.stringify(value) });
export const updateUpdater = (id: string, value: UpdaterInput) => apiRequest<Updater>(`/updaters/${id}`, { method: "PUT", body: JSON.stringify(value) });
export const deleteUpdater = (id: string) => apiRequest<void>(`/updaters/${id}`, { method: "DELETE" });

export const fetchEscrows = (query: PageQuery & { driverId?: string; status?: string; group?: string; includeInactive?: boolean }) => apiRequest<EscrowPage>(withQuery("/escrows", query));

export const saveEscrowRelease = (escrowId: string, input: EscrowReleaseInput) => apiRequest<{ saved: boolean }>(`/escrows/${escrowId}/releases${input.version ? `/${input.id}` : ""}`, { method: input.version ? "PUT" : "POST", body: JSON.stringify(input) });

export const fetchEscrowTask = (id: string) => apiRequest<import("./types").EscrowTaskDetail>(`/tasks/escrow/${id}`);
export const completeEscrowTask = (id: string, input: import("./types").EscrowTaskDecision) => apiRequest<void>(`/tasks/escrow/${id}/complete`, { method: "POST", body: JSON.stringify(input) });
export const saveEscrowOpening = (id: string, input: { openingPaid: string; version: number; reason: string }) => apiRequest<void>(`/escrows/${id}/opening`, { method: "PUT", body: JSON.stringify(input) });
export const fetchWeighMyTruck = (signal?: AbortSignal) => apiRequest<import("./types").WeighMyTruckList>("/weighmytruck", { signal });
export const changeWeighMyTruck = (entry: import("./types").WeighMyTruckEntry, add: boolean) => apiRequest<void>("/weighmytruck/change", {
  method: "POST", body: JSON.stringify({ driverId: entry.driverId, membershipId: entry.id, version: entry.version, add }),
});
export const verifyWeighMyTruck = (entry: import("./types").WeighMyTruckEntry, enrolled: boolean, reason: string) => apiRequest<void>(`/weighmytruck/${entry.id}/verify`, {
  method: "POST", body: JSON.stringify({ version: entry.version, enrolled, reason }),
});
export const linkWeighMyTruck = (entry: import("./types").WeighMyTruckEntry, driverId: string) => apiRequest<void>(`/weighmytruck/${entry.id}/link`, {
  method: "POST", body: JSON.stringify({ version: entry.version, driverId }),
});

export const fetchWMTTransactions = (query: PageQuery & { from: string; to: string }, signal?: AbortSignal) => apiRequest<import("./types").WMTTransactions>(withQuery("/weighmytruck/transactions", query), { signal });
export const syncWMTTransactions = () => apiRequest<{ tickets: number }>("/weighmytruck/sync", { method: "POST" });
export const linkWMTTransaction = (entry: import("./types").WMTTransaction, driverId: string, truckId: string) => apiRequest<void>(`/weighmytruck/transactions/${entry.id}/link`, { method: "PUT", body: JSON.stringify({ driverId, truckId, version: entry.version }) });
import type { RelayDriverList, RelayDriverChange } from "./types";
export const fetchRelayDrivers = (signal?: AbortSignal) => apiRequest<RelayDriverList>("/relay-drivers", { signal });
export const refreshRelayDrivers = () => apiRequest<void>("/relay-drivers/refresh", { method: "POST" });
export const changeRelayDriver = (input: RelayDriverChange) => apiRequest<void>("/relay-drivers/change", { method: "POST", body: JSON.stringify(input) });
export const linkRelayRosterDriver = (relayId: string, driverId: string) => apiRequest<{ transactionsLinked: number }>(`/relay-drivers/${encodeURIComponent(relayId)}/link`, { method: "POST", body: JSON.stringify({ driverId }) });
export const verifyRelayDriverOperation = (id: string) => apiRequest<void>(`/relay-drivers/operations/${id}/verify`, { method: "POST", body: JSON.stringify({ reviewed: true }) });

import type { PrePassTruckLinks, PrePassTruckLinkInput, PrePassTruckPreview } from "./types";
export const fetchPrePassTruckLinks = (from: string, signal?: AbortSignal) => apiRequest<PrePassTruckLinks>(withQuery("/tolls/truck-links", { from }), { signal });
export const previewPrePassTruckLink = (input: PrePassTruckLinkInput) => apiRequest<PrePassTruckPreview>("/tolls/truck-links/preview", { method: "POST", body: JSON.stringify(input) });
export const savePrePassTruckLink = (input: PrePassTruckLinkInput) => apiRequest<{ saved: boolean }>("/tolls/truck-links", { method: "POST", body: JSON.stringify(input) });
export const undoPrePassTruckLink = (id: string, version: number) => apiRequest<{ saved: boolean }>(`/tolls/truck-links/${id}/undo`, { method: "POST", body: JSON.stringify({ version }) });
import type { RelayBulkResponse } from "./types";
export const changeRelayDrivers = (changes: RelayDriverChange[]) => apiRequest<RelayBulkResponse>("/relay-drivers/bulk", { method: "POST", body: JSON.stringify({ changes }) });

export const fetchDriverServices = (id: string, signal?: AbortSignal) => apiRequest<import("./types").DriverServiceSetup>(`/driver-services/${id}`, { signal });
export const requestDriverService = (id: string, input: import("./types").DriverServiceIntent) => apiRequest<void>(`/driver-services/${id}`, { method: "POST", body: JSON.stringify(input) });

export const fetchFiveELDTruckReview = (id: string) => apiRequest<import("./types").FiveELDTruckReview>(`/tasks/five-eld/${id}`);
export const completeFiveELDTruckReview = (id: string, version: number, truck: TruckInput) => apiRequest<{ truckId: string }>(`/tasks/five-eld/${id}/complete`, { method: "POST", body: JSON.stringify({ version, truck }) });

export type MaintenanceQuery = { truckId?: string; vendorId?: string; typeCode?: string; status?: string; search?: string; dateFrom?: string; dateTo?: string; expenseId?: string; page?: number; pageSize?: number };
export const fetchMaintenanceOptions = () => apiRequest<import("./types").MaintenanceOptions>("/maintenance/options");
export const fetchMaintenanceOdometer = (truckId: string, signal?: AbortSignal) => apiRequest<{reading: {mileage: number; reportedAt: string; source?: "samsara" | "five_eld" | "manual"} | null; message: string}>(withQuery("/maintenance/odometer",{truckId}),{signal});
export const fetchMaintenancePage = (query: MaintenanceQuery, signal?: AbortSignal) => apiRequest<import("./types").PaginatedResponse<import("./types").MaintenanceRecord>>(withQuery("/maintenance",query),{signal});
export const fetchMaintenance = (id: string) => apiRequest<import("./types").MaintenanceRecord>(`/maintenance/${id}`);
export const saveMaintenance = (input: import("./types").MaintenanceInput) => {
 const {legacyPaymentTotal,...rest}=input;
 const body={...rest};delete body.savedOwnerName;
 const expense=input.expense ? expensePayload({...input.expense,costBreakdown:input.expense.costBreakdown ?? input.services.filter(s=>s.amount!==null).map(s=>({name:s.name,amount:s.amount!}))}) : null;
 if(expense && legacyPaymentTotal!==undefined && expense.amount===legacyPaymentTotal && expense.paymentType.trim().toLowerCase()==="efs") {delete (expense as {baseAmount?:string}).baseAmount;}
 return apiRequest<import("./types").MaintenanceRecord>(input.version ? `/maintenance/${input.id}` : "/maintenance",{method:input.version ? "PUT":"POST",body:JSON.stringify({...body,expense})});
};
export const voidMaintenance = (id: string, version: number, reason: string) => apiRequest<void>(`/maintenance/${id}/void`,{method:"POST",body:JSON.stringify({version,reason})});
export const fetchMaintenanceHistory = (id: string) => apiRequest<import("./types").MaintenanceChange[]>(`/maintenance/${id}/history`);
export const fetchMaintenanceReports = (query: MaintenanceQuery) => apiRequest<import("./types").MaintenanceReport>(withQuery("/maintenance/reports",query));
export const fetchMaintenanceDue = (truckId?: string) => apiRequest<import("./types").MaintenanceDue[]>(withQuery("/maintenance/due",{truckId}));
export const fetchMaintenanceReadings = (truckId?: string) => apiRequest<import("./types").MaintenanceReading[]>(withQuery("/maintenance/readings",{truckId}));
export const saveMaintenanceReading = (input: import("./types").MaintenanceReading) => apiRequest<import("./types").MaintenanceReading>(input.id ? `/maintenance/readings/${input.id}` : "/maintenance/readings",{method:input.id ? "PUT":"POST",body:JSON.stringify({truckId:input.truckId,date:input.date,mileage:input.mileage,version:input.version,observedAt:input.observedAt,note:input.note})});
export const fetchOdometerSources = (truckId: string) => apiRequest<import("./types").OdometerSources>(`/maintenance/odometer-sources/${truckId}`);
export const saveOdometerSource = (truckId: string, source: import("./types").OdometerSource, version: number) => apiRequest<void>(`/maintenance/odometer-sources/${truckId}`,{method:"PUT",body:JSON.stringify({source,version})});
export const saveMaintenanceSchedule = (input: import("./types").MaintenanceSchedule) => apiRequest<import("./types").MaintenanceSchedule>(input.id ? `/maintenance/schedules/${input.id}` : "/maintenance/schedules",{method:input.id ? "PUT":"POST",body:JSON.stringify(input)});
export const saveMaintenanceVendor = (input: import("./types").MaintenanceVendor) => apiRequest<import("./types").MaintenanceVendor>(input.id ? `/maintenance/vendors/${input.id}` : "/maintenance/vendors",{method:input.id ? "PUT":"POST",body:JSON.stringify(input)});
export const saveMaintenanceType = (input: import("./types").MaintenanceType) => apiRequest<import("./types").MaintenanceType>(`/maintenance/types/${input.code}`,{method:"PUT",body:JSON.stringify(input)});
export const uploadMaintenanceDocument = (id: string,file: File) => {const body=new FormData();body.append("file",file);return apiRequest<import("./types").MaintenanceDocument>(`/maintenance/${id}/documents`,{method:"POST",body});};
export const maintenanceDocumentUrl = (id: string) => `${API_BASE}/maintenance/documents/${id}`;
export const extractMaintenance = (text: string,file: File | null) => {const body=new FormData();body.append("text",text);if(file)body.append("file",file);return apiRequest<{drafts:import("./types").MaintenanceAIDraft[]}>("/maintenance/extract",{method:"POST",body});};

export const fetchMaintenanceAccess = () => apiRequest<import("./types").ExpenseCategoryAccess>("/maintenance/access");

export const fetchPreventativeMaintenance = (query: {search: string; includeInactive: boolean; page: number; pageSize: number}, signal?: AbortSignal) => apiRequest<import("./types").PaginatedResponse<import("./types").PreventativeTruck>>(withQuery("/maintenance/preventative",query),{signal});
export const fetchFleetScopeSync = (signal?: AbortSignal) => apiRequest<import("./types").FleetScopeSyncView>("/fleetscope-sync", { signal });
export const captureFleetScopeSnapshot = () => apiRequest<void>("/fleetscope-sync/snapshot", { method: "POST", body: "{}" });
export const reviewFleetScopeRecord = (input: import("./types").FleetScopeReview) => apiRequest<void>("/fleetscope-sync/review", { method: "POST", body: JSON.stringify(input) });
export const fleetScopeSyncAction = (action: "assignments" | "activate" | "pause" | "resume", input: { snapshotId: string; sourceRevision: number; version: number }) => apiRequest<void>(`/fleetscope-sync/${action}`, { method: "POST", body: JSON.stringify(input) });
export const fleetScopeOperationAction = (id: string, action: "retry" | "supersede") => apiRequest<void>(`/fleetscope-sync/operations/${encodeURIComponent(id)}/${action}`, { method: "POST", body: "{}" });

export const fetchDriverOptions=()=>apiRequest<import("./types").DriverOption[]>("/lookups/drivers");
export const fetchTruckOptions=()=>apiRequest<import("./types").TruckOption[]>("/lookups/trucks");
export const fetchDispatcherOptions=()=>apiRequest<import("./types").DispatcherOption[]>("/lookups/dispatchers");
export const fetchUpdaterOptions=()=>apiRequest<import("./types").UpdaterOption[]>("/lookups/updaters");
export const fetchInvestorOptions=()=>apiRequest<import("./types").InvestorOption[]>("/lookups/investors");
export type DispatcherPayReport=Pick<FinancialDashboard,"period"|"availableWeeks"|"dispatchers">;
export const fetchDispatcherPay=(query:{weekStart?:string})=>apiRequest<DispatcherPayReport>(withQuery("/dispatcher-pay",query));

export const saveMaintenancePM = (input: Pick<import("./types").PreventativeTruck, "truckId" | "lastDate" | "lastMiles" | "recordId" | "recordVersion" | "baselineVersion">) => apiRequest<void>(`/maintenance/preventative/${input.truckId}`, {method:"PUT",body:JSON.stringify(input)});

export const fetchServicePrograms=(signal?:AbortSignal)=>apiRequest<import("./types").ServiceProgram[]>("/maintenance/programs",{signal});
export const saveServiceProgram=(p:import("./types").ServiceProgram)=>apiRequest<import("./types").ServiceProgram>(p.id?`/maintenance/programs/${p.id}`:"/maintenance/programs",{method:p.id?"PUT":"POST",body:JSON.stringify(p)});
export const fetchProgramTrucks=(program:string,truckId?:string,signal?:AbortSignal)=>apiRequest<import("./types").ProgramTruck[]>(withQuery(program?`/maintenance/programs/${program}/trucks`:"/maintenance/program-trucks",{truckId}),{signal});
export const saveProgramTrucks=(program:string,items:import("./types").ProgramTruckInput[])=>apiRequest<void>(`/maintenance/programs/${program}/trucks`,{method:"POST",body:JSON.stringify({items})});
export const saveProgramBaseline=(program:string,truck:import("./types").ProgramTruck,lastDate:string,lastMiles:number|null)=>apiRequest<void>(`/maintenance/programs/${program}/trucks/${truck.truckId}/baseline`,{method:"PUT",body:JSON.stringify({memberVersion:truck.memberVersion,recordId:truck.lastRecordId,recordVersion:truck.recordVersion,baselineVersion:truck.baselineVersion,lastDate,lastMiles})});
export const fetchProgramAssignees=()=>apiRequest<{people:{id:string;name:string}[];roles:{id:string;name:string}[]}>("/maintenance/program-assignees");
