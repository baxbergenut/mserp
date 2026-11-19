"use client";
import { useMemo } from "react";
import { useLiveBoard, type LiveBoardOptions } from "@/app/components/boards/useLiveBoard";
import { fetchGrossPresence, updateGrossPresence, clearGrossPresence, subscribeGrossEvents } from "@/app/lib/api";
import type { GrossBoard, GrossBoardPresence } from "@/app/lib/types";
import { selectedGrossCell, type GrossCell } from "./grossCells";

export function useGrossLive(week: string, onChanged: (board?: GrossBoard) => void) {
  const options = useMemo<LiveBoardOptions<GrossBoard,GrossBoardPresence,GrossCell>>(() => ({
    read: () => fetchGrossPresence(week),
    write: (client: string, cell: NonNullable<ReturnType<typeof selectedGrossCell>>) => updateGrossPresence(client, { ...cell, weekStart: week }),
    clear: clearGrossPresence,
    select: selectedGrossCell,
    subscribe: (...args) => subscribeGrossEvents(week, ...args),
  }), [week]);
  return useLiveBoard(options, onChanged);
}
