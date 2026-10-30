"use client";

import { memo, useCallback } from "react";
import type { DriverPayLoad, LoadWeekTransferMode, PayLoadPlacementInput, PayLoadPlacementResult } from "@/app/lib/types";
import type { DriverCardProps } from "./DriverCard";
import { LazyDriverCard } from "./LazyDriverCard";

export type PayRowActions = {
  place: (input: PayLoadPlacementInput) => Promise<PayLoadPlacementResult | void>;
  transfer: (id: string, load: DriverPayLoad, mode: LoadWeekTransferMode, reason: string) => Promise<void>;
  settle: (id: string, reopen: boolean) => void;
  accept: (id: string, load: DriverPayLoad, field: "originalRate" | "totalMiles") => Promise<void>;
  reload: (id: string) => Promise<void>;
  openExpense: (id: string, expense: string) => void;
};

type Props = Omit<DriverCardProps, "onReload" | "onSettlement" | "onTransfer" | "onPlacement" | "onAcceptSource" | "onOpenExpense"> & {
  actions: PayRowActions;
  canEdit: boolean;
  canSettle: boolean;
  detailRevision: string;
  onDetails: Parameters<typeof LazyDriverCard>[0]["onDetails"];
};

// Bind row identity once; another row's edits no longer replace every callback.
export const StatementRow = memo(function StatementRow({ actions, canEdit, canSettle, ...props }: Props) {
  const id = props.driver.id;
  const transfer = useCallback((load: DriverPayLoad, mode: LoadWeekTransferMode, reason: string) => actions.transfer(id, load, mode, reason), [actions, id]);
  const settle = useCallback((reopen: boolean) => actions.settle(id, reopen), [actions, id]);
  const accept = useCallback((load: DriverPayLoad, field: "originalRate" | "totalMiles") => actions.accept(id, load, field), [actions, id]);
  const reload = useCallback(() => actions.reload(id), [actions, id]);
  const openExpense = useCallback((expense: string) => actions.openExpense(id, expense), [actions, id]);
  return <LazyDriverCard {...props} onPlacement={canEdit ? actions.place : undefined} onTransfer={canEdit ? transfer : undefined} onSettlement={canSettle ? settle : undefined} onAcceptSource={canEdit ? accept : undefined} onReload={reload} onOpenExpense={openExpense} />;
});
