"use client";

import { useEffect, useState } from "react";
import { fetchDriverPay, fetchInvestorPay } from "@/app/lib/api";
import type { DriverPayDriver } from "@/app/lib/types";
import { DriverCard, type DriverCardProps } from "./DriverCard";

export function LazyDriverCard({ onDetails, ...props }: DriverCardProps & { onDetails: (expected: DriverPayDriver, detail: DriverPayDriver) => void }) {
  const { driver, open } = props;
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!open || !driver.summary) return;
    const controller = new AbortController();
    const query = { page: 1, pageSize: 1, statementId: driver.id, search: "", dispatcherId: "" };
    const request = driver.investorId ? fetchInvestorPay(driver.edits.weekStart, driver.id, controller.signal, query) : fetchDriverPay(driver.edits.weekStart, controller.signal, query);
    request.then(report => {
      if (controller.signal.aborted) return;
      const detail = report.drivers.find(row => row.id === driver.id);
      if (!detail || detail.summary) throw new Error("Statement changed. Reload this week to see the current list.");
      onDetails(driver, detail);
    }).catch(reason => { if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : "Unable to load statement"); });
    return () => controller.abort();
  }, [driver, open, onDetails, attempt]);
  return <DriverCard {...props} detailError={error} onRetryDetails={() => { setError(""); setAttempt(value => value + 1); }} />;
}
