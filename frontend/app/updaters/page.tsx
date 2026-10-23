"use client";

import { useCallback, useEffect, useState } from "react";
import { ErrorBanner } from "../components/management/ManagementUI";
import { fetchUpdaters } from "../lib/api";
import type { Updater } from "../lib/types";
import { UpdaterPanel } from "./UpdaterPanel";

export default function UpdatersPage() {
  const [updaters, setUpdaters] = useState<Updater[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState("");
  const loadData = useCallback(async () => {
    setIsLoading(true);
    try {
      setUpdaters(await fetchUpdaters());
      setError("");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to load updaters");
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    const timeout = window.setTimeout(() => void loadData(), 0);
    return () => window.clearTimeout(timeout);
  }, [loadData]);

  return <div className="space-y-5 animate-fade-in">
    {error && <div className="space-y-2"><ErrorBanner message={error} /><button type="button" onClick={() => void loadData()} className="text-sm text-blue-400 hover:underline">Retry loading updaters</button></div>}
    <UpdaterPanel updaters={updaters} isLoading={isLoading} onChanged={loadData} />
  </div>;
}
