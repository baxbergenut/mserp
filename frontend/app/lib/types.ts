export interface Load {
  ID: number;
  LoadID: string;
  DriverID: string | null;
  DispatcherID: string | null;
  ShipmentID: string;
  Status: string;
  LoadPay: string;
  TotalOtherPay: string;
  TotalPay: string;
  TotalMiles: string;
  PerMileRevenue: string;
  DispatcherName: string;
  DriverName: string;
  TeamDriverName: string | null;
  TruckUnit: string;
  CustomerName: string;
  PickupTime: string;
  DeliveryTime: string;
  PickupAppointmentTime: string;
  DeliveryAppointmentTime: string;
  CreatedDatetime: string;
  SyncedAt: string;
  RawPayload: unknown | null;
}

export interface PaginatedResponse<T> {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

export interface LoadPage extends PaginatedResponse<Load> {
  options: {
    statuses: string[];
    customers: string[];
    dispatchers: string[];
    drivers: string[];
  };
}

export interface SyncLoadsResult {
  fetched: number;
  saved: number;
  since: string;
}

export interface TransactionLoadEvidence {
  id: number;
  loadId: string;
  truckUnit: string;
  driverName: string;
  pickupDate: string;
  deliveryDate: string;
  appointmentFallback: boolean;
}

export interface TransactionFlag {
  status: "review" | "data_issue";
  reason: string;
  truckUnit: string;
  transactionDate: string;
  bufferDays: number;
  previousLoad: TransactionLoadEvidence | null;
  nextLoad: TransactionLoadEvidence | null;
  relatedLoad: TransactionLoadEvidence | null;
  loadsSyncedAt: string | null;
}

export interface RelayIdentityTask {
  id: string;
  environment: "production" | "staging";
  relayDriverId: string;
  integrationId: string | null;
  name: string;
  email: string | null;
  /** Ten ASCII digits without the +1 country code, or null when not recorded. */
  phone: string | null;
  transactionCount: number;
  latestTransaction: string | null;
  rejectedDriverIds: string[];
  suggestions: {
    driverId: string;
    name: string;
    email: string | null;
    /** Ten ASCII digits without the +1 country code, or null when not recorded. */
    phone: string | null;
    active: boolean;
    reasons: string[];
  }[];
}

export interface FuelTransaction {
  flag?: TransactionFlag;
  id: string;
  relayTransactionId: string;
  driverId: string | null;
  driverName: string;
  relayDriverId: string;
  relayIntegrationId: string | null;
  purchasedAt: string;
  merchantName: string;
  locationName: string;
  city: string;
  state: string;
  timezone: string;
  totalAmountPaid: number;
  totalRetailPrice: number;
  totalAmountSaved: number;
  cashAdvance: number | null;
  currencyCode: string;
  fuelAmount: number;
  defAmount: number;
  otherAmount: number;
  fuelVolume: number;
  defVolume: number;
  fuelCodeType: string | null;
  isDirectBill: boolean;
}

export interface FuelTransactionPage extends PaginatedResponse<FuelTransaction> {
  options: { drivers: string[]; states: string[] };
  summary: { spend: number; saved: number; gallons: number };
}

export interface FuelDashboard {
  year: number;
  totals: { spend: number; gallons: number; saved: number };
  monthly: Array<{
    month: string;
    spend: number;
    gallons: number;
    pricePerGallon: number;
    discountPerGallon: number;
  }>;
  weekly: Array<{
    weekStart: string;
    fuelSpend: number;
    grossRevenue: number;
    miles: number;
    fuelToGrossRatio: number | null;
    averageFuelPrice: number | null;
    revenuePerMile: number | null;
  }>;
  statePrices: Array<{
    state: string;
    averagePrice: number;
    gallons: number;
    transactionCount: number;
  }>;
  methodology: {
    fuelScope: string;
    revenueScope: string;
    revenueDate: string;
    weekStartsOn: string;
    fuelDateTimezone: string;
  };
}

export interface FinancialDashboard {
  period: {
    kind: "week";
    dateFrom: string | null;
    dateTo: string | null;
  };
  availableWeeks: string[];
  totals: {
    gross: number;
    driverPay: number;
    fuel: number;
    tolls: number;
    deductedFuel: number;
    deductedTolls: number;
    knownExpenses: number;
    estimatedProfit: number;
    estimatedProfitMargin: number;
    miles: number;
    loadCount: number;
    revenuePerMile: number;
    unattributedTolls: number;
    unattributedFuel: number;
  };
  expenses: Array<{
    category: string;
    amount: number | null;
    available: boolean;
    note: string;
  }>;
  drivers: Array<{
    driverId: string;
    driverName: string;
    isOwnerOperator: boolean;
    deductsExpenses: boolean;
    payType: PayType;
    payRate: number;
    gross: number;
    pay: number;
    fuel: number;
    tolls: number;
    miles: number;
    loadCount: number;
    loadNumbers: string[];
    revenuePerMile: number;
    settlement: number;
    contribution: number;
  }>;
  dispatchers: Array<{
    dispatcherId: string | null;
    dispatcherName: string;
    gross: number;
    driverCount: number;
    loadCount: number;
    payPercentage: number | null;
    pay: number | null;
  }>;
  methodology: {
    gross: string;
    driverPay: string;
    fuel: string;
    tolls: string;
    profit: string;
    week: string;
  };
}

export interface SyncFuelResult {
  fetched: number;
  saved: number;
  excluded: number;
  daysFetched: number;
  daysSkipped: number;
  startDate: string;
  endDate: string;
}

export interface SyncTollsResult {
  fetched: number;
  saved: number;
  unmatched: number;
  daysFetched: number;
  daysSkipped: number;
  startDate: string;
  endDate: string;
}

export type SortKey =
  | "PickupTime"
  | "DeliveryTime"
  | "TotalPay"
  | "TotalMiles"
  | "PerMileRevenue";

export type SortDir = "asc" | "desc";

export type PeriodKey =
  | "today"
  | "thisWeek"
  | "thisMonth"
  | "lastMonth"
  | "allTime";

export interface ChartDataPoint {
  label: string;
  value: number;
}

export type PayType = "cpm" | "gross_percentage";
export type TruckStatus =
  | "available"
  | "assigned"
  | "maintenance"
  | "out_of_service";

export interface Driver {
  driverHome: string;
  homeVersion: number;
  id: string;
  fullName: string;
  driverType: "O" | "M" | "%-O" | "M-O" | "%";
  isOwnerOperator: boolean;
  payType: PayType;
  payRate: number;
  /** Ten ASCII digits without the +1 country code, or null when not recorded. */
  phone: string | null;
  email: string | null;
  licenseNumber: string | null;
  licenseState: string | null;
  licenseExpires: string | null;
  hireDate: string | null;
  address: string | null;
  city: string | null;
  state: string | null;
  postalCode: string | null;
  emergencyContact: string | null;
  dispatcherId: string | null;
  dispatcherName: string | null;
  truckId: string | null;
  truckUnit: string | null;
  active: boolean;
  notes: string | null;
  cdlFileId: string | null;
  cdlFileName: string | null;
  cdlFileContentType: string | null;
  cdlFileSizeBytes: number | null;
  createdAt: string;
  updatedAt: string;
}

export interface DriverInput {
  assignmentWeek?: string;
  driverHome?: string;
  homeVersion?: number;
  chargePauseWeek?: string;
  fullName: string;
  isOwnerOperator: boolean;
  payType: PayType;
  payRate: number;
  /** Optional phone: ten ASCII digits, or an empty string. */
  phone: string;
  email: string;
  licenseNumber: string;
  licenseState: string;
  licenseExpires: string;
  hireDate: string;
  address: string;
  city: string;
  state: string;
  postalCode: string;
  emergencyContact: string;
  dispatcherId: string | null;
  truckId: string | null;
  active: boolean;
  notes: string;
  cdlFileId: string | null;
}

export interface DriverBoardEntry {
  undoId?: number;
  statusEdited?: boolean;
  resolveCurrentLoad?: boolean;
  driverId: string;
  currentLoad: string;
  trailerNumber: string;
  status: string;
  destination: string;
  eta: string;
  notes: string;
  homeTime: string;
  driverHome: string;
  homeVersion: number;
  version: number;
}

export interface DriverBoardDriver {
  dispatcherExtension?: number | null;
  mainUpdaterName?: string;
  mainUpdaterExtension?: number | null;
  afterHoursUpdaterName?: string;
  afterHoursUpdaterExtension?: number | null;
  id: string;
  fullName: string;
  driverType: "O" | "M" | "%-O" | "M-O" | "%";
  truckUnit: string;
  phone: string;
  dispatcherId: string;
  dispatcherName: string;
  location: DriverBoardLocation | null;
}

export interface DriverBoardLocation {
  latitude: number;
  longitude: number;
  reportedAt: string;
  providerTruckNumber: string;
}

export interface FiveELDBoardSummary {
  configured: boolean;
  lastAttemptAt: string | null;
  lastSuccessAt: string | null;
  lastError: string;
  unmatched: number;
  ambiguous: number;
  invalid: number;
}

export interface DriverBoard {
  weekStart: string;
  drivers: DriverBoardDriver[];
  entries: DriverBoardEntry[];
  grossEntries: GrossBoardEntry[];
  loads: Record<string, BoardLoads>;
  eld: FiveELDBoardSummary;
}

export interface SyncFiveELDResult {
  fetched: number;
  saved: number;
  unmatched: number;
  ambiguous: number;
  invalid: number;
  syncedAt: string;
}

export interface BoardStop { key: string; type: string; location: string; appointment: string }
export interface BoardLoad {
  planId: string; date: string; slot: number; number: string; loadId: number | null;
  sourceStatus: string; sourceDriver: string; syncedAt: string; stops: BoardStop[]; warning: string;
}
export interface BoardLoads {
  week: BoardLoad[];
  current: BoardLoad | null; next: BoardLoad[]; earlier: BoardLoad[]; hidden: BoardLoad[]; unavailable: BoardLoad[];
  destinationSource: boolean; sourceDestination: string; stopKey: string;
  fromDate: string; revision: string; customOrder: boolean;
}
export interface BoardLoadAction {
  action: "advance" | "select" | "clear" | "order" | "reset_order" | "hide" | "restore" | "source" | "manual";
  planId?: string; order?: string[]; stopKey?: string;
}

export interface DriverBoardEvent {
  id: number; driverId: string; driverName: string; actorId: string; actorName: string;
  source: string; undoOf: number | null; before: Record<string, string>; after: Record<string, string>; createdAt: string;
}
export interface DriverBoardHistory { items: DriverBoardEvent[]; nextCursor: number }

export type DriverDirectoryEntry = Driver & { intakeId?: string };

export interface DriverIntake {
  id: string;
  receivedAt: string;
  driver: {
    id: string;
    fullName: string;
    driverType: "company" | "owner_operator";
    hireDate: string;
    phone?: string;
    email?: string;
    address?: string;
    city?: string;
    state?: string;
    postalCode?: string;
    licenseNumber?: string;
    licenseState?: string;
    licenseExpires?: string;
  };
  candidates: Pick<Driver, "id" | "fullName" | "phone" | "email">[];
}

export interface Truck {
  ownerId: string;
  ownerName: string;
  id: string;
  unitNumber: string;
  vin: string | null;
  year: number | null;
  make: string | null;
  model: string | null;
  licensePlate: string | null;
  licenseState: string | null;
  isCompanyOwned: boolean;
  status: TruckStatus;
  mileage: number | null;
  registrationExpires: string | null;
  insuranceExpires: string | null;
  lastServiceDate: string | null;
  nextServiceMiles: number | null;
  driverId: string | null;
  driverName: string | null;
  active: boolean;
  notes: string | null;
  irpFileId: string | null;
  irpFileName: string | null;
  irpFileContentType: string | null;
  irpFileSizeBytes: number | null;
  createdAt: string;
  updatedAt: string;
}

export interface TruckInput {
  assignmentWeek?: string;
  ownerId: string | null;
  unitNumber: string;
  vin: string;
  year: number | null;
  make: string;
  model: string;
  licensePlate: string;
  licenseState: string;
  isCompanyOwned: boolean;
  status: TruckStatus;
  mileage: number | null;
  registrationExpires: string;
  insuranceExpires: string;
  lastServiceDate: string;
  nextServiceMiles: number | null;
  driverId: string | null;
  active: boolean;
  notes: string;
  irpFileId: string | null;
}

export interface StoredFileMetadata {
  id: string;
  fileName: string;
  contentType: string;
  sizeBytes: number;
  sha256: string;
  createdAt: string;
}

export interface CabCardFields {
  unitNumber: string;
  vin: string;
  year: number | null;
  make: string;
  model: string;
  licensePlate: string;
  licenseState: string;
  registrationExpires: string;
}

export interface IRPFileUploadResult {
  file: StoredFileMetadata;
  fields: CabCardFields;
}

export interface CDLFields {
  fullName: string;
  licenseNumber: string;
  licenseState: string;
  licenseExpires: string;
  address: string;
  city: string;
  state: string;
  postalCode: string;
}

export interface CDLFileUploadResult {
  file: StoredFileMetadata;
  fields: CDLFields;
}

export interface Updater {
  id: string;
  fullName: string;
  shift: "main" | "after_hours";
  extension: number | null;
  version: number;
  dispatcherNames: string[];
}
export type UpdaterInput = Pick<Updater, "fullName" | "shift" | "extension"> & { version?: number };

export interface Dispatcher {
  extension: number | null;
  mainUpdaterId: string | null;
  afterHoursUpdaterId: string | null;
  id: string;
  fullName: string;
  email: string | null;
  /** Ten ASCII digits without the +1 country code, or null when not recorded. */
  phone: string | null;
  payPercentage: number | null;
  active: boolean;
  notes: string | null;
  driverCount: number;
  createdAt: string;
  updatedAt: string;
}

export interface DispatcherInput {
  extension?: number | null;
  updaters?: { mainUpdaterId: string | null; afterHoursUpdaterId: string | null };
  assignmentWeek?: string;
  fullName: string;
  email: string;
  /** Optional phone: ten ASCII digits, or an empty string. */
  phone: string;
  payPercentage: number | null;
  driverIds: string[];
  active: boolean;
  notes: string;
}

export interface Toll {
  flag?: TransactionFlag;
  id: string;
  truckId: string | null;
  truckUnit: string;
  postingDate: string;
  invoiceDate: string;
  customerId: string;
  source: string;
  readType: string;
  prePassTagId: string | null;
  transponderOrPlate: string;
  equipmentUnit: string;
  agency: string;
  tollAgencyState: string | null;
  tollAgencyName: string | null;
  entryPlazaName: string | null;
  exitPlazaName: string | null;
  entryPlaza: string | null;
  entryDate: string | null;
  entryTime: string | null;
  exitPlaza: string;
  exitDate: string;
  exitTime: string;
  tollClass: string;
  miles: number | null;
  amount: number;
  reportFileName: string;
}

export interface TollPage extends PaginatedResponse<Toll> {
  options: { units: string[]; agencies: string[] };
  summary: { amount: number; truckCount: number };
}

export interface TollDashboardPoint {
  label: string;
  spend: number;
  transactionCount: number;
}

export interface TollDashboard {
  dateFrom: string;
  dateTo: string;
  totals: { spend: number; transactionCount: number; truckCount: number };
  monthly: TollDashboardPoint[];
  weekly: TollDashboardPoint[];
  agencies: TollDashboardPoint[];
  trucks: TollDashboardPoint[];
  states: TollDashboardPoint[];
  unmapped: TollDashboardPoint[];
}

export interface Expense {
 payments: {weekStart: string; amount: string}[];
 ownerId: string | null;
 ownerName: string | null;
 chargeDriverId: string | null;
  paidAmount: string | null;
  remainingAmount: string | null;
  driverSettled: boolean;
  id: string;
  truckId: string | null;
  driverId: string | null;
  company: string;
  category: ExpenseCategory;
  weekStart: string | null;
  expenseDate: string | null;
  unitNumber: string | null;
  driverName: string | null;
  amount: string | null;
  paymentType: string | null;
  expenseType: string | null;
  referenceNumber: string | null;
  description: string | null;
  coveredBy: string | null;
  paidBy: string | null;
  managerVerified: boolean;
  accountingVerified: boolean;
  sourceSpreadsheetId: string | null;
  sourceSheet: string | null;
  sourceRow: number | null;
  createdAt: string;
  updatedAt: string;
}

export type ExpenseCategory = string;
export type ExpenseSettingKind = "category" | "name" | "payment_method" | "payer";
export interface ExpenseSetting {
  id: string;
  kind: ExpenseSettingKind;
  categoryId: string | null;
  name: string;
  active: boolean;
  version: number;
}

export interface ExpenseInput {
 ownerId?: string | null;
  company: string;
  category: ExpenseCategory;
  expenseDate: string;
  truckId: string | null;
  driverId: string | null;
  unitNumber: string;
  driverName: string;
  amount: string;
  paymentType: string;
  expenseType: string;
  referenceNumber: string;
  description: string;
  coveredBy: string;
  paidBy: string;
  managerVerified: boolean;
  accountingVerified: boolean;
}

export interface ExpensePage extends PaginatedResponse<Expense> {
  options: {
    settings: ExpenseSetting[];
    categories: string[];
    companies: string[];
    paymentTypes: string[];
    expenseTypes: string[];
    paidBy: string[];
    coveredBy: string[];
  };
  summary: {
    amount: string;
    incompleteCount: number;
  };
}

export interface AIExpenseDraft extends Omit<ExpenseInput, "managerVerified" | "accountingVerified"> {
  confidence: number;
  evidence: string[];
}

export interface ExpenseExtraction {
  expenses: AIExpenseDraft[];
}

export interface AuthUser {
  id: string;
  username: string;
  email: string;
  roleId: string;
  permissions: string[];
}

export interface ManagedUser {
  id: string; username: string; email: string; roleId: string;
  active: boolean; version: number; password?: string;
}
export interface AccessRole {
  id: string; name: string; permissions: string[]; system: boolean; version: number;
}
export interface AccessData {
  users: ManagedUser[]; roles: AccessRole[]; permissions: { key: string; label: string }[];
}

export interface AuthSession {
  user: AuthUser;
  csrfToken: string;
  expiresAt: string;
}

export type GrossBoardDayStatus = "" | "SHOP" | "HOME" | "RESET" | "IN TRANSIT" | "REJECTED" | "LOAD CANCELLED" | "NO LOAD" | "STUCK" | "LATE DEL" | "TRUCK ISSUE" | "LEFT" | "NEW DRIVER" | "DEADHEAD";

export interface GrossBoardEntry {
  slot: number;
  deleted: boolean;
  driverId: string;
  date: string;
  loadNumber: string;
  loadRecordId: number | null;
  dayStatus: GrossBoardDayStatus;
  originalRate: string;
  driverRate: string;
  miles: string;
  version: number;
  enteredOriginalRate: string;
  enteredMiles: string;
  systemOriginalRate?: string;
  systemMiles?: string;
  duplicate: boolean;
  acceptSystemValues?: boolean;
}

export interface GrossBoardDriver {
  id: string;
  fullName: string;
  truckUnit: string;
  dispatcherId: string;
  dispatcherName: string;
  active: boolean;
}

export interface GrossBoard {
  weekStart: string;
  drivers: GrossBoardDriver[];
  entries: GrossBoardEntry[];
  balances: { driverId: string; openingBalance: string; openingIncomplete: number }[];
}

export interface GrossBoardBalanceLine {
  date: string;
  loadNumber: string;
  originalRate: string;
  driverRate: string;
  change: string;
  balance: string;
  duplicate: boolean;
}

export interface GrossBoardLoad {
  id: number;
  loadNumber: string;
  originalRate: string;
  miles: string;
  driverName: string;
  pickupDate: string;
}
export interface CustomTask {
  id: string;
  title: string;
  notes: string;
  completedAt: string | null;
  createdBy: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface CustomTaskInput {
  title: string;
  notes: string;
}

export interface DriverPayAdjustment {
  id: string;
  kind: "reimbursement" | "addition" | "deduction";
  name: string;
  note: string;
  amount: string;
}
export interface ExpenseDeduction {
  category?: ExpenseCategory;
  expenseId: string;
  name: string;
  expenseDate: string;
  total: string;
  available: string;
  openingBalance: string;
  amount: string;
  remaining: string;
  version: number;
  saved: boolean;
  apply?: boolean;
}
export interface DriverPayEdits {
  expenseDeductions?: ExpenseDeduction[];
  generatedCharges?: ChargeOccurrence[];
  driverId: string;
  weekStart: string;
  notes: string;
  comments: Record<string, string>;
  adjustments: DriverPayAdjustment[];
  fuelOverride: string | null;
  tollOverride: string | null;
  version: number;
}
export interface DriverPayLoad {
  sourceDriverId?: string;
  driverFee?: string;
  date: string;
  slot: number;
  loadNumber: string;
  loadRecordId: number | null;
  commentKey: string;
  pickupDate: string;
  pickupLocation: string;
  deliveryLocation: string;
  originalRate: string;
  driverGross: string;
  totalMiles: string;
  loadedMiles: string;
  deadheadMiles: string;
  fee: string;
  issues: string[];
}
export interface DriverPayDriver {
  investorId?: string;
  truckId?: string;
  autoCharges?: { name: string; amount: string; source: string }[];
  issues?: string[];
 settlement?: PayrollSettlement;
  id: string;
  isOwnerOperator: boolean;
  fullName: string;
  truckUnit: string;
  dispatcherId: string;
  dispatcherName: string;
  payType: PayType;
  payRate: string;
  fuelTotal: string;
  tollTotal: string;
  loads: DriverPayLoad[];
  edits: DriverPayEdits;
}
export interface DriverPayWeek {
 issues?: string[];
 revision: string;
  weekStart: string;
  drivers: DriverPayDriver[];
}
export interface AssignmentHistoryEntry {
  id: string;
  kind: "truck" | "dispatcher";
  relatedId: string | null;
  name: string;
  assignedAt: string;
  unassignedAt: string | null;
  startKnown: boolean;
  source: string;
}

export interface Investor {
  id: string;
  fullName: string;
  driverId: string | null;
  isCompany: boolean;
  email: string | null;
  /** Ten ASCII digits without the +1 country code, or null when not recorded. */
  phone: string | null;
  notes: string | null;
  active: boolean;
  trucks: Array<{ id: string; unitNumber: string }>;
  createdAt: string;
  updatedAt: string;
}
export interface InvestorInput {
  fullName: string;
  driverId: string | null;
  email: string;
  /** Optional phone: ten ASCII digits, or an empty string. */
  phone: string;
  notes: string;
  active: boolean;
}

export type ChargeEligibility = "calendar" | "loads" | "no_loads";
export interface ChargeCell {
 driverId: string; typeId: string; weekStart: string; included: boolean; amount: string;
 scheduleId: string; version: number; typeVersion: number;
}
export interface ChargeType {
 amounts: string[]; eligibility: ChargeEligibility; rules: {weekStart: string; eligibility: ChargeEligibility}[];

  id: string; name: string; direction: "charge" | "reimbursement"; amount: string; archived: boolean; version: number;
}
export interface ChargePhase { weekStart: string; amount: string; paused: boolean }
export interface ChargeOccurrence {
  scheduleId: string; weekStart: string; kind: "recurring" | "installment"; name: string;
  scheduledAmount: string; amount: string; overridden: boolean; confirmedAt: string | null; confirmedBy: string | null;
  version: number; scheduleVersion: number; typeVersion: number; reset?: boolean;
}
export interface ChargeSchedule {
  typeVersion: number;
  installmentCount: number;
  id: string; driverId: string; driverName: string; typeId: string | null; kind: "recurring" | "installment";
  name: string; direction: "charge" | "reimbursement"; startWeek: string; endWeek: string | null;
  eligibility: ChargeEligibility; total: string | null; version: number; phases: ChargePhase[]; occurrences: ChargeOccurrence[];
  confirmed: string; remaining: string; scheduled: string; completionWeek: string; status: string;
}
export interface ChargeData { types: ChargeType[]; schedules: ChargeSchedule[]; currentWeek: string }
export interface ChargeCreate {
  driverIds: string[]; typeId: string; kind: "recurring" | "installment"; name: string; amount: string; total: string;
  installments: number; startWeek: string; endWeek: string | null; eligibility: "calendar" | "loads";
}
export interface ChargeBulk {
  targets: {id: string; version: number}[]; action: "amount" | "pause" | "resume" | "end"; weekStart: string; amount: string;
}
export interface ChargeEvent { id: number; action: string; actor: string; details: unknown; createdAt: string }

export interface PayrollSettlement { finalized: boolean; version: number; finalizedAt: string; finalizedBy: string; reopenedAt: string | null; reason: string }
export interface DriverPayHistoryRow { weekStart: string; driver: DriverPayDriver }
export interface SettlementEvent { action: string; version: number; actor: string; reason: string; createdAt: string; report: DriverPayDriver }

export interface TruckTerm { truckId: string; ownerId: string; weekStart: string; sharePercent: string; version: number }
export interface TruckChargePhase { truckId: string; typeId: string; weekStart: string; amount: string; included: boolean; version: number; typeVersion: number; moveScheduleId?: string; moveScheduleVersion?: number }
export interface TruckChargeData { eligibleTruckIds: string[]; terms: TruckTerm[]; phases: TruckChargePhase[] }
