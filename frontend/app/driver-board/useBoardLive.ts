"use client";
import { useLiveBoard } from "@/app/components/boards/useLiveBoard";
import { clearBoardPresence, fetchBoardPresence, subscribeBoardEvents, updateBoardPresence } from "@/app/lib/api";
import type { DriverBoard } from "@/app/lib/types";
import { selectedBoardCell } from "./boardCells";
const options = {
  read: fetchBoardPresence,
  write: (client: string, cell: NonNullable<ReturnType<typeof selectedBoardCell>>) => updateBoardPresence(client, cell.driverId, cell.column),
  clear: clearBoardPresence,
  select: selectedBoardCell,
  subscribe: subscribeBoardEvents,
};
export function useBoardLive(onChanged: (board?: DriverBoard) => void) { return useLiveBoard(options, onChanged); }
