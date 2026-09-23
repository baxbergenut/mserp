"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { ArrowLeft, UserRound } from "lucide-react";
import { fetchDriver } from "@/app/lib/api";
import type { Driver } from "@/app/lib/types";
import { ErrorBanner } from "@/app/components/management/ManagementUI";
import { RelatedExpenses } from "@/app/components/expenses/RelatedExpenses";

export default function DriverDetailPage() {
  const [driver, setDriver] = useState<Driver | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    const driverID = new URLSearchParams(window.location.search).get("id") ?? "";
    const timeout = window.setTimeout(() => {
      if (!driverID) {
        setError("No driver was selected.");
        return;
      }
      void fetchDriver(driverID)
        .then(setDriver)
        .catch((reason) => setError(reason instanceof Error ? reason.message : "Failed to load driver"));
    }, 0);
    return () => window.clearTimeout(timeout);
  }, []);

  return (
    <div className="space-y-6 animate-fade-in">
      <Link href="/drivers" className="inline-flex items-center gap-1.5 text-[12px] text-zinc-500 transition hover:text-zinc-300">
        <ArrowLeft className="h-3.5 w-3.5" /> Back to drivers
      </Link>
      {error && <ErrorBanner message={error} />}
      {driver && (
        <>
          <div className="rounded-xl border border-zinc-800/60 bg-zinc-900/30 p-5">
            <div className="flex items-center gap-3">
              <UserRound className="h-5 w-5 text-blue-400" />
              <div>
                <h1 className="text-lg font-semibold text-zinc-100">{driver.fullName}</h1>
                <p className="mt-1 text-[13px] text-zinc-500">{driver.isOwnerOperator ? "Owner-operator" : "Company driver"}</p>
              </div>
            </div>
            <dl className="mt-5 grid gap-4 text-[12px] sm:grid-cols-4">
              <div><dt className="text-zinc-600">Truck</dt><dd className="mt-1 font-mono text-zinc-300">{driver.truckUnit || "Unassigned"}</dd></div>
              <div><dt className="text-zinc-600">Dispatcher</dt><dd className="mt-1 text-zinc-300">{driver.dispatcherName || "Unassigned"}</dd></div>
              <div><dt className="text-zinc-600">Phone</dt><dd className="mt-1 text-zinc-300">{driver.phone || "—"}</dd></div>
              <div><dt className="text-zinc-600">Status</dt><dd className="mt-1 text-zinc-300">{driver.active ? "Active" : "Inactive"}</dd></div>
            </dl>
          </div>
          <RelatedExpenses driverId={driver.id} />
        </>
      )}
    </div>
  );
}
