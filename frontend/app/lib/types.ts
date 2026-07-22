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

export interface FuelTransaction {
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

export interface SyncFuelResult {
  fetched: number;
  saved: number;
  excluded: number;
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
  id: string;
  truckId: string;
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

export interface UnmatchedTollUnit {
  unitNumber: string;
  rowCount: number;
}

export interface TollImportResult {
  reportId: string;
  fileName: string;
  rowCount: number;
  importedCount: number;
  duplicateCount: number;
  unmatchedCount: number;
  unmatchedUnits: UnmatchedTollUnit[];
  totalAmount: number;
  importedAmount: number;
  postingDateStart: string;
  postingDateEnd: string;
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
