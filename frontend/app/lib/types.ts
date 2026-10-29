export interface DispatcherCommissionRules {
  homeDayCredit: number;
  maxTierPromotion: number;
  tiers: { minGross: number; brackets: { minRPM: number; percent: number }[] }[];
}
export interface DispatcherCommissionRuleVersion {
  id: number;
  effectiveWeek: string;
  rules: DispatcherCommissionRules;
  createdAt: string;
}
export interface DispatcherCommissionRow {
  driverId: string;
  driverName: string;
  truckUnits: string[];
  homeDays: number;
  autoHomeDays?: boolean;
  boardHomeDays?: number;
  note: string;
  version: number;
  gross: number;
  creditedGross: number;
  creditedMiles?: number;
  miles: number;
  rpm: number;
  adjustedGross: number;
  actualTier: number;
  selectedTier: number;
  rate: number;
  pay: number;
  loadCount: number;
  issues: string[];
}
export interface DispatcherCommissionStatement {
  dispatcherId: string;
  dispatcherName: string;
  rows: DispatcherCommissionRow[];
  gross: number;
  pay: number;
  loadCount: number;
  rule: DispatcherCommissionRuleVersion | null;
  revision: string;
  finalized: boolean;
  sourceChanged: boolean;
  version: number;
  finalizedAt?: string;
}
export interface DispatcherCommissionReport {
  weekStart: string;
  availableWeeks: string[];
  rule: DispatcherCommissionRuleVersion | null;
  statements: DispatcherCommissionStatement[];
}

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

export type EmploymentType = "company" | "owner_operator" | "investor_driver" | "unassigned";
export type PayType = "cpm" | "gross_percentage";
export type { InventoryFields, InventoryRecord, InventoryCollection, InventoryData, InventoryMovement, InventoryBalance, InventoryEvent, InventorySnapshot, InventoryImport, InventoryImportRow } from "./inventoryTypes";
export type TruckStatus =
  | "available"
  | "assigned"
  | "maintenance"
  | "out_of_service"
 | "terminated";

export type DriverStatus = "active" | "vacation" | "home" | "terminated";
export type DriverAvailability = "active" | "home" | "vacation" | "rest" | "reset" | "family_issue" | "health_issue" | "new_driver";
export interface FleetStatusRule { boardStatus:string;availability:string;truckCondition:string;requiresActive:boolean;requiresReady:boolean }
export interface DriverLeavePlan { homeStart?: string; homeEnd?: string; vacationReturn?: string }
export interface FleetStatusChange {status:string;version:number;effectiveAt?:string;reason:string;leavePlan?:DriverLeavePlan;leavePlanVersion?:number}
export interface FleetStatusHistoryEntry { id:number;entityKind:"driver"|"truck";entityId:string;entityName:string;track:string;previousStatus:string;status:string;effectiveAt:string;endedAt:string|null;recordedAt:string;actorName:string;source:string;sourceRef:string;relatedTruckName:string;actionId:number;baseline:boolean;reason:string }
export interface FleetStatusHistory {items:FleetStatusHistoryEntry[];hasMore:boolean;snapshot:number}
export interface DriverStatusChange {
  status: DriverStatus;
  terminationDate: string;
  assignmentWeek: string;
  chargePauseWeek: string;
  updatedAt: string;
}
export interface Driver {
 leavePlan?: DriverLeavePlan;
 leavePlanVersion?: number;
	employmentStatus?: "active" | "terminated";
	truckOperatingStatus?:TruckOperatingStatus|"";
	availabilityStatus?: DriverAvailability;
	statusVersion?: number;
  firstName?: string | null;
  lastName?: string | null;
  employmentType: EmploymentType;
  teamVersion?: number;
  teamTruckId?: string;
  teamTruckUnit?: string;
  teamStartWeek?: string;
  teamRole?: "main" | "team";
  teamPartnerId?: string;
  teamPartnerName?: string;
  status?: DriverStatus;
  terminationDate?: string | null;
  id: string;
  fullName: string;
  driverType: "O" | "M" | "%-O" | "M-O" | "%";
  isOwnerOperator: boolean;
  payType?: PayType;
  payRate?: number;
  /** Ten ASCII digits without the +1 country code, or null when not recorded. */
  phone: string | null;
  email: string | null;
  licenseNumber?: string | null;
  licenseState?: string | null;
  licenseExpires?: string | null;
  hireDate: string | null;
	dateOfBirth?: string | null;
  address?: string | null;
  city?: string | null;
  state?: string | null;
  postalCode?: string | null;
  emergencyContact?: string | null;
  dispatcherId: string | null;
  dispatcherName: string | null;
  truckId: string | null;
  truckUnit: string | null;
  active: boolean;
  notes: string | null;
  cdlFileId?: string | null;
  cdlFileName?: string | null;
  cdlFileContentType?: string | null;
  cdlFileSizeBytes?: number | null;
  createdAt: string;
  updatedAt: string;
}

export interface DriverInput {
	statusVersion?:number;
  firstName?: string;
  lastName?: string;
  newTruck?: TruckInput;
  newTruckOwnedByDriver?: boolean;
  services?: DriverServiceIntent[];
  isTeamDriver?: boolean;
  teamVersion?: number;
  teamMainDriverId?: string;
  teamMainVersion?: number;
  status?: DriverStatus;
  terminationDate?: string;
  /** Effective calendar date; legacy field name retained for API compatibility. */
  assignmentWeek?: string;
  chargePauseWeek?: string;
  escrowAmount: string;
  fullName: string;
  payType: PayType;
  payRate: number;
  /** Optional phone: ten ASCII digits, or an empty string. */
  phone: string;
  email: string;
  licenseNumber: string;
  licenseState: string;
  licenseExpires: string;
  hireDate: string;
	dateOfBirth?: string;
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

export interface DriverSetupDefaults {
  escrowAmount: string;
}

export interface DriverBoardEntry {
 statusReason?: string;
 leavePlan?: DriverLeavePlan;
	effectiveAt?: string;
  undoId?: number;
  statusEdited?: boolean;
  resolveCurrentLoad?: boolean;
  driverId: string;
  currentLoad: string;
  trailerNumber: string;
  trailerNote: string;
  status: string;
  destination: string;
  eta: string;
  notes: string;
  homeTime: string;
  version: number;
}

export interface DriverBoardDriver {
	availabilityStatus?: DriverAvailability;
	statusVersion?: number;
 truckOperatingStatus?: TruckOperatingStatus | "";
  truckId: string;
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
  source?: "samsara" | "five_eld";
  heading: number | null;
  latitude: number;
  longitude: number;
  reportedAt: string;
  providerTruckNumber: string;
}

export interface FleetLocation {
  truckId: string;
  truckUnit: string;
  location: DriverBoardLocation | null;
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
	statusSyncEnabled?: boolean;
  driverOrderRevisions: Record<string, string>;
  dispatcherOrderRevision: string;
  weekStart: string;
  drivers: DriverBoardDriver[];
  entries: DriverBoardEntry[];
  grossEntries: GrossBoardEntry[];
  loads: Record<string, BoardLoads>;
  eld: FiveELDBoardSummary;
}

export interface DriverBoardOrderAction {
  dispatcherId: string;
  driverId: string;
  targetDriverId: string;
  after: boolean;
  revision: string;
}

export interface DriverBoardOrder {
  dispatcherId: string;
  driverIds: string[];
  revision: string;
}

export interface DispatcherBoardOrderAction {
  dispatcherId: string;
  targetDispatcherId: string;
  after: boolean;
  revision: string;
}

export interface DispatcherBoardOrder {
  dispatcherIds: string[];
  revision: string;
}

export interface SyncFiveELDResult {
  fetched: number;
  saved: number;
  unmatched: number;
  ambiguous: number;
  invalid: number;
  syncedAt: string;
}

export interface BoardSchedule { kind: string; label: string; detail: string; reason?: string; beginAt?: string; endAt?: string }
export interface BoardAutomation {
  context: string; appointment?: BoardSchedule; manualStopTime: string; stopType?: "pickup" | "delivery"; arrived: boolean;
  arrivalOverride: "auto" | "arrived" | "not_arrived"; observedAt: string; stale: boolean;
  travelMode: "auto" | "manual"; travelValue: string; reason?: string;
  travelStale?: boolean; travelArrivalAt?: string;
  requiresConfirmation: boolean; routingEnabled: boolean; manualETAValid: boolean;
}
export interface BoardStop { key: string; type: string; location: string; appointment: string; providerId?: number; latitude?: number; longitude?: number; schedule?: BoardSchedule }
export interface BoardLoad {
  manual?: boolean;
  planId: string; date: string; slot: number; number: string; loadId: number | null;
  sourceStatus: string; sourceDriver: string; syncedAt: string; stops: BoardStop[]; warning: string;
}
export interface BoardLoads {
  detailsOmitted?: boolean;
  resolutionWarning?: string;
  automation?: BoardAutomation;
  week: BoardLoad[];
  current: BoardLoad | null; next: BoardLoad[]; earlier: BoardLoad[]; hidden: BoardLoad[]; unavailable: BoardLoad[];
  destinationSource: boolean; sourceDestination: string; stopKey: string;
  fromDate: string; revision: string; customOrder: boolean;
}
export interface BoardLoadAction {
  action: "add" | "advance" | "select" | "clear" | "order" | "reset_order" | "hide" | "restore" | "source" | "manual" | "stop_time";
  number?: string; planId?: string; order?: string[]; stopKey?: string; manualStopTime?: string;
}

export interface DriverBoardEvent {
  id: number; driverId: string; driverName: string; actorId: string; actorName: string;
  source: string; undoOf: number | null; before: Record<string, string>; after: Record<string, string>; createdAt: string;
}
export interface DriverBoardHistory { items: DriverBoardEvent[]; nextCursor: number }
export interface BoardPresence {
  clientId: string; userId: string; userName: string; driverId: string; column: string; expiresAt: string;
}
export interface GrossBoardCell { driverId: string; weekStart: string; date: string; slot: number; column: string }
export interface GrossBoardPresence extends GrossBoardCell { clientId: string; userId: string; userName: string; expiresAt: string }

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

export interface PrePassVehicleInput {
  vehicleNumber: string;
  licensePlate: { temporary: boolean; number: string; state: string };
  declaredWeight: number;
  irp: { accountNumber: string; state: string };
  ifta: { accountNumber: string; state: string };
  vrn: string;
  hazmatHauler: boolean;
}
export interface PrePassVehicle {
  accountNumber: string; costCenter: string; vin: string; vehicleNumber: string;
  tempPlate: boolean; licensePlateNumber: string; licensePlateState: string;
  declaredWeight: number; irpNumber: string; irpState: string; iftaNumber: string; iftaState: string;
  transponderNumber: string; vrn: string; hazmatHauler: boolean;
  vehicleImage: string; leasedVehicle: boolean; leasingCompany: string; leaseExpirationDate: string;
}
export interface PrePassTransponder {
  accountNumber: string; costCenter: string; vin: string; vehicleNumber: string; transponderNumber: string; vrn: string;
}
export interface PrePassFleetEntry {
  vehicle: PrePassVehicle; truckId: string; truckUnit: string; revision: string; warning: string; suggestions: string[];
  tollsLinked: boolean; tollFrom: string;
}
export interface PrePassFleetTruck {
  id: string; unit: string; vin: string; plate: string; plateState: string; active: boolean; updatedAt: string;
}
export interface PrePassFleetOperation {
  id: string; accountNumber: string; vin: string; action: string; state: "pending" | "queued" | "confirmed" | "failed" | "verified"; actor: string; createdAt: string; transponderNumber: string;
}
export interface PrePassFleetList {
  configured: boolean; environment: string; refreshedAt: string | null; accounts: string[];
  vehicles: PrePassFleetEntry[]; transponders: PrePassTransponder[]; trucks: PrePassFleetTruck[]; operations: PrePassFleetOperation[];
}
export interface PrePassFleetChange {
  plateSource?: "mserp" | "prepass";
  requestId: string; action: "add" | "edit" | "remove" | "assign" | "link";
  accountNumber: string; vin: string; revision?: string; truckId?: string; truckUpdatedAt?: string;
  vehicle?: PrePassVehicleInput; transponderNumber?: string;
  tollFrom?: string; tollRevision?: string;
}

export type TruckOperatingStatus = "ready" | "in_use" | "shop" | "out_of_service" | "terminated";
export interface TruckPage extends PaginatedResponse<Truck> {
 statusCounts: Record<TruckOperatingStatus, number>;
 gpsAlertCounts: Record<"overdue" | "missing", number>;
}
export interface TrailerPage extends PaginatedResponse<import("./inventoryTypes").InventoryRecord> {
 statusCounts: Record<TruckOperatingStatus,number>;
 storageLocations: string[];
}
export interface Truck {
 gpsReportedAt?: string | null;
 gpsSource?: "five_eld" | "samsara" | null;
 gpsAlert?: "" | "overdue" | "missing";
 statusReason?: string;
	statusVersion?: number;
 operatingStatus?: TruckOperatingStatus;
  ownerSince?: string | null;
  ownershipVersion?: string;
  ownerId: string;
  ownerName: string;
  id: string;
  unitNumber: string;
  vin: string | null;
  year: number | null;
  make: string | null;
  model: string | null;
  color: string | null;
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
  irpFileId?: string | null;
  irpFileName?: string | null;
  irpFileContentType?: string | null;
  irpFileSizeBytes?: number | null;
  createdAt: string;
  updatedAt: string;
}

export interface DataTruckEnrollment { state: string; message: string; version: number; providerId?: number }
export interface TruckInput {
 statusReason?: string;
	statusVersion?:number;
  addToDataTruck?: boolean;
  ownershipEffectiveDate?: string;
  expectedOwnershipVersion?: string;
  ownerDriverId?: string | null;
  /** Effective calendar date; legacy field name retained for API compatibility. */
  assignmentWeek?: string;
  ownerId: string | null;
  unitNumber: string;
  vin: string;
  year: number | null;
  make: string;
  model: string;
  color: string;
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
 userAccountId?: string | null;
  extension: number | null;
  mainUpdaterId: string | null;
  afterHoursUpdaterId: string | null;
  id: string;
  fullName: string;
  email: string | null;
  /** Ten ASCII digits without the +1 country code, or null when not recorded. */
  phone: string | null;
  active: boolean;
  notes: string | null;
  driverCount: number;
  createdAt: string;
  updatedAt: string;
}

export interface DispatcherInput {
 password?: string;
  extension?: number | null;
  updaters?: { mainUpdaterId: string | null; afterHoursUpdaterId: string | null };
  /** Effective calendar date; legacy field name retained for API compatibility. */
  assignmentWeek?: string;
  fullName: string;
  email: string;
  /** Optional phone: ten ASCII digits, or an empty string. */
  phone: string;
  driverIds: string[];
  active: boolean;
  notes: string;
}

export interface TollFlagReviewChange { tollId: string; id: string; revision: string; version: string }

export interface Toll {
  reviewRevision?: string;
  reviewVersion?: string;
  reviewed?: boolean;
  reviewedAt?: string;
  reviewedBy?: string;
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

export interface ExpenseVerification {
  id: string;
  role: "accountant";
  userId: string | null;
  userName: string;
  verifiedAt: string | null;
}

export interface ExpenseCostItem { name: string; amount: string; }

export interface MaintenanceType { programId?:string|null; code: string; name: string; active: boolean; version: number; }
export interface MaintenanceVendor { id: string; name: string; address: string; phone: string; contact: string; active: boolean; version: number; }
export interface MaintenanceWork { typeCode: string; name: string; amount: string | null; trackingCode: "" | "pm"; }
export interface MaintenanceDocument { id: string; fileName: string; contentType: string; sizeBytes: number; createdAt: string; }
export type ExpenseDocument = MaintenanceDocument;
export type WorkOrderStatus = "open" | "scheduled" | "in_progress" | "completed" | "closed" | "canceled" | "needs_review" | "voided";
export interface MaintenanceWorkflow { workStatus?:WorkOrderStatus; dispatchStatus?:""|Exclude<TruckOperatingStatus,"ready">; waitingOn?:string; scheduledFor?:string|null; expectedReady?:string|null; priority?:"normal"|"high"|"urgent"; noCharge?:boolean; cancelReason?:string; }
export interface MaintenanceRecord extends MaintenanceWorkflow {
  canEdit?: boolean;
 historicalPaid?:boolean;
 paymentConfirmation?:{expenseId:string;amount:string;method:string;reference:string;paidBy:string;confirmedBy:string;confirmedAt:string;invoiceCorrections?:{expenseId:string;amount:string;method:string;reference:string;paidBy:string;correctedBy:string;correctedAt:string}[]}|null;
  programIds?: string[];
  oilType: string;
  id: string; expenseId: string | null; truckId: string | null; unitNumber: string; driverId: string | null; driverName: string; vendorId: string | null;
  vendorName: string; vendorAddress: string; vendorPhone: string; vendorContact: string;
  serviceDate: string | null; odometer: number | null; location: "shop" | "roadside" | "internal" | "unknown";
  status: "completed" | "needs_review" | "voided"; workOrder: string; notes: string; services: MaintenanceWork[]; version: number;
  createdByName: string; updatedByName: string; createdAt: string; updatedAt: string; voidReason: string;
  expense: Expense | null; documents: MaintenanceDocument[];
}
export interface MaintenanceInput extends MaintenanceWorkflow {
 confirmPayment?:boolean;
  programIds?: string[];
  oilType: string;
  legacyPaymentTotal?: string;
  savedOwnerName?: string;
  driverId: string | null;
  driverName: string;
  id: string; version: number; truckId: string | null; vendorId: string | null; vendorName: string; vendorAddress: string; vendorPhone: string; vendorContact: string;
  serviceDate: string; odometer: number | null; location: MaintenanceRecord["location"]; status: "completed" | "needs_review";
  workOrder: string; notes: string; services: MaintenanceWork[]; expense: ExpenseInput | null;
}
export interface MaintenanceTruck { id: string; unitNumber: string; active: boolean; ownerId: string | null; ownerName: string; ownerDriverId: string | null; isCompanyOwned: boolean; driverId: string | null; driverName: string; }
export interface MaintenanceOptions { canManagePrograms?: boolean; programs?: ServiceProgram[]; programEnrollments?: {programId:string;truckId:string}[]; categoryId: string; types: MaintenanceType[]; vendors: MaintenanceVendor[]; schedules: MaintenanceSchedule[]; names: string[]; paymentMethods: string[]; users: string[]; currentUser: string; currentUserId: string; trucks: MaintenanceTruck[]; drivers: {id: string; name: string; active: boolean}[]; administrator: boolean; verificationRole: string; }
export interface MaintenanceSchedule { intervalMonths?: number | null; id: string; truckId: string | null; serviceCode: "pm"; intervalMiles: number | null; intervalDays: number | null; warningMiles: number; warningDays: number; enabled: boolean; version: number; }
export interface MaintenanceReading { id: string; truckId: string; recordId: string | null; date: string; mileage: number; source: "manual" | "service"; version: number; actorName: string; updatedAt: string; observedAt?: string | null; note?: string; }
export type OdometerSource = "auto" | "samsara" | "five_eld" | "manual";
export interface OdometerSourceReading { source: Exclude<OdometerSource,"auto"> | "service"; mileage: number | null; observedAt: string | null; date: string | null; valid: boolean; problem: string; }
export interface OdometerSources { truckId: string; unitNumber: string; source: OdometerSource; version: number; current: OdometerSourceReading | null; readings: OdometerSourceReading[]; manual: MaintenanceReading | null; }
export interface MaintenanceDue { currentReportedAt: string | null; lastRecordId: string | null; truckId: string; unitNumber: string; serviceCode: "pm"; status: "missing_information" | "disabled" | "overdue" | "due" | "upcoming" | "current"; reason: string; lastDate: string | null; lastMiles: number | null; currentDate: string | null; currentMiles: number | null; nextDate: string | null; nextMiles: number | null; schedule: MaintenanceSchedule | null; }
export interface MaintenanceReportRow { group: string; amount: string; workItems: number; visits: number; }
export interface MaintenanceReport { serviceTypes: MaintenanceReportRow[]; trucks: MaintenanceReportRow[]; vendors: MaintenanceReportRow[]; repeatedWork: MaintenanceReportRow[]; total: string; fees: string; unallocated: string; }
export type MaintenanceAIDetails = { oilType: string | null; hasInvoice: boolean; vendorName: string | null; vendorAddress: string | null; vendorPhone: string | null; vendorContact: string | null; serviceDate: string | null; odometer: number | null; location: MaintenanceRecord["location"]; workOrder: string | null; services: MaintenanceWork[]; };
export interface MaintenanceAIDraft extends AIExpenseDraft { maintenance: MaintenanceAIDetails | null; }
export interface MaintenanceChange { actorName: string; happenedAt: string; before: MaintenanceRecord | null; after: MaintenanceRecord; }

export interface ExpenseApplicant { id: string; fullName: string; status: "applied" | "hired" | "rejected" | "withdrawn"; }

export interface ExpenseMaintenance { recordId?: string; version?: number; odometer: number | null; vendorId: string | null; vendorName: string; vendorAddress: string; vendorPhone: string; vendorContact: string; oilType: string; }

export interface Expense {
 managedInSheets?:boolean; balanceVersion:number;
  maintenance?: ExpenseMaintenance;
  canEdit?: boolean;
  applicantId?: string | null;
  costBreakdown?: ExpenseCostItem[];
	baseAmount?: string | null;
	paymentFee?: string;
  verifications: ExpenseVerification[];
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
  category: ExpenseCategory;
  categoryId: string;
  createdByName: string | null;
  weekStart: string | null;
  expenseDate: string | null;
  unitNumber: string | null;
  driverName: string | null;
  amount: string | null;
  paymentType: string | null;
  expenseType: string | null;
  referenceNumber: string | null;
  description: string | null;
  workOrderDescription?: string | null;
  coveredBy: string | null;
  paidBy: string | null;
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
  driverOnly?: boolean;
}

export interface DriverEscrowSetting {
  defaultAmount: string;
  version: number;
}

export interface ExpenseInput {
  savedAmount?: string;
  legacyPaymentTotal?: string;
  maintenance?: ExpenseMaintenance;
  applicantId?: string | null;
  costBreakdown?: ExpenseCostItem[];
	updatedAt?: string;
 ownerId?: string | null;
  categoryId: string;
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
  accountingVerified: boolean;
}

export interface ExpensePage extends PaginatedResponse<Expense> {
  options: {
    applicants?: ExpenseApplicant[];
    workOrdersCategoryId?: string;
    settings: ExpenseSetting[];
    categories: { id: string; name: string }[];
    filters: { drivers: string[]; trucks: string[]; paymentTypes: string[]; paidBy: string[]; coveredBy: string[] };
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

export interface AIExpenseDraft extends Omit<ExpenseInput, "accountingVerified" | "maintenance"> {
  maintenance?: MaintenanceAIDetails | null;
  category: ExpenseCategory;
  confidence: number;
  evidence: string[];
}

export interface ExpenseExtraction {
  expenses: AIExpenseDraft[];
}

export interface AuthUser {
  administrator: boolean;
  accessRevision?: string;
  expenseVerificationRole: "accountant" | "";
  theme: string;
  id: string;
  username: string;
  email: string;
  roleId: string;
  permissions: string[];
  expenseCategoryAccess: ExpenseCategoryAccess[];
}

export interface ManagedUser {
  firstName?: string; lastName?: string; dateOfBirth?: string; phone?: string;
  id: string; username: string; email: string; roleId: string;
  active: boolean; version: number; password?: string;
  lastLoginAt?: string | null; online?: boolean;
}
export interface AccessRole {
 permanent?: boolean;
  id: string; name: string; permissions: string[]; expenseCategoryAccess: ExpenseCategoryAccess[]; system: boolean; version: number;
}
export interface AccessData {
  users: ManagedUser[]; roles: AccessRole[]; permissions: PermissionDefinition[];
  expenseCategories: { id: string; name: string; active: boolean }[];
}

export interface ExpenseCategoryAccess {
  categoryId: string;
  canView: boolean;
  canCreate: boolean;
  canEdit: boolean;
  canDelete: boolean;
}

export interface PermissionDefinition { key: string; label: string; group: string; action: string; requires: string[]; sensitive: boolean; }

export interface AuthSession {
  permissionCatalog?: PermissionDefinition[];
  user: AuthUser;
  csrfToken: string;
  expiresAt: string;
}

export type GrossBoardDayStatus = "" | "SHOP" | "HOME" | "RESET" | "IN TRANSIT" | "REJECTED" | "LOAD CANCELLED" | "NO LOAD" | "STUCK" | "LATE DEL" | "TRUCK ISSUE" | "LEFT" | "NEW DRIVER" | "DEADHEAD";

export interface GrossBoardTrip {
  id: number; number: string; driverName: string; truckUnit: string; originalRate: string; miles: string; loadedMiles: string; emptyMiles: string; startDate: string; endDate: string;
}

export interface GrossBoardEntry {
 sourceDate?: string;
 sourceSlot?: number;
 transferVersion?: number;
 transferOut?: boolean;
 creditedDispatcherId?: string;
 creditedDispatcherName?: string;
  loadTripId?: number | null;
  split?: boolean;
  tripReview?: boolean;
  tripReviewReason?: string;
  trips?: GrossBoardTrip[];
  note?: string;
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
  assignments?: { date: string; dispatcherId: string; dispatcherName: string; truckId: string; truckUnit: string }[];
  id: string;
  fullName: string;
  truckId: string;
  truckUnit: string;
  dispatcherId: string;
  dispatcherName: string;
  active: boolean;
}

export interface GrossBoard {
 managedInSheets?: boolean; cutoverWeek?: string;
  weekStart: string;
  drivers: GrossBoardDriver[];
  entries: GrossBoardEntry[];
  balances: { driverId: string; openingBalance: string; openingIncomplete: number; openingReference?:string }[];
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
  tripReviewReason?: string;
  trips?: GrossBoardTrip[];
  split?: boolean;
  id: number;
  loadNumber: string;
  originalRate: string;
  miles: string;
  driverName: string;
  pickupDate: string;
}
export type TaskStatus = "open" | "in_process" | "completed";

export interface CustomTask {
  serviceDriverId?: string;
  maintenanceTruckId?: string;
  maintenanceService?: string;
  status: TaskStatus;
  id: string;
  title: string;
  notes: string;
  completedAt: string | null;
  createdBy: string | null;
  assignedTo: string | null;
  assignedBy: string | null;
  assigneeName: string;
  assignerName: string;
  completedByName: string;
  outcome?: string;
  systemTaskKind: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface CustomTaskInput {
  title: string;
  notes: string;
  assignedTo?: string;
}

export interface TaskUser { id: string; name: string }
export interface SystemTaskAssignment { assigneeRoleIds?: string[]; kind: "maintenance_pm" | "five_eld_truck" | "driver_onboarding" | "driver_offboarding" | "relay_review" | "escrow_release" | "relay_setup" | "weighmytruck_setup" | "datatruck_setup"; assigneeId: string | null; assigneeIds?: string[]; version: number }

export interface DriverPayAdjustment {
 legacySheets?:boolean;
  id: string;
  kind: "reimbursement" | "addition" | "deduction";
  name: string;
  note: string;
  amount: string;
}
export interface ExpenseDeduction {
  source?: "escrow" | "reimburse";
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
export interface DriverPaySettings { layoverDailyAmount: string; version: number }
export interface PaySourceCharge { source: string; ownerId: string; name: string; base: string; carry: string; due: string; amount: string; revision: string; saved: boolean }
export interface DriverPayEdits {
  sourceCharges?: PaySourceCharge[];
  layoverExcludedDates?: string[];
  costs?: { revision: string; fuelCarry: string; tollCarry: string; fuelDue: string; tollDue: string };
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
export type LoadWeekTransferMode = "everyone" | "dispatch_only" | "restore";
export interface DriverPayLoad {
 placementVersion?: number;
 sourceSlot?: number;
 sourceDate?: string;
 dispatchDate?: string;
 dispatchSlot?: number;
 transferVersion?: number;
 creditedDispatcherId?: string;
 creditedDispatcherName?: string;
  loadTripId?: number;
  tripReview?: boolean;
  boardVersion?: number;
  systemOriginalRate?: string;
  systemMiles?: string;
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
  dayStatuses?: { date: string; status: Exclude<GrossBoardDayStatus, ""> }[];
  dispatchers?: { id: string; name: string }[];
  summary?: { loads: number; missingFees: number; review: number; fee: string; payable: string };
  layovers?: { date: string; amount: string }[];
  employmentType?: EmploymentType;
  teamNotice?: string;
  teamDrivers?: { id: string; name: string }[];
  truckInactive?: boolean;
  operatingDrivers?: { id: string; name: string }[];
  investorId?: string;
  profileDriverId?: string;
  truckId?: string;
  autoCharges?: { name: string; amount: string; source: string; expenseDate?: string }[];
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
export interface PaySourceReceipt { undoId: string }
export interface PaySourceAcceptance {
  field?: "originalRate" | "totalMiles";
  driverId: string;
  date: string;
  slot: number;
  version: number;
  loadRecordId: number;
  originalRate: string;
  miles: string;
}
export interface PayPageQuery {
  summary?: string;
  detailIds?: string;
  page: number;
  pageSize: number;
  search: string;
  dispatcherId: string;
  statementId: string;
}
export interface PayPageInfo {
  page: number;
  pageSize: number;
  total: number;
  totalPages: number;
  dispatchers: { id: string; name: string }[];
  rowIds?: string[];
  finalized: number;
  statements: number;
  loads: number;
  review: number;
  fee: string;
  payable: string;
}
export interface DriverPayWeek {
 notices?: string[];
 managedInSheets?:boolean;
  pagination?: PayPageInfo;
  setupRequired?: { truckId: string; truckUnit: string; ownerId: string; ownerName: string; driverId?: string; driverName?: string; dispatcherId?: string; dispatcherName?: string }[];
 issues?: string[];
 revision: string;
  weekStart: string;
  drivers: DriverPayDriver[];
}
export interface AssignmentHistoryEntry {
  id: string;
  kind: "truck" | "dispatcher" | "driver";
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

export interface ProfileNote {
  id: string;
  body: string;
  actorName: string;
  createdAt: string;
}
export interface InvestorStatementWeek {
  weekStart: string;
  trucks: DriverPayDriver[];
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
  baseAmount?: string; carryForward?: string; waiveRemainder?: boolean;
  scheduleId: string; weekStart: string; kind: "recurring" | "installment"; name: string;
  scheduledAmount: string; amount: string; overridden: boolean; confirmedAt: string | null; confirmedBy: string | null;
  version: number; scheduleVersion: number; typeVersion: number; reset?: boolean;
}
export interface ChargeSchedule {
  managedInSheets?: boolean;
  typeVersion: number;
  installmentCount: number;
  id: string; driverId: string; driverName: string; typeId: string | null; kind: "recurring" | "installment";
  name: string; direction: "charge" | "reimbursement"; startWeek: string; endWeek: string | null;
  eligibility: ChargeEligibility; total: string | null; version: number; phases: ChargePhase[]; occurrences: ChargeOccurrence[];
  confirmed: string; remaining: string; scheduled: string; completionWeek: string; status: string;
}
export interface ChargeData { types: ChargeType[]; schedules: ChargeSchedule[]; currentWeek: string; teamPeriods?: DriverTeamPeriod[] }

export interface DriverTeamPeriod {
  id: string;
  mainDriverId: string;
  mainDriverName: string;
  teamDriverId: string;
  teamDriverName: string;
  mainActive: boolean;
  teamActive: boolean;
  mainTerminationDate?: string;
  teamTerminationDate?: string;
  startWeek: string;
  endWeek: string | null;
}
export interface DriverTeam { version: number; periods: DriverTeamPeriod[] }
export interface DriverTeamInput { teamDriverId: string; weekStart: string; version: number }
export interface DriverTeamTruck { truckId: string; unitNumber: string; mainDriverId: string; mainDriverName: string; mainVersion: number; dispatcherId: string | null; dispatcherName: string | null; teamDriverId: string | null }
export interface ChargeCreate {
  driverIds: string[]; typeId: string; kind: "recurring" | "installment"; name: string; amount: string; total: string;
  installments: number; startWeek: string; endWeek: string | null; eligibility: "calendar" | "loads";
}
export interface ChargeBulk {
  targets: {id: string; version: number}[]; action: "amount" | "pause" | "resume" | "end"; weekStart: string; amount: string;
}
export interface ChargeEvent { id: number; action: string; actor: string; details: unknown; createdAt: string }

export interface PayrollSettlement { finalized: boolean; override?: boolean; version: number; finalizedAt: string; finalizedBy: string; reopenedAt: string | null; reason: string }
export interface PayrollSettlementInput { weekStart: string; revision: string; driverId?: string; reason: string; override: boolean }
export interface DriverPayHistoryRow { weekStart: string; driver: DriverPayDriver }
export interface SettlementEvent { action: string; version: number; actor: string; reason: string; createdAt: string; report: DriverPayDriver }

export interface TruckTerm { truckId: string; ownerId: string; weekStart: string; sharePercent: string; version: number }
export interface TruckChargePhase { truckId: string; typeId: string; weekStart: string; amount: string; included: boolean; version: number; typeVersion: number; moveScheduleId?: string; moveScheduleVersion?: number }
export interface TruckChargeBulkResult { truckId: string; typeId: string; weekStart: string; error?: string }
export interface TruckChargeOwner { truckId: string; ownerId: string; ownerName: string }
export interface TruckChargeData { eligibleTruckIds: string[]; owners?: TruckChargeOwner[]; terms?: TruckTerm[]; phases: TruckChargePhase[]; lockedCharges?: { truckId: string; typeId: string }[] }

export interface EscrowRelease { id: string; weekStart: string; amount: string; cancelled: boolean; version: number; editable: boolean }
export interface EscrowReleaseInput { id: string; weekStart: string; amount: string; version: number; escrowVersion: number; cancelled: boolean }
export interface Escrow {
  managedInSheets?: boolean;
  heldAmount: string;
  releasedAmount: string;
  releases: EscrowRelease[];
  id: string;
  driverId: string | null;
  driverName: string;
  active: boolean;
  startDate: string;
  amount: string;
  openingPaid: string;
  paidAmount: string;
  remainingAmount: string;
  status: "paid" | "partial" | "unpaid" | "released" | "partially_released" | "not_released";
  version: number;
  payments: { weekStart: string; amount: string }[];
}

export interface EscrowPage extends PaginatedResponse<Escrow> {
 summary: { held: string; released: string; target: string; paid: string; remaining: string; drivers: number; paidDrivers: number; partialDrivers: number; unpaidDrivers: number };
}

// Versioned compact read transport; mutations still return complete queue values.
export type CompactBoardLoads = Omit<BoardLoads, "current" | "week" | "next" | "earlier" | "hidden" | "unavailable"> & {
  current: number | null; week: number[]; next: number[]; earlier: number[]; hidden: number[]; unavailable: number[];
};
export type CompactDriverBoard = Omit<DriverBoard, "loads"> & { plans: BoardLoad[]; loads: Record<string, CompactBoardLoads> };

export interface EscrowTaskDetail {
 decision: EscrowTaskDecision["decision"]; driverId: string; terminationDate: string; dueDate: string; escrows: Escrow[] }
export interface EscrowTaskDecision { decision: "released" | "partially_released" | "kept"; reason: string; versions: Record<string, number> }
export type WeighMyTruckEntry = {
  id: string; driverId: string; name: string; email: string; phone: string;
  driverCode: string; driverStatus: string; enrolled: boolean;
  investorEligible: boolean; payer: WMTPayer;
  state: "confirmed" | "pending" | "review"; version: number;
  warning: string; addBlockReason: string; canVerify: boolean; updatedAt: string | null;
};
export type WMTPayer = "company" | "driver" | "investor" | "review";
export type WMTBilling = { driverId: string; weekStart: string; payer: WMTPayer; version: number };
export type WeighMyTruckList = { items: WeighMyTruckEntry[]; configured: boolean; initialized: boolean; billing: WMTBilling[]; requests: { driverId: string; name: string; state: string; error: string }[] };
export type WMTTransaction = { id: string; date: string; time: string; ticket: string; originalTicket: string; amount: string; truckUnit: string; driverCode: string; driverId: string; driverName: string; truckId: string; location: string; city: string; state: string; payer: WMTPayer; investorName: string; warning: string; version: number };
export type WMTTransactions = PaginatedResponse<WMTTransaction> & { totalAmount: string; lastSync: string | null };
export interface RelayPolicyLimit { type: string; mode: string; amount: string; unit: string; frequency: string; usage?: string; remaining?: string }
export interface RelayDriverPolicy { id: string; name: string; eld_enabled: boolean; authorization_prompts: { name: string; validation_mode: string }[]; limits: RelayPolicyLimit[] }
export interface RelayPolicyAssignment { id: string; policy_id: string; policy_name: string; enabled: boolean; limits: RelayPolicyLimit[] }
export interface RelayRosterEntry {
  driver: { id: string; first_name: string; last_name: string; phone: string; email: string; integration_id: string; updated_at: string; data_fields?: { field_name: string; field_value: string }[] };
  assignment: RelayPolicyAssignment | null; revision: string; identityId: string; driverId: string; driverName: string;
  status: string; warning: string; pending: boolean; suggestions: RelayIdentityTask["suggestions"];
}
export interface RelayDriverChoice { id: string; name: string; firstName?: string; lastName?: string; status: string; phone: string; email: string; truck: string }
export interface RelayDriverOperation { id: string; action: string; relayDriverId: string; driverId: string; createdAt: string }
export interface RelayDriverList {
  items: RelayRosterEntry[]; policies: RelayDriverPolicy[]; configured: boolean; refreshedAt: string | null; environment: string;
  canLink: boolean; drivers: RelayDriverChoice[]; operations: RelayDriverOperation[];
}
export type RelayDriverAction = "add" | "edit" | "remove" | "enable" | "disable" | "assign" | "unassign";
export interface RelayDriverChange {
  requestId: string; action: RelayDriverAction; relayDriverId?: string; driverId?: string; revision?: string; policyId?: string;
  firstName?: string; lastName?: string; phone?: string; email?: string; driverPin?: string; truckNumber?: string;
}
export interface SidebarCounts { fuel: number; weighMyTruck: number; tolls: number; tollsFrom: string }
export interface PrePassTruckGroup { environment: string; identityKey: string; units: string[]; devices: string[]; count: number; amount: string; firstExit: string; lastExit: string }
export interface PrePassTruckChoice { id: string; unit: string; vin: string; plate: string }
export interface PrePassTruckMapping { id: string; environment: string; identityKey: string; truckId: string; unit: string; from: string; to: string; version: number; count: number }
export interface PrePassTruckLinks { dateFrom: string; transactions: Toll[]; groups: PrePassTruckGroup[]; trucks: PrePassTruckChoice[]; mappings: PrePassTruckMapping[] }
export interface PrePassTruckLinkInput { id: string; environment: string; identityKey: string; truckId: string; from: string; to: string; revision: string; transactionIds?: string[] }
export interface PrePassTruckPreview { count: number; amount: string; units: string[]; revision: string; closed: boolean }
export interface RelayBulkResponse { results: { relayDriverId: string; error?: string }[]; warning?: string }

export type DriverService = "relay" | "weighmytruck" | "datatruck";
export interface DriverServiceIntent { service: DriverService; action: "add" | "defer"; followUpOn?: string; version?: number }
export interface DriverServiceState {
  dataTruckSyncState?: string; dataTruckSyncMessage?: string;
  dataTruckId?: number;
  service: DriverService; state: string; requestState: string; version: number; message: string; followUpOn: string; configured: boolean;
  relay?: RelayRosterEntry; membership?: WeighMyTruckEntry; candidates: RelayRosterEntry[]; operations: RelayDriverOperation[];
}
export interface DriverServiceSetup {
  driverId: string; name: string; driverStatus: string; tomorrow: string; items: DriverServiceState[];
  policies: RelayDriverPolicy[] | null; canLinkRelay: boolean; environment: string; checkedAt: string;
}

export interface FiveELDTruckReview { id: string; vin: string; unitNumber: string; version: number; completed: boolean }

export interface PreventativeTruck { recordVersion: number; baselineVersion: number; currentReportedAt: string | null; currentSource: string; nextMiles: number | null; remainingMiles: number | null; nextDate: string | null; status: MaintenanceDue["status"]; reason: string; truckId: string; unitNumber: string; active: boolean; recordId: string | null; lastDate: string | null; lastMiles: number | null; currentMiles: number | null; currentDate: string | null; oilType: string; }
export type FleetScopeOwnership = { kind: "company" | "driver" | "investor"; mserpOwnerId: string; displayName: string; mserpOwnerDriverId: string | null; effectiveDate: string | null };
export type FleetScopeTruckProjection = { mserpTruckId: string; unitNumber: string; vin: string | null; year: number | null; make: string | null; model: string | null; licensePlate: string | null; licenseState: string | null; operationalActive: boolean; operationalStatus: string; ownership: FleetScopeOwnership };
export type FleetScopeProvisioning = { color: string; truckClass: string; gvwrLbs: number };
export type FleetScopeSyncView = {
 pendingCount: number; canReview: boolean; canManage: boolean; configured: boolean; configuredMode: string; companyId: string; environment: string; contractHash: string; mode: string; version: number; epoch: string; lastError: string; lastCheckedAt: string | null;
 local: { sourceRevision: number; week: string; trucks: { truck: FleetScopeTruckProjection; primaryId: string; secondaryId: string; issues: string[] }[]; drivers: { id: string; name: string; status: string; fleetScopeId: string; hireEventId: string }[] };
 links: { kind: string; localId: string; remoteId: string; remoteRevision: string; assignmentRevision: string; sourceRevision: number; acknowledged: FleetScopeTruckProjection | null; acknowledgedHash: string; provisioning: FleetScopeProvisioning | null; excludedReason: string; lastError: string; lastSyncedAt: string | null; version: number }[];
 operations: { id: string; entityKey: string; kind: string; state: string; attempts: number; lastError: string; createdAt: string; appliedAt: string | null }[];
 snapshotId: string; snapshotSourceRevision: number; snapshotCreatedAt: string | null;
 remote: { id: string; companyId: string; environment: string; epoch: string; capturedAt: string; nextCursor: string; drivers: { id: string; name: string; status: string; revision: string; mserpId: string }[]; trucks: { id: string; unitNumber: string; vin: string; revision: string; assignmentRevision: string; mserpId: string; primaryId: string; secondaryId: string; safetyHold: boolean; projection: FleetScopeTruckProjection | null; ownership: FleetScopeOwnership | null }[] } | null;
};
export type FleetScopeReview = { snapshotId: string; sourceRevision: number; action: "link" | "create" | "exclude" | "refresh"; kind: "truck" | "driver"; localId: string; remoteId: string; reason: string; provisioning?: FleetScopeProvisioning };

// Lookup contracts contain operational choices only, never complete profiles.
export type DriverOption = Pick<Driver, "id" | "fullName" | "active" | "status" | "employmentType" | "driverType" | "isOwnerOperator" | "dispatcherId" | "dispatcherName" | "truckId" | "truckUnit" | "teamRole" | "teamTruckId" | "teamTruckUnit" | "teamPartnerId" | "teamPartnerName" | "teamStartWeek" | "teamVersion"> & { phone?: Driver["phone"]; email?: Driver["email"] };
export type TruckOption = Pick<Truck, "id" | "unitNumber" | "active" | "status" | "operatingStatus" | "driverId" | "driverName" | "ownerId" | "ownerName" | "ownerSince" | "ownershipVersion" | "isCompanyOwned" | "year" | "make" | "model">;
export type DispatcherOption = Pick<Dispatcher, "id" | "fullName" | "active" | "extension" | "mainUpdaterId" | "afterHoursUpdaterId" | "driverCount">;
export type InvestorOption = Pick<Investor, "id" | "fullName" | "active" | "driverId" | "isCompany" | "trucks">;
export type UpdaterOption = Pick<Updater, "id" | "fullName" | "shift" | "extension">;

export interface ServiceProgram {
 serviceTypeCodes?:string[]; serviceTypeVersions?:Record<string,number>;
 id:string; name:string; instructions:string; legacyPM:boolean;
 intervalMiles:number|null; intervalDays:number|null; intervalMonths:number|null;
 warningMiles:number; warningDays:number; active:boolean; useDefaultAssignees:boolean;
 assigneeIds:string[]; assigneeRoleIds:string[]; version:number;
 truckCount:number; upcomingCount:number; overdueCount:number;
}
export interface ProgramTruck extends MaintenanceDue {
 programId:string; programName:string; active:boolean; enrolled:boolean;
 truckOperatingStatus:TruckOperatingStatus;
 memberVersion:number; recordVersion:number; baselineVersion:number; currentSource:string;
 milesSinceService:number|null; overrideInterval:boolean;
 intervalMiles:number|null; intervalDays:number|null; intervalMonths:number|null;
}
export type ProgramTruckInput=Pick<ProgramTruck,"truckId"|"overrideInterval"|"intervalMiles"|"intervalDays"|"intervalMonths"> & {version:number;active:boolean};
export type PTICrew = {id:string;name:string;availability:string;employment:string};
export type PTIRow = {
 mark?: "/"|"1"|"0";
 weekStart:string;truckId:string;truckUnit:string;crew:PTICrew[];condition:string;
 snapshotEligibility:string;snapshotReason:string;eligibility:"required"|"exempt"|"unknown";eligibilityReason:string;
 received:boolean;receivedAt:string|null;receivedBy:string;review:"pending"|"checked"|"issues";reviewedAt:string|null;reviewedBy:string;
 videoUrl:string;notes:string;updatedAt:string|null;updatedBy:string;version:number;
};
export type PTIWeek = {weekStart:string;exists:boolean;createdAt:string|null;createdBy:string;lateSnapshot:boolean;rows:PTIRow[]};
export type PTIChange = Pick<PTIRow,"version"|"received"|"review"|"notes"> & {mark?:PTIRow["mark"]|"";requestId:string;undoRequestId?:string};
export type PTIGroup = "company"|"owner_operator";
export type PTIMatrix = {layoutVersion:number;weeks:string[];trucks:{id:string;unit:string;ownership:"company"|"investor"|"owner_operator";group:PTIGroup;position:number|null}[];rows:PTIRow[]};
export type PTIEvent = {id:string;before:PTIRow;after:PTIRow;actorName:string;createdAt:string};

export interface DriverDirectoryFilters {
 employment?: "" | "all" | "active" | "terminated" | "pending";
 availability?: "" | "active" | "active_or_new" | "home" | "vacation" | "rest" | "reset" | "family_issue" | "health_issue" | "new_driver";
 driverType?: "" | "company" | "owner_operator" | "investor_driver" | "unassigned";
 assignment?: "" | "assigned" | "unassigned";
}
export type { CompanyDashboard, DashboardPeriod, DashboardMetric, DashboardSection, DashboardItem, DashboardLoad, DashboardPerformance, DashboardQuery } from "./dashboardTypes";
export type FeedbackStatus = "open" | "in_progress" | "completed" | "discarded";
export type FeedbackAttachment = { id: string; fileName: string; contentType: string; size: number };
export type FeedbackPost = {
  id: string; authorId: string | null; authorName: string; title: string; body: string;
  tags: string[]; status: FeedbackStatus; version: number; votes: number; voted: boolean;
  commentCount: number; createdAt: string; updatedAt: string; attachments: FeedbackAttachment[];
};
export type FeedbackDetail = FeedbackPost & {
  comments: { id: string; authorName: string; body: string; createdAt: string }[];
  history: { actorName: string; oldStatus: FeedbackStatus; newStatus: FeedbackStatus; createdAt: string }[];
};
export type FeedbackInput = { id: string; title: string; body: string; tags: string[]; status?: FeedbackStatus; version?: number };
export type FeedbackMessage = { id: string; postId: string; title: string; completedBy: string; createdAt: string; readAt: string | null };

export type ExpenseDriverChoice = Pick<DriverOption, "id" | "fullName" | "truckId">;
export type ExpenseTruckChoice = Pick<TruckOption, "id" | "unitNumber" | "driverId" | "isCompanyOwned"> & { ownerId: string | null };
export type ExpenseOwnerChoice = Pick<InvestorOption, "id" | "fullName" | "driverId" | "isCompany">;
export interface ExpenseFormChoices { options: ExpensePage["options"]; drivers: ExpenseDriverChoice[]; trucks: ExpenseTruckChoice[]; owners: ExpenseOwnerChoice[]; }

export interface ExternalExpenseSettlement {id:string;amount:string;settledOn:string;reference:string;reversesId?:string;version:number;actor?:string;createdAt?:string}

export interface ServiceProgramWorkspace { programs: ServiceProgram[]; trucks: PaginatedResponse<ProgramTruck> }
