export type DashboardPreset = "week" | "thisWeek" | "lastWeek" | "thisMonth" | "lastMonth" | "custom";
export interface DashboardPeriod { preset: DashboardPreset; dateFrom: string; dateTo: string; compareFrom: string; compareTo: string; bucket: "day" | "week" }
export interface DashboardMetric {
  gross: string; driverGross: string; miles: string; rpm: string | null; loads: number;
  missingGross: number; missingDriverGross: number; missingMiles: number; pairedLoads: number;
  provisional: number; provisionalGross: string; conflicts: number; conflictGross: string;
  repeats: number; repeatedGross: string; rawGross: string;
}
export interface DashboardBreakdown extends DashboardMetric { id: string; name: string }
export interface DashboardPerformance { current: DashboardMetric; previous: DashboardMetric; trend: DashboardBreakdown[]; dispatchers: DashboardBreakdown[] }
export interface DashboardItem { id: string; label: string; count: number; amount?: string; previousAmount?: string; incomplete?: number; href: string; note?: string }
export interface DashboardWaterfall { gross: string; fuel: string; tolls: string; driverPay: string; investorPay: string; profit: string | null; review: DashboardItem[] }
export interface DashboardSection { kind: "performance" | "fleet" | "cost" | "attention" | "waterfall"; title: string; updatedAt: string; error?: string; items: DashboardItem[]; performance?: DashboardPerformance; waterfall?: DashboardWaterfall }
export interface CompanyDashboard { period: DashboardPeriod; sections: Record<string, DashboardSection> }
export interface DashboardLoad { driverId: string; driverName: string; date: string; slot: number; loadNumber: string; loadRecordId: number | null; gross: string | null; driverGross: string | null; miles: string | null; dispatcherId: string; dispatcherName: string; inclusion: "included" | "provisional" | "conflict" | "repeat" }
export interface DashboardQuery { preset?: DashboardPreset; dateFrom?: string; dateTo?: string; view?: "weekly" | "current" }
