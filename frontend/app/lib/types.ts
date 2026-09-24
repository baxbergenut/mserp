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

export interface FuelTransaction {
  flag?: TransactionFlag;
  id: string;
  relayTransactionId: string;
  driverId: string;
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
  id: string;
  fullName: string;
  isOwnerOperator: boolean;
  payType: PayType;
  payRate: number;
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
  fullName: string;
  isOwnerOperator: boolean;
  payType: PayType;
  payRate: number;
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

export interface Truck {
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

export interface Dispatcher {
  id: string;
  fullName: string;
  email: string | null;
  phone: string | null;
  payPercentage: number | null;
  active: boolean;
  notes: string | null;
  driverCount: number;
  createdAt: string;
  updatedAt: string;
}

export interface DispatcherInput {
  fullName: string;
  email: string;
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

export type ExpenseCategory =
  | "Maintenance"
  | "Other"
  | "Safety"
  | "HR"
  | "Administrative";

export interface ExpenseInput {
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

export type TelegramExpenseStatus =
  | "queued"
  | "processing"
  | "retry"
  | "completed"
  | "ignored"
  | "needs_review"
  | "failed";

export interface TelegramExpenseCandidate {
  confidence: number;
  company: string | null;
  category: string | null;
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
  evidence: string[];
}

export interface TelegramExpenseExtraction extends Partial<TelegramExpenseCandidate> {
  isExpense: boolean;
  expenses?: TelegramExpenseCandidate[];
  containsMultipleExpenses?: boolean;
}

export interface TelegramExpenseActivity {
  updateId: number;
  chatId: number;
  messageId: number;
  chatType: "group" | "supergroup";
  chatTitle: string | null;
  senderName: string | null;
  messageText: string | null;
  fileName: string | null;
  mimeType: string | null;
  mediaGroupId: string | null;
  status: TelegramExpenseStatus;
  attempts: number;
  nextAttemptAt: string;
  startedAt: string | null;
  completedAt: string | null;
  lastError: string | null;
  extractedData: TelegramExpenseExtraction | null;
  expenseId: string | null;
  expenseCount: number;
  expenseDate: string | null;
  company: string | null;
  category: string | null;
  amount: string | null;
  unitNumber: string | null;
  driverName: string | null;
  truckId: string | null;
  driverId: string | null;
  expenseType: string | null;
  description: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface TelegramExpenseActivityPage
  extends PaginatedResponse<TelegramExpenseActivity> {
  summary: {
    received24Hours: number;
    completed: number;
    inProgress: number;
    needsReview: number;
    ignored: number;
    failed: number;
    unmatched: number;
    missingExpense: number;
    lastCompletedAt: string | null;
  };
}

export interface AuthUser {
  id: string;
  username: string;
}

export interface AuthSession {
  user: AuthUser;
  csrfToken: string;
  expiresAt: string;
}
