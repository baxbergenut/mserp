"use client";

import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { fetchPayStatement } from "@/app/lib/api";
import type { DriverPayDriver } from "@/app/lib/types";
import { DriverCard, type DriverCardProps } from "./DriverCard";

export function LazyDriverCard({ onDetails, detailRevision, ...props }: DriverCardProps & { detailRevision?: string; onDetails: (expected: DriverPayDriver, detail: DriverPayDriver, revision: string) => void }) {
  const { driver, open } = props;
  const expected = useRef(driver);
  useLayoutEffect(() => { expected.current = driver; }, [driver]);
  const id = driver.id, week = driver.edits.weekStart, investor = !!driver.investorId, summary = !!driver.summary, version = driver.edits.version;
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!open || !summary) return;
    const controller = new AbortController();
    const request = fetchPayStatement(investor, week, id, controller.signal);
    request.then(report => {
      if (controller.signal.aborted) return;
      const detail = report.drivers.find(row => row.id === id);
      if (!detail || detail.summary) throw new Error("Statement changed. Reload this week to see the current list.");
      onDetails(expected.current, detail, report.revision);
    }).catch(reason => { if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : "Unable to load statement"); });
    return () => controller.abort();
  }, [id, week, investor, summary, version, detailRevision, open, onDetails, attempt]);
  const retry = useCallback(() => { setError(""); setAttempt(value => value + 1); }, []);
  return <DriverCard {...props} detailError={error} onRetryDetails={retry} />;
}
