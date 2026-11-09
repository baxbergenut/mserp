"use client";

/* eslint-disable @next/next/no-img-element -- Authenticated receipt blobs are local object URLs, not optimizer sources. */

import { useEffect, useState } from "react";
import { ExternalLink, FileText } from "lucide-react";
import { expenseDocumentUrl, fetchExpenseDocuments } from "../lib/api";
import type { ExpenseDocument } from "../lib/types";

export function ExpenseDocuments({ id }: { id: string }) {
  const [documents, setDocuments] = useState<ExpenseDocument[]>([]);
  const [selected, setSelected] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [previewUrl, setPreviewUrl] = useState("");
  const [previewError, setPreviewError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    fetchExpenseDocuments(id, controller.signal).then(items => {
      if (controller.signal.aborted) return;
      setDocuments(items); setSelected(items[0]?.id ?? ""); setLoading(false);
    }).catch(reason => {
      if (!controller.signal.aborted) { setError(reason instanceof Error ? reason.message : "Unable to load receipts"); setLoading(false); }
    });
    return () => controller.abort();
  }, [id, attempt]);
  const document = documents.find(item => item.id === selected);
  const url = document ? expenseDocumentUrl(document.id) : "";
  useEffect(() => {
    if (!url) return;
    const controller = new AbortController();
    let objectUrl = "";
    fetch(url, { credentials: "include", signal: controller.signal }).then(response => {
      if (!response.ok) throw new Error("Unable to preview this attachment. Use Open file to try again.");
      return response.blob();
    }).then(blob => {
      if (controller.signal.aborted) return;
      objectUrl = URL.createObjectURL(blob); setPreviewUrl(objectUrl);
    }).catch(reason => { if (!controller.signal.aborted) setPreviewError(reason instanceof Error ? reason.message : "Preview unavailable"); });
    return () => { controller.abort(); if (objectUrl) URL.revokeObjectURL(objectUrl); };
  }, [url]);
  return <section aria-label="Invoice and receipt preview" className="flex h-full min-h-0 flex-col bg-zinc-950/30">
    <div className="flex shrink-0 items-center justify-between gap-3 border-b border-zinc-800 p-3">
      <h3 className="flex items-center gap-2 text-sm font-medium text-zinc-200"><FileText className="h-4 w-4 text-blue-400" />Invoice / receipt</h3>
      {document && <a href={url} target="_blank" rel="noreferrer" className="flex items-center gap-1 text-xs text-blue-400">Open file<ExternalLink className="h-3 w-3" /></a>}
    </div>
    {documents.length > 1 && <div className="shrink-0 p-3"><label className="sr-only" htmlFor={`receipt-${id}`}>Select invoice or receipt</label><select id={`receipt-${id}`} value={selected} onChange={event => { setPreviewUrl(""); setPreviewError(""); setSelected(event.target.value); }} className="ui-input w-full">{documents.map(item => <option key={item.id} value={item.id}>{item.fileName}</option>)}</select></div>}
    {loading ? <p role="status" className="m-auto p-6 text-sm text-zinc-500">Loading invoice / receipt...</p> : error ? <div className="m-auto space-y-3 p-6 text-center"><p role="alert" className="text-sm text-red-400">{error}</p><button type="button" className="ui-button" onClick={() => { setLoading(true); setError(""); setAttempt(value => value + 1); }}>Retry</button></div> : !document ? <div className="m-auto p-6 text-center text-zinc-500"><FileText className="mx-auto mb-3 h-10 w-10" /><p className="text-sm">No invoice or receipt attached</p></div> : <>
      <p className="shrink-0 truncate px-3 py-2 text-xs text-zinc-400" title={document.fileName}>{document.fileName}</p>
      <div className="min-h-0 flex-1 overflow-auto p-3">
        {previewError ? <p role="alert" className="p-3 text-sm text-red-400">{previewError}</p> : !previewUrl ? <p role="status" className="p-3 text-sm text-zinc-500">Loading preview...</p> : document.contentType === "application/pdf" ? <iframe key={document.id} title={document.fileName} src={previewUrl} className="h-full min-h-64 w-full rounded border-0 bg-white" /> : document.contentType.startsWith("image/") ? <img key={document.id} src={previewUrl} alt={document.fileName} className="h-full w-full object-contain" /> : <div className="flex h-full items-center justify-center text-sm text-zinc-400">Preview unavailable. Use Open file to view this attachment.</div>}
      </div>
    </>}
  </section>;
}
