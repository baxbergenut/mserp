"use client";

import type { InventoryRecord } from "@/app/lib/types";
import { InventoryEditor, type InventoryContext } from "../inventory/InventoryForms";

const context: InventoryContext = {
  data: { assets: [], lines: [], supplies: [], locations: [], equipment: [], "transponder-notes": [], balances: [], movements: [] },
  trucks: [],
};

export function TrailerEditor({ trailer, onClose, onSaved }: { trailer: InventoryRecord; onClose: () => void; onSaved: (record: InventoryRecord) => void }) {
  return <InventoryEditor collection="trailers" record={trailer} context={context} onClose={onClose} onSaved={onSaved} />;
}
