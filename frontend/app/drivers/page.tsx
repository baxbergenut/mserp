"use client";

import { formatPhone } from "../lib/phone";

import { useViewState } from "@/app/lib/viewMemory";
import { useQuickCreate } from "@/app/lib/topNavigation";

import { Suspense, useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { FileBadge, Users } from "lucide-react";
import {
  createDriver,
  completeDriverIntake,
  deleteDriver,
  fileDownloadUrl,
  fetchDispatchers,
  fetchDriverDirectory,
  fetchDriverIntakeById,
  fetchDriverSetupDefaults,
  fetchDrivers,
  fetchTrucks,
  uploadCDLFile,
  updateDriver,
} from "../lib/api";
import { renderPDFPages } from "../lib/pdf";
import { useDebouncedValue } from "../lib/useDebouncedValue";
import type { Dispatcher, Driver, DriverInput, DriverIntake, DriverDirectoryEntry, Truck } from "../lib/types";
import {
  controlClass,
  Field,
  ConfirmDialog,
  EmptyState,
  ErrorBanner,
  LoadingTable,
  ManagementHeader,
  ManagementSearch,
  Modal,
  RowActions,
  StatusBadge,
  TablePagination,
  TableShell,
} from "../components/management/ManagementUI";
import { DriverForm, driverToInput, emptyDriverInput } from "./DriverForm";


export default function DriversPage() {
  return <Suspense><DriversContent /></Suspense>;
}

function DriversContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const setupID = searchParams.get("setup");
  const [drivers, setDrivers] = useState<DriverDirectoryEntry[]>([]);
  const [trucks, setTrucks] = useState<Truck[]>([]);
  const [dispatchers, setDispatchers] = useState<Dispatcher[]>([]);
  const [search, setSearch] = useViewState("page:search", "");
  const [showInactiveDrivers, setShowInactiveDrivers] = useViewState("page:showInactiveDrivers", false);
  const [page, setPage] = useViewState("page:page", 1);
  const [pageSize, setPageSize] = useViewState("page:pageSize", 25);
  const [total, setTotal] = useState(0);
  const [totalPages, setTotalPages] = useState(1);
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [isUploadingCDL, setIsUploadingCDL] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Driver | null | undefined>(undefined);
  const [pendingDelete, setPendingDelete] = useState<Driver | null>(null);
  const [form, setForm] = useState<DriverInput>(emptyDriverInput);
  const [cdlFileName, setCDLFileName] = useState<string | null>(null);
  const [intake, setIntake] = useState<DriverIntake | null>(null);
  const [existingDrivers, setExistingDrivers] = useState<Driver[]>([]);
  const [linkDriverID, setLinkDriverID] = useState("");
  const requestSequence = useRef(0);
  const [separateConfirmed, setSeparateConfirmed] = useState(false);
  const debouncedSearch = useDebouncedValue(search);

  const loadData = useCallback(async (silent = false) => {
    const sequence = ++requestSequence.current;
    if (!silent) setIsLoading(true);
    try {
      const driverPage = await fetchDriverDirectory({
        page, pageSize, search: debouncedSearch, includeInactive: showInactiveDrivers,
      });
      if (sequence !== requestSequence.current) return;
      setDrivers(driverPage.items);
      setPage(driverPage.page);
      setTotal(driverPage.total);
      setTotalPages(driverPage.totalPages);
      if (!silent) setError("");
    } catch (reason) {
      if (sequence === requestSequence.current) setError(reason instanceof Error ? reason.message : "Failed to load drivers");
    } finally {
      if (sequence === requestSequence.current) setIsLoading(false);
    }
  }, [setPage, debouncedSearch, page, pageSize, showInactiveDrivers]);

  const loadLookups = useCallback(async () => {
    try {
      const [truckRows, dispatcherRows, driverRows] = await Promise.all([
        fetchTrucks(), fetchDispatchers(), fetchDrivers(),
      ]);
      setTrucks(truckRows);
      setDispatchers(dispatcherRows);
      setExistingDrivers(driverRows);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to load assignment options");
    }
  }, []);

  useEffect(() => {
    const sequenceRef = requestSequence;
    const timeout = window.setTimeout(() => void loadData(), 0);
    const refresh = () => { if (!document.hidden) void loadData(true); };
    const interval = window.setInterval(refresh, 15000);
    document.addEventListener("visibilitychange", refresh);
    return () => { sequenceRef.current++; window.clearTimeout(timeout); window.clearInterval(interval); document.removeEventListener("visibilitychange", refresh); };
  }, [loadData]);

  const openCreate = () => {
    setIntake(null);
    setLinkDriverID("");
    void loadLookups();
    setForm({ ...emptyDriverInput, escrowAmount: "" });
    void fetchDriverSetupDefaults().then(defaults => {
      setForm(current => current.escrowAmount === "" ? { ...current, escrowAmount: defaults.escrowAmount } : current);
    }).catch(reason => setError(reason instanceof Error ? reason.message : "Failed to load the escrow default"));
    setCDLFileName(null);
    setEditing(null);
    setError("");
  };

  useQuickCreate(openCreate);

  const openEdit = (driver: Driver) => {
    setIntake(null);
    setLinkDriverID("");
    void loadLookups();
    setForm(driverToInput(driver));
    setCDLFileName(driver.cdlFileName);
    setEditing(driver);
    setError("");
  };

  const openIntake = useCallback((hire: DriverIntake) => {
    void loadLookups();
    const source = hire.driver;
    setForm({ ...emptyDriverInput, escrowAmount: "", fullName: source.fullName,
      isOwnerOperator: source.driverType === "owner_operator", hireDate: source.hireDate,
      phone: source.phone ?? "", email: source.email ?? "", address: source.address ?? "",
      city: source.city ?? "", state: source.state ?? "", postalCode: source.postalCode ?? "",
      licenseNumber: source.licenseNumber ?? "", licenseState: source.licenseState ?? "",
      licenseExpires: source.licenseExpires ?? "",
    });
    void fetchDriverSetupDefaults().then(defaults => {
      setForm(current => current.escrowAmount === "" ? { ...current, escrowAmount: defaults.escrowAmount } : current);
    }).catch(reason => setError(reason instanceof Error ? reason.message : "Failed to load the escrow default"));
    setIntake(hire);
    setLinkDriverID("");
    setSeparateConfirmed(false);
    setCDLFileName(null);
    setEditing(null);
    setError("");
  }, [loadLookups]);

  useEffect(() => {
    if (!setupID) return;
    let active = true;
    void fetchDriverIntakeById(setupID).then((hire) => { if (active) openIntake(hire); })
      .catch(() => { if (active) setError("This setup task is no longer available. It may already be completed."); });
    return () => { active = false; };
  }, [setupID, openIntake]);

  const startSetup = async (id: string) => {
    try { openIntake(await fetchDriverIntakeById(id)); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "Could not open setup."); }
  };

  const uploadCDL = async (file: File) => {
    if (file.size > 10 * 1024 * 1024) {
      setError("CDL files must be 10 MB or smaller.");
      return;
    }

    setIsUploadingCDL(true);
    setError("");
    try {
      const isPDF = file.type === "application/pdf" || file.name.toLowerCase().endsWith(".pdf");
      const pages = isPDF ? await renderPDFPages(file) : [];
      const result = await uploadCDLFile(file, pages);
      setCDLFileName(result.file.fileName);
      setForm((current) => ({
        ...current,
        cdlFileId: result.file.id,
        fullName: result.fields.fullName || current.fullName,
        licenseNumber: result.fields.licenseNumber || current.licenseNumber,
        licenseState: result.fields.licenseState || current.licenseState,
        licenseExpires: result.fields.licenseExpires || current.licenseExpires,
        address: result.fields.address || current.address,
        city: result.fields.city || current.city,
        state: result.fields.state || current.state,
        postalCode: result.fields.postalCode || current.postalCode,
      }));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to read CDL");
    } finally {
      setIsUploadingCDL(false);
    }
  };

  const removeCDL = () => {
    setCDLFileName(null);
    setForm((current) => ({ ...current, cdlFileId: null }));
  };

  const save = async () => {
    setIsSaving(true);
    setError("");
    try {
      if (intake) {
        await completeDriverIntake(intake.id, linkDriverID ? { linkDriverId: linkDriverID } : { driver: form, separateConfirmed });
        setIntake(null);
        if (setupID) router.replace("/drivers", { scroll: false });
      } else if (editing) await updateDriver(editing.id, form);
      else await createDriver(form);
      setEditing(undefined);
      await loadData();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to save driver");
    } finally {
      setIsSaving(false);
    }
  };

  const remove = async () => {
    if (!pendingDelete) return;
    setIsDeleting(true);
    setError("");
    try {
      await deleteDriver(pendingDelete.id);
      setPendingDelete(null);
      await loadData();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to delete driver");
      setPendingDelete(null);
    } finally {
      setIsDeleting(false);
    }
  };

  return (
    <div className="space-y-5 animate-fade-in">
      <ManagementHeader
        icon={Users}
        title="Drivers"

        count={total}
        actionLabel="Add driver"
        onAction={openCreate}
      />

      {error && <ErrorBanner message={error} />}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <ManagementSearch value={search} onChange={(value) => { setSearch(value); setPage(1); }} placeholder="Search drivers…" />
        <label className="inline-flex cursor-pointer select-none items-center gap-2 text-[12px] text-zinc-400">
          <input
            type="checkbox"
            checked={showInactiveDrivers}
            onChange={(event) => { setShowInactiveDrivers(event.target.checked); setPage(1); }}
            className="h-4 w-4 rounded border-zinc-700 bg-zinc-900 accent-blue-600"
          />
          Show inactive drivers
        </label>
      </div>

      <TableShell>
        {isLoading ? (
          <LoadingTable columns={8} />
        ) : drivers.length === 0 ? (
          <EmptyState
            message={
              search
                ? "No drivers match your search."
                : !showInactiveDrivers && drivers.some((driver) => !driver.active)
                  ? "No active drivers. Check Show inactive drivers to view inactive records."
                  : "No drivers yet. Add your first driver to get started."
            }
          />
        ) : (
          <table className="w-full min-w-[920px] text-left text-[13px]">
            <thead>
              <tr className="border-b border-zinc-800/50 text-zinc-500">
                <th className="px-4 py-3 font-medium">Driver</th>
                <th className="px-4 py-3 font-medium">Type</th>
                <th className="px-4 py-3 font-medium">Compensation</th>
                <th className="px-4 py-3 font-medium">Dispatcher</th>
                <th className="px-4 py-3 font-medium">Truck</th>
                <th className="px-4 py-3 font-medium">CDL</th>
                <th className="px-4 py-3 font-medium">Status</th>
                <th className="px-4 py-3 font-medium text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {drivers.map((driver) => (
                <tr key={driver.id} className="border-b border-zinc-900/70 text-zinc-300 transition last:border-0 hover:bg-zinc-800/15">
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2">{driver.intakeId ? <button type="button" onClick={() => void startSetup(driver.intakeId!)} className="font-medium text-zinc-200 hover:text-blue-400">{driver.fullName}</button> : <Link href={`/drivers/detail?id=${driver.id}`} className="font-medium text-zinc-200 transition hover:text-blue-400">{driver.fullName}</Link>}{driver.intakeId && <span className="rounded bg-blue-500/15 px-1.5 py-0.5 text-[10px] font-medium text-blue-300">New</span>}</div>
                    <div className="mt-0.5 text-[11px] text-zinc-600">{formatPhone(driver.phone) || driver.email || "No contact info"}</div>
                  </td>
                  <td className="px-4 py-3 text-zinc-400">{driver.isOwnerOperator ? "Owner-operator" : "Company"}</td>
                  <td className="px-4 py-3 font-mono tabular-nums text-zinc-300">
                    {driver.intakeId ? <span className="font-sans text-zinc-500">Not set</span> : driver.payType === "cpm" ? `$${driver.payRate.toFixed(4)}/mi` : `${driver.payRate.toFixed(2)}% gross`}
                  </td>
                  <td className="px-4 py-3 text-zinc-400">{driver.dispatcherName ?? "—"}</td>
                  <td className="px-4 py-3 font-mono text-zinc-300">{driver.truckUnit ?? "—"}</td>
                  <td className="px-4 py-3">
                    {driver.cdlFileId ? (
                      <a
                        href={fileDownloadUrl(driver.cdlFileId)}
                        target="_blank"
                        rel="noreferrer"
                        className="inline-flex items-center gap-1.5 text-[12px] text-blue-400 transition hover:text-blue-300"
                        title={driver.cdlFileName ?? "Open CDL"}
                      >
                        <FileBadge className="h-3.5 w-3.5" />
                        CDL
                      </a>
                    ) : (
                      <span className="text-zinc-600">—</span>
                    )}
                  </td>
                  <td className="px-4 py-3">{driver.intakeId ? <span className="text-xs text-zinc-500">Needs setup</span> : <StatusBadge active={driver.active} />}</td>
                  <td className="px-4 py-3">{driver.intakeId ? <button type="button" onClick={() => void startSetup(driver.intakeId!)} className="float-right text-xs text-blue-400 hover:text-blue-300">Set up</button> : <RowActions onEdit={() => openEdit(driver)} onDelete={() => setPendingDelete(driver)} />}</td>
                </tr>
              ))}
            </tbody>
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
          title={intake ? `Set up ${intake.driver.fullName}` : editing ? `Edit ${editing.fullName}` : "Add driver"}
          description={intake ? "Review the FleetScope details, enter a positive pay rate, and choose assignments. Complete setup to make this driver available for accounting." : "Assignment changes are reflected on both the driver and truck records."}
          isSaving={isSaving || isUploadingCDL}
          submitLabel={intake ? (linkDriverID ? "Confirm link" : "Complete setup") : editing ? "Save changes" : "Create driver"}
          onClose={() => { setEditing(undefined); setIntake(null); if (setupID) router.replace("/drivers", { scroll: false }); }}
          onSubmit={(event) => { event.preventDefault(); void save(); }}
        >
          {error && <div className="mb-4"><ErrorBanner message={error} /></div>}
          {intake && <div className="mb-5"><Field label="Already in MSERP?"><select className={controlClass} value={linkDriverID} onChange={(event) => setLinkDriverID(event.target.value)}>
            <option value="">Set up as a new driver</option>
            {existingDrivers.map((driver) => <option key={driver.id} value={driver.id}>{driver.fullName} · {formatPhone(driver.phone) || driver.email || "No contact details"}{driver.active ? "" : " (inactive)"}</option>)}
          </select></Field>{linkDriverID && <p className="mt-3 text-sm text-zinc-400">Confirm this is the same person as {intake.driver.fullName}. Linking keeps the existing profile, pay rates and assignments.</p>}</div>}
          {intake && !linkDriverID && intake.candidates.length > 0 && <div className="mb-5 rounded-lg border border-amber-500/30 bg-amber-500/5 p-3 text-xs text-amber-200">
            <p>Possible existing drivers: {intake.candidates.map((candidate) => `${candidate.fullName} (${formatPhone(candidate.phone) || candidate.email || "no contact details"})`).join(", ")}. If this is the same person, select the existing driver above.</p>
            <label className="mt-3 flex items-center gap-2"><input type="checkbox" required checked={separateConfirmed} onChange={(event) => setSeparateConfirmed(event.target.checked)} />I reviewed the matches. This is a different person.</label>
          </div>}
          {!linkDriverID && <DriverForm
            value={form}
            onChange={setForm}
            dispatchers={dispatchers}
            trucks={trucks}
            cdlFileName={cdlFileName}
            isUploadingCDL={isUploadingCDL}
            onUploadCDL={uploadCDL}
            onRemoveCDL={removeCDL}
            showEscrow={editing === null}
          />}
        </Modal>
      )}

      {pendingDelete && (
        <ConfirmDialog
          title="Delete driver?"
          message={`This permanently deletes ${pendingDelete.fullName}. Their load history remains and any truck assignment will be released.`}
          isDeleting={isDeleting}
          onCancel={() => setPendingDelete(null)}
          onConfirm={() => void remove()}
        />
      )}
    </div>
  );
}
