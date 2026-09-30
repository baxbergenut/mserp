"use client";

import { useState } from "react";
import type { ClipboardEvent, DragEvent } from "react";
import { FileUp, LoaderCircle, Sparkles, Trash2, X } from "lucide-react";
import { extractExpenses } from "../lib/api";
import type { AIExpenseDraft, Driver, ExpenseInput, Truck } from "../lib/types";
import { controlClass } from "../components/management/ManagementUI";
import { expenseAssignment } from "./assignments";
import type { Investor } from "../lib/types";
import { EXPENSE_CATEGORIES } from "./ExpenseForm";

const compactControl = `${controlClass} min-w-32 px-2 py-1.5 text-[12px]`;
const MAX_ATTACHMENT_SIZE = 20 << 20;
const SUPPORTED_ATTACHMENT_TYPES = new Set([
  "application/pdf",
  "text/plain",
  "image/png",
  "image/jpeg",
  "image/webp",
]);
const SUPPORTED_ATTACHMENT_EXTENSIONS = [".pdf", ".txt", ".png", ".jpg", ".jpeg", ".webp"];

function isSupportedAttachment(file: File) {
  if (SUPPORTED_ATTACHMENT_TYPES.has(file.type)) return true;
  const name = file.name.toLowerCase();
  return (!file.type || file.type === "application/octet-stream") &&
    SUPPORTED_ATTACHMENT_EXTENSIONS.some((extension) => name.endsWith(extension));
}

function toInput(draft: AIExpenseDraft): ExpenseInput {
  return {
    company: draft.company,
    category: draft.category,
    expenseDate: draft.expenseDate,
    truckId: draft.truckId,
    driverId: draft.driverId,
    unitNumber: draft.unitNumber,
    driverName: draft.driverName,
    amount: draft.amount,
    paymentType: draft.paymentType,
    expenseType: draft.expenseType,
    referenceNumber: draft.referenceNumber,
    description: draft.description,
    coveredBy: draft.coveredBy,
    paidBy: draft.paidBy,
    managerVerified: false,
    accountingVerified: false,
  };
}

export function ExpenseAIImport({
  onExtracted,
  onError,
}: {
  onExtracted: (drafts: ExpenseInput[]) => void;
  onError: (message: string) => void;
}) {
  const [text, setText] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [isAnalyzing, setIsAnalyzing] = useState(false);
  const [isDragging, setIsDragging] = useState(false);

  function selectFile(nextFile: File) {
    if (!isSupportedAttachment(nextFile)) {
      onError("Choose a PDF, TXT, PNG, JPEG, or WEBP file.");
      return;
    }
    if (nextFile.size === 0) {
      onError("The selected attachment is empty.");
      return;
    }
    if (nextFile.size > MAX_ATTACHMENT_SIZE) {
      onError("The expense attachment must be 20 MB or smaller.");
      return;
    }
    setFile(nextFile);
    onError("");
  }

  function handleDrop(event: DragEvent<HTMLDivElement>) {
    event.preventDefault();
    setIsDragging(false);
    const droppedFiles = Array.from(event.dataTransfer.files);
    if (droppedFiles.length === 0) return;
    if (droppedFiles.length > 1) {
      onError("Attach one file at a time. A single file can still contain several transactions.");
      return;
    }
    selectFile(droppedFiles[0]);
  }

  function handlePaste(event: ClipboardEvent<HTMLElement>) {
    const imageItem = Array.from(event.clipboardData.items).find((item) => item.type.startsWith("image/"));
    const pastedImage = imageItem?.getAsFile();
    if (!pastedImage) return;
    const extension = pastedImage.type === "image/jpeg" ? "jpg" : pastedImage.type.split("/")[1] || "png";
    const namedImage = pastedImage.name
      ? pastedImage
      : new File([pastedImage], `pasted-receipt.${extension}`, { type: pastedImage.type });
    selectFile(namedImage);
  }

  async function analyze() {
    if (!text.trim() && !file) {
      onError("Paste transaction text or choose a receipt, invoice, or transaction file.");
      return;
    }
    setIsAnalyzing(true);
    onError("");
    try {
      const result = await extractExpenses(text, file);
      if (result.expenses.length === 0) {
        onError("AI could not find an expense in that content. Add more detail or try a clearer file.");
        return;
      }
      onExtracted(result.expenses.map(toInput));
    } catch (reason) {
      onError(reason instanceof Error ? reason.message : "The transaction could not be analyzed");
    } finally {
      setIsAnalyzing(false);
    }
  }

  return (
    <section
      onPaste={handlePaste}
      className="rounded-xl border border-blue-500/20 bg-blue-500/[0.04] p-4"
    >
      <div className="flex items-start gap-3">
        <div className="rounded-lg bg-blue-500/10 p-2 text-blue-400">
          <Sparkles className="h-4 w-4" />
        </div>
        <div>
          <h3 className="text-[13px] font-semibold text-zinc-200">Fill with AI</h3>
          <p className="mt-0.5 text-[11px] leading-4 text-zinc-500">
            Paste transaction details, paste an image, or attach a PDF, TXT, PNG, JPEG, or WEBP. Review every suggestion before saving.
          </p>
        </div>
      </div>
      <textarea
        rows={3}
        value={text}
        onChange={(event) => setText(event.target.value)}
        className={`${controlClass} mt-3`}
        placeholder="Paste one or more transactions, receipt text, invoice details…"
      />
      <div
        onDragEnter={(event) => { event.preventDefault(); setIsDragging(true); }}
        onDragOver={(event) => { event.preventDefault(); event.dataTransfer.dropEffect = "copy"; setIsDragging(true); }}
        onDragLeave={(event) => {
          if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setIsDragging(false);
        }}
        onDrop={handleDrop}
        className={`mt-3 rounded-xl border border-dashed px-4 py-4 transition-colors ${isDragging ? "border-blue-400 bg-blue-500/10" : "border-zinc-700/80 bg-zinc-950/30"}`}
      >
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex min-w-0 items-center gap-3">
            <FileUp className={`h-5 w-5 shrink-0 ${isDragging ? "text-blue-400" : "text-zinc-500"}`} />
            <div className="min-w-0">
              <p aria-live="polite" className="truncate text-[12px] font-medium text-zinc-300">
                {file?.name ?? (isDragging ? "Drop the file here" : "Drop a file here or paste an image")}
              </p>
              <p className="mt-0.5 text-[11px] text-zinc-600">One file, up to 20 MB</p>
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <label className="cursor-pointer rounded-lg border border-zinc-700 px-3 py-2 text-[12px] font-medium text-zinc-400 transition hover:border-zinc-600 hover:bg-zinc-800/60 hover:text-zinc-200">
              Browse
              <input
                type="file"
                accept="application/pdf,text/plain,image/png,image/jpeg,image/webp"
                className="sr-only"
                onChange={(event) => {
                  const selected = event.target.files?.[0];
                  if (selected) selectFile(selected);
                  event.currentTarget.value = "";
                }}
              />
            </label>
            {file && (
              <button
                type="button"
                onClick={() => setFile(null)}
                className="rounded-md p-2 text-zinc-500 transition hover:bg-zinc-800 hover:text-zinc-200"
                aria-label="Remove attachment"
              >
                <X className="h-4 w-4" />
              </button>
            )}
          </div>
        </div>
      </div>
      <div className="mt-3 flex justify-end">
        <button
          type="button"
          disabled={isAnalyzing}
          onClick={() => void analyze()}
          className="inline-flex min-w-28 items-center justify-center gap-2 rounded-lg bg-blue-600 px-3 py-2 text-[12px] font-medium text-white transition hover:bg-blue-500 disabled:cursor-wait disabled:opacity-60"
        >
          {isAnalyzing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Sparkles className="h-4 w-4" />}
          {isAnalyzing ? "Analyzing…" : "Analyze"}
        </button>
      </div>
    </section>
  );
}

export function ExpenseBatchEditor({
  values,
  drivers,
  trucks,
  owners,
  onChange,
}: {
  values: ExpenseInput[];
  drivers: Driver[];
  trucks: Truck[];
  owners: Investor[];
  onChange: (values: ExpenseInput[]) => void;
}) {
  function update(index: number, patch: Partial<ExpenseInput>) {
    onChange(values.map((value, valueIndex) => valueIndex === index ? { ...value, ...patch } : value));
  }

  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-[13px] font-semibold text-zinc-200">Review {values.length} transactions</h3>
        <p className="mt-1 text-[11px] text-zinc-500">Every cell is editable. Scroll horizontally to review all details before creating the expenses.</p>
      </div>
      <div className="overflow-x-auto rounded-xl border border-zinc-800/70">
        <table className="min-w-[2500px] text-left text-[12px]">
          <thead className="bg-zinc-950/70 text-zinc-500">
            <tr>
              <th className="px-2 py-2 font-medium">#</th>
              <th className="px-2 py-2 font-medium">Company</th>
              <th className="px-2 py-2 font-medium">Category</th>
              <th className="px-2 py-2 font-medium">Name</th>
              <th className="px-2 py-2 font-medium">Date</th>
              <th className="px-2 py-2 font-medium">Truck / unit</th>
              <th className="px-2 py-2 font-medium">Driver</th>
              <th className="px-2 py-2 font-medium">Amount</th>
              <th className="px-2 py-2 font-medium">Payment</th>
              <th className="px-2 py-2 font-medium">Reference</th>
              <th className="px-2 py-2 font-medium">Description</th>
              <th className="px-2 py-2 font-medium">Covered by</th>
              <th className="px-2 py-2 font-medium">Paid by</th>
              <th className="px-2 py-2 font-medium">Verified</th>
              <th className="w-10 px-2 py-2"><span className="sr-only">Remove</span></th>
            </tr>
          </thead>
          <tbody>
            {values.map((value, index) => (
              <tr key={index} className="border-t border-zinc-800/60 align-top">
                <td className="px-2 py-2 font-mono text-zinc-500">{index + 1}</td>
                <td className="px-2 py-2"><input required value={value.company} onChange={(event) => update(index, { company: event.target.value })} className={compactControl} /></td>
                <td className="px-2 py-2">
                  <select required value={value.category} onChange={(event) => update(index, { category: event.target.value as ExpenseInput["category"] })} className={compactControl}>
                    {EXPENSE_CATEGORIES.map((category) => <option key={category}>{category}</option>)}
                  </select>
                </td>
                <td className="px-2 py-2"><input required pattern=".*\S.*" aria-label={`Transaction ${index + 1} name`} value={value.expenseType} onChange={(event) => update(index, { expenseType: event.target.value })} className={compactControl} /></td>
                <td className="px-2 py-2"><input required type="date" value={value.expenseDate} onChange={(event) => update(index, { expenseDate: event.target.value })} className={compactControl} /></td>
                <td className="space-y-1 px-2 py-2">
                  <select
                    value={value.truckId ?? ""}
                    onChange={(event) => {
                      update(index, expenseAssignment(value,"truck",event.target.value,drivers,trucks,owners));
                    }}
                    className={compactControl}
                  >
                    <option value="">No linked truck</option>
                    {trucks.map((truck) => <option key={truck.id} value={truck.id}>{truck.unitNumber}</option>)}
                  </select>
                  <input value={value.unitNumber} onChange={(event) => update(index, { truckId: null, unitNumber: event.target.value })} className={compactControl} placeholder="Raw unit" />
                </td>
                <td className="space-y-1 px-2 py-2">
                  <select
                    value={value.driverId ?? ""}
                    onChange={(event) => {
                      update(index, expenseAssignment(value,"driver",event.target.value,drivers,trucks,owners));
                    }}
                    className={`${compactControl} min-w-44`}
                  >
                    <option value="">No linked driver</option>
                    {drivers.map((driver) => <option key={driver.id} value={driver.id}>{driver.fullName}</option>)}
                  </select>
                  <input value={value.driverName} onChange={(event) => update(index, { driverId: null, driverName: event.target.value })} className={`${compactControl} min-w-44`} placeholder="Raw driver" />
                </td>
                <td className="px-2 py-2"><input required type="number" step="0.01" value={value.amount} onChange={(event) => update(index, { amount: event.target.value })} className={compactControl} /></td>
                <td className="px-2 py-2"><input value={value.paymentType} onChange={(event) => update(index, { paymentType: event.target.value })} className={compactControl} /></td>
                <td className="px-2 py-2"><input value={value.referenceNumber} onChange={(event) => update(index, { referenceNumber: event.target.value })} className={compactControl} /></td>
                <td className="px-2 py-2"><textarea rows={3} value={value.description} onChange={(event) => update(index, { description: event.target.value })} className={`${compactControl} min-w-56`} /></td>
                <td className="px-2 py-2"><select aria-label={`Transaction ${index + 1} responsibility`} value={value.coveredBy} onChange={event => update(index, {coveredBy:event.target.value,ownerId:null})} className={compactControl}>{Array.from(new Set(["Company","Driver","Truck Owner",value.coveredBy])).filter(Boolean).map(v => <option key={v}>{v}</option>)}</select>{value.coveredBy === "Truck Owner" && <select required aria-label={`Transaction ${index + 1} owner`} value={value.ownerId ?? ""} onChange={event => update(index,{ownerId:event.target.value || null})} className={compactControl}><option value="">Select responsible owner</option>{owners.filter(o => !o.isCompany).map(o => <option key={o.id} value={o.id}>{o.fullName}</option>)}</select>}</td>
                <td className="px-2 py-2"><input value={value.paidBy} onChange={(event) => update(index, { paidBy: event.target.value })} className={compactControl} /></td>
                <td className="px-2 py-2">
                  <label className="flex items-center gap-2 whitespace-nowrap text-zinc-400"><input type="checkbox" checked={value.managerVerified} onChange={(event) => update(index, { managerVerified: event.target.checked })} /> Manager</label>
                  <label className="mt-2 flex items-center gap-2 whitespace-nowrap text-zinc-400"><input type="checkbox" checked={value.accountingVerified} onChange={(event) => update(index, { accountingVerified: event.target.checked })} /> Accounting</label>
                </td>
                <td className="px-2 py-2">
                  <button type="button" disabled={values.length === 1} onClick={() => onChange(values.filter((_, valueIndex) => valueIndex !== index))} className="rounded-md p-2 text-zinc-600 transition hover:bg-red-500/10 hover:text-red-400 disabled:cursor-not-allowed disabled:opacity-30" aria-label={`Remove transaction ${index + 1}`}>
                    <Trash2 className="h-4 w-4" />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
