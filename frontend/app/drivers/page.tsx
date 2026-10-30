"use client";

import { statusClass, displayStatus } from "@/app/lib/statusColors";

import type { DriverOption, TruckOption, DispatcherOption, InvestorOption } from "@/app/lib/types";

import { TableFilters } from "@/app/components/management/TableFilters";
import { employmentTypeLabel } from "../lib/driverType";

import { formatPhone } from "../lib/phone";

import { useViewState } from "@/app/lib/viewMemory";
import { useQuickCreate } from "@/app/lib/topNavigation";

import { Suspense, useCallback, useEffect, useRef, useState } from "react";
import Link from "@/app/components/AccessLink";
import { useRouter, useSearchParams } from "next/navigation";
import { FileBadge, Users } from "lucide-react";
import {
  createDriver,
  completeDriverIntake,
  deleteDriver,
  fileDownloadUrl,
  fetchDispatcherOptions,
  fetchDriverDirectory,
  fetchDriverIntakeById,
  fetchDriverSetupDefaults,
  fetchDriverOptions,
  fetchTruckOptions,
  fetchInvestorOptions,
  updateDriver,
} from "../lib/api";
import { useDebouncedValue } from "../lib/useDebouncedValue";
import type { Driver, DriverInput, DriverIntake, DriverDirectoryEntry, DriverDirectoryFilters } from "../lib/types";
import {
  controlClass,
  ConfirmDialog,
  EmptyState,
  ErrorBanner,
  ManagementHeader,
  ManagementSearch,
  Modal,
  RowActions,
  TablePagination,
  TableShell,
} from "../components/management/ManagementUI";
import { usePermissions, canCreateDriver } from "../lib/access";
import { StatementMenu } from "../accounting/driver-pay/StatementMenu";
import { DriverServiceCards, defaultDriverServices } from "./DriverServices";
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
  const [investors, setInvestors] = useState<InvestorOption[]>([]);
  const [loadingLookups, setLoadingLookups] = useState(false);
  const [trucks, setTrucks] = useState<TruckOption[]>([]);
  const [dispatchers, setDispatchers] = useState<DispatcherOption[]>([]);
  const [search, setSearch] = useViewState("page:search", "");
  const [filters, setFilters] = useViewState<DriverDirectoryFilters>("drivers:filters", { employment: "active" });
  const changeFilter = (key: keyof DriverDirectoryFilters, value: string) => { setFilters(current => ({ ...current, [key]: value })); setPage(1); };
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
  const openedSetupID = useRef<string | null>(null);
  const [existingDrivers, setExistingDrivers] = useState<DriverOption[]>([]);
  const [linkDriverID, setLinkDriverID] = useState("");
  const requestSequence = useRef(0);
  const [separateConfirmed, setSeparateConfirmed] = useState(false);
  const debouncedSearch = useDebouncedValue(search);

  const loadData = useCallback(async (silent = false) => {
    const sequence = ++requestSequence.current;
    if (!silent) setIsLoading(true);
    try {
      const driverPage = await fetchDriverDirectory({
        page, pageSize, search: debouncedSearch, ...filters,
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
  }, [setPage, debouncedSearch, page, pageSize, filters]);

  const loadLookups = useCallback(async () => {
    setLoadingLookups(true);
    try {
      const [truckRows, dispatcherRows, driverRows, ownerRows] = await Promise.all([
        fetchTruckOptions(), fetchDispatcherOptions(), fetchDriverOptions(), fetchInvestorOptions(),
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
 if(!canCreateDriver(permissions))return;
    setServiceChoices(defaultDriverServices()); setSavedDriver(null);
    setIntake(null);
    setLinkDriverID("");
    void loadLookups();
    setForm({ ...emptyDriverInput, newTruck: permissions.includes("trucks.create") ? { ...emptyTruckInput } : undefined, hireDate: new Intl.DateTimeFormat("en-CA", { timeZone: "America/New_York" }).format(new Date()), escrowAmount: "" });
    void fetchDriverSetupDefaults().then(defaults => {
      setForm(current => current.escrowAmount === "" ? { ...current, escrowAmount: defaults.escrowAmount } : current);
    }).catch(reason => setError(reason instanceof Error ? reason.message : "Failed to load the escrow default"));
    setEditing(null);
    setError("");
  };

  useQuickCreate(openCreate);

  const openEdit = (driver: Driver) => {
 if(!permissions.includes("drivers.edit"))return;
    setServiceChoices([]);
    setIntake(null);
    setLinkDriverID("");
    void loadLookups();
    setForm(driverToInput(driver));
    setEditing(driver);
    setError("");
  };

  const openIntake = useCallback((hire: DriverIntake) => {
    if (!permissions.includes("tasks.review")) { setError("Completing an intake requires Tasks Review permission."); return; }
 if(!canCreateDriver(permissions)) {setError("Driver setup requires Add driver, private details editing, compensation editing and escrow creation.");return;}
    void loadLookups();
    const source = hire.driver;
    setForm({ ...emptyDriverInput, newTruck: permissions.includes("trucks.create") ? { ...emptyTruckInput } : undefined, escrowAmount: "", fullName: source.fullName,
      firstName: source.firstName ?? "", lastName: source.lastName ?? "",
      dateOfBirth: source.dateOfBirth ?? "",
      hireDate: source.hireDate,
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
    setSeparateConfirmed(false); setServiceChoices(defaultDriverServices()); setSavedDriver(null);
    setEditing(null);
    setError("");
    return true;
  }, [loadLookups, permissions]);

  useEffect(() => {
    if (!setupID) { openedSetupID.current = null; return; }
    // Session refreshes replace permissions and openIntake. Initialize a setup
    // link only once so those refreshes cannot overwrite the user's draft.
    if (openedSetupID.current === setupID) return;
    let active = true;
    void fetchDriverIntakeById(setupID).then((hire) => {
      if (active && openIntake(hire)) openedSetupID.current = setupID;
    })
      .catch(() => { if (active) setError("This setup task is no longer available. It may already be completed."); });
    return () => { active = false; };
  }, [setupID, openIntake]);

  const startSetup = async (id: string) => {
    try { openIntake(await fetchDriverIntakeById(id)); }
    catch (reason) { setError(reason instanceof Error ? reason.message : "Could not open setup."); }
  };

  const save = async () => {
    if (editing ? !permissions.includes("drivers.edit") : !canCreateDriver(permissions)) return;
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
    if (!pendingDelete || !permissions.includes("drivers.delete")) return;
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

      {error && <ErrorBanner message={error} />}
      {permissions.includes("drivers.create") && !canCreateDriver(permissions) && <p role="status" className="text-sm text-amber-300">Driver setup also requires: {[["drivers.private.edit", "private details editing"], ["driver_compensation.edit", "compensation editing"], ["escrow.create", "escrow creation"]].filter(([key]) => !permissions.includes(key)).map(([, label]) => label).join(", ")}.</p>}
      <div className="flex flex-wrap items-center gap-3">
        <ManagementSearch value={search} onChange={(value) => { setSearch(value); setPage(1); }} placeholder="Search drivers…" />
        <TableFilters>
          <select aria-label="Employment" className={controlClass} value={filters.employment ?? "active"} onChange={e => changeFilter("employment", e.target.value)}><option value="active">Active drivers</option><option value="all">All drivers</option><option value="terminated">Terminated</option><option value="pending">Needs setup</option></select>
          <select aria-label="Availability" className={controlClass} value={filters.availability ?? ""} onChange={e => changeFilter("availability", e.target.value)}><option value="">All availability</option>{[["active","Available"],["active_or_new","Active (incl. new)"],["home","Home"],["vacation","Vacation"],["rest","Rest"],["family_issue","Family issue"],["health_issue","Health issue"],["new_driver","New driver"]].map(([value,label]) => <option key={value} value={value}>{label}</option>)}</select>
          <select aria-label="Driver type" className={controlClass} value={filters.driverType ?? ""} onChange={e => changeFilter("driverType", e.target.value)}><option value="">All driver types</option><option value="company">Company driver</option><option value="owner_operator">Owner operator</option><option value="investor_driver">Investor truck driver</option><option value="unassigned">Unassigned</option></select>
          <select aria-label="Truck assignment" className={controlClass} value={filters.assignment ?? ""} onChange={e => changeFilter("assignment", e.target.value)}><option value="">All assignments</option><option value="assigned">With truck</option><option value="unassigned">Without truck</option></select>
          {(search || filters.employment !== "active" || filters.availability || filters.driverType || filters.assignment) && <button className="ui-button" onClick={() => { setSearch(""); setFilters({ employment: "active" }); setPage(1); }}>Clear filters</button>}
        </TableFilters>

      <div className="ml-auto flex flex-wrap items-center gap-2"><ManagementHeader
        icon={Users}
        title="Drivers"

        count={total}
        actionLabel="Add driver"
        onAction={openCreate}
      /></div></div>

      <TableShell>
        <table className="w-full min-w-[920px] text-left text-[13px]">
            <thead>
              <tr className="border-b border-zinc-800/50 text-zinc-500">
                <th className="px-4 py-3 font-medium">Driver</th>
                <th className="px-4 py-3 font-medium">Type</th>
                {permissions.includes("driver_compensation.read") && <th className="px-4 py-3 font-medium">Compensation</th>}
                <th className="px-4 py-3 font-medium">Dispatcher</th>
                <th className="px-4 py-3 font-medium">Truck</th>
                {permissions.includes("drivers.documents.read") && <th className="px-4 py-3 font-medium">CDL</th>}
                <th className="px-4 py-3 font-medium">Status</th>
                <th className="px-4 py-3 font-medium text-right">Actions</th>
              </tr>
            </thead>
            <tbody>{isLoading ? <tr><td colSpan={8} className="p-8 text-center text-zinc-500" role="status">Loading…</td></tr> : drivers.length === 0 ? <tr><td colSpan={8}>{(
          <EmptyState
            message="No drivers match this view."

          />
        )}</td></tr> : <>
              {drivers.map((driver) => (
                <tr key={driver.id} tabIndex={!driver.intakeId && permissions.includes("drivers.edit") ? 0 : undefined} onContextMenu={event => { if (!driver.intakeId && permissions.includes("drivers.edit")) { event.preventDefault(); setStatusMenu({ driver, x: event.clientX, y: event.clientY }); } }} onKeyDown={event => { if (!driver.intakeId && permissions.includes("drivers.edit") && (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10"))) { event.preventDefault(); const rect = event.currentTarget.getBoundingClientRect(); setStatusMenu({ driver, x: rect.left + 12, y: rect.bottom }); } }} className="border-b border-zinc-900/70 text-zinc-300 transition last:border-0 hover:bg-zinc-800/15">
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2">{driver.intakeId ? <button disabled={!canCreateDriver(permissions)} type="button" onClick={() => void startSetup(driver.intakeId!)} className="font-medium text-zinc-200 hover:text-blue-400">{driver.fullName}</button> : <Link href={`/drivers/detail?id=${driver.id}`} className="font-medium text-zinc-200 transition hover:text-blue-400">{driver.fullName}</Link>}{driver.intakeId && <span className="rounded bg-blue-500/15 px-1.5 py-0.5 text-[10px] font-medium text-blue-300">New</span>}</div>
                    <div className="mt-0.5 text-[11px] text-zinc-600">{formatPhone(driver.phone) || driver.email || "No contact info"}</div>
                    {driver.teamRole && <Link href={`/drivers/detail?id=${driver.teamPartnerId}`} className="text-[11px] text-blue-400">{driver.teamRole === "main" ? "Main" : "Team"} · {driver.teamPartnerName}</Link>}
                  </td>
                  <td className="px-4 py-3 text-zinc-400">{employmentTypeLabel(driver.employmentType)}</td>
                  {permissions.includes("driver_compensation.read") && <td className="px-4 py-3 font-mono tabular-nums text-zinc-300">
                    {driver.teamRole === "team" ? <span className="font-sans text-zinc-500">Main driver’s statement</span> : driver.intakeId ? <span className="font-sans text-zinc-500">Not set</span> : driver.payType === "cpm" ? `$${driver.payRate?.toFixed(4)}/mi` : `${driver.payRate?.toFixed(2)}% gross`}
                  </td>}
                  <td className="px-4 py-3 text-zinc-400">{driver.dispatcherName ?? "—"}</td>
                  <td className="px-4 py-3 font-mono text-zinc-300">{driver.truckUnit ?? driver.teamTruckUnit ?? "—"}</td>
                  {permissions.includes("drivers.documents.read") && <td className="px-4 py-3">
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
                  </td>}
                  <td className="px-4 py-3">{driver.intakeId ? <span className={`rounded px-2 py-0.5 text-xs ${statusClass("new_driver")}`}>Needs setup</span> : <span className={`rounded px-2 py-0.5 capitalize ${statusClass(driver.status ?? (driver.active ? "active" : "terminated"))}`}>{displayStatus(driver.status ?? (driver.active ? "active" : "terminated"))}</span>}</td>
                  <td className="px-4 py-3">{driver.intakeId ? <button type="button" onClick={() => void startSetup(driver.intakeId!)} className="float-right text-xs text-blue-400 hover:text-blue-300">Set up</button> : <RowActions onEdit={() => openEdit(driver)} onDelete={() => setPendingDelete(driver)} />}</td>
                </tr>
              ))}
            </>}</tbody>
          </table>
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
      {editing === null && <DriverSetup value={form} onChange={next => { setError(""); setForm(next); }} intake={intake} drivers={existingDrivers} trucks={trucks} dispatchers={dispatchers} investors={investors} linkId={linkDriverID} onLink={id => { setLinkDriverID(id); setServiceChoices(defaultDriverServices()); }} separateConfirmed={separateConfirmed} onConfirmSeparate={setSeparateConfirmed} services={serviceChoices} onServices={setServiceChoices} saving={isSaving} loading={loadingLookups} error={error} onSave={save} onClose={() => { setEditing(undefined); setIntake(null); if (setupID) router.replace("/drivers", { scroll: false }); }} />}
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
