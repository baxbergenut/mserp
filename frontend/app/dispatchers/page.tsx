"use client";

import Link from "next/link";
import { UpdaterPanel } from "./UpdaterPanel";
import { fetchUpdaters } from "../lib/api";
import { formatPhone } from "../lib/phone";

import { useViewState } from "@/app/lib/viewMemory";

import { useCallback, useEffect, useState } from "react";
import { Headset } from "lucide-react";
import {
  createDispatcher,
  deleteDispatcher,
  fetchDispatchersPage,
  fetchDrivers,
  updateDispatcher,
} from "../lib/api";
import type { Dispatcher, DispatcherInput, Driver, Updater } from "../lib/types";
import { useDebouncedValue } from "../lib/useDebouncedValue";
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
import {
  DispatcherForm,
  dispatcherToInput,
  emptyDispatcherInput,
} from "./DispatcherForm";

export default function DispatchersPage() {
  const [dispatchers, setDispatchers] = useState<Dispatcher[]>([]);
  const [updaters, setUpdaters] = useState<Updater[]>([]);
  const [drivers, setDrivers] = useState<Driver[]>([]);
  const [search, setSearch] = useViewState("page:search", "");
  const [page, setPage] = useViewState("page:page", 1);
  const [pageSize, setPageSize] = useViewState("page:pageSize", 25);
  const [total, setTotal] = useState(0);
  const [totalPages, setTotalPages] = useState(1);
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<Dispatcher | null | undefined>(undefined);
  const [pendingDelete, setPendingDelete] = useState<Dispatcher | null>(null);
  const [form, setForm] = useState<DispatcherInput>(emptyDispatcherInput);
  const debouncedSearch = useDebouncedValue(search);

  const loadData = useCallback(async () => {
    setIsLoading(true);
    try {
      const [dispatcherPage, updaterRows] = await Promise.all([fetchDispatchersPage({
        page, pageSize, search: debouncedSearch,
      }), fetchUpdaters()]);
      setUpdaters(updaterRows);
      setDispatchers(dispatcherPage.items);
      setPage(dispatcherPage.page);
      setTotal(dispatcherPage.total);
      setTotalPages(dispatcherPage.totalPages);
      setError("");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to load dispatchers");
    } finally {
      setIsLoading(false);
    }
  }, [setPage, debouncedSearch, page, pageSize]);

  useEffect(() => {
    const timeout = window.setTimeout(() => void loadData(), 0);
    return () => window.clearTimeout(timeout);
  }, [loadData]);

  const openCreate = async () => {
    setError("");
    try {
      const rows = await fetchDrivers();
      setDrivers(rows);
      setForm({ ...emptyDispatcherInput, driverIds: [] });
      setEditing(null);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to load driver options");
    }
  };

  const openEdit = async (dispatcher: Dispatcher) => {
    setError("");
    try {
      const rows = await fetchDrivers();
      setDrivers(rows);
      setForm(dispatcherToInput(dispatcher, rows));
      setEditing(dispatcher);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to load driver options");
    }
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
        title="Dispatchers and updaters"

        count={total}
        actionLabel="Add dispatcher"
        onAction={() => void openCreate()}
      />
      {error && <ErrorBanner message={error} />}
      <ManagementSearch value={search} onChange={(value) => { setSearch(value); setPage(1); }} placeholder="Search dispatchers…" />

      <TableShell>
        {isLoading ? <LoadingTable columns={8} /> : dispatchers.length === 0 ? (
          <EmptyState message={search ? "No dispatchers match your search." : "No dispatchers yet. Add your first dispatcher to get started."} />
        ) : (
          <table className="w-full min-w-[760px] text-left text-[13px]">
            <thead><tr className="border-b border-zinc-800/50 text-zinc-500">
              <th className="px-4 py-3 font-medium">Dispatcher</th>
              <th className="px-4 py-3 font-medium">Contact</th>
              <th className="px-4 py-3 font-medium">Main updater</th><th className="px-4 py-3 font-medium">After hours updater</th>
              <th className="px-4 py-3 font-medium">Commission</th>
              <th className="px-4 py-3 font-medium">Drivers</th>
              <th className="px-4 py-3 font-medium">Status</th>
              <th className="px-4 py-3 font-medium text-right">Actions</th>
            </tr></thead>
            <tbody>{dispatchers.map((dispatcher) => (
              <tr key={dispatcher.id} className="border-b border-zinc-900/70 text-zinc-300 transition last:border-0 hover:bg-zinc-800/15">
                <td className="px-4 py-3 font-medium text-zinc-200"><Link href={`/dispatchers/detail?id=${dispatcher.id}`}>{dispatcher.fullName}</Link>{dispatcher.extension == null ? "" : ` (${dispatcher.extension})`}</td>
                <td className="px-4 py-3"><div className="text-zinc-400">{formatPhone(dispatcher.phone) || "—"}</div><div className="mt-0.5 text-[11px] text-zinc-600">{dispatcher.email ?? "No email"}</div></td>
                {[dispatcher.mainUpdaterId, dispatcher.afterHoursUpdaterId].map((id, index) => { const updater = updaters.find(u => u.id === id); return <td key={index} className="px-4 py-3 text-zinc-400">{updater ? `${updater.fullName}${updater.extension == null ? "" : ` (${updater.extension})`}` : "—"}</td>; })}
                <td className="px-4 py-3 font-mono tabular-nums text-zinc-300">{dispatcher.payPercentage === null ? "—" : `${dispatcher.payPercentage.toFixed(2)}%`}</td>
                <td className="px-4 py-3"><span className="rounded-full bg-zinc-800/60 px-2 py-1 text-[11px] text-zinc-400">{dispatcher.driverCount} {dispatcher.driverCount === 1 ? "driver" : "drivers"}</span></td>
                <td className="px-4 py-3"><StatusBadge active={dispatcher.active} /></td>
                <td className="px-4 py-3"><RowActions onEdit={() => void openEdit(dispatcher)} onDelete={() => setPendingDelete(dispatcher)} /></td>
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

      {!isLoading && <UpdaterPanel updaters={updaters} onChanged={loadData} />}

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
            updaters={updaters}
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
