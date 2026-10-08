"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { Check, Columns3, List, ListChecks, LockKeyhole, Pencil, Play, RefreshCw, RotateCcw, Trash2 } from "lucide-react";
import { useViewState } from "../lib/viewMemory";
import { useQuickCreate } from "../lib/topNavigation";
import { usePermissions } from "../lib/access";
import { useDebouncedValue } from "../lib/useDebouncedValue";
import { confirmOffboarding, createCustomTask, deleteCustomTask, fetchTasks, fetchTaskUsers, setTaskStatus, updateCustomTask } from "../lib/api";
import type { CustomTask, TaskStatus, PaginatedResponse, TaskUser } from "../lib/types";
import { ConfirmDialog, controlClass, ErrorBanner, ManagementHeader, ManagementSearch, Modal, TablePagination, TableShell } from "../components/management/ManagementUI";
import { useTaskUpdates } from "../components/TaskUpdatesProvider";
import { TaskForm } from "./TaskForm";
import { RelayReview } from "./RelayReview";

type TaskPage = PaginatedResponse<CustomTask>;
type Status = TaskStatus | "all";
const statusLabels = { open: "Open", in_process: "In process", completed: "Completed" };
const taskStatus = (task: CustomTask): TaskStatus => task.status ?? (task.completedAt ? "completed" : "open");
const dateTime = (value: string) => new Date(value).toLocaleString("en-US", { timeZone: "America/New_York", month: "short", day: "numeric", year: "numeric", hour: "numeric", minute: "2-digit" });

function taskMetadata(task: CustomTask) {
  return [
    'Assigned by ' + (task.systemTaskKind ? 'System' : task.assignerName || 'Unknown'),
    'Created ' + dateTime(task.createdAt) + ' NY',
    ...(task.completedAt ? ['Completed by ' + (task.completedByName || 'Unknown'), dateTime(task.completedAt) + ' NY'] : []),
  ].join('\n');
}

export default function TasksPage() {
  const permissions = usePermissions();
  const canWrite = permissions.includes("tasks.write");
  const { revision, refresh } = useTaskUpdates();
  const [view, setView] = useViewState<"list" | "kanban">("tasks:view", "list");
  const [status, setStatus] = useViewState<Status>("tasks:status", "open");
  const [search, setSearch] = useViewState("tasks:search", "");
  const query = useDebouncedValue(search, 250);
  const [page, setPage] = useViewState("tasks:page", 1);
  const [pageSize, setPageSize] = useViewState("tasks:pageSize", 25);
  const [openPage, setOpenPage] = useState(1);
  const [processPage, setProcessPage] = useState(1);
  const [completedPage, setCompletedPage] = useState(1);
  const [data, setData] = useState<TaskPage | null>(null);
  const [columns, setColumns] = useState<Record<TaskStatus, TaskPage> | null>(null);
  const [users, setUsers] = useState<TaskUser[]>([]);
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<CustomTask | null>(null);
  const [deleting, setDeleting] = useState<CustomTask | null>(null);
  const [details, setDetails] = useState<CustomTask | null>(null);
  const [relay, setRelay] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const mutation = useRef(false);
  const dragId = useRef<string | null>(null);
  const [dropTarget, setDropTarget] = useState<Status | null>(null);
  useQuickCreate(() => { if (canWrite) setCreating(true); });
  useEffect(() => {
    let active = true;
    if (canWrite) fetchTaskUsers().then(value => { if (active) setUsers(value); }).catch(e => { if (active) setError(e instanceof Error ? e.message : "Could not load assignees"); });
    return () => { active = false; };
  }, [canWrite]);
  useEffect(() => {
    let active = true;
    async function load() {
      setLoading(true);
      try {
        if (view === "kanban") {
          const [open, inProcess, completed] = await Promise.all([fetchTasks({ page: openPage, pageSize, search: query, status: "open" }), fetchTasks({ page: processPage, pageSize, search: query, status: "in_process" }), fetchTasks({ page: completedPage, pageSize, search: query, status: "completed" })]);
          if (active) { setColumns({ open, in_process: inProcess, completed }); setError(""); }
        } else {
          const result = await fetchTasks({ page, pageSize, search: query, status });
          if (active) { setData(result); setError(""); }
        }
      } catch (e) { if (active) { setError(e instanceof Error ? e.message : "Could not load tasks"); setData(null); setColumns(null); } }
      finally { if (active) setLoading(false); }
    }
    void load();
    return () => { active = false; };
  }, [view, page, pageSize, openPage, processPage, completedPage, query, status, revision]);
  async function mutate(action: () => Promise<unknown>, message: string) {
    if (mutation.current) return;
    mutation.current = true; setSaving(true); setError("");
    try { await action(); if (creating) { setStatus("open"); setPage(1); setOpenPage(1); } setCreating(false); setEditing(null); setDeleting(null); setDetails(null); setNotice(message); refresh(); }
    catch (e) { setError(e instanceof Error ? e.message : "Could not save task"); throw e; }
    finally { mutation.current = false; setSaving(false); }
  }
  const move = (task: CustomTask, nextStatus: TaskStatus) => {
    if (!canWrite || task.systemTaskKind || taskStatus(task) === nextStatus) return;
    void mutate(() => setTaskStatus(task.id, nextStatus), `Task moved to ${statusLabels[nextStatus].toLowerCase()}.`).catch(() => {});
  };
  const actions = (task: CustomTask) => <div className="flex shrink-0 items-center justify-end gap-1">
    {!task.systemTaskKind && canWrite && <>
      {!task.completedAt && <button type="button" className="ui-button" disabled={saving} aria-label={`${taskStatus(task) === "in_process" ? "Move to open" : "Start"} ${task.title}`} onClick={() => move(task, taskStatus(task) === "in_process" ? "open" : "in_process")}>{taskStatus(task) === "in_process" ? <RotateCcw className="h-4 w-4" /> : <Play className="h-4 w-4" />}</button>}
      <button type="button" className="ui-button" disabled={saving} aria-label={`${task.completedAt ? "Reopen" : "Complete"} ${task.title}`} onClick={() => move(task, task.completedAt ? "open" : "completed")}>{task.completedAt ? <RotateCcw className="h-4 w-4" /> : <Check className="h-4 w-4" />}</button>
      <button type="button" className="ui-button" disabled={saving} aria-label={`Edit ${task.title}`} onClick={() => setEditing(task)}><Pencil className="h-4 w-4" /></button>
      <button type="button" className="ui-button" disabled={saving} aria-label={`Delete ${task.title}`} onClick={() => setDeleting(task)}><Trash2 className="h-4 w-4" /></button>
    </>}
    {task.systemTaskKind && <LockKeyhole aria-label="Automatic status" className="h-4 w-4 text-zinc-500" />}
  </div>;
  const title = (task: CustomTask) => <button type="button" title={taskMetadata(task)} className="min-w-0 break-words text-left font-medium text-zinc-100 hover:text-accent" onClick={() => { setError(""); setDetails(task); }}>{task.title}</button>;
  const pagination = (result: TaskPage, change: (page: number) => void) => result.total > 0 && <TablePagination page={result.page} pageSize={result.pageSize} totalItems={result.total} totalPages={result.totalPages} onPageChange={change} onPageSizeChange={size => { setPageSize(size); setPage(1); setOpenPage(1); setProcessPage(1); setCompletedPage(1); }} />;
  return <div className="animate-fade-in space-y-4">
    <ManagementHeader icon={ListChecks} title="Tasks" count={view === "list" ? data?.total : columns ? columns.open.total + columns.in_process.total + columns.completed.total : undefined} actionLabel={canWrite ? "Add task" : undefined} onAction={() => setCreating(true)} />
    <div className="flex flex-wrap items-center gap-3">
      <ManagementSearch value={search} onChange={value => { setSearch(value); setPage(1); setOpenPage(1); setProcessPage(1); setCompletedPage(1); }} placeholder="Search tasks…" />
      {view === "list" && <select aria-label="Task status" className={`${controlClass} sm:w-40`} value={status} onChange={event => { setStatus(event.target.value as Status); setPage(1); }}><option value="open">Open</option><option value="in_process">In process</option><option value="completed">Completed</option><option value="all">All tasks</option></select>}
      <div className="ml-auto flex items-center gap-2">
        <button type="button" className="ui-button" aria-label="Refresh tasks" onClick={refresh}><RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} /></button>
        <div className="flex gap-1" role="group" aria-label="Task view">
          <button type="button" className={view === "list" ? "ui-button ui-button-primary" : "ui-button"} aria-pressed={view === "list"} onClick={() => setView("list")}><List className="h-4 w-4" />List</button>
          <button type="button" className={view === "kanban" ? "ui-button ui-button-primary" : "ui-button"} aria-pressed={view === "kanban"} onClick={() => setView("kanban")}><Columns3 className="h-4 w-4" />Kanban</button>
        </div>
      </div>
    </div>
    {error && <ErrorBanner message={error} />}
    {notice && <p role="status" className="text-xs text-zinc-400">{notice}</p>}
    {loading && !data && !columns && <p role="status" className="py-8 text-center text-zinc-500">Loading…</p>}
    {view === "list" && data && <>
      <TableShell><table className="w-full min-w-[560px] text-left text-[13px]">
        <thead className="bg-zinc-900 text-xs text-zinc-500"><tr>{["Task", "Status", "Assignee", ""].map((label, i) => <th key={i} className="px-3 py-2 font-medium">{label}</th>)}</tr></thead>
        <tbody className="divide-y divide-zinc-800">{data.items.map(task => <tr key={task.id} className="bg-card hover:bg-zinc-800/20">
          <td className="max-w-sm px-3 py-2">{title(task)}</td><td className="whitespace-nowrap px-3 py-2 text-zinc-400">{statusLabels[taskStatus(task)]}</td>
          <td className="px-3 py-2 text-zinc-400">{task.assigneeName || "Unassigned"}</td>
          <td className="px-3 py-1">{actions(task)}</td>
        </tr>)}</tbody>
      </table>{data.items.length === 0 && <p className="p-6 text-center text-sm text-zinc-500">No tasks</p>}</TableShell>{pagination(data, setPage)}
    </>}
    {view === "kanban" && columns && <div className="grid items-start gap-4 xl:grid-cols-3">
      {(["open", "in_process", "completed"] as const).map(column => <section key={column} aria-label={`${statusLabels[column]} tasks`} className={`min-w-0 rounded-lg border p-4 ${dropTarget === column ? "border-accent bg-accent/5" : "border-zinc-800 bg-zinc-900/30"}`}
        onDragOver={event => { if (dragId.current && canWrite && !saving) { event.preventDefault(); event.dataTransfer.dropEffect = "move"; setDropTarget(column); } }}
        onDragLeave={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDropTarget(null); }}
        onDrop={event => { event.preventDefault(); const task = [...columns.open.items, ...columns.in_process.items, ...columns.completed.items].find(task => task.id === dragId.current); dragId.current = null; setDropTarget(null); if (task) move(task, column); }}>
        <h2 className="mb-3 flex items-center justify-between text-sm font-semibold text-zinc-200">{statusLabels[column]}<span className="text-xs tabular-nums text-zinc-500">{columns[column].total}</span></h2>
        <div className="min-h-24 space-y-3">{columns[column].items.map(task => <article key={task.id} aria-label={task.title} draggable={canWrite && !task.systemTaskKind && !saving}
          onDragStart={event => { if (!canWrite || task.systemTaskKind || saving) { event.preventDefault(); return; } dragId.current = task.id; event.dataTransfer.effectAllowed = "move"; event.dataTransfer.setData("text/plain", task.id); }} onDragEnd={() => { dragId.current = null; setDropTarget(null); }}
          className={`rounded-lg border border-zinc-800 bg-card p-4 ${canWrite && !task.systemTaskKind ? "cursor-grab active:cursor-grabbing" : ""}`}>
          <div>{title(task)}</div>
          {task.notes && <p className="mt-2 line-clamp-2 whitespace-pre-wrap break-words text-xs text-zinc-400">{task.notes}</p>}
          <div className="mt-3 flex flex-wrap items-center justify-between gap-2"><p className="text-xs font-medium text-zinc-300" aria-label={`Assignee: ${task.assigneeName || "Unassigned"}`}>{task.assigneeName || "Unassigned"}</p>{actions(task)}</div>
        </article>)}</div>{columns[column].total > pageSize && pagination(columns[column], column === "open" ? setOpenPage : column === "in_process" ? setProcessPage : setCompletedPage)}
      </section>)}
    </div>}
    {(creating || editing) && <TaskForm key={editing?.id ?? "new"} task={editing} saving={saving} users={users} onClose={() => { setCreating(false); setEditing(null); }} onSave={input => mutate(() => editing ? updateCustomTask(editing.id, input) : createCustomTask(input), editing ? "Task updated." : "Task created.")} />}
    {deleting && <ConfirmDialog title="Delete task" message={`Delete “${deleting.title}”? This cannot be undone.`} isDeleting={saving} onCancel={() => setDeleting(null)} onConfirm={() => { void mutate(() => deleteCustomTask(deleting.id), "Task deleted.").catch(() => {}); }} />}
    {details && <TaskDetails task={details} saving={saving} canWrite={canWrite} canSetup={permissions.includes("fleet.write")} onClose={() => setDetails(null)} onRelay={() => { setRelay(details.id); setDetails(null); }} onConfirm={checklist => mutate(() => confirmOffboarding(details.id, checklist), "Offboarding completed.")} />}
    {relay && <RelayReview id={relay} onClose={() => setRelay(null)} onChanged={refresh} />}
  </div>;
}

function TaskDetails({ task, saving, canWrite, canSetup, onClose, onRelay, onConfirm }: {
  task: CustomTask; saving: boolean; canWrite: boolean; canSetup: boolean; onClose: () => void; onRelay: () => void;
  onConfirm: (checklist: { equipment: boolean; access: boolean; settlement: boolean }) => Promise<void>;
}) {
  const [checklist, setChecklist] = useState({ equipment: false, access: false, settlement: false });
  const [error, setError] = useState("");
  const offboarding = task.systemTaskKind === "driver_offboarding" && !task.completedAt && canWrite;
  return <Modal title={task.title} isSaving={saving} onClose={onClose} submitLabel={offboarding ? "Confirm offboarding" : "Close"} onSubmit={event => {
    event.preventDefault(); if (!offboarding) { onClose(); return; }
    if (!Object.values(checklist).every(Boolean)) { setError("Confirm each checklist item."); return; }
    void onConfirm(checklist).catch(e => setError(e instanceof Error ? e.message : "Could not save offboarding"));
  }}><div className="space-y-4 text-sm text-zinc-300">
    {error && <ErrorBanner message={error} />}
    <div className="grid gap-1 text-xs text-zinc-400"><p>Assigned by {task.systemTaskKind ? "System" : task.assignerName || "Unknown"}</p><p>Assigned to {task.assigneeName || "Unassigned"}</p>{task.completedAt && <p>Completed by {task.completedByName || "Unknown"} · {dateTime(task.completedAt)} NY</p>}</div>
    {task.notes && <p className="whitespace-pre-wrap break-words">{task.notes}</p>}{task.outcome && <p>{task.outcome}</p>}
    {!task.completedAt && task.systemTaskKind === "driver_onboarding" && canSetup && <Link className="ui-button ui-button-primary" href={`/drivers?setup=${task.id}`}>Set up driver</Link>}
    {!task.completedAt && task.systemTaskKind === "relay_review" && canWrite && <button type="button" className="ui-button ui-button-primary" onClick={onRelay}>Review account</button>}
    {offboarding && <div className="space-y-3">{([["equipment", "Equipment and documents collected"], ["access", "Fuel/toll cards and external access closed"], ["settlement", "Driver status, charges and final settlement reviewed"]] as const).map(([key, label]) => <label key={key} className="flex items-center gap-2"><input type="checkbox" checked={checklist[key]} disabled={saving} onChange={event => setChecklist(value => ({ ...value, [key]: event.target.checked }))} />{label}</label>)}</div>}
  </div></Modal>;
}
