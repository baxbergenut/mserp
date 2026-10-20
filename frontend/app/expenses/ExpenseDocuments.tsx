"use client";

import { useEffect, useState } from "react";
import { expenseDocumentUrl, fetchExpenseDocuments } from "../lib/api";
import type { ExpenseDocument } from "../lib/types";

export function ExpenseDocuments({ id }: { id: string }) {
  const [documents, setDocuments] = useState<ExpenseDocument[]>([]);
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    fetchExpenseDocuments(id, controller.signal).then(setDocuments).catch(reason => {
      if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : "Unable to load receipts");
    });
    return () => controller.abort();
  }, [id]);
  return <div className="mt-4 space-y-2 text-xs">
    {error && <p role="alert" className="text-red-300">{error}</p>}
    {documents.map(document => <a key={document.id} href={expenseDocumentUrl(document.id)} target="_blank" rel="noreferrer" className="block break-all text-blue-400">{document.fileName}</a>)}
  </div>;
}
