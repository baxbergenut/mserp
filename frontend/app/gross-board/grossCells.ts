export type GrossCell = { driverId: string; date: string; slot: number; column: string };
export function selectedGrossCell(target: EventTarget | null): GrossCell | null {
  if (!(target instanceof Element)) return null;
  const cell = target.closest<HTMLElement>('[data-gross-driver]');
  if (!cell) return null;
  const column = target.closest<HTMLElement>('[data-gross-field]')?.dataset.grossField ?? cell.dataset.grossField ?? "loadNumber";
  return { driverId: cell.dataset.grossDriver!, date: cell.dataset.grossDate ?? "", slot: Number(cell.dataset.grossSlot ?? 0), column };
}
