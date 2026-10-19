export type InventoryCollection = "assets" | "lines" | "locations" | "supplies" | "equipment" | "trailers" | "transponder-notes";
export interface InventoryFields {
  name?: string; deviceType?: string; imei?: string; serial?: string; assetLabel?: string;
  make?: string; model?: string; condition?: string; status?: string; ownership?: string;
  truckId?: string; locationId?: string; custodian?: string; gatewayId?: string; assetId?: string;
  wirelessNumber?: string; carrier?: string; iccid?: string; plan?: string; activationDate?: string;
  contractStart?: string; contractEnd?: string; sourceUpdated?: string; unit?: string; reusable?: boolean;
  batchYear?: string; supplyId?: string; equipmentState?: string; sent?: boolean; attached?: boolean;
  sentDate?: string; attachedDate?: string; verifiedAt?: string; verifiedBy?: string;
  unitNumber?: string; vin?: string; year?: string; plate?: string; plateState?: string;
  storageLocation?: string; usage?: string; registrationUrl?: string;
  providerEnvironment?: string; providerAccount?: string; notes?: string; source?: string; active: boolean;
}
export interface InventoryRecord { id: string; version: number; data: InventoryFields; updatedAt: string; updatedBy: string }
export interface InventoryEvent { id: string; collection: string; recordId: string; action: string; before: InventoryRecord | null; after: InventoryRecord; actor: string; createdAt: string }
export interface InventoryMovement { id: string; supplyId: string; kind: string; from: string; to: string; quantity: number; reason: string; physicalCount: boolean; reversesId?: string; actor: string; createdAt: string }
export interface InventoryBalance { supplyId: string; holder: string; quantity: number; verified: boolean }
export type InventoryData = Record<Exclude<InventoryCollection, "trailers">, InventoryRecord[]> & { balances: InventoryBalance[]; movements: InventoryMovement[] };
export interface InventorySnapshot { spreadsheetId: string; capturedAt: string; sheets: { name: string; rows: string[][] }[] }
export interface InventoryImportRow { key: string; sources: string[]; collection: InventoryCollection | ""; record: InventoryRecord; issues: string[]; decision: "include" | "exclude" | "archive"; reason: string; reviewed: boolean }
export interface InventoryImport { id: string; version: number; status: string; sourceHash: string; spreadsheetId: string; capturedAt: string; rows: InventoryImportRow[]; createdAt: string }
