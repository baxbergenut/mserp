"use client";

import { useEffect, useState } from "react";
import { Check, MessageSquare } from "lucide-react";
import Link from "../components/AccessLink";
import { fetchFeedbackMessages, readFeedbackMessage } from "../lib/api";
import type { FeedbackMessage, PaginatedResponse } from "../lib/types";
import { ErrorBanner } from "../components/management/ManagementUI";
import { useTaskUpdates } from "../components/TaskUpdatesProvider";

export function FeedbackMessages({ showEmpty = false }: { showEmpty?: boolean }) {
  const { revision, refresh } = useTaskUpdates();
  const [page, setPage] = useState(1), [poll, setPoll] = useState(0);
  const [data, setData] = useState<PaginatedResponse<FeedbackMessage> | null>(null);
  const [error, setError] = useState(""), [saving, setSaving] = useState("");
  useEffect(() => {
    const update = () => { if (!document.hidden) setPoll(value => value + 1); };
    const timer = window.setInterval(update, 30000); window.addEventListener("focus", update); document.addEventListener("visibilitychange", update);
    return () => { clearInterval(timer); window.removeEventListener("focus", update); document.removeEventListener("visibilitychange", update); };
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    fetchFeedbackMessages(page, controller.signal).then(value => { setData(value); setError(""); }).catch(e => { if (!controller.signal.aborted) setError(e instanceof Error ? e.message : "Could not load feedback updates"); });
    return () => controller.abort();
  }, [page, poll, revision]);
  async function markRead(id: string) {
    if (saving) return; setSaving(id); setError("");
    try { await readFeedbackMessage(id); refresh(); } catch (e) { setError(e instanceof Error ? e.message : "Could not mark this message as read"); } finally { setSaving(""); }
  }
  if (!showEmpty && !error && (!data || !data.total)) return null;
  return <section aria-label="Feedback updates" className="ui-card p-4">
    <h2 className="mb-3 flex items-center gap-2 font-semibold"><MessageSquare className="h-4 w-4 text-blue-400" />Feedback updates</h2>
    {error && <div className="mb-3"><ErrorBanner message={error} /><button type="button" className="ui-button mt-2" onClick={() => setPoll(value => value + 1)}>Try again</button></div>}
    {!data && !error && <p role="status" className="text-zinc-500">Loading feedback updates…</p>}
    {data && !data.total && <p className="text-zinc-500">When your feedback is completed, you’ll receive a message here.</p>}
    <div className="space-y-2">{data?.items.map(item => <article key={item.id} className={`flex flex-wrap items-center gap-3 rounded-lg border p-3 ${item.readAt ? "border-zinc-800" : "border-blue-500/30 bg-blue-500/5"}`}>
      <div className="min-w-0 flex-1"><p className="[overflow-wrap:anywhere]">Your feedback <span className="font-semibold">“{item.title}”</span> has been completed.</p><p className="mt-1 text-xs text-zinc-500">{item.completedBy} · {new Date(item.createdAt).toLocaleString("en-US", { timeZone: "America/New_York", month: "short", day: "numeric", hour: "numeric", minute: "2-digit" })} NY{item.readAt ? " · Read" : " · New"}</p></div>
      <Link href={`/feedback?post=${item.postId}`} className="ui-button">View feedback</Link>
      {!item.readAt && <button type="button" className="ui-button" disabled={!!saving} onClick={() => void markRead(item.id)}><Check className="h-4 w-4" />Mark as read</button>}
    </article>)}</div>
    {data && data.totalPages > 1 && <div className="mt-3 flex items-center justify-end gap-3 text-xs"><button type="button" className="ui-button" disabled={data.page <= 1} onClick={() => setPage(data.page - 1)}>Previous updates</button><span>{data.page} / {data.totalPages}</span><button type="button" className="ui-button" disabled={data.page >= data.totalPages} onClick={() => setPage(data.page + 1)}>Next updates</button></div>}
  </section>;
}
