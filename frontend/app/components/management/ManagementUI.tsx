"use client";

import { usePathname } from "next/navigation";
import { usePermissions, pageActionPermission, canCreateDriver } from "../../lib/access";
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
import { PageHeader } from "../PageHeader";

export const controlClass =
  "ui-input w-full rounded-lg border px-3 py-2 text-[13px] outline-none transition-colors disabled:cursor-not-allowed disabled:opacity-60";

function OverlayPortal({ children }: { children: ReactNode }) {
  if (typeof document === "undefined") return null;
  return createPortal(children, document.body);
}

export function ManagementHeader({
  icon: Icon,
  title,
  count,
  actionLabel,
  onAction,
  actionIcon: ActionIcon = Plus,
  secondaryAction,
  hideTitle = false,
}: {
  icon: LucideIcon;
  title: string;
  count?: number;
  actionLabel?: string;
  onAction?: () => void;
  actionIcon?: LucideIcon;
  secondaryAction?: ReactNode;
  hideTitle?: boolean;
}) {
  const path=usePathname(); const permissions=usePermissions(); const required=pageActionPermission(path,"create");
  const allowed=!required || (permissions.includes(required) && (required!=="drivers.create" || canCreateDriver(permissions)));
  const action = allowed && actionLabel && onAction ? <button
    type="button"
    onClick={onAction}
    className="ui-button ui-button-primary"
  >
    <ActionIcon className="h-4 w-4" />
    {actionLabel}
  </button> : null;
  return (

    <div className={action || secondaryAction ? "flex flex-wrap items-center gap-2" : "contents"}>
      {!hideTitle && <PageHeader><div>
        <div className="flex items-center gap-3">
          <Icon className="h-5 w-5 text-zinc-500" />
          <h1 className="text-lg font-semibold text-zinc-100">{title}</h1>
          {count !== undefined && <span className="text-xs tabular-nums text-zinc-500">
            {count.toLocaleString("en-US")}
          </span>}
        </div>

      </div></PageHeader>}
      {secondaryAction ? <div className="flex flex-wrap items-center gap-2">{secondaryAction}{action}</div> : action}
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
        aria-label={placeholder}
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
  submitDisabled = false,
  hideSubmit = false,
  cancelLabel = "Cancel",
  onClose,
  onSubmit,
  wide = false,
  footerStart,
}: {
  title: string;
  description?: string;
  children: ReactNode;
  isSaving: boolean;
  submitLabel: string;
  submitDisabled?: boolean;
  hideSubmit?: boolean;
  cancelLabel?: string;
  onClose: () => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  wide?: boolean;
  footerStart?: ReactNode;
}) {
  return (
    <OverlayPortal>
      <div className="mserp-ui fixed inset-0 z-50 overflow-y-auto bg-black/60 animate-fade-in">
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
            className={`ui-modal-panel flex max-h-[calc(100dvh-2rem)] w-full flex-col overflow-hidden rounded-xl border border-zinc-800 bg-card animate-scale-in sm:max-h-[calc(100dvh-4rem)] ${wide ? "max-w-[96vw]" : "max-w-3xl"}`}
          >
            <div className="flex shrink-0 items-start justify-between gap-4 border-b border-zinc-800 px-4 py-4">
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
                className="flex w-8 shrink-0 items-center justify-center rounded-md text-zinc-500 transition-colors hover:bg-zinc-800 hover:text-zinc-200 disabled:opacity-50"
                aria-label="Close"
              >
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="min-h-0 overflow-y-auto p-4">{children}</div>
            <div className="flex shrink-0 flex-wrap justify-end gap-2 border-t border-zinc-800 px-4 py-3">
              {footerStart && <div className="mr-auto">{footerStart}</div>}
              <button
                type="button"
                onClick={onClose}
                disabled={isSaving}
                className="ui-button transition-colors"
              >
                {cancelLabel}
              </button>
              {!hideSubmit && <button
                type="submit"
                disabled={isSaving || submitDisabled}
                className="ui-button ui-button-primary min-w-24 transition-colors disabled:cursor-wait"
              >
                {isSaving && <LoaderCircle className="h-4 w-4 animate-spin" />}
                {submitLabel}
              </button>}
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
  hideTitle = false,
}: {
  title: string;
  children: ReactNode;
  hideTitle?: boolean;
}) {
  return (
    <section className="space-y-3">
      {!hideTitle && <h3 className="border-b border-zinc-800/50 pb-2 text-xs font-medium text-zinc-400">
        {title}
      </h3>}
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
    <label className={`space-y-2 ${wide ? "sm:col-span-2" : ""}`}>
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
    <div className="flex flex-col gap-3 rounded-xl border border-zinc-800/60 bg-card px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
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
  permissionArea,
  onEdit,
  onDelete,
}: {
  permissionArea?: string;
  onEdit?: () => void;
  onDelete?: () => void;
}) {
  const path=usePathname();const permissions=usePermissions();
  const can=(action:"edit"|"delete")=>{const key=permissionArea?permissionArea+"."+action:pageActionPermission(path,action);return !key||permissions.includes(key);};
  return (
    <div className="flex justify-end gap-1">
      {can("edit") && onEdit && <button
        type="button"
        onClick={onEdit}
        className="rounded-md p-1.5 text-zinc-600 transition hover:bg-zinc-800 hover:text-zinc-200"
        aria-label="Edit"
      >
        <Pencil className="h-3.5 w-3.5" />
      </button>}
      {can("delete") && onDelete && <button
        type="button"
        onClick={onDelete}
        className="rounded-md p-1.5 text-zinc-600 transition hover:bg-red-500/10 hover:text-red-400"
        aria-label="Delete"
      >
        <Trash2 className="h-3.5 w-3.5" />
      </button>}
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
      <div className="mserp-ui fixed inset-0 z-50 flex items-center justify-center overflow-y-auto bg-black/60 p-4">
        <div className="ui-modal-panel w-full max-w-md rounded-xl border border-zinc-800 bg-card p-4 animate-scale-in">
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
