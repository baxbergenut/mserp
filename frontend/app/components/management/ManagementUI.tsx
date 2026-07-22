"use client";

import type { FormEvent, ReactNode } from "react";
import { createPortal } from "react-dom";
import type { LucideIcon } from "lucide-react";
import {
  AlertCircle,
  ChevronFirst,
  ChevronLast,
  ChevronLeft,
  ChevronRight,
  LoaderCircle,
  Pencil,
  Plus,
  Search,
  Trash2,
  X,
} from "lucide-react";
import { PAGE_SIZE_OPTIONS } from "../../lib/pagination";

export const controlClass =
  "w-full rounded-lg border border-zinc-800/80 bg-zinc-950/60 px-3 py-2 text-[13px] text-zinc-200 outline-none transition placeholder:text-zinc-600 focus:border-blue-500/60 focus:ring-2 focus:ring-blue-500/10 disabled:cursor-not-allowed disabled:opacity-60";

function OverlayPortal({ children }: { children: ReactNode }) {
  if (typeof document === "undefined") return null;
  return createPortal(children, document.body);
}

export function ManagementHeader({
  icon: Icon,
  title,
  description,
  count,
  actionLabel,
  onAction,
}: {
  icon: LucideIcon;
  title: string;
  description: string;
  count: number;
  actionLabel: string;
  onAction: () => void;
}) {
  return (
    <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <div className="flex items-center gap-3">
          <Icon className="h-5 w-5 text-zinc-500" />
          <h1 className="text-lg font-semibold text-zinc-100">{title}</h1>
          <span className="rounded-full bg-zinc-800/60 px-2.5 py-0.5 text-[12px] font-medium text-zinc-400">
            {count}
          </span>
        </div>
        <p className="mt-1.5 text-[13px] text-zinc-500">{description}</p>
      </div>
      <button
        type="button"
        onClick={onAction}
        className="inline-flex items-center justify-center gap-2 rounded-lg bg-blue-600 px-3.5 py-2 text-[13px] font-medium text-white transition hover:bg-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30"
      >
        <Plus className="h-4 w-4" />
        {actionLabel}
      </button>
    </div>
  );
}

export function ManagementSearch({
  value,
  onChange,
  placeholder,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
}) {
  return (
    <div className="relative w-full max-w-sm">
      <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-zinc-600" />
      <input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        className={`${controlClass} pl-9`}
      />
    </div>
  );
}

export function Modal({
  title,
  description,
  children,
  isSaving,
  submitLabel,
  onClose,
  onSubmit,
}: {
  title: string;
  description?: string;
  children: ReactNode;
  isSaving: boolean;
  submitLabel: string;
  onClose: () => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
}) {
  return (
    <OverlayPortal>
      <div className="fixed inset-0 z-50 overflow-y-auto bg-black/70 backdrop-blur-sm animate-fade-in">
        <div
          className="flex min-h-full items-start justify-center p-4 sm:items-center sm:p-8"
          role="dialog"
          aria-modal="true"
          aria-label={title}
          onMouseDown={(event) => {
            if (event.currentTarget === event.target && !isSaving) onClose();
          }}
        >
          <form
            onSubmit={onSubmit}
            className="flex max-h-[calc(100dvh-2rem)] w-full max-w-3xl flex-col overflow-hidden rounded-xl border border-zinc-800 bg-[#111113] shadow-2xl animate-scale-in sm:max-h-[calc(100dvh-4rem)]"
          >
            <div className="flex shrink-0 items-start justify-between border-b border-zinc-800/70 px-5 py-4">
              <div>
                <h2 className="text-base font-semibold text-zinc-100">{title}</h2>
                {description && (
                  <p className="mt-1 text-[12px] text-zinc-500">{description}</p>
                )}
              </div>
              <button
                type="button"
                onClick={onClose}
                disabled={isSaving}
                className="rounded-md p-1.5 text-zinc-500 transition hover:bg-zinc-800 hover:text-zinc-200 disabled:opacity-50"
                aria-label="Close"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="min-h-0 overflow-y-auto px-5 py-5">{children}</div>
            <div className="flex shrink-0 justify-end gap-2 border-t border-zinc-800/70 px-5 py-3.5">
              <button
                type="button"
                onClick={onClose}
                disabled={isSaving}
                className="rounded-lg border border-zinc-800 px-3.5 py-2 text-[13px] font-medium text-zinc-400 transition hover:bg-zinc-800/60 hover:text-zinc-200 disabled:opacity-50"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={isSaving}
                className="inline-flex min-w-24 items-center justify-center gap-2 rounded-lg bg-blue-600 px-3.5 py-2 text-[13px] font-medium text-white transition hover:bg-blue-500 disabled:cursor-wait disabled:opacity-60"
              >
                {isSaving && <LoaderCircle className="h-4 w-4 animate-spin" />}
                {submitLabel}
              </button>
            </div>
          </form>
        </div>
      </div>
    </OverlayPortal>
  );
}

export function FormSection({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <section className="space-y-3">
      <h3 className="border-b border-zinc-800/50 pb-2 text-[12px] font-semibold uppercase tracking-wider text-zinc-500">
        {title}
      </h3>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">{children}</div>
    </section>
  );
}

export function Field({
  label,
  children,
  wide = false,
  hint,
}: {
  label: string;
  children: ReactNode;
  wide?: boolean;
  hint?: string;
}) {
  return (
    <label className={`space-y-1.5 ${wide ? "sm:col-span-2" : ""}`}>
      <span className="text-[12px] font-medium text-zinc-400">{label}</span>
      {children}
      {hint && <span className="block text-[11px] text-zinc-600">{hint}</span>}
    </label>
  );
}

export function Toggle({
  checked,
  onChange,
  label,
  description,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label: string;
  description?: string;
}) {
  return (
    <label className="flex cursor-pointer items-center justify-between gap-4 rounded-lg border border-zinc-800/70 bg-zinc-950/30 px-3 py-2.5 sm:col-span-2">
      <span>
        <span className="block text-[13px] font-medium text-zinc-300">{label}</span>
        {description && (
          <span className="block text-[11px] text-zinc-600">{description}</span>
        )}
      </span>
      <input
        type="checkbox"
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
        className="h-4 w-4 rounded border-zinc-700 bg-zinc-900 accent-blue-600"
      />
    </label>
  );
}

export function TableShell({ children }: { children: ReactNode }) {
  return (
    <div className="overflow-hidden rounded-xl border border-zinc-800/60 bg-card">
      <div className="overflow-x-auto">{children}</div>
    </div>
  );
}

export function TablePagination({
  page,
  pageSize,
  totalItems,
  totalPages,
  onPageChange,
  onPageSizeChange,
}: {
  page: number;
  pageSize: number;
  totalItems: number;
  totalPages: number;
  onPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: number) => void;
}) {
  if (totalItems === 0) return null;

  const firstItem = (page - 1) * pageSize + 1;
  const lastItem = Math.min(page * pageSize, totalItems);
  const buttonClass =
    "inline-flex h-8 w-8 items-center justify-center rounded-md border border-zinc-800 text-zinc-500 transition hover:border-zinc-700 hover:bg-zinc-800/60 hover:text-zinc-200 disabled:cursor-not-allowed disabled:opacity-35 disabled:hover:border-zinc-800 disabled:hover:bg-transparent disabled:hover:text-zinc-500";

  return (
    <div className="flex flex-col gap-3 rounded-xl border border-zinc-800/60 bg-card px-3 py-2.5 sm:flex-row sm:items-center sm:justify-between">
      <div className="text-[12px] text-zinc-500">
        Showing <span className="font-medium text-zinc-300">{firstItem.toLocaleString()}–{lastItem.toLocaleString()}</span> of{" "}
        <span className="font-medium text-zinc-300">{totalItems.toLocaleString()}</span>
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <label className="flex items-center gap-2 text-[12px] text-zinc-500">
          Rows per page
          <select
            value={pageSize}
            onChange={(event) => onPageSizeChange(Number(event.target.value))}
            className="rounded-md border border-zinc-800 bg-zinc-950 px-2 py-1.5 text-[12px] text-zinc-300 outline-none focus:border-blue-500/60"
          >
            {PAGE_SIZE_OPTIONS.map((size) => (
              <option key={size} value={size}>{size}</option>
            ))}
          </select>
        </label>
        <span className="min-w-20 text-center text-[12px] text-zinc-500">
          Page {page.toLocaleString()} of {totalPages.toLocaleString()}
        </span>
        <div className="flex items-center gap-1">
          <button type="button" onClick={() => onPageChange(1)} disabled={page === 1} className={buttonClass} aria-label="First page">
            <ChevronFirst className="h-4 w-4" />
          </button>
          <button type="button" onClick={() => onPageChange(page - 1)} disabled={page === 1} className={buttonClass} aria-label="Previous page">
            <ChevronLeft className="h-4 w-4" />
          </button>
          <button type="button" onClick={() => onPageChange(page + 1)} disabled={page === totalPages} className={buttonClass} aria-label="Next page">
            <ChevronRight className="h-4 w-4" />
          </button>
          <button type="button" onClick={() => onPageChange(totalPages)} disabled={page === totalPages} className={buttonClass} aria-label="Last page">
            <ChevronLast className="h-4 w-4" />
          </button>
        </div>
      </div>
    </div>
  );
}

export function LoadingTable({ columns }: { columns: number }) {
  return (
    <div className="space-y-px p-2" aria-label="Loading records">
      {Array.from({ length: 5 }).map((_, row) => (
        <div key={row} className="flex gap-4 px-3 py-3">
          {Array.from({ length: columns }).map((__, column) => (
            <div
              key={column}
              className="h-4 flex-1 animate-pulse rounded bg-zinc-800/50"
            />
          ))}
        </div>
      ))}
    </div>
  );
}

export function EmptyState({ message }: { message: string }) {
  return <div className="px-5 py-14 text-center text-[13px] text-zinc-600">{message}</div>;
}

export function ErrorBanner({ message }: { message: string }) {
  return (
    <div className="flex items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/5 px-3 py-2.5 text-[13px] text-red-300">
      <AlertCircle className="h-4 w-4 shrink-0" />
      {message}
    </div>
  );
}

export function StatusBadge({ active }: { active: boolean }) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full px-2 py-1 text-[11px] font-medium ${
        active
          ? "bg-emerald-500/10 text-emerald-400"
          : "bg-zinc-800/70 text-zinc-500"
      }`}
    >
      <span className={`h-1.5 w-1.5 rounded-full ${active ? "bg-emerald-400" : "bg-zinc-600"}`} />
      {active ? "Active" : "Inactive"}
    </span>
  );
}

export function RowActions({
  onEdit,
  onDelete,
}: {
  onEdit: () => void;
  onDelete: () => void;
}) {
  return (
    <div className="flex justify-end gap-1">
      <button
        type="button"
        onClick={onEdit}
        className="rounded-md p-1.5 text-zinc-600 transition hover:bg-zinc-800 hover:text-zinc-200"
        aria-label="Edit"
      >
        <Pencil className="h-3.5 w-3.5" />
      </button>
      <button
        type="button"
        onClick={onDelete}
        className="rounded-md p-1.5 text-zinc-600 transition hover:bg-red-500/10 hover:text-red-400"
        aria-label="Delete"
      >
        <Trash2 className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}

export function ConfirmDialog({
  title,
  message,
  isDeleting,
  onCancel,
  onConfirm,
}: {
  title: string;
  message: string;
  isDeleting: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <OverlayPortal>
      <div className="fixed inset-0 z-50 flex items-center justify-center overflow-y-auto bg-black/70 p-4 backdrop-blur-sm">
        <div className="w-full max-w-md rounded-xl border border-zinc-800 bg-[#111113] p-5 shadow-2xl animate-scale-in">
          <h2 className="text-base font-semibold text-zinc-100">{title}</h2>
          <p className="mt-2 text-[13px] leading-5 text-zinc-500">{message}</p>
          <div className="mt-5 flex justify-end gap-2">
            <button
              type="button"
              onClick={onCancel}
              disabled={isDeleting}
              className="rounded-lg border border-zinc-800 px-3.5 py-2 text-[13px] text-zinc-400 hover:bg-zinc-800/60 disabled:opacity-50"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={onConfirm}
              disabled={isDeleting}
              className="inline-flex items-center gap-2 rounded-lg bg-red-600 px-3.5 py-2 text-[13px] font-medium text-white hover:bg-red-500 disabled:opacity-60"
            >
              {isDeleting && <LoaderCircle className="h-4 w-4 animate-spin" />}
              Delete
            </button>
          </div>
        </div>
      </div>
    </OverlayPortal>
  );
}
