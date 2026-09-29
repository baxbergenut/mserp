"use client";

import { useViewState } from "@/app/lib/viewMemory";

import { useEffect, useState } from "react";
import Link from "next/link";
import { ArrowRight, UserRound } from "lucide-react";
import { fetchDriverIntake } from "../lib/api";
import type { DriverIntake, PaginatedResponse } from "../lib/types";
import { ErrorBanner, TablePagination } from "../components/management/ManagementUI";

export function DriverSetupTasks({ search = "", revision = 0, onCount }: { search?: string; revision?: number; onCount?: (count: number) => void }) {
  const [data, setData] = useState<PaginatedResponse<DriverIntake> | null>(null);
  const [page, setPage] = useViewState("DriverSetupTasks:page", 1);
  const [pageSize, setPageSize] = useViewState("DriverSetupTasks:pageSize", 25);
  const [error, setError] = useState("");

  useEffect(() => {
    let active = true;
    let inFlight = false;
    const refresh = async () => {
      if (inFlight || document.hidden) return;
      inFlight = true;
      try {
        const result = await fetchDriverIntake({ page, pageSize, search });
        if (active) { setData(result); onCount?.(result.total); setPage(result.page); setError(""); }
      } catch (reason) {
        if (active) setError(reason instanceof Error ? reason.message : "Could not load driver setup tasks.");
      } finally { inFlight = false; }
    };
    void refresh();
    const interval = window.setInterval(() => void refresh(), 15000);
    document.addEventListener("visibilitychange", refresh);
    return () => { active = false; window.clearInterval(interval); document.removeEventListener("visibilitychange", refresh); };
  }, [page, pageSize, search, revision, onCount, setPage]);

  return (
    <section aria-label="Driver setup tasks" className="space-y-3">
      {error && <ErrorBanner message={error} />}
      {!data && !error && <p className="text-sm text-zinc-500">Loading driver setup tasks…</p>}
      {data && data.total === 0 && <p className="text-sm text-zinc-500">{search ? "No matching driver setup tasks." : "No drivers awaiting setup."}</p>}
      {data && data.items.length > 0 && <div className="divide-y divide-zinc-800 rounded-xl border border-zinc-800 bg-card">
        {data.items.map((task) => <Link key={task.id} href={`/drivers?setup=${task.id}`} className="flex items-center gap-3 px-4 py-3 transition hover:bg-zinc-800/30">
          <UserRound className="h-4 w-4 shrink-0 text-zinc-500" />
          <div className="min-w-0 flex-1"><p className="text-sm font-medium text-zinc-200">Set up {task.driver.fullName}</p><p className="mt-0.5 text-xs text-zinc-500">Driver setup · Pay rate and assignments</p></div>
          <ArrowRight className="h-4 w-4 shrink-0 text-zinc-500" />
        </Link>)}
      </div>}
      {data && data.total > pageSize && <TablePagination page={data.page} pageSize={data.pageSize} totalItems={data.total} totalPages={data.totalPages} onPageChange={setPage} onPageSizeChange={(size) => { setPageSize(size); setPage(1); }} />}
    </section>
  );
}
