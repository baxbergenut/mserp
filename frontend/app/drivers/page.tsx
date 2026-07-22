"use client";

import { useCallback, useEffect, useState } from "react";
import { FileBadge, Users } from "lucide-react";
import {
  createDriver,
  deleteDriver,
  fileDownloadUrl,
  fetchDispatchers,
  fetchDriversPage,
  fetchTrucks,
  uploadCDLFile,
  updateDriver,
} from "../lib/api";
import { renderPDFPages } from "../lib/pdf";
import { useDebouncedValue } from "../lib/useDebouncedValue";
import type { Dispatcher, Driver, DriverInput, Truck } from "../lib/types";
import {
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
  const [drivers, setDrivers] = useState<Driver[]>([]);
  const [trucks, setTrucks] = useState<Truck[]>([]);
  const [dispatchers, setDispatchers] = useState<Dispatcher[]>([]);
  const [search, setSearch] = useState("");
  const [showInactiveDrivers, setShowInactiveDrivers] = useState(false);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(25);
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
  const debouncedSearch = useDebouncedValue(search);

  const loadData = useCallback(async () => {
    setIsLoading(true);
    try {
      const driverPage = await fetchDriversPage({
        page, pageSize, search: debouncedSearch, includeInactive: showInactiveDrivers,
      });
      setDrivers(driverPage.items);
      setPage(driverPage.page);
      setTotal(driverPage.total);
      setTotalPages(driverPage.totalPages);
      setError("");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to load drivers");
    } finally {
      setIsLoading(false);
    }
  }, [debouncedSearch, page, pageSize, showInactiveDrivers]);

  const loadLookups = async () => {
    try {
      const [truckRows, dispatcherRows] = await Promise.all([
        fetchTrucks(), fetchDispatchers(),
      ]);
      setTrucks(truckRows);
      setDispatchers(dispatcherRows);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to load assignment options");
    }
  };

  useEffect(() => {
    const timeout = window.setTimeout(() => void loadData(), 0);
    return () => window.clearTimeout(timeout);
  }, [loadData]);

  const openCreate = () => {
    void loadLookups();
    setForm({ ...emptyDriverInput });
    setCDLFileName(null);
    setEditing(null);
    setError("");
  };

  const openEdit = (driver: Driver) => {
    void loadLookups();
    setForm(driverToInput(driver));
    setCDLFileName(driver.cdlFileName);
    setEditing(driver);
    setError("");
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
      if (editing) await updateDriver(editing.id, form);
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
        description="Manage driver profiles, compensation, equipment, and dispatcher assignments."
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
                    <div className="font-medium text-zinc-200">{driver.fullName}</div>
                    <div className="mt-0.5 text-[11px] text-zinc-600">{driver.phone || driver.email || "No contact info"}</div>
                  </td>
                  <td className="px-4 py-3 text-zinc-400">{driver.isOwnerOperator ? "Owner-operator" : "Company"}</td>
                  <td className="px-4 py-3 font-mono tabular-nums text-zinc-300">
                    {driver.payType === "cpm" ? `$${driver.payRate.toFixed(4)}/mi` : `${driver.payRate.toFixed(2)}% gross`}
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
                  <td className="px-4 py-3"><StatusBadge active={driver.active} /></td>
                  <td className="px-4 py-3"><RowActions onEdit={() => openEdit(driver)} onDelete={() => setPendingDelete(driver)} /></td>
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
          title={editing ? `Edit ${editing.fullName}` : "Add driver"}
          description="Assignment changes are reflected on both the driver and truck records."
          isSaving={isSaving || isUploadingCDL}
          submitLabel={editing ? "Save changes" : "Create driver"}
          onClose={() => setEditing(undefined)}
          onSubmit={(event) => { event.preventDefault(); void save(); }}
        >
          {error && <div className="mb-4"><ErrorBanner message={error} /></div>}
          <DriverForm
            value={form}
            onChange={setForm}
            dispatchers={dispatchers}
            trucks={trucks}
            cdlFileName={cdlFileName}
            isUploadingCDL={isUploadingCDL}
            onUploadCDL={uploadCDL}
            onRemoveCDL={removeCDL}
          />
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
