"use client";

import { useViewState } from "@/app/lib/viewMemory";

import { useEffect, useRef, useState } from "react";
import { Check, Pencil, RotateCcw, Trash2 } from "lucide-react";
import { createCustomTask, deleteCustomTask, fetchCustomTasks, fetchTaskUsers, setCustomTaskCompleted, updateCustomTask } from "../lib/api";
import type { CustomTask, CustomTaskInput, PaginatedResponse, TaskUser } from "../lib/types";
import { ConfirmDialog, controlClass, ErrorBanner, Modal, TablePagination } from "../components/management/ManagementUI";

export function CustomTasks({ search, revision, creating, onCloseCreate, onCount }: {
  search: string; revision: number; creating: boolean; onCloseCreate: () => void; onCount: (count: number) => void;
}) {
  const [data, setData] = useState<PaginatedResponse<CustomTask> | null>(null);
  const [users, setUsers] = useState<TaskUser[]>([]);
  const [status, setStatus] = useViewState<"open" | "completed" | "all">("CustomTasks:status", "open");
  const [position, setPosition] = useViewState("CustomTasks:position", { search, page: 1 });
  const page = position.search === search ? position.page : 1;
  const setPage = (nextPage: number) => setPosition({ search, page: nextPage });
  const [pageSize, setPageSize] = useViewState("CustomTasks:pageSize", 25);
  const [refresh, setRefresh] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [editing, setEditing] = useState<CustomTask | null>(null);
  const [deleting, setDeleting] = useState<CustomTask | null>(null);
  const [saving, setSaving] = useState(false);
  const mutation = useRef(false);

  useEffect(() => {
    let active = true;
    fetchTaskUsers().then(value => { if (active) setUsers(value); }).catch(e => { if (active) setError(e instanceof Error ? e.message : "Could not load assignees"); });
    return () => { active = false; };
  }, [revision]);

  useEffect(() => {
    const refreshVisible = () => {
      if (document.visibilityState === "visible" && !mutation.current && !creating && !editing && !deleting) {
        setRefresh((value) => value + 1);
      }
    };
    const timer = window.setInterval(refreshVisible, 15000);
    window.addEventListener("focus", refreshVisible);
    return () => { window.clearInterval(timer); window.removeEventListener("focus", refreshVisible); };
  }, [creating, editing, deleting]);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      setLoading(true);
      setError("");
      try {
        const result = await fetchCustomTasks({ page, pageSize, search, status });
        if (!cancelled) { setData(result); onCount(result.total); }
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : "Could not load custom tasks");
      } finally { if (!cancelled) setLoading(false); }
    };
    void Promise.resolve().then(() => { if (!cancelled) return load(); });
    return () => { cancelled = true; };
  }, [page, pageSize, search, status, revision, refresh, onCount]);

  async function mutate(action: () => Promise<unknown>, message: string) {
    if (mutation.current) return;
    mutation.current = true;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      await action();
      if (creating) { setStatus("open"); setPage(1); }
      setEditing(null);
      setDeleting(null);
      onCloseCreate();
      setNotice(message);
      setRefresh((value) => value + 1);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not save task");
      throw e;
    } finally { mutation.current = false; setSaving(false); }
  }

  const actionClass = "inline-flex items-center gap-1.5 rounded-lg border border-zinc-700 px-3 py-2 text-xs text-zinc-300 hover:bg-zinc-800 disabled:opacity-50";
  return (
    <section aria-label="Custom tasks" className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold text-zinc-200">Custom tasks</h2>
          <p className="mt-1 text-xs text-zinc-500">Assigned tasks are visible to the assignee, assigning user and Administrators.</p>
        </div>
        <select aria-label="Custom task status" value={status} className={`${controlClass} sm:w-48`}
          onChange={(event) => { setStatus(event.target.value as typeof status); setPage(1); }}>
          <option value="open">Open</option><option value="completed">Completed</option><option value="all">All custom tasks</option>
        </select>
      </div>
      {error && !creating && !editing && <ErrorBanner message={error} />}
      {notice && <p role="status" className="text-sm text-emerald-300">{notice}</p>}
      {loading ? <p role="status" className="py-5 text-sm text-zinc-400">Loading custom tasks…</p>
        : !error && data?.items.length === 0 ? <p className="py-5 text-sm text-zinc-500">{search ? "No matching custom tasks." : status === "open" ? "No open custom tasks. Use Add task to create one." : "No custom tasks in this view."}</p>
        : data?.items.map((task) => (
          <article key={task.id} className="rounded-xl border border-zinc-800 bg-card p-4 sm:p-5">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="min-w-0 flex-1">
                <h3 className={`break-words text-sm font-semibold ${task.completedAt ? "text-zinc-500 line-through" : "text-zinc-100"}`}>{task.title}</h3>
                <p className="mt-1 text-xs text-zinc-500">{task.completedAt ? "Completed" : "Open"} · Created {new Date(task.createdAt).toLocaleDateString()}</p>
                {!task.systemTaskKind && <p className="mt-1 text-xs text-zinc-400">{task.assignedTo ? `Assigned to ${task.assigneeName || "Unavailable user"}` : "Unassigned · Shared with the team"}</p>}
              </div>
              <div className="flex flex-wrap gap-2">
                <button disabled={saving} className={actionClass} onClick={() => { void mutate(() => setCustomTaskCompleted(task.id, !task.completedAt), task.completedAt ? "Task reopened." : "Task completed.").catch(() => {}); }}>
                  {task.completedAt ? <RotateCcw className="h-3.5 w-3.5" /> : <Check className="h-3.5 w-3.5" />}{task.completedAt ? "Reopen" : "Complete"}
                </button>
                <button disabled={saving} className={actionClass} aria-label={`Edit ${task.title}`} onClick={() => { setError(""); setEditing(task); }}><Pencil className="h-3.5 w-3.5" />Edit</button>
                <button disabled={saving} className={actionClass} aria-label={`Delete ${task.title}`} onClick={() => { setError(""); setDeleting(task); }}><Trash2 className="h-3.5 w-3.5" />Delete</button>
              </div>
            </div>
            {task.notes && <p className="mt-3 whitespace-pre-wrap break-words text-sm text-zinc-400">{task.notes}</p>}
          </article>
        ))}
      {!loading && data && data.total > 0 && <TablePagination page={data.page} pageSize={data.pageSize} totalItems={data.total} totalPages={data.totalPages}
        onPageChange={setPage} onPageSizeChange={(size) => { setPageSize(size); setPage(1); }} />}
      {(creating || editing) && <TaskForm key={editing?.id ?? "new"} task={editing} saving={saving}
        users={users}
        onClose={() => { onCloseCreate(); setEditing(null); setError(""); }}
        onSave={(input) => mutate(() => editing ? updateCustomTask(editing.id, input) : createCustomTask(input), editing ? "Task updated." : "Task created.")} />}
      {deleting && <ConfirmDialog title="Delete custom task" message={`Delete “${deleting.title}”? This cannot be undone.`} isDeleting={saving}
        onCancel={() => setDeleting(null)} onConfirm={() => { void mutate(() => deleteCustomTask(deleting.id), "Task deleted.").catch(() => setDeleting(null)); }} />}
    </section>
  );
}

function TaskForm({ task, saving, users, onClose, onSave }: {
  task: CustomTask | null; saving: boolean; onClose: () => void; onSave: (input: CustomTaskInput) => Promise<void>;
  users: TaskUser[];
}) {
  const [title, setTitle] = useState(task?.title ?? "");
  const [notes, setNotes] = useState(task?.notes ?? "");
  const [assignedTo, setAssignedTo] = useState(task?.assignedTo ?? "");
  const [error, setError] = useState("");
  return <Modal title={task ? "Edit task" : "Add custom task"} description={task?.systemTaskKind ? "System task assignments are managed in Settings." : "Unassigned tasks are shared. Assigned tasks are private to the assignee, assigning user and Administrators."}
    isSaving={saving} submitLabel={task ? "Save changes" : "Add task"} onClose={onClose}
    onSubmit={(event) => {
      event.preventDefault();
      if (saving) return;
      if (!title.trim()) { setError("Enter a task title."); return; }
      setError("");
      void onSave({ title: title.trim(), notes: notes.trim(), ...(task?.systemTaskKind ? {} : { assignedTo }) }).catch((e) => setError(e instanceof Error ? e.message : "Could not save task"));
    }}>
    <div className="space-y-4">
      {error && <ErrorBanner message={error} />}
      {!task?.systemTaskKind && <label className="block text-sm text-zinc-300">Assign to
        <select aria-label="Assign to" disabled={saving} value={assignedTo} onChange={e => setAssignedTo(e.target.value)} className={`${controlClass} mt-2`}>
          <option value="">Unassigned · Shared with the team</option>
          {assignedTo && !users.some(u => u.id === assignedTo) && <option value={assignedTo}>{task?.assigneeName || "Unavailable user"} (disabled)</option>}
          {users.map(u => <option key={u.id} value={u.id}>{u.name}</option>)}
        </select>
      </label>}
      <label className="block text-sm text-zinc-300">Title
        <input autoFocus required maxLength={200} disabled={saving} value={title} onChange={(e) => setTitle(e.target.value)} className={`${controlClass} mt-2`} placeholder="What needs to be done?" />
      </label>
      <label className="block text-sm text-zinc-300">Notes <span className="text-zinc-500">(optional)</span>
        <textarea maxLength={5000} rows={5} disabled={saving} value={notes} onChange={(e) => setNotes(e.target.value)} className={`${controlClass} mt-2`} placeholder="Add details or instructions…" />
      </label>
    </div>
  </Modal>;
}
