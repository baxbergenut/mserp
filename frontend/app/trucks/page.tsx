"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Truck as TruckIcon } from "lucide-react";
import {
  createTruck,
  deleteTruck,
  fetchDrivers,
  fetchTrucks,
  updateTruck,
} from "../lib/api";
import type { Driver, Truck, TruckInput, TruckStatus } from "../lib/types";
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
import { emptyTruckInput, TruckForm, truckToInput } from "./TruckForm";

const STATUS_LABELS: Record<TruckStatus, string> = {
  available: "Available",
  assigned: "Assigned",
  maintenance: "Maintenance",
  out_of_service: "Out of service",
};

const STATUS_CLASSES: Record<TruckStatus, string> = {
  available: "bg-emerald-500/10 text-emerald-400",
  assigned: "bg-blue-500/10 text-blue-400",
  maintenance: "bg-amber-500/10 text-amber-400",
  out_of_service: "bg-red-500/10 text-red-400",
};

export default function TrucksPage() {
  const [trucks, setTrucks] = useState<Truck[]>([]);
  const [drivers, setDrivers] = useState<Driver[]>([]);
  const [search, setSearch] = useState("");
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Truck | null | undefined>(undefined);
  const [pendingDelete, setPendingDelete] = useState<Truck | null>(null);
  const [form, setForm] = useState<TruckInput>(emptyTruckInput);

  const loadData = useCallback(async () => {
    try {
      const [truckRows, driverRows] = await Promise.all([fetchTrucks(), fetchDrivers()]);
      setTrucks(truckRows);
      setDrivers(driverRows);
      setError("");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to load trucks");
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    Promise.all([fetchTrucks(), fetchDrivers()])
      .then(([truckRows, driverRows]) => {
        if (cancelled) return;
        setTrucks(truckRows);
        setDrivers(driverRows);
        setError("");
      })
      .catch((reason: unknown) => {
        if (!cancelled) {
          setError(reason instanceof Error ? reason.message : "Failed to load trucks");
        }
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });
    return () => { cancelled = true; };
  }, []);

  const displayed = useMemo(() => {
    const term = search.trim().toLowerCase();
    if (!term) return trucks;
    return trucks.filter((truck) =>
      [truck.unitNumber, truck.vin, truck.make, truck.model, truck.licensePlate, truck.driverName]
        .some((value) => value?.toLowerCase().includes(term)),
    );
  }, [search, trucks]);

  const openCreate = () => {
    setForm({ ...emptyTruckInput });
    setEditing(null);
    setError("");
  };

  const openEdit = (truck: Truck) => {
    setForm(truckToInput(truck));
    setEditing(truck);
    setError("");
  };

  const save = async () => {
    setIsSaving(true);
    setError("");
    try {
      if (editing) await updateTruck(editing.id, form);
      else await createTruck(form);
      setEditing(undefined);
      await loadData();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to save truck");
    } finally {
      setIsSaving(false);
    }
  };

  const remove = async () => {
    if (!pendingDelete) return;
    setIsDeleting(true);
    setError("");
    try {
      await deleteTruck(pendingDelete.id);
      setPendingDelete(null);
      await loadData();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to delete truck");
      setPendingDelete(null);
    } finally {
      setIsDeleting(false);
    }
  };

  return (
    <div className="space-y-5 animate-fade-in">
      <ManagementHeader
        icon={TruckIcon}
        title="Trucks"
        description="Track equipment details, compliance dates, maintenance, and driver assignments."
        count={trucks.length}
        actionLabel="Add truck"
        onAction={openCreate}
      />
      {error && <ErrorBanner message={error} />}
      <ManagementSearch value={search} onChange={setSearch} placeholder="Search trucks…" />

      <TableShell>
        {isLoading ? <LoadingTable columns={7} /> : displayed.length === 0 ? (
          <EmptyState message={search ? "No trucks match your search." : "No trucks yet. Add your first truck to get started."} />
        ) : (
          <table className="w-full min-w-[980px] text-left text-[13px]">
            <thead><tr className="border-b border-zinc-800/50 text-zinc-500">
              <th className="px-4 py-3 font-medium">Unit</th>
              <th className="px-4 py-3 font-medium">Equipment</th>
              <th className="px-4 py-3 font-medium">Driver</th>
              <th className="px-4 py-3 font-medium">Mileage</th>
              <th className="px-4 py-3 font-medium">Operational status</th>
              <th className="px-4 py-3 font-medium">Record</th>
              <th className="px-4 py-3 font-medium text-right">Actions</th>
            </tr></thead>
            <tbody>{displayed.map((truck) => (
              <tr key={truck.id} className="border-b border-zinc-900/70 text-zinc-300 transition last:border-0 hover:bg-zinc-800/15">
                <td className="px-4 py-3"><div className="font-mono font-medium text-zinc-200">{truck.unitNumber}</div><div className="mt-0.5 text-[11px] text-zinc-600">{truck.isCompanyOwned ? "Company owned" : "Owner / leased"}</div></td>
                <td className="px-4 py-3"><div className="text-zinc-300">{[truck.year, truck.make, truck.model].filter(Boolean).join(" ") || "—"}</div><div className="mt-0.5 font-mono text-[11px] text-zinc-600">{truck.vin ?? truck.licensePlate ?? "No VIN or plate"}</div></td>
                <td className="px-4 py-3 text-zinc-400">{truck.driverName ?? "Unassigned"}</td>
                <td className="px-4 py-3 font-mono tabular-nums text-zinc-400">{truck.mileage?.toLocaleString() ?? "—"}</td>
                <td className="px-4 py-3"><span className={`rounded-full px-2 py-1 text-[11px] font-medium ${STATUS_CLASSES[truck.status]}`}>{STATUS_LABELS[truck.status]}</span></td>
                <td className="px-4 py-3"><StatusBadge active={truck.active} /></td>
                <td className="px-4 py-3"><RowActions onEdit={() => openEdit(truck)} onDelete={() => setPendingDelete(truck)} /></td>
              </tr>
            ))}</tbody>
          </table>
        )}
      </TableShell>

      {editing !== undefined && (
        <Modal
          title={editing ? `Edit truck ${editing.unitNumber}` : "Add truck"}
          description="Assignment changes automatically update the related driver record."
          isSaving={isSaving}
          submitLabel={editing ? "Save changes" : "Create truck"}
          onClose={() => setEditing(undefined)}
          onSubmit={(event) => { event.preventDefault(); void save(); }}
        >
          {error && <div className="mb-4"><ErrorBanner message={error} /></div>}
          <TruckForm value={form} onChange={setForm} drivers={drivers} />
        </Modal>
      )}

      {pendingDelete && (
        <ConfirmDialog
          title="Delete truck?"
          message={`This permanently deletes truck ${pendingDelete.unitNumber} and its assignment history. The driver record will remain.`}
          isDeleting={isDeleting}
          onCancel={() => setPendingDelete(null)}
          onConfirm={() => void remove()}
        />
      )}
    </div>
  );
}
