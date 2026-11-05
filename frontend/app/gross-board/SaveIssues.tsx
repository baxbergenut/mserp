import type { ReactNode } from "react";
import type { GrossBoardDriver, GrossBoardSaveIssue } from "@/app/lib/types";
import { amountDisplay } from "./board";
import { grossLocationKey, type GrossDrafts } from "./drafts";

const fieldLabels: Record<string, string> = {
  loadNumber: "Load number", loadRecordId: "Load number", loadTripId: "Trip",
  dayStatus: "Day status", deleted: "Remove load", originalRate: "Total gross", driverRate: "Driver gross",
  miles: "Miles", enteredOriginalRate: "Total gross", enteredMiles: "Miles", note: "Note",
  acceptSystemValues: "DataTruck values", acceptDriverGross: "Driver gross", splitReview: "Split review",
};
const sourceFields = ["originalRate", "driverRate", "miles"] as const;

export function SaveIssues({ issues, changes, drivers, saving, onDiscard, actions }: {
  issues: GrossBoardSaveIssue[];
  changes: GrossDrafts;
  drivers: GrossBoardDriver[];
  saving: boolean;
  onDiscard: (issue: GrossBoardSaveIssue) => void;
  actions: ReactNode;
}) {
  const names = new Map(drivers.map(driver => [driver.id, driver.fullName]));
  return <section role="alert" className="space-y-3 rounded-lg border border-red-500/20 bg-red-500/10 p-3 text-sm text-red-200">
    <p className="font-medium">Review the edits that could not be saved</p>
    <ul className="space-y-3">
      {issues.map(issue => {
        const key = grossLocationKey(issue);
        const draft = changes[key];
        const fields = [...new Set(issue.fields?.map(field => fieldLabels[field] ?? field))];
        return <li key={key} className="space-y-1 border-l border-red-500/40 pl-3">
          <p className="font-medium">{names.get(issue.driverId) ?? "Driver"} · {issue.date} · Load {issue.slot + 1}{issue.loadNumber ? ` · ${issue.loadNumber}` : ""}{fields.length ? ` · ${fields.join(", ")}` : ""}</p>
          <p>{issue.message}</p>
          {issue.currentLoad && <p className="text-xs">Your draft load {issue.loadNumber || "Empty"} · Current saved slot {issue.currentLoad}</p>}
          {issue.source && draft && <table className="text-left text-xs">
            <thead><tr><th className="pr-4">Field</th><th className="pr-4">Your draft</th><th>Current DataTruck value</th></tr></thead>
            <tbody>{sourceFields.filter(field => issue.fields?.includes(field) && issue.source?.[field] !== undefined).map(field =>
              <tr key={field}><th className="pr-4">{fieldLabels[field]}</th><td className="pr-4">{amountDisplay(draft[field], field !== "miles") || "Empty"}</td><td>{amountDisplay(issue.source?.[field] ?? "", field !== "miles") || "Unavailable"}</td></tr>
            )}</tbody>
          </table>}
          {issue.pending.length > 1 && <p className="text-xs">{issue.pending.length} related edits are still pending together.</p>}
          <button type="button" className="text-xs underline disabled:opacity-40" disabled={saving} onClick={() => onDiscard(issue)}>Discard edits for this issue</button>
        </li>;
      })}
    </ul>
    <div className="flex flex-wrap gap-2">{actions}</div>
  </section>;
}
