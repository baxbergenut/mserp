"use client";
import { useState } from "react";
import type { CustomTask, CustomTaskInput, TaskUser } from "../lib/types";
import { controlClass, ErrorBanner, Modal } from "../components/management/ManagementUI";

export function TaskForm({ task, saving, users, onClose, onSave }: {
  task: CustomTask | null; saving: boolean; onClose: () => void; onSave: (input: CustomTaskInput) => Promise<void>;
  users: TaskUser[];
}) {
  const [title, setTitle] = useState(task?.title ?? "");
  const [notes, setNotes] = useState(task?.notes ?? "");
  const [assignedTo, setAssignedTo] = useState(task?.assignedTo ?? "");
  const [error, setError] = useState("");
  return <Modal title={task ? "Edit task" : "Add task"}
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
          <option value="">Unassigned</option>
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
