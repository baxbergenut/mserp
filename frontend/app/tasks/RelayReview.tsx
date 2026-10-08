"use client";

import { useEffect, useState } from "react";
import { fetchDrivers, fetchRelayIdentityTasks, reviewRelayIdentity } from "../lib/api";
import type { Driver, RelayIdentityTask } from "../lib/types";
import { usePermissions } from "../lib/access";
import { ErrorBanner, Modal } from "../components/management/ManagementUI";
import { RelayTaskCard } from "./RelayTaskCard";

export function RelayReview({ id, onClose, onChanged }: { id: string; onClose: () => void; onChanged: () => void }) {
  const fleetRead = usePermissions().includes("fleet.read");
  const [task, setTask] = useState<RelayIdentityTask | null>(null);
  const [drivers, setDrivers] = useState<Driver[]>([]);
  const [decision, setDecision] = useState<{ driver: { id: string; name: string }; action: "link" | "reject" } | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    let active = true;
    Promise.all([fetchRelayIdentityTasks({ page: 1, pageSize: 1, search: id }), fleetRead ? fetchDrivers() : Promise.resolve([])])
      .then(([data, fleet]) => { if (active) { setTask(data.items[0] ?? null); setDrivers(fleet); if (!data.items.length) setError("This task is no longer available."); } })
      .catch(e => { if (active) setError(e instanceof Error ? e.message : "Could not load review"); });
    return () => { active = false; };
  }, [id, fleetRead, revision]);
  async function confirm() {
    if (!decision || saving) return;
    setSaving(true); setError("");
    try {
      await reviewRelayIdentity(id, decision.driver.id, decision.action);
      onChanged();
      if (decision.action === "link") onClose();
      else { setDecision(null); setRevision(value => value + 1); }
    } catch (e) { setError(e instanceof Error ? e.message : "Could not save review"); }
    finally { setSaving(false); }
  }
  return <Modal title="Review Relay account" onClose={onClose} isSaving={saving}
    submitLabel={decision ? decision.action === "link" ? "Link account" : "Not a match" : "Close"}
    onSubmit={event => { event.preventDefault(); if (decision) void confirm(); else onClose(); }}>
    {error && <ErrorBanner message={error} />}
    {!task && !error && <p role="status">Loading…</p>}
    {task && !decision && <RelayTaskCard task={task} drivers={drivers} disabled={saving} onDecision={(driver, action) => setDecision({ driver, action })} />}
    {decision && <div className="space-y-3 text-sm text-zinc-300">
      <p>{decision.action === "link" ? `Link this account to ${decision.driver.name}? Existing unassigned purchases and future purchases will use this driver. This can change driver settlements.` : `Dismiss the suggestion for ${decision.driver.name}?`}</p>
      <button type="button" className="ui-button" disabled={saving} onClick={() => setDecision(null)}>Back</button>
    </div>}
  </Modal>;
}
