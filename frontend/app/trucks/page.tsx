"use client";
import type { DriverOption, InvestorOption } from "@/app/lib/types";
import { usePermissions } from "../lib/access";
import { useQuickCreate } from "@/app/lib/topNavigation";

import { useViewState } from "@/app/lib/viewMemory";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { Check, Truck as TruckIcon } from "lucide-react";
import {
  createTruck,
  fetchDriverOptions,
  fetchTrucksPage,
  fetchInvestorOptions,
  uploadIRPFile,
} from "../lib/api";
import { renderPDFPages } from "../lib/pdf";
import { useDebouncedValue } from "../lib/useDebouncedValue";
import {FleetStatusBadge,operatingStatus} from "../components/FleetStatusBadge";
import { conditionLabels } from "../lib/fleetStatus";
import type { Truck, TruckInput, TruckPage } from "../lib/types";
import {
  EmptyState,
  ErrorBanner,
  LoadingTable,
  ManagementHeader,
  ManagementSearch,
  Modal,
  TablePagination,
  TableShell,
} from "../components/management/ManagementUI";
import { emptyTruckInput, TruckForm } from "./TruckForm";
import { DataTruckSetup } from "./DataTruckSetup";

const statusCards = [
  { code: "", label: "All", color: "text-zinc-200 bg-zinc-500/5", selected: "border-zinc-400 ring-zinc-400/30" },
  { code: "ready", label: conditionLabels.ready, color: "text-emerald-400 bg-emerald-500/5", selected: "border-emerald-400 ring-emerald-400/30" },
  { code: "shop", label: conditionLabels.shop, color: "text-amber-400 bg-amber-500/5", selected: "border-amber-400 ring-amber-400/30" },
  { code: "in_use", label: conditionLabels.in_use, color: "text-blue-400 bg-blue-500/5", selected: "border-blue-400 ring-blue-400/30" },
  { code: "out_of_service", label: conditionLabels.out_of_service, color: "text-red-400 bg-red-500/5", selected: "border-red-400 ring-red-400/30" },
  { code: "terminated", label: conditionLabels.terminated, color: "text-zinc-400 bg-zinc-500/5", selected: "border-zinc-400 ring-zinc-400/30" },
] as const;

export default function TrucksPage() {
  const router = useRouter();
  const permissions = usePermissions();
  const [investors, setInvestors] = useState<InvestorOption[]>([]);
  const [trucks, setTrucks] = useState<Truck[]>([]);
  const [drivers, setDrivers] = useState<DriverOption[]>([]);
  const [search, setSearch] = useViewState("page:search", "");
  const [status, setStatus] = useViewState("page:truckStatus", "");
  const [page, setPage] = useViewState("page:page", 1);
  const [pageSize, setPageSize] = useViewState("page:pageSize", 25);
  const [total, setTotal] = useState(0);
  const [statusCounts, setStatusCounts] = useState<TruckPage["statusCounts"] | null>(null);
  const [totalPages, setTotalPages] = useState(1);
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [isUploadingIRP, setIsUploadingIRP] = useState(false);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Truck | null | undefined>(undefined);
  const [savedTruck, setSavedTruck] = useState<Truck | null>(null);
  const [form, setForm] = useState<TruckInput>(emptyTruckInput);
  const [irpFileName, setIRPFileName] = useState<string | null>(null);
  const debouncedSearch = useDebouncedValue(search);

  const requestSequence = useRef(0);
  const loadData = useCallback(async (signal?: AbortSignal, background=false) => {
    const sequence=++requestSequence.current;
    if(!background)setIsLoading(true);
    try {
      const truckPage = await fetchTrucksPage({ page, pageSize, search: debouncedSearch, status },signal);
      if(signal?.aborted || sequence!==requestSequence.current)return;
      setTrucks(truckPage.items);
      setPage(truckPage.page);
      setTotal(truckPage.total);
      setStatusCounts(truckPage.statusCounts);
      setTotalPages(truckPage.totalPages);
      setError("");
    } catch (reason) {
      if(!signal?.aborted && sequence===requestSequence.current)setError(reason instanceof Error ? reason.message : "Failed to load trucks");
    } finally {
      if(!signal?.aborted && sequence===requestSequence.current)setIsLoading(false);
    }
  }, [setPage, debouncedSearch, page, pageSize, status]);

  const loadOptions = async () => {
    try {
      const [drivers, owners] = await Promise.all([fetchDriverOptions(), fetchInvestorOptions()]);
      setDrivers(drivers);
      setInvestors(owners);
      return true;
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to load truck options");
      return false;
    }
  };

  useEffect(() => {
    const controller=new AbortController();
    const timeout = window.setTimeout(() => void loadData(controller.signal), 0);
    const refresh=()=>{if(document.visibilityState==="visible")void loadData(controller.signal,true);};
    const timer=window.setInterval(refresh,30000);window.addEventListener("focus",refresh);
    return () => {controller.abort();window.clearTimeout(timeout);window.clearInterval(timer);window.removeEventListener("focus",refresh);};
  }, [loadData]);

  const openCreate = async () => {
 if(!permissions.includes("trucks.create"))return;
    if (!await loadOptions()) return;
    setForm({ ...emptyTruckInput });
    setIRPFileName(null);
    setEditing(null);
    setError("");
  };

  useQuickCreate(openCreate);

  const uploadCabCard = async (file: File) => {
    if (file.size > 10 * 1024 * 1024) {
      setError("Cab-card files must be 10 MB or smaller.");
      return;
    }

    setIsUploadingIRP(true);
    setError("");
    try {
      const isPDF = file.type === "application/pdf" || file.name.toLowerCase().endsWith(".pdf");
      const pages = isPDF ? await renderPDFPages(file) : [];
      const result = await uploadIRPFile(file, pages);
      setIRPFileName(result.file.fileName);
      setForm((current) => ({
        ...current,
        irpFileId: result.file.id,
        unitNumber: result.fields.unitNumber || current.unitNumber,
        vin: result.fields.vin || current.vin,
        year: result.fields.year ?? current.year,
        make: result.fields.make || current.make,
        model: result.fields.model || current.model,
        licensePlate: result.fields.licensePlate || current.licensePlate,
        licenseState: result.fields.licenseState || current.licenseState,
        registrationExpires:
          result.fields.registrationExpires || current.registrationExpires,
      }));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to read cab card");
    } finally {
      setIsUploadingIRP(false);
    }
  };

  const removeCabCard = () => {
    setIRPFileName(null);
    setForm((current) => ({ ...current, irpFileId: null }));
  };

  const save = async () => {
    if (!permissions.includes(editing ? "trucks.edit" : "trucks.create")) return;
    setIsSaving(true);
    setError("");
    try {
      { const saved = await createTruck(form); if (form.addToDataTruck) setSavedTruck(saved); }
      setEditing(undefined);
      await loadData();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to save truck");
    } finally {
      setIsSaving(false);
    }
  };

  return (
    <div className="space-y-5 animate-fade-in">

      {error && <ErrorBanner message={error} />}
      <div role="group" aria-label="Filter truck status" className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
        {statusCards.map(card => {
          const selected = status === card.code;
          const count = statusCounts ? (card.code ? statusCounts[card.code] : Object.values(statusCounts).reduce((sum, value) => sum + value, 0)) : null;
          return <button key={card.code} type="button" aria-pressed={selected} aria-label={`${card.label}, ${count ?? "loading"} trucks`}
            onClick={() => { setStatus(card.code); setPage(1); }}
            className={`rounded-xl border p-4 text-left transition hover:brightness-125 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500 ${card.color} ${selected ? `${card.selected} ring-1` : "border-zinc-800"}`}>
            <span className="flex items-center justify-between gap-2 text-xs font-medium">{card.label}<Check aria-hidden="true" className={`h-4 w-4 ${selected ? "" : "invisible"}`} /></span>
            <span className="mt-2 block text-2xl font-semibold tabular-nums">{count === null ? "—" : count.toLocaleString()}</span>
          </button>;
        })}
      </div>
      <div className="flex flex-wrap items-center gap-3"><ManagementSearch value={search} onChange={(value) => { setSearch(value); setPage(1); }} placeholder="Search trucks…" /><div className="ml-auto flex flex-wrap items-center gap-2"><ManagementHeader
        icon={TruckIcon}
        title="Trucks"

        count={total}
        actionLabel="Add truck"
        onAction={openCreate}
      /></div></div>

      <TableShell>
        {isLoading ? <LoadingTable columns={5} /> : trucks.length === 0 ? (
          <EmptyState message={search || status ? "No trucks match your search." : "No trucks yet. Add your first truck to get started."} />
        ) : (
          <table className="w-full min-w-[760px] text-left text-[13px]">
            <thead><tr className="border-b border-zinc-800/50 text-zinc-500">
              <th className="px-4 py-3 font-medium">Unit</th>
              <th className="px-4 py-3 font-medium">Equipment</th>
              <th className="px-4 py-3 font-medium">Driver</th>
              <th className="px-4 py-3 font-medium">Status</th>
              <th className="px-4 py-3 font-medium">Status notes</th>
            </tr></thead>
            <tbody>{trucks.map((truck) => (
              <tr key={truck.id} tabIndex={0} aria-label={`Open truck ${truck.unitNumber} details`}
                onClick={() => router.push(`/trucks/detail?id=${truck.id}`)}
                onKeyDown={event => { if (event.key === "Enter" || event.key === " ") { event.preventDefault(); router.push(`/trucks/detail?id=${truck.id}`); } }}
                className="cursor-pointer border-b border-zinc-900/70 text-zinc-300 transition last:border-0 hover:bg-zinc-800/15 focus-visible:outline-2 focus-visible:outline-blue-500 focus-visible:-outline-offset-2">
                <td className="px-4 py-3"><span className="font-mono font-medium text-zinc-200">{truck.unitNumber}</span><div className="mt-0.5 text-[11px] text-zinc-600">{truck.ownerName}</div></td>
                <td className="px-4 py-3"><div className="text-zinc-300">{[truck.year, truck.make, truck.model].filter(Boolean).join(" ") || "—"}</div><div className="mt-0.5 font-mono text-[11px] text-zinc-600">{truck.vin ?? truck.licensePlate ?? "No VIN or plate"}</div></td>
                <td className="px-4 py-3 text-zinc-400">{truck.driverName ?? "Unassigned"}</td>
                <td className="px-4 py-3"><FleetStatusBadge status={truck.operatingStatus ?? operatingStatus(truck.status)} showReady /></td>
                <td className="max-w-sm whitespace-pre-line break-words px-4 py-3 text-xs text-zinc-400">{truck.statusReason?.trim() || "—"}</td>
              </tr>
            ))}</tbody>
          </table>
        )}
      </TableShell>
      {!isLoading && (
        <TablePagination
          page={page}
          pageSize={pageSize}
          totalItems={total}
          totalPages={totalPages}
          onPageChange={setPage}
          onPageSizeChange={(value) => { setPageSize(value); setPage(1); }}
        />
      )}

      {editing !== undefined && (
        <Modal
          title={editing ? `Edit truck ${editing.unitNumber}` : "Add truck"}
          description="Assignment changes automatically update the related driver record."
          isSaving={isSaving || isUploadingIRP}
          submitLabel={editing ? "Save changes" : "Create truck"}
          onClose={() => setEditing(undefined)}
          onSubmit={(event) => { event.preventDefault(); void save(); }}
        >
          {error && <div className="mb-4"><ErrorBanner message={error} /></div>}
          <TruckForm
            creating={!editing}
            investors={investors}
            value={form}
            onChange={setForm}
            drivers={drivers}
            irpFileName={irpFileName}
            isUploadingIRP={isUploadingIRP}
            onUploadIRP={uploadCabCard}
            onRemoveIRP={removeCabCard}
          />
        </Modal>
      )}

      {savedTruck && <Modal isSaving={false} title={`Truck ${savedTruck.unitNumber} saved`} submitLabel="Done" onClose={() => setSavedTruck(null)} onSubmit={event => {event.preventDefault();setSavedTruck(null);}}><DataTruckSetup key={savedTruck.id} truckId={savedTruck.id}/></Modal>}
    </div>
  );
}
