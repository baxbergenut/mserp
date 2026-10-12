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
  fetchInvestors,
  updateDriver,
} from "../lib/api";
import { useDebouncedValue } from "../lib/useDebouncedValue";
import type { Dispatcher, Driver, DriverInput, DriverIntake, DriverDirectoryEntry, Investor, Truck } from "../lib/types";
import {
  ConfirmDialog,
  EmptyState,
  ErrorBanner,
  LoadingTable,
  ManagementHeader,
  ManagementSearch,
  Modal,
  RowActions,
  TablePagination,
  TableShell,
} from "../components/management/ManagementUI";
import { usePermissions } from "../lib/access";
import { StatementMenu } from "../accounting/driver-pay/StatementMenu";
import { DriverServiceCards } from "./DriverServices";
import type { DriverServiceIntent } from "../lib/types";
import { DriverStatusDialog } from "./DriverStatusDialog";
import { DriverSetup } from "./DriverSetup";
import { emptyTruckInput } from "../trucks/TruckForm";
import { DriverForm, driverToInput, emptyDriverInput } from "./DriverForm";


export default function DriversPage() {
  return <Suspense><DriversContent /></Suspense>;
}

function DriversContent() {
  const permissions = usePermissions();
  const [statusDriver, setStatusDriver] = useState<Driver | null>(null);
  const [statusMenu, setStatusMenu] = useState<{ driver: Driver; x: number; y: number } | null>(null);
  const closeStatusMenu = useCallback(() => setStatusMenu(null), []);
  const router = useRouter();
  const searchParams = useSearchParams();
  const setupID = searchParams.get("setup");
  const [drivers, setDrivers] = useState<DriverDirectoryEntry[]>([]);
  const [investors, setInvestors] = useState<Investor[]>([]);
  const [loadingLookups, setLoadingLookups] = useState(false);
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
  const [isDeleting, setIsDeleting] = useState(false);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Driver | null | undefined>(undefined);
  const [pendingDelete, setPendingDelete] = useState<Driver | null>(null);
  const [form, setForm] = useState<DriverInput>(emptyDriverInput);
  const [serviceChoices, setServiceChoices] = useState<DriverServiceIntent[]>([]);
  const [savedDriver, setSavedDriver] = useState<Driver | null>(null);
  const saveLatch = useRef(false);
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
    setLoadingLookups(true);
    try {
      const [truckRows, dispatcherRows, driverRows, ownerRows] = await Promise.all([
        fetchTrucks(), fetchDispatchers(), fetchDrivers(), fetchInvestors(),
      ]);
      setInvestors(ownerRows);
      setTrucks(truckRows);
      setDispatchers(dispatcherRows);
      setExistingDrivers(driverRows);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to load assignment options");
    } finally { setLoadingLookups(false); }
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
    setServiceChoices([]); setSavedDriver(null);
    setIntake(null);
    setLinkDriverID("");
    void loadLookups();
    setForm({ ...emptyDriverInput, newTruck: { ...emptyTruckInput }, hireDate: new Intl.DateTimeFormat("en-CA", { timeZone: "America/New_York" }).format(new Date()), escrowAmount: "" });
    void fetchDriverSetupDefaults().then(defaults => {
      setForm(current => current.escrowAmount === "" ? { ...current, escrowAmount: defaults.escrowAmount } : current);
    }).catch(reason => setError(reason instanceof Error ? reason.message : "Failed to load the escrow default"));
    setEditing(null);
    setError("");
  };

  useQuickCreate(openCreate);

  const openEdit = (driver: Driver) => {
    setServiceChoices([]);
    setIntake(null);
    setLinkDriverID("");
    void loadLookups();
    setForm(driverToInput(driver));
    setEditing(driver);
    setError("");
  };

  const openIntake = useCallback((hire: DriverIntake) => {
    void loadLookups();
    const source = hire.driver;
    setForm({ ...emptyDriverInput, newTruck: { ...emptyTruckInput }, escrowAmount: "", fullName: source.fullName,
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
    setSeparateConfirmed(false); setServiceChoices([]); setSavedDriver(null);
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

  const save = async () => {
    if (saveLatch.current) return;
    saveLatch.current = true;
    setIsSaving(true);
    setError("");
    try {
      if (intake) {
        const saved = await completeDriverIntake(intake.id, { ...(linkDriverID ? { linkDriverId: linkDriverID } : { driver: form, separateConfirmed }), services: serviceChoices });
        setSavedDriver(saved);
        setIntake(null);
        if (setupID) router.replace("/drivers", { scroll: false });
      } else if (editing) await updateDriver(editing.id, form);
      else setSavedDriver(await createDriver({ ...form, services: serviceChoices }));
      setEditing(undefined);
      await loadData();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to save driver");
    } finally {
      setIsSaving(false); saveLatch.current = false;
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
          Show terminated drivers
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
                  ? "No active drivers. Check Show terminated drivers to view terminated records."
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
                <tr key={driver.id} tabIndex={!driver.intakeId && permissions.includes("fleet.write") ? 0 : undefined} onContextMenu={event => { if (!driver.intakeId && permissions.includes("fleet.write")) { event.preventDefault(); setStatusMenu({ driver, x: event.clientX, y: event.clientY }); } }} onKeyDown={event => { if (!driver.intakeId && permissions.includes("fleet.write") && (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10"))) { event.preventDefault(); const rect = event.currentTarget.getBoundingClientRect(); setStatusMenu({ driver, x: rect.left + 12, y: rect.bottom }); } }} className="border-b border-zinc-900/70 text-zinc-300 transition last:border-0 hover:bg-zinc-800/15">
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2">{driver.intakeId ? <button type="button" onClick={() => void startSetup(driver.intakeId!)} className="font-medium text-zinc-200 hover:text-blue-400">{driver.fullName}</button> : <Link href={`/drivers/detail?id=${driver.id}`} className="font-medium text-zinc-200 transition hover:text-blue-400">{driver.fullName}</Link>}{driver.intakeId && <span className="rounded bg-blue-500/15 px-1.5 py-0.5 text-[10px] font-medium text-blue-300">New</span>}</div>
                    <div className="mt-0.5 text-[11px] text-zinc-600">{formatPhone(driver.phone) || driver.email || "No contact info"}</div>
                    {driver.teamRole && <Link href={`/drivers/detail?id=${driver.teamPartnerId}`} className="text-[11px] text-blue-400">{driver.teamRole === "main" ? "Main" : "Team"} · {driver.teamPartnerName}</Link>}
                  </td>
                  <td className="px-4 py-3 text-zinc-400">{driver.isOwnerOperator ? "Owner-operator" : "Company"}</td>
                  <td className="px-4 py-3 font-mono tabular-nums text-zinc-300">
                    {driver.teamRole === "team" ? <span className="font-sans text-zinc-500">Main driver’s statement</span> : driver.intakeId ? <span className="font-sans text-zinc-500">Not set</span> : driver.payType === "cpm" ? `$${driver.payRate.toFixed(4)}/mi` : `${driver.payRate.toFixed(2)}% gross`}
                  </td>
                  <td className="px-4 py-3 text-zinc-400">{driver.dispatcherName ?? "—"}</td>
                  <td className="px-4 py-3 font-mono text-zinc-300">{driver.truckUnit ?? driver.teamTruckUnit ?? "—"}</td>
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
                  <td className="px-4 py-3">{driver.intakeId ? <span className="text-xs text-zinc-500">Needs setup</span> : <span className="capitalize text-zinc-400">{driver.status ?? (driver.active ? "active" : "terminated")}</span>}</td>
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

      {statusMenu && <StatementMenu {...statusMenu} label="Change driver status" onClose={closeStatusMenu} onSelect={() => setStatusDriver(statusMenu.driver)} />}
      {statusDriver && <DriverStatusDialog driver={statusDriver} onClose={() => setStatusDriver(null)} onSaved={() => { setStatusDriver(null); void loadData(); }} />}
      {editing === null && <DriverSetup value={form} onChange={next => { setError(""); setForm(next); }} intake={intake} drivers={existingDrivers} trucks={trucks} dispatchers={dispatchers} investors={investors} linkId={linkDriverID} onLink={id => { setLinkDriverID(id); setServiceChoices([]); }} separateConfirmed={separateConfirmed} onConfirmSeparate={setSeparateConfirmed} services={serviceChoices} onServices={setServiceChoices} saving={isSaving} loading={loadingLookups} error={error} onSave={save} onClose={() => { setEditing(undefined); setIntake(null); if (setupID) router.replace("/drivers", { scroll: false }); }} />}
      {editing && <Modal title={`Edit ${editing.fullName}`} isSaving={isSaving} submitLabel="Save changes" onClose={() => setEditing(undefined)} onSubmit={event => { event.preventDefault(); void save(); }}>
        {error && <ErrorBanner message={error} />}
        <DriverForm value={form} onChange={setForm} dispatchers={dispatchers} trucks={trucks} editingDriverId={editing.id} />
      </Modal>}

      {savedDriver && <Modal isSaving={false} title={`${savedDriver.fullName} saved`} submitLabel="Done" cancelLabel="Close" onClose={() => setSavedDriver(null)} onSubmit={event => { event.preventDefault(); setSavedDriver(null); }}><DriverServiceCards driverId={savedDriver.id} allowReminders /><Link className="mt-4 inline-block text-xs text-blue-400" href={`/drivers/detail?id=${savedDriver.id}&services=1`}>Open driver profile</Link></Modal>}

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
