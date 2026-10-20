"use client";
import { useState } from "react";
import type { InventoryCollection, InventoryRecord } from "@/app/lib/types";
import {
  ManagementSearch,
  TableShell,
  TablePagination,
  controlClass,
} from "@/app/components/management/ManagementUI";
import { ColumnFilter } from "@/app/components/management/ColumnFilter";
import { inventoryLabel, type InventoryContext } from "./InventoryForms";
import { inventoryIssues, deviceDescription } from "./inventoryView";

export function InventoryReview({
  context,
  onOpen,
  onEdit,
}: {
  context: InventoryContext;
  onOpen: (collection: InventoryCollection, record: InventoryRecord) => void;
  onEdit?: (collection: InventoryCollection, record: InventoryRecord) => void;
}) {
  const [search, setSearch] = useState(""),
    [issue, setIssue] = useState("");
  const [page, setPage] = useState(1),
    [pageSize, setPageSize] = useState(25);
  const rows = (["assets", "lines", "equipment"] as const)
    .flatMap((collection) =>
      context.data[collection].map((record) => ({
        collection,
        record,
        issues: inventoryIssues(collection, record, context.data),
      })),
    )
    .filter((row) => row.issues.length);
  const choices = [...new Set(rows.flatMap((row) => row.issues))].sort();
  const filtered = rows.filter(
    (row) =>
      (!issue || row.issues.includes(issue)) &&
      (!search ||
        [
          inventoryLabel(row.record),
          deviceDescription(row.record.data),
          row.record.data.name,
          context.trucks.find((t) => t.id === row.record.data.truckId)
            ?.unitNumber,
          ...row.issues,
        ]
          .join(" ")
          .toLowerCase()
          .includes(search.toLowerCase())),
  );
  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize)),
    currentPage = Math.min(page, totalPages);
  return (
    <div className="space-y-4">
      <ManagementSearch
        value={search}
        onChange={(value) => {
          setSearch(value);
          setPage(1);
        }}
        placeholder="Search records needing attention…"
      />
      <TableShell>
        <table className="w-full text-[13px]">
          <thead>
            <tr className="text-left text-xs text-zinc-500">
              <th className="px-4 py-2">Record</th>
              <th className="px-4 py-2">
                <ColumnFilter label="Needs attention" active={!!issue}>
                  <select
                    className={controlClass}
                    aria-label="Attention filter"
                    value={issue}
                    onChange={(e) => {
                      setIssue(e.target.value);
                      setPage(1);
                    }}
                  >
                    <option value="">All issues</option>
                    {choices.map((value) => (
                      <option key={value}>{value}</option>
                    ))}
                  </select>
                </ColumnFilter>
              </th>
              <th className="px-4 py-2" />
            </tr>
          </thead>
          <tbody>
            {filtered
              .slice((currentPage - 1) * pageSize, currentPage * pageSize)
              .map(({ collection, record, issues }) => (
                <tr key={record.id} className="border-t border-zinc-800/60">
                  <td className="px-4 py-2">
                    <button
                      className="text-left text-blue-400"
                      onClick={() => onOpen(collection, record)}
                    >
                      {collection === "equipment"
                        ? [
                            context.trucks.find(
                              (t) => t.id === record.data.truckId,
                            )?.unitNumber,
                            record.data.name,
                            record.data.batchYear || "Year unknown",
                          ]
                            .filter(Boolean)
                            .join(" · ")
                        : inventoryLabel(record)}
                    </button>
                    {collection === "assets" && (
                      <p className="text-xs text-zinc-500">
                        {deviceDescription(record.data)}
                      </p>
                    )}
                  </td>
                  <td className="px-4 py-2 text-xs text-zinc-400">
                    {issues.join(" · ")}
                  </td>
                  <td className="px-4 py-2">
                    {onEdit && (
                      <button
                        className="text-xs text-blue-400"
                        onClick={() => onEdit(collection, record)}
                      >
                        Review
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            {!filtered.length && (
              <tr>
                <td colSpan={3} className="p-8 text-center text-zinc-500">
                  No matching records.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </TableShell>
      <TablePagination
        page={currentPage}
        pageSize={pageSize}
        totalItems={filtered.length}
        totalPages={totalPages}
        onPageChange={setPage}
        onPageSizeChange={(value) => {
          setPageSize(value);
          setPage(1);
        }}
      />
    </div>
  );
}
