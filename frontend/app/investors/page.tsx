"use client";

import { formatPhone } from "../lib/phone";

import { useViewState } from "@/app/lib/viewMemory";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { Landmark, Pencil } from "lucide-react";
import { createInvestor, updateInvestor, fetchInvestorsPage, fetchDrivers, fetchInvestors } from "../lib/api";
import type { Driver, Investor, InvestorInput } from "../lib/types";
import { useDebouncedValue } from "../lib/useDebouncedValue";
import { EmptyState, ErrorBanner, LoadingTable, ManagementHeader, ManagementSearch, Modal, StatusBadge, TablePagination, TableShell } from "../components/management/ManagementUI";
import { emptyInvestorInput, InvestorForm } from "./InvestorForm";

export default function InvestorsPage() {
  const [investors, setInvestors] = useState<Investor[]>([]);
  const [drivers, setDrivers] = useState<Driver[]>([]);
  const [search, setSearch] = useViewState("page:search", "");
  const [showCompany, setShowCompany] = useViewState("page:showCompany", false);
  const [page, setPage] = useViewState("page:page", 1);
  const [pageSize, setPageSize] = useViewState("page:pageSize", 25);
  const [total, setTotal] = useState(0);
  const [totalPages, setTotalPages] = useState(1);
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Investor | null | undefined>(undefined);
  const [form, setForm] = useState<InvestorInput>(emptyInvestorInput);
  const debouncedSearch = useDebouncedValue(search);
  const loadData = useCallback(async () => {
    setIsLoading(true);
    try {
      const result = await fetchInvestorsPage({ page, pageSize, search: debouncedSearch, includeCompany: showCompany });
      setInvestors(result.items); setPage(result.page); setTotal(result.total); setTotalPages(result.totalPages); setError("");
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Failed to load investors"); }
    finally { setIsLoading(false); }
  }, [setPage, page, pageSize, debouncedSearch, showCompany]);
  useEffect(() => { const timer = window.setTimeout(() => void loadData(), 0); return () => window.clearTimeout(timer); }, [loadData]);

  const open = async (investor: Investor | null) => {
    setError("");
    try {
      const [driverRows, owners] = await Promise.all([fetchDrivers(), fetchInvestors()]);
      setDrivers(driverRows.filter((d) => d.id === investor?.driverId || !owners.some((i) => i.driverId === d.id)));
      setForm(investor ? { fullName: investor.fullName, driverId: investor.driverId, email: investor.email ?? "", phone: investor.phone ?? "", notes: investor.notes ?? "", active: investor.active } : { ...emptyInvestorInput });
      setEditing(investor);
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Failed to load driver options"); }
  };
  const save = async () => {
    setIsSaving(true); setError("");
    try {
      if (editing) await updateInvestor(editing.id, form); else await createInvestor(form);
      setEditing(undefined); await loadData();
    } catch (reason) { setError(reason instanceof Error ? reason.message : "Failed to save investor"); }
    finally { setIsSaving(false); }
  };
  return <div className="space-y-5 animate-fade-in">
    <ManagementHeader icon={Landmark} title="Investors" description="Independent investors and drivers who own additional fleet trucks." count={total} actionLabel="Add investor" onAction={() => void open(null)} />
    {error && <ErrorBanner message={error} />}
    <div className="flex flex-wrap items-center gap-4">
      <ManagementSearch value={search} onChange={(v) => { setSearch(v); setPage(1); }} placeholder="Search investors or truck units…" />
      <label className="flex cursor-pointer items-center gap-2 text-[12px] text-zinc-400">
        <input type="checkbox" checked={showCompany} onChange={(event) => { setShowCompany(event.target.checked); setPage(1); }} className="h-4 w-4 rounded border-zinc-700 bg-zinc-900 accent-blue-600" />
        Show company
      </label>
    </div>
    <TableShell>
      {isLoading ? <LoadingTable columns={6} /> : investors.length === 0 ? <EmptyState message={search ? "No investors match your search." : "No investors yet."} /> :
        <table className="w-full min-w-[760px] text-left text-[13px]">
          <thead><tr className="border-b border-zinc-800/50 text-zinc-500">{["Investor", "Type", "Contact", "Owned trucks", "Status", "Actions"].map((label) => <th key={label} className={`px-4 py-3 font-medium ${label === "Actions" ? "text-right" : ""}`}>{label}</th>)}</tr></thead>
          <tbody>{investors.map((investor) => <tr key={investor.id} className="border-b border-zinc-900/70 text-zinc-300 transition last:border-0 hover:bg-zinc-800/15">
            <td className="px-4 py-3 font-medium text-zinc-200">{investor.driverId ? <Link className="hover:text-blue-400" href={`/drivers/detail?id=${investor.driverId}`}>{investor.fullName}</Link> : investor.fullName}</td>
            <td className="px-4 py-3"><span className="rounded-full bg-blue-500/10 px-2 py-1 text-[11px] text-blue-400">{investor.isCompany ? "Company" : investor.driverId ? "Driver" : "Investor"}</span></td>
            <td className="px-4 py-3"><div className="text-zinc-400">{formatPhone(investor.phone) || "—"}</div><div className="mt-0.5 text-[11px] text-zinc-500">{investor.email ?? "No email"}</div></td>
            <td className="max-w-xs px-4 py-3"><div className="flex flex-wrap gap-1.5">{investor.trucks.length ? investor.trucks.map((truck) => <Link key={truck.id} href={`/trucks/detail?id=${truck.id}`} className="rounded bg-zinc-800/60 px-2 py-1 font-mono text-[11px] hover:text-blue-400">{truck.unitNumber}</Link>) : <span className="text-zinc-500">No trucks</span>}</div></td>
            <td className="px-4 py-3"><StatusBadge active={investor.active} /></td>
            <td className="px-4 py-3 text-right">{!investor.isCompany && <button aria-label={`Edit ${investor.fullName}`} onClick={() => void open(investor)} className="rounded-md p-2 text-zinc-500 hover:bg-zinc-800/60 hover:text-zinc-200"><Pencil className="h-3.5 w-3.5" /></button>}</td>
          </tr>)}</tbody>
        </table>}
    </TableShell>
    {!isLoading && <TablePagination page={page} pageSize={pageSize} totalItems={total} totalPages={totalPages} onPageChange={setPage} onPageSizeChange={(v) => { setPageSize(v); setPage(1); }} />}
    <p className="text-[12px] text-zinc-500">Assign truck owners from <Link href="/trucks" className="text-blue-400 hover:text-blue-300">Trucks</Link>. Manage recurring fees in <Link href="/accounting/driver-charges?tab=trucks" className="text-blue-400 hover:text-blue-300">Recurring Charges</Link> and review <Link href="/accounting/investor-pay" className="text-blue-400 hover:text-blue-300">Investor Pay</Link>. Drivers with fewer than two owned trucks are kept out of this directory.</p>
    {editing !== undefined && <Modal title={editing ? `Edit ${editing.fullName}` : "Add investor"} description="A driver can also be an investor. Ownership stays with the investor when the operating driver changes." isSaving={isSaving} submitLabel={editing ? "Save changes" : "Create investor"} onClose={() => setEditing(undefined)} onSubmit={(event) => { event.preventDefault(); void save(); }}>
      {error && <div className="mb-4"><ErrorBanner message={error} /></div>}
      <InvestorForm value={form} onChange={setForm} drivers={drivers} editing={!!editing} />
    </Modal>}
  </div>;
}
