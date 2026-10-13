import type { ClipboardEvent, KeyboardEvent, MouseEvent } from "react";

type Cell = HTMLElement;
const cellSelector = '[data-grid-cell],td,th';
const textInputSelector = 'input:not(:disabled):not([readonly]):not([type=checkbox]):not([type=radio]):not([type=file]):not([type=hidden]):not([type=button]):not([type=submit])';

function excluded(target: HTMLElement) {
  if (target.closest('[role="menu"],[role="listbox"],[contenteditable="true"],[data-grid-ignore]')) return true;
  const dialog = target.closest('[role="dialog"]');
  return !!dialog && !target.closest('[data-spreadsheet-grid]');
}
type Position = { cell: Cell; row: number; column: number };
const positions = new WeakMap<HTMLTableElement, Position>();
const inputs = new WeakMap<HTMLInputElement, (value: string) => void>();
const caretEditing = new WeakSet<HTMLInputElement>();
const replacementEditing = new WeakSet<HTMLInputElement>();
export function isGridReplacementInput(target: HTMLElement) {
  return target instanceof HTMLInputElement && replacementEditing.has(target);
}
export function registerGridInput(input: HTMLInputElement, update: (value: string) => void) {
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
  // Gross Board stacks four fields in each day. Treat those fields as real
  // rows, preserving the same field when moving between days or drivers.
  return cells.flatMap(row => {
    const fields = row.map(cell => Array.from(cell.querySelectorAll<HTMLElement>('[data-grid-cell]')).filter(field => field.closest('table') === table));
    const height = Math.max(1, ...fields.map(items => items.length));
    return Array.from({ length: height }, (_, index) => row.map((cell, column) => fields[column][Math.min(index, fields[column].length - 1)] ?? cell));
  });
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

export function selectGridCell(event: MouseEvent<HTMLElement>) {
  if (event.defaultPrevented || event.button !== 0) return;
  const target = event.target as HTMLElement;
  if (excluded(target) || target.closest('button,a,textarea,select,input[type=checkbox],input[type=radio],input[type=file]')) return;
  if (target instanceof HTMLInputElement && document.activeElement === target && !target.readOnly) return;
  const cell = target.closest<Cell>(cellSelector);
  const table = cell?.closest("table");
  if (!cell || !table || cell.querySelector("table")) return;
  const position = locate(matrix(table), cell);
  if (position) { event.preventDefault(); focusCell(table, position.row, position.column); }
}

function editCell(cell: Cell, value?: string) {
  const input = cell.querySelector<HTMLInputElement>(textInputSelector);
  if (!input) {
    if (value === undefined) {
      const control = cell.querySelector<HTMLElement>('select:not(:disabled),[data-grid-action]:not(:disabled)');
      control?.focus();
      if (control instanceof HTMLButtonElement) control.click();
    }
    return;
  }
  if (value === undefined) { caretEditing.add(input); replacementEditing.delete(input); }
  else { caretEditing.delete(input); replacementEditing.add(input); }
  input.focus({ preventScroll: true });
  if (value !== undefined) {
    const update = inputs.get(input);
    if (update) update(value);
    else {
      // Go through React's normal onChange path for existing controlled fields.
      // Bypass only the DOM value tracker so React sees the replacement.
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value);
      input.dispatchEvent(new Event('input', { bubbles: true }));
    }
  } else if (input.selectionStart !== null) input.setSelectionRange(input.value.length, input.value.length);
}

export function editGridCell(event: MouseEvent<HTMLElement>) {
  const target = event.target as HTMLElement;
  if (event.defaultPrevented || excluded(target) || target.closest('button,a,select,textarea')) return;
  const cell = target.closest<Cell>(cellSelector);
  if (cell) { event.preventDefault(); event.stopPropagation(); editCell(cell); }
}

export function copyGridCell(event: ClipboardEvent<HTMLElement>) {
  const target = event.target as HTMLElement;
  if (event.defaultPrevented || excluded(target) || target.closest('textarea,select')) return;
  const cell = target.closest<Cell>(cellSelector);
  if (!cell || cell.querySelector("table")) return;
  if (target instanceof HTMLInputElement && target.selectionStart !== target.selectionEnd) return;
  // Copy the field itself, excluding reset buttons and source/status badges.
  const input = cell.querySelector<HTMLInputElement>("input");
  const select = cell.querySelector<HTMLSelectElement>("select");
  const clone = cell.cloneNode(true) as Cell;
  clone.querySelectorAll("button:not([data-grid-action]),[role=menu],.sr-only").forEach(node => node.remove());
  event.clipboardData.setData("text/plain", input?.value ?? select?.value ?? clone.textContent?.trim() ?? "");
  event.preventDefault();
  event.stopPropagation();
}

export function navigateGrid(event: KeyboardEvent<HTMLElement>) {
  if (event.defaultPrevented || event.altKey || event.ctrlKey || event.metaKey || event.nativeEvent.isComposing) return;
  const target = event.target as HTMLElement;
  if (excluded(target) || target.closest('button,a,textarea,select,input[type=checkbox],input[type=radio],input[type=file]')) return;
  const cell = target.closest<Cell>(cellSelector);
  const table = cell?.closest("table");
  if (!cell || !table) return;
  if (target instanceof HTMLInputElement) {
    // F2/double-click edits the existing text; replacement typing keeps the
    // spreadsheet arrow navigation available after the new value is entered.
    if (event.key === "Enter" || event.key === "Escape") { event.preventDefault(); cell.tabIndex = -1; cell.focus(); return; }
    if (!replacementEditing.has(target) || caretEditing.has(target) || !event.key.startsWith("Arrow")) return;
  }
  if (event.key === "F2" || event.key === "Enter") { event.preventDefault(); editCell(cell); return; }
  if (event.key.length === 1 || event.key === "Backspace" || event.key === "Delete") {
    event.preventDefault(); editCell(cell, event.key.length === 1 ? event.key : ""); return;
  }
  if (event.shiftKey || !event.key.startsWith("Arrow")) return;
  const cells = matrix(table);
  const previous = positions.get(table);
  const start = previous?.cell === cell && cells[previous.row]?.[previous.column] === cell ? previous : locate(cells, cell);
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
  const parentCell = table.parentElement?.closest<HTMLTableCellElement>("td,th");
  const parentTable = parentCell?.closest("table");
  if (!parentCell || !parentTable) return;
  const origin = locate(matrix(parentTable), parentCell)!;
  const outerRow = vertical ? (delta < 0 ? origin.row - 1 : origin.row + parentCell.rowSpan) : origin.row + Math.min(start.row, parentCell.rowSpan - 1);
  const outerColumn = vertical ? origin.column + start.column : (delta < 0 ? origin.column - 1 : origin.column + parentCell.colSpan);
  focusCell(parentTable, outerRow, outerColumn);
}

// Opt in only editable grids; ordinary directory/list tables retain native keys.
export const spreadsheetGridEvents = {
  onMouseDown: selectGridCell,
  onDoubleClick: editGridCell,
  onKeyDown: navigateGrid,
  onCopy: copyGridCell,
};
