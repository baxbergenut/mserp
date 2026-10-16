export const boardCellKeys = ["currentLoad", "driver", "truck", "trailerNumber", "originalGross", "driverGross", "phone", "status", "destination", "notes", "eta", "loadPlan", "homeTime"];

export function selectedBoardCell(target: EventTarget | null) {
  if (!(target instanceof Element)) return null;
  const popup = target.closest<HTMLElement>('[data-board-driver]');
  if (popup) return { driverId: popup.dataset.boardDriver!, column: "trailerNumber" };
  const cell = target.closest<HTMLTableCellElement>("td,th");
  const row = cell?.closest<HTMLTableRowElement>("tr[data-driver-id]");
  if (!cell || !row || !row.closest('[data-status-board-grid]')) return null;
  return { driverId: row.dataset.driverId!, column: boardCellKeys[cell.cellIndex] };
}
