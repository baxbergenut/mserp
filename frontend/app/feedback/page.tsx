"use client";

import { useCallback, useContext, useEffect, useRef, useState, type ClipboardEvent, type FormEvent } from "react";
import { ArrowUp, Columns3, List, MessageSquare, Paperclip, Pencil, Plus, X } from "lucide-react";
import { CurrentUserIDContext, useAdministrator } from "../lib/access";
import { commentFeedback, createFeedback, feedbackAttachmentURL, fetchFeedback, fetchFeedbackDetail, fetchFeedbackTags, updateFeedback, voteFeedback } from "../lib/api";
import type { FeedbackAttachment, FeedbackDetail, FeedbackInput, FeedbackPost, FeedbackStatus, PaginatedResponse } from "../lib/types";
import { controlClass, ErrorBanner, ManagementHeader, ManagementSearch, Modal, TablePagination } from "../components/management/ManagementUI";
import { useDebouncedValue } from "../lib/useDebouncedValue";

const statuses: FeedbackStatus[] = ["open", "in_progress", "completed", "discarded"];
const labels: Record<FeedbackStatus, string> = { open: "Open", in_progress: "In progress", completed: "Completed", discarded: "Discarded" };
const colors: Record<FeedbackStatus, string> = { open: "bg-blue-500/10 text-blue-400", in_progress: "bg-amber-500/10 text-amber-400", completed: "bg-emerald-500/10 text-emerald-400", discarded: "bg-zinc-500/10 text-zinc-400" };
const defaultTags = ["bug", "feature", "ui", "ux", "improvement", "question"];
const tagLabel = (tag: string) => tag === "ui" || tag === "ux" ? tag.toUpperCase() : tag.charAt(0).toUpperCase() + tag.slice(1);
const when = (value: string) => new Date(value).toLocaleString("en-US", { timeZone: "America/New_York", month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
const message = (e: unknown) => e instanceof Error ? e.message : "Something went wrong. Please try again.";
const isImage = (type: string) => ["image/png", "image/jpeg", "image/webp", "image/gif"].includes(type);
const size = (bytes: number) => bytes >= 1048576 ? `${(bytes / 1048576).toFixed(1)} MB` : `${Math.ceil(bytes / 1024)} KB`;
type FeedbackPage = PaginatedResponse<FeedbackPost>;

function PostMedia({ files }: { files: FeedbackAttachment[] }) {
  if (!files.length) return null;
  const images = files.filter(file => isImage(file.contentType));
  const documents = files.filter(file => !isImage(file.contentType));
  return <div className="mt-3 space-y-2">
    {!!images.length && <div aria-label="Post images" className={`grid gap-2 ${images.length > 1 ? "sm:grid-cols-2" : "grid-cols-1"}`}>
      {images.map(file => <a key={file.id} href={feedbackAttachmentURL(file.id)} target="_blank" rel="noreferrer" aria-label={`View ${file.fileName}`} className="block overflow-hidden rounded-lg border border-zinc-800 bg-zinc-950/40">
        {/* Authenticated byte streams must use the session cookie directly. */}
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img src={feedbackAttachmentURL(file.id)} alt={file.fileName} loading="lazy" className="max-h-[420px] w-full object-contain" />
      </a>)}
    </div>}
    {!!documents.length && <div className="flex flex-wrap items-start gap-2">{documents.map(file => <a key={file.id} href={feedbackAttachmentURL(file.id, true)} className="flex max-w-full items-center gap-2 rounded-lg border border-zinc-800 px-2 py-1 text-xs text-zinc-400 hover:text-blue-400" download>
      <Paperclip className="h-3 w-3 shrink-0" /><span className="max-w-52 truncate">{file.fileName}</span><span className="shrink-0">{size(file.size)}</span>
    </a>)}</div>}
  </div>;
}

function FilePreview({ file }: { file: File }) {
  const image = useRef<HTMLImageElement>(null);
  useEffect(() => { if (!isImage(file.type) || !image.current) return; const value = URL.createObjectURL(file); image.current.src = value; return () => URL.revokeObjectURL(value); }, [file]);
  // eslint-disable-next-line @next/next/no-img-element
  return isImage(file.type) ? <img ref={image} alt={file.name} className="h-16 w-24 rounded object-cover" /> : <Paperclip className="h-4 w-4 text-zinc-500" />;
}

function Composer({ post, tags, onClose, onSaved }: { post?: FeedbackPost; tags: string[]; onClose: () => void; onSaved: (id: string) => Promise<void> }) {
  const [id] = useState(() => post?.id ?? crypto.randomUUID());
  const [title, setTitle] = useState(post?.title ?? "");
  const [body, setBody] = useState(post?.body ?? "");
  const [selected, setSelected] = useState<string[]>(post?.tags ?? []);
  const [custom, setCustom] = useState("");
  const [files, setFiles] = useState<File[]>([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const pending = useRef(false);
  const fileInput = useRef<HTMLInputElement>(null);
  function addFiles(added: File[]) {
    if (saving) return;
    const next = [...files, ...added];
    if (next.length > 5 || next.some(f => f.size === 0 || f.size > 10 * 1048576) || next.reduce((n, f) => n + f.size, 0) > 20 * 1048576) { setError("Attach up to 5 nonempty files, 10 MB each and 20 MB total."); return; }
    setFiles(next); setError("");
  }
  function paste(e: ClipboardEvent) {
    if (post) return;
    const images = Array.from(e.clipboardData.items).filter(item => item.kind === "file" && item.type.startsWith("image/")).map(item => item.getAsFile()).filter((file): file is File => !!file);
    if (images.length) { e.preventDefault(); addFiles(images); }
  }
  function toggle(tag: string) { if (saving) return; if (!selected.includes(tag) && selected.length === 8) { setError("Choose up to 8 tags."); return; } setSelected(values => values.includes(tag) ? values.filter(t => t !== tag) : [...values, tag]); }
  function addTag() {
    const tag = custom.trim().toLowerCase();
    if (!/^[\p{L}\p{N} -]{1,24}$/u.test(tag)) { setError("Use 1–24 letters, numbers, spaces or hyphens for a tag."); return; }
    if (!selected.includes(tag)) toggle(tag); setCustom("");
  }
  async function submit(e: FormEvent) {
    e.preventDefault(); if (pending.current) return;
    pending.current = true; setSaving(true); setError("");
    try {
      const input: FeedbackInput = { id, title, body, tags: selected, ...(post ? { status: post.status, version: post.version } : {}) };
      if (post) await updateFeedback(input); else await createFeedback(input, files);
      await onSaved(id);
    } catch (e) { setError(message(e)); } finally { pending.current = false; setSaving(false); }
  }
  return <Modal title={post ? "Edit feedback" : "Share feedback"} description="Everyone in Clustie can see your feedback and attachments." isSaving={saving} submitLabel={post ? "Save" : "Post feedback"} onClose={onClose} onSubmit={submit}>
    <div className="space-y-4" onPaste={paste} onDragOver={e => { if (!post) e.preventDefault(); }} onDrop={e => { if (!post) { e.preventDefault(); addFiles(Array.from(e.dataTransfer.files)); } }}>
      {error && <ErrorBanner message={error} />}
      <label className="block space-y-1"><span className="text-xs text-zinc-400">Title</span><input aria-label="Title" autoFocus required maxLength={200} value={title} onChange={e => setTitle(e.target.value)} disabled={saving} className={controlClass} placeholder="What could we improve?" /></label>
      <label className="block space-y-1"><span className="text-xs text-zinc-400">Details</span><textarea aria-label="Details" required maxLength={10000} rows={8} value={body} onChange={e => setBody(e.target.value)} disabled={saving} className={controlClass} placeholder={"Describe the problem or idea in as much detail as possible.\n\nFor a bug: which page, steps to reproduce, what you expected, and what happened?\nFor an idea: who would use it, what problem does it solve, and how should it work?\n\nInclude examples, links, dates, your browser/device and screenshots where helpful."} /></label>
      <p className="text-xs text-zinc-500">More detail helps us understand and act on your request. Include the page, examples, impact and steps to reproduce. Avoid passwords and private customer information.</p>
      <fieldset disabled={saving} className="space-y-2"><legend className="mb-2 text-xs text-zinc-400">Tags <span className="text-zinc-500">· up to 8</span></legend>
        <div className="flex flex-wrap gap-2">{Array.from(new Set([...tags, ...selected])).map(tag => <button key={tag} type="button" aria-pressed={selected.includes(tag)} onClick={() => toggle(tag)} className={`rounded-full border px-2.5 py-1 text-xs ${selected.includes(tag) ? "border-blue-500/60 bg-blue-500/10 text-blue-400" : "border-zinc-800 text-zinc-400"}`}>{tagLabel(tag)}</button>)}</div>
        <div className="flex gap-2"><input aria-label="Custom tag" value={custom} maxLength={24} onChange={e => setCustom(e.target.value)} onKeyDown={e => { if (e.key === "Enter") { e.preventDefault(); addTag(); } }} className={`${controlClass} max-w-56`} placeholder="Add a tag" /><button type="button" onClick={addTag} disabled={!custom.trim()} className="ui-button"><Plus className="h-4 w-4" />Add tag</button></div>
      </fieldset>
      {!post && <div className="rounded-lg border border-dashed border-zinc-700 p-4">
        <button type="button" className="ui-button" disabled={saving} onClick={() => fileInput.current?.click()}><Paperclip className="h-4 w-4" />Attach files</button>
        <input ref={fileInput} type="file" multiple className="sr-only" aria-label="Feedback attachments" disabled={saving} onChange={e => { addFiles(Array.from(e.target.files ?? [])); e.target.value = ""; }} />
        <p className="mt-2 text-xs text-zinc-500">Paste screenshots here with Ctrl+V, or drop files. Up to 5 files, 10 MB each, 20 MB total.</p>
        <ul className="mt-2 space-y-2">{files.map((file, index) => <li key={index} className="flex items-center gap-2 text-xs"><FilePreview file={file} /><span className="min-w-0 flex-1 truncate">{file.name} · {size(file.size)}</span><button type="button" disabled={saving} aria-label={`Remove ${file.name}`} onClick={() => setFiles(values => values.filter((_, i) => i !== index))} className="ui-button"><X className="h-4 w-4" /></button></li>)}</ul>
      </div>}
    </div>
  </Modal>;
}

export default function Feedback() {
  const administrator = useAdministrator(), userID = useContext(CurrentUserIDContext);
  const [view, setView] = useState<"feed" | "kanban">("feed");
  const [search, setSearch] = useState(""), query = useDebouncedValue(search, 250);
  const [tag, setTag] = useState(""), [status, setStatus] = useState(""), [sort, setSort] = useState("newest");
  const [page, setPage] = useState(1), [pageSize, setPageSize] = useState(25), [columnPages, setColumnPages] = useState<Record<FeedbackStatus, number>>({ open: 1, in_progress: 1, completed: 1, discarded: 1 });
  const [data, setData] = useState<FeedbackPage | null>(null), [columns, setColumns] = useState<Partial<Record<FeedbackStatus, FeedbackPage>>>({});
  const [tags, setTags] = useState(defaultTags), [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true), [error, setError] = useState("");
  const [composer, setComposer] = useState<FeedbackPost | "new" | null>(null);
  const [detail, setDetail] = useState<FeedbackDetail | null>(null), [detailError, setDetailError] = useState("");
  const [comment, setComment] = useState(""), [busy, setBusy] = useState("");
  const pending = useRef(false), detailRequest = useRef(0), commentID = useRef("");
  const refresh = useCallback(() => setRevision(value => value + 1), []);
  const resetPages = () => { setPage(1); setColumnPages({ open: 1, in_progress: 1, completed: 1, discarded: 1 }); };
  useEffect(() => { const focus = () => { if (document.visibilityState === "visible") refresh(); }; const timer = window.setInterval(focus, 30000); window.addEventListener("focus", focus); document.addEventListener("visibilitychange", focus); return () => { clearInterval(timer); window.removeEventListener("focus", focus); document.removeEventListener("visibilitychange", focus); }; }, [refresh]);
  useEffect(() => {
    let active = true; const controller = new AbortController();
    async function load() {
      setLoading(true);
      try {
        const common = { search: query, tag, sort, pageSize };
        if (view === "feed") { const result = await fetchFeedback({ ...common, page, status }, controller.signal); if (active) setData(result); }
        else { const result = await Promise.all(statuses.map(s => fetchFeedback({ ...common, page: columnPages[s], status: s }, controller.signal))); if (active) setColumns(Object.fromEntries(statuses.map((s, i) => [s, result[i]]))); }
        const choices = await fetchFeedbackTags(); if (active) { setTags(choices); setError(""); }
      } catch (e) { if (active) setError(message(e)); } finally { if (active) setLoading(false); }
    }
    void load(); return () => { active = false; controller.abort(); };
  }, [view, query, tag, status, sort, page, pageSize, columnPages, revision]);
  async function open(id: string) {
    const request = ++detailRequest.current; setError("");
    try { const value = await fetchFeedbackDetail(id); if (request === detailRequest.current) { setDetail(value); setDetailError(""); setComment(""); commentID.current = ""; } } catch (e) { if (request === detailRequest.current) setError(message(e)); }
  }
  function closeDetail() { detailRequest.current++; setDetail(null); setDetailError(""); }
  async function mutate(id: string, action: () => Promise<void>) {
    if (pending.current) return;
    pending.current = true; setBusy(id); setError(""); setDetailError("");
    try { await action(); refresh(); if (detail?.id === id) setDetail(await fetchFeedbackDetail(id)); }
    catch (e) { if (detail) setDetailError(message(e)); else setError(message(e)); }
    finally { pending.current = false; setBusy(""); }
  }
  function changeStatus(post: FeedbackPost, next: FeedbackStatus) {
    return mutate(post.id, () => updateFeedback({ id: post.id, title: post.title, body: post.body, tags: post.tags, version: post.version, status: next }));
  }
  async function addComment(e: FormEvent) {
    e.preventDefault(); if (!detail || !comment.trim() || pending.current) return;
    const id = detail.id; if (!commentID.current) commentID.current = crypto.randomUUID();
    await mutate(id, async () => { await commentFeedback(id, { id: commentID.current, body: comment }); setComment(""); commentID.current = ""; });
  }
  const vote = (post: FeedbackPost) => mutate(post.id, () => voteFeedback(post.id, !post.voted));
  function card(post: FeedbackPost, compact = false) {
    return <article key={post.id} className="ui-card min-w-0 p-4" draggable={administrator && !busy} onDragStart={e => { e.dataTransfer.setData("application/x-clustie-feedback", post.id); e.dataTransfer.effectAllowed = "move"; }}>
      <div className="flex gap-3">
        <button type="button" aria-label={`${post.voted ? "Remove upvote from" : "Upvote"} ${post.title}`} aria-pressed={post.voted} disabled={!!busy} onClick={() => void vote(post)} className={`flex h-fit min-w-10 shrink-0 flex-col items-center rounded-lg border p-2 text-xs ${post.voted ? "border-blue-500/50 bg-blue-500/10 text-blue-400" : "border-zinc-800 text-zinc-400 hover:border-blue-500/50"}`}><ArrowUp className="h-4 w-4" /><span>{post.votes}</span></button>
        <div className="min-w-0 flex-1">
          <div className="mb-2 flex flex-wrap items-center gap-2"><span className={`rounded-full px-2 py-0.5 text-xs ${colors[post.status]}`}>{labels[post.status]}</span>{post.tags.map(t => <button key={t} type="button" onClick={() => { setTag(t); resetPages(); }} className="rounded-full bg-zinc-800/60 px-2 py-0.5 text-xs text-zinc-400">{tagLabel(t)}</button>)}</div>
          <button type="button" className="text-left font-semibold text-zinc-100 hover:text-blue-400 [overflow-wrap:anywhere]" onClick={() => void open(post.id)}>{post.title}</button>
          {!compact && <PostMedia files={post.attachments} />}
          <p className={`mt-3 whitespace-pre-wrap text-zinc-400 [overflow-wrap:anywhere] ${compact ? "line-clamp-3" : "line-clamp-6"}`}>{post.body}</p>
          <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-zinc-500"><span>{post.authorName}</span><time dateTime={post.createdAt} title={`${when(post.createdAt)} NY`}>{when(post.createdAt)}</time><button type="button" onClick={() => void open(post.id)} className="inline-flex items-center gap-1 hover:text-blue-400"><MessageSquare className="h-3.5 w-3.5" />{post.commentCount} comments</button>{compact && post.attachments.length > 0 && <span className="inline-flex items-center gap-1"><Paperclip className="h-3.5 w-3.5" />{post.attachments.length}</span>}</div>
        </div>
      </div>
    </article>;
  }
  return <div className="space-y-4">
    <ManagementHeader icon={MessageSquare} title="Feedback" actionLabel="Share feedback" onAction={() => setComposer("new")} />
    <div className="flex flex-wrap items-center gap-2">
      <div role="tablist" aria-label="Feedback view" className="mr-2 flex gap-3"><button type="button" role="tab" aria-selected={view === "feed"} onClick={() => setView("feed")} className="flex items-center gap-1.5 px-1 py-2"><List className="h-4 w-4" />Feed</button><button type="button" role="tab" aria-selected={view === "kanban"} onClick={() => setView("kanban")} className="flex items-center gap-1.5 px-1 py-2"><Columns3 className="h-4 w-4" />Kanban</button></div>
      <ManagementSearch value={search} onChange={value => { setSearch(value); resetPages(); }} placeholder="Search feedback…" />
      <select aria-label="Filter by tag" className={`${controlClass} !w-auto`} value={tag} onChange={e => { setTag(e.target.value); resetPages(); }}><option value="">All tags</option>{tags.map(t => <option key={t} value={t}>{tagLabel(t)}</option>)}</select>
      {view === "feed" && <select aria-label="Filter by status" className={`${controlClass} !w-auto`} value={status} onChange={e => { setStatus(e.target.value); resetPages(); }}><option value="">All statuses</option>{statuses.map(s => <option key={s} value={s}>{labels[s]}</option>)}</select>}
      <select aria-label="Sort feedback" className={`${controlClass} !w-auto`} value={sort} onChange={e => { setSort(e.target.value); resetPages(); }}><option value="newest">Newest first</option><option value="top">Most upvoted</option></select>
    </div>
    {error && <div><ErrorBanner message={error} /><button type="button" className="ui-button mt-2" onClick={refresh}>Try again</button></div>}
    {loading && !data && !Object.keys(columns).length && <p role="status" className="text-zinc-500">Loading feedback…</p>}
    {view === "feed" && data && <div aria-busy={loading} className="space-y-3">{data.items.map(post => card(post))}{data.items.length === 0 && <div className="ui-card p-8 text-center text-zinc-500">{query || tag || status ? "No feedback matches these filters." : "Have an idea or found a bug? Share the details and help shape Clustie."}</div>}<TablePagination page={data.page} pageSize={data.pageSize} totalItems={data.total} totalPages={data.totalPages} onPageChange={setPage} onPageSizeChange={value => { setPageSize(value); resetPages(); }} /></div>}
    {view === "kanban" && <div className="overflow-x-auto pb-2" aria-busy={loading}><div className="grid min-w-[1000px] grid-cols-4 gap-3">{statuses.map(s => <section key={s} aria-label={labels[s]} className="min-w-0 rounded-lg border border-zinc-800 bg-zinc-950/30 p-3" onDragOver={e => { if (administrator && e.dataTransfer.types.includes("application/x-clustie-feedback")) e.preventDefault(); }} onDrop={e => { e.preventDefault(); if (!administrator) return; const id = e.dataTransfer.getData("application/x-clustie-feedback"); const post = Object.values(columns).flatMap(p => p?.items ?? []).find(p => p.id === id); if (post && post.status !== s) void changeStatus(post, s); }}>
      <h2 className="mb-3 flex items-center justify-between font-semibold"><span className={`rounded-full px-2 py-0.5 text-xs ${colors[s]}`}>{labels[s]}</span><span className="text-xs text-zinc-500">{columns[s]?.total ?? 0}</span></h2>
      <div className="space-y-3">{columns[s]?.items.map(post => card(post, true))}{columns[s]?.total === 0 && <p className="py-6 text-center text-xs text-zinc-500">No requests</p>}</div>
      {columns[s] && columns[s]!.totalPages > 1 && <div className="mt-3 flex items-center justify-between text-xs"><button type="button" className="ui-button" aria-label={`Previous ${labels[s]} page`} disabled={columns[s]!.page <= 1 || loading} onClick={() => setColumnPages(p => ({ ...p, [s]: columns[s]!.page - 1 }))}>Previous</button><span>{columns[s]!.page} / {columns[s]!.totalPages}</span><button type="button" className="ui-button" aria-label={`Next ${labels[s]} page`} disabled={columns[s]!.page >= columns[s]!.totalPages || loading} onClick={() => setColumnPages(p => ({ ...p, [s]: columns[s]!.page + 1 }))}>Next</button></div>}
    </section>)}</div></div>}
    {detail && !composer && <Modal title={detail.title} isSaving={!!busy} submitLabel="Add comment" submitDisabled={!comment.trim()} onClose={closeDetail} onSubmit={addComment} cancelLabel="Close">
      <div className="space-y-4">
        {detailError && <ErrorBanner message={detailError} />}
        <div className="flex flex-wrap items-center gap-2"><span className={`rounded-full px-2 py-0.5 text-xs ${colors[detail.status]}`}>{labels[detail.status]}</span>{detail.tags.map(t => <span key={t} className="rounded-full bg-zinc-800/60 px-2 py-0.5 text-xs text-zinc-400">{tagLabel(t)}</span>)}
          {(administrator || detail.authorId === userID) && <button type="button" disabled={!!busy} className="ui-button ml-auto" onClick={() => setComposer(detail)}><Pencil className="h-4 w-4" />Edit</button>}
        </div>
        <p className="text-xs text-zinc-500">{detail.authorName} · {when(detail.createdAt)} NY</p>
        <PostMedia files={detail.attachments} />
        <p className="whitespace-pre-wrap [overflow-wrap:anywhere]">{detail.body}</p>
        <div className="flex flex-wrap items-center gap-3"><button type="button" disabled={!!busy} aria-pressed={detail.voted} onClick={() => void vote(detail)} className={`ui-button ${detail.voted ? "text-blue-400" : ""}`}><ArrowUp className="h-4 w-4" />{detail.voted ? "Upvoted" : "Upvote"} · {detail.votes}</button>
          {administrator && <select aria-label="Request status" disabled={!!busy} value={detail.status} onChange={e => void changeStatus(detail, e.target.value as FeedbackStatus)} className={`${controlClass} !w-auto`}>{statuses.map(s => <option key={s} value={s}>{labels[s]}</option>)}</select>}
        </div>
        {!!detail.history.length && <details className="text-xs text-zinc-500"><summary className="cursor-pointer">Status history</summary><ul className="mt-2 space-y-1">{detail.history.map((h, i) => <li key={i}>{h.actorName}: {labels[h.oldStatus]} → {labels[h.newStatus]} · {when(h.createdAt)} NY</li>)}</ul></details>}
        <section className="border-t border-zinc-800 pt-4"><h3 className="mb-3 font-semibold">Comments ({detail.comments.length})</h3><div className="space-y-3">{detail.comments.map(c => <article key={c.id} className="rounded-lg bg-zinc-800/30 p-3"><p className="mb-1 text-xs text-zinc-500">{c.authorName} · {when(c.createdAt)} NY</p><p className="whitespace-pre-wrap [overflow-wrap:anywhere]">{c.body}</p></article>)}</div></section>
        <label className="block space-y-1"><span className="text-xs text-zinc-400">Add details or join the discussion</span><textarea aria-label="Add details or join the discussion" className={controlClass} maxLength={5000} rows={3} disabled={!!busy} value={comment} onChange={e => { setComment(e.target.value); commentID.current = ""; }} placeholder="Share an example, workaround or additional context…" /></label>
      </div>
    </Modal>}
    {composer && <Composer key={composer === "new" ? "new" : composer.id} post={composer === "new" ? undefined : composer} tags={tags} onClose={() => setComposer(null)} onSaved={async id => { refresh(); await open(id); setComposer(null); }} />}
  </div>;
}
