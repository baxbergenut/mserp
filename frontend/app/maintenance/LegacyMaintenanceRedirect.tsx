"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { fetchMaintenance, fetchMaintenanceOptions } from "@/app/lib/api";
import { ErrorBanner } from "@/app/components/management/ManagementUI";

export function LegacyMaintenanceRedirect() {
  const router = useRouter();
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    const query = new URLSearchParams(window.location.search);
    const recordId = query.get("recordId") || query.get("id");
    const target = new URLSearchParams();
    if (query.get("truckId")) target.set("truckId", query.get("truckId")!);
    void (async () => {
      if (recordId) {
        const record = await fetchMaintenance(recordId);
        if (record.expenseId) target.set("expenseId", record.expenseId);
      } else {
        const options = await fetchMaintenanceOptions();
        target.set("categoryId", options.categoryId);
      }
      if (active) router.replace(`/expenses?${target}`);
    })().catch(reason => { if (active) setError(reason.message); });
    return () => { active = false; };
  }, [router]);
  return error ? <ErrorBanner message={error} /> : <p role="status" className="py-12 text-center text-sm text-zinc-500">Opening maintenance expenses…</p>;
}
