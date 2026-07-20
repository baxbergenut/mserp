"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Users } from "lucide-react";
import {
  createDriver,
  deleteDriver,
  fetchDispatchers,
  fetchDrivers,
  fetchTrucks,
  updateDriver,
} from "../lib/api";
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
  TableShell,
} from "../components/management/ManagementUI";
import { DriverForm, driverToInput, emptyDriverInput } from "./DriverForm";

export default function DriversPage() {
  const [drivers, setDrivers] = useState<Driver[]>([]);
  const [trucks, setTrucks] = useState<Truck[]>([]);
  const [dispatchers, setDispatchers] = useState<Dispatcher[]>([]);
  const [search, setSearch] = useState("");
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Driver | null | undefined>(undefined);
  const [pendingDelete, setPendingDelete] = useState<Driver | null>(null);
  const [form, setForm] = useState<DriverInput>(emptyDriverInput);

  const loadData = useCallback(async () => {
    try {
      const [driverRows, truckRows, dispatcherRows] = await Promise.all([
        fetchDrivers(),
        fetchTrucks(),
        fetchDispatchers(),
      ]);
      setDrivers(driverRows);
      setTrucks(truckRows);
      setDispatchers(dispatcherRows);
      setError("");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to load drivers");
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    Promise.all([fetchDrivers(), fetchTrucks(), fetchDispatchers()])
      .then(([driverRows, truckRows, dispatcherRows]) => {
        if (cancelled) return;
        setDrivers(driverRows);
        setTrucks(truckRows);
        setDispatchers(dispatcherRows);
        setError("");
      })
      .catch((reason: unknown) => {
        if (!cancelled) {
          setError(reason instanceof Error ? reason.message : "Failed to load drivers");
        }
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });
    return () => { cancelled = true; };
  }, []);

  const displayed = useMemo(() => {
    const term = search.trim().toLowerCase();
    if (!term) return drivers;
    return drivers.filter((driver) =>
      [
        driver.fullName,
        driver.email,
        driver.phone,
        driver.truckUnit,
        driver.dispatcherName,
        driver.licenseNumber,
      ].some((value) => value?.toLowerCase().includes(term)),
    );
  }, [drivers, search]);

  const openCreate = () => {
    setForm({ ...emptyDriverInput });
    setEditing(null);
    setError("");
  };

  const openEdit = (driver: Driver) => {
    setForm(driverToInput(driver));
    setEditing(driver);
    setError("");
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
        count={drivers.length}
        actionLabel="Add driver"
        onAction={openCreate}
      />

      {error && <ErrorBanner message={error} />}
      <ManagementSearch value={search} onChange={setSearch} placeholder="Search drivers…" />

      <TableShell>
        {isLoading ? (
          <LoadingTable columns={7} />
        ) : displayed.length === 0 ? (
          <EmptyState message={search ? "No drivers match your search." : "No drivers yet. Add your first driver to get started."} />
        ) : (
          <table className="w-full min-w-[920px] text-left text-[13px]">
            <thead>
              <tr className="border-b border-zinc-800/50 text-zinc-500">
                <th className="px-4 py-3 font-medium">Driver</th>
                <th className="px-4 py-3 font-medium">Type</th>
                <th className="px-4 py-3 font-medium">Compensation</th>
                <th className="px-4 py-3 font-medium">Dispatcher</th>
                <th className="px-4 py-3 font-medium">Truck</th>
                <th className="px-4 py-3 font-medium">Status</th>
                <th className="px-4 py-3 font-medium text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {displayed.map((driver) => (
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
                  <td className="px-4 py-3"><StatusBadge active={driver.active} /></td>
                  <td className="px-4 py-3"><RowActions onEdit={() => openEdit(driver)} onDelete={() => setPendingDelete(driver)} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </TableShell>

      {editing !== undefined && (
        <Modal
          title={editing ? `Edit ${editing.fullName}` : "Add driver"}
          description="Assignment changes are reflected on both the driver and truck records."
          isSaving={isSaving}
          submitLabel={editing ? "Save changes" : "Create driver"}
          onClose={() => setEditing(undefined)}
          onSubmit={(event) => { event.preventDefault(); void save(); }}
        >
          {error && <div className="mb-4"><ErrorBanner message={error} /></div>}
          <DriverForm value={form} onChange={setForm} dispatchers={dispatchers} trucks={trucks} />
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
