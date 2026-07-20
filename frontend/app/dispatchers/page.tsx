"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Headset } from "lucide-react";
import {
  createDispatcher,
  deleteDispatcher,
  fetchDispatchers,
  fetchDrivers,
  updateDispatcher,
} from "../lib/api";
import type { Dispatcher, DispatcherInput, Driver } from "../lib/types";
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
import {
  DispatcherForm,
  dispatcherToInput,
  emptyDispatcherInput,
} from "./DispatcherForm";

export default function DispatchersPage() {
  const [dispatchers, setDispatchers] = useState<Dispatcher[]>([]);
  const [drivers, setDrivers] = useState<Driver[]>([]);
  const [search, setSearch] = useState("");
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Dispatcher | null | undefined>(undefined);
  const [pendingDelete, setPendingDelete] = useState<Dispatcher | null>(null);
  const [form, setForm] = useState<DispatcherInput>(emptyDispatcherInput);

  const loadData = useCallback(async () => {
    try {
      const [dispatcherRows, driverRows] = await Promise.all([
        fetchDispatchers(),
        fetchDrivers(),
      ]);
      setDispatchers(dispatcherRows);
      setDrivers(driverRows);
      setError("");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to load dispatchers");
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    Promise.all([fetchDispatchers(), fetchDrivers()])
      .then(([dispatcherRows, driverRows]) => {
        if (cancelled) return;
        setDispatchers(dispatcherRows);
        setDrivers(driverRows);
        setError("");
      })
      .catch((reason: unknown) => {
        if (!cancelled) {
          setError(reason instanceof Error ? reason.message : "Failed to load dispatchers");
        }
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false);
      });
    return () => { cancelled = true; };
  }, []);

  const displayed = useMemo(() => {
    const term = search.trim().toLowerCase();
    if (!term) return dispatchers;
    return dispatchers.filter((dispatcher) =>
      [dispatcher.fullName, dispatcher.email, dispatcher.phone]
        .some((value) => value?.toLowerCase().includes(term)),
    );
  }, [dispatchers, search]);

  const openCreate = () => {
    setForm({ ...emptyDispatcherInput, driverIds: [] });
    setEditing(null);
    setError("");
  };

  const openEdit = (dispatcher: Dispatcher) => {
    setForm(dispatcherToInput(dispatcher, drivers));
    setEditing(dispatcher);
    setError("");
  };

  const save = async () => {
    setIsSaving(true);
    setError("");
    try {
      if (editing) await updateDispatcher(editing.id, form);
      else await createDispatcher(form);
      setEditing(undefined);
      await loadData();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to save dispatcher");
    } finally {
      setIsSaving(false);
    }
  };

  const remove = async () => {
    if (!pendingDelete) return;
    setIsDeleting(true);
    setError("");
    try {
      await deleteDispatcher(pendingDelete.id);
      setPendingDelete(null);
      await loadData();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to delete dispatcher");
      setPendingDelete(null);
    } finally {
      setIsDeleting(false);
    }
  };

  return (
    <div className="space-y-5 animate-fade-in">
      <ManagementHeader
        icon={Headset}
        title="Dispatchers"
        description="Manage dispatcher profiles, commissions, and driver rosters."
        count={dispatchers.length}
        actionLabel="Add dispatcher"
        onAction={openCreate}
      />
      {error && <ErrorBanner message={error} />}
      <ManagementSearch value={search} onChange={setSearch} placeholder="Search dispatchers…" />

      <TableShell>
        {isLoading ? <LoadingTable columns={6} /> : displayed.length === 0 ? (
          <EmptyState message={search ? "No dispatchers match your search." : "No dispatchers yet. Add your first dispatcher to get started."} />
        ) : (
          <table className="w-full min-w-[760px] text-left text-[13px]">
            <thead><tr className="border-b border-zinc-800/50 text-zinc-500">
              <th className="px-4 py-3 font-medium">Dispatcher</th>
              <th className="px-4 py-3 font-medium">Contact</th>
              <th className="px-4 py-3 font-medium">Commission</th>
              <th className="px-4 py-3 font-medium">Drivers</th>
              <th className="px-4 py-3 font-medium">Status</th>
              <th className="px-4 py-3 font-medium text-right">Actions</th>
            </tr></thead>
            <tbody>{displayed.map((dispatcher) => (
              <tr key={dispatcher.id} className="border-b border-zinc-900/70 text-zinc-300 transition last:border-0 hover:bg-zinc-800/15">
                <td className="px-4 py-3 font-medium text-zinc-200">{dispatcher.fullName}</td>
                <td className="px-4 py-3"><div className="text-zinc-400">{dispatcher.phone ?? "—"}</div><div className="mt-0.5 text-[11px] text-zinc-600">{dispatcher.email ?? "No email"}</div></td>
                <td className="px-4 py-3 font-mono tabular-nums text-zinc-300">{dispatcher.payPercentage === null ? "—" : `${dispatcher.payPercentage.toFixed(2)}%`}</td>
                <td className="px-4 py-3"><span className="rounded-full bg-zinc-800/60 px-2 py-1 text-[11px] text-zinc-400">{dispatcher.driverCount} {dispatcher.driverCount === 1 ? "driver" : "drivers"}</span></td>
                <td className="px-4 py-3"><StatusBadge active={dispatcher.active} /></td>
                <td className="px-4 py-3"><RowActions onEdit={() => openEdit(dispatcher)} onDelete={() => setPendingDelete(dispatcher)} /></td>
              </tr>
            ))}</tbody>
          </table>
        )}
      </TableShell>

      {editing !== undefined && (
        <Modal
          title={editing ? `Edit ${editing.fullName}` : "Add dispatcher"}
          description="Drivers selected here are reassigned from their current dispatcher when saved."
          isSaving={isSaving}
          submitLabel={editing ? "Save changes" : "Create dispatcher"}
          onClose={() => setEditing(undefined)}
          onSubmit={(event) => { event.preventDefault(); void save(); }}
        >
          {error && <div className="mb-4"><ErrorBanner message={error} /></div>}
          <DispatcherForm
            value={form}
            onChange={setForm}
            drivers={drivers}
            dispatcherId={editing?.id ?? null}
          />
        </Modal>
      )}

      {pendingDelete && (
        <ConfirmDialog
          title="Delete dispatcher?"
          message={`This permanently deletes ${pendingDelete.fullName}. Their ${pendingDelete.driverCount} assigned driver(s) will become unassigned.`}
          isDeleting={isDeleting}
          onCancel={() => setPendingDelete(null)}
          onConfirm={() => void remove()}
        />
      )}
    </div>
  );
}
