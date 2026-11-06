export const boardColumns = {
  currentLoad: ["Current load", 90], driver: ["Driver", 180], truck: ["Truck", 64], trailerNumber: ["Trailer", 100],
  originalGross: ["Total gross", 110], driverGross: ["Driver gross", 110], phone: ["Phone", 124], status: ["Status", 70],
  destination: ["Destination", 160], eta: ["ETA", 180], loadPlan: ["Next load", 180], notes: ["Notes", 130],
  homeTime: ["Home time", 120], driverAddress: ["Driver address", 140],
} as const;
export type BoardColumn = keyof typeof boardColumns;
export const defaultColumnOrder = Object.keys(boardColumns) as BoardColumn[];

const historyFields: Partial<Record<BoardColumn, string>> = {
  currentLoad: "currentLoad", trailerNumber: "trailerNumber", status: "status",
  destination: "destination", eta: "eta", loadPlan: "loadPlan", notes: "notes", homeTime: "leavePlan",
};

export function boardHistoryField(cell?: HTMLTableCellElement | null) {
  return historyFields[cell?.dataset.boardColumn as BoardColumn];
}

export function selectedBoardCell(target: EventTarget | null) {
  if (!(target instanceof Element)) return null;
  const popup = target.closest<HTMLElement>('[data-board-driver]');
  if (popup) return { driverId: popup.dataset.boardDriver!, column: "trailerNumber" };
  const cell = target.closest<HTMLTableCellElement>("td,th");
  const row = cell?.closest<HTMLTableRowElement>("tr[data-driver-id]");
  if (!cell || !row || !row.closest('[data-status-board-grid]')) return null;
  const column = cell.dataset.boardColumn;
  if (!column || column === "driverAddress") return null; // Profile-only columns have no editable board cursor.
  return { driverId: row.dataset.driverId!, column };
}
