import type { ClipboardEvent, KeyboardEvent, MouseEvent } from "react";

type Cell = HTMLTableCellElement;
type Position = { cell: Cell; row: number; column: number };
const positions = new WeakMap<HTMLTableElement, Position>();
const inputs = new WeakMap<HTMLInputElement, (value: string) => void>();
const caretEditing = new WeakSet<HTMLInputElement>();
const replacementEditing = new WeakSet<HTMLInputElement>();
export function isPayReplacementInput(target: HTMLElement) {
  return target instanceof HTMLInputElement && replacementEditing.has(target);
}
export function registerPayInput(input: HTMLInputElement, update: (value: string) => void) {
  inputs.set(input, update);
  return () => { inputs.delete(input); };
}

// Expand merged cells to retain columns across no-load rows and day labels.
function matrix(table: HTMLTableElement) {
  const cells: Cell[][] = [];
  Array.from(table.rows).forEach((row, r) => {
    cells[r] ??= [];
    let c = 0;
    Array.from(row.cells).forEach(cell => {
      while (cells[r][c]) c += 1;
      for (let y = r; y < r + cell.rowSpan; y += 1) {
        cells[y] ??= [];
        for (let x = c; x < c + cell.colSpan; x += 1) cells[y][x] = cell;
      }
      c += cell.colSpan;
    });
  });
  return cells;
}

function locate(cells: Cell[][], cell: Cell): Position | undefined {
  for (let row = 0; row < cells.length; row += 1) {
    const column = cells[row].indexOf(cell);
    if (column >= 0) return { cell, row, column };
  }
}

function focusCell(table: HTMLTableElement, row: number, column: number) {
  const cells = matrix(table);
  const cell = cells[row]?.[column];
  if (!cell) return;
  const nested = cell.querySelector("table");
  if (nested) {
    const origin = locate(cells, cell)!;
    focusCell(nested, Math.min(row - origin.row, nested.rows.length - 1), column - origin.column);
    return;
  }
  positions.set(table, { cell, row, column });
  cell.tabIndex = -1;
  cell.focus({ preventScroll: true });
  cell.scrollIntoView({ block: "nearest", inline: "nearest" });
}

export function selectPayCell(event: MouseEvent<HTMLElement>) {
  const target = event.target as HTMLElement;
  if (target.closest('button,a,textarea,[role="menu"],[role="dialog"]')) return;
  if (target instanceof HTMLInputElement && document.activeElement === target && !target.readOnly) return;
  const cell = target.closest<Cell>("td,th");
  const table = cell?.closest("table");
  if (!cell || !table || cell.querySelector("table")) return;
  const position = locate(matrix(table), cell);
  if (position) { event.preventDefault(); focusCell(table, position.row, position.column); }
}

function editCell(cell: Cell, value?: string) {
  const input = cell.querySelector<HTMLInputElement>('input:not(:disabled):not([readonly])');
  if (!input) return;
  if (value === undefined) { caretEditing.add(input); replacementEditing.delete(input); }
  else { caretEditing.delete(input); replacementEditing.add(input); }
  input.focus({ preventScroll: true });
  if (value !== undefined) inputs.get(input)?.(value);
  else input.setSelectionRange(input.value.length, input.value.length);
}

export function editPayCell(event: MouseEvent<HTMLElement>) {
  const cell = (event.target as HTMLElement).closest<Cell>("td,th");
  if (cell) { event.preventDefault(); editCell(cell); }
}

export function copyPayCell(event: ClipboardEvent<HTMLElement>) {
  const target = event.target as HTMLElement;
  const cell = target.closest<Cell>("td,th");
  if (!cell || cell.querySelector("table")) return;
  if (target instanceof HTMLInputElement && target.selectionStart !== target.selectionEnd) return;
  // Copy the field itself, excluding reset buttons and source/status badges.
  const input = cell.querySelector<HTMLInputElement>("input");
  const clone = cell.cloneNode(true) as Cell;
  clone.querySelectorAll("button,[role=menu],.sr-only").forEach(node => node.remove());
  event.clipboardData.setData("text/plain", input?.value ?? clone.textContent?.trim() ?? "");
  event.preventDefault();
  event.stopPropagation();
}

export function navigatePayGrid(event: KeyboardEvent<HTMLElement>) {
  if (event.altKey || event.ctrlKey || event.metaKey || event.nativeEvent.isComposing) return;
  const target = event.target as HTMLElement;
  const cell = target.closest<Cell>("td,th");
  const table = cell?.closest("table");
  if (!cell || !table || target.closest('[role="dialog"],[role="menu"]')) return;
  if (target instanceof HTMLInputElement) {
    // F2/double-click edits the existing text; replacement typing keeps the
    // spreadsheet arrow navigation available after the new value is entered.
    if (event.key === "Enter" || event.key === "Escape") { event.preventDefault(); cell.tabIndex = -1; cell.focus(); return; }
    if (caretEditing.has(target) || !event.key.startsWith("Arrow")) return;
  }
  if (event.key === "F2" || event.key === "Enter") { event.preventDefault(); editCell(cell); return; }
  if (event.key.length === 1 || event.key === "Backspace" || event.key === "Delete") {
    event.preventDefault(); editCell(cell, event.key.length === 1 ? event.key : ""); return;
  }
  if (event.shiftKey || !event.key.startsWith("Arrow")) return;
  const cells = matrix(table);
  const previous = positions.get(table);
  const start = previous?.cell === cell ? previous : locate(cells, cell);
  if (!start) return;
  const vertical = event.key === "ArrowUp" || event.key === "ArrowDown";
  const delta = event.key === "ArrowUp" || event.key === "ArrowLeft" ? -1 : 1;
  let { row, column } = start;
  do {
    if (vertical) row += delta; else column += delta;
  } while (cells[row]?.[column] === cell);
  event.preventDefault();
  event.stopPropagation();
  if (cells[row]?.[column]) { focusCell(table, row, column); return; }

  // Leave the independently scrolling charges pane through its neighboring
  // load cells, headings or totals rather than trapping focus inside it.
  const parentCell = table.parentElement?.closest<Cell>("td,th");
  const parentTable = parentCell?.closest("table");
  if (!parentCell || !parentTable) return;
  const origin = locate(matrix(parentTable), parentCell)!;
  const outerRow = vertical ? (delta < 0 ? origin.row - 1 : origin.row + parentCell.rowSpan) : origin.row + Math.min(start.row, parentCell.rowSpan - 1);
  const outerColumn = vertical ? origin.column + start.column : (delta < 0 ? origin.column - 1 : origin.column + parentCell.colSpan);
  focusCell(parentTable, outerRow, outerColumn);
}
