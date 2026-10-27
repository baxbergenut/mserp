"use client";

import { ExpenseNameSelect } from "./ExpenseNameSelect";
import { ExpenseFeePreview } from "./ExpenseFeePreview";

import { useRef, useState } from "react";
import type { ClipboardEvent, DragEvent } from "react";
import { FileUp, LoaderCircle, Sparkles, Trash2, X } from "lucide-react";
import { extractExpenses } from "../lib/api";
import type { AIExpenseDraft, Driver, ExpenseInput, ExpenseSetting, Truck } from "../lib/types";
import { controlClass } from "../components/management/ManagementUI";
import { ExpensePayerSelect, ExpensePaymentMethodSelect, ExpenseResponsibilitySelect } from "./ExpenseFields";
import { ExpenseDriverSelect } from "./ExpenseDriverSelect";
import { expenseAssignment } from "./assignments";
import type { Investor } from "../lib/types";
import { activeExpenseCategories, applyExpenseCategoryRules, changeExpenseCategory, expenseNames, isDriverOnlyExpense } from "./catalog";

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
    costBreakdown: [],
    maintenance: draft.maintenance ? { odometer: draft.maintenance.odometer, vendorId: null, vendorName: draft.maintenance.vendorName ?? "", vendorAddress: draft.maintenance.vendorAddress ?? "", vendorPhone: draft.maintenance.vendorPhone ?? "", vendorContact: draft.maintenance.vendorContact ?? "", oilType: draft.maintenance.oilType ?? "" } : undefined,
    categoryId: draft.categoryId,
    expenseDate: draft.expenseDate,
    truckId: draft.truckId,
    driverId: draft.driverId,
    applicantId: draft.applicantId ?? null,
    unitNumber: draft.unitNumber,
    driverName: draft.driverName,
    amount: draft.amount,
    paymentType: draft.paymentType,
    expenseType: draft.expenseType,
    referenceNumber: draft.referenceNumber,
    description: draft.description,
    coveredBy: draft.coveredBy,
    paidBy: draft.paidBy,
    accountingVerified: false,
  };
}

export function ExpenseAIImport({
  onExtracted,
  onError, onBusyChange,
}: {
  onExtracted: (drafts: ExpenseInput[], file: File | null, source: AIExpenseDraft[]) => void;
  onBusyChange?: (value: boolean) => void;
  onError: (message: string) => void;
}) {
  const fileInput = useRef<HTMLInputElement>(null);
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

  function handleDrop(event: DragEvent<HTMLTextAreaElement>) {
    setIsDragging(false);
    if (!event.dataTransfer.types.includes("Files")) return;
    event.preventDefault();
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
    if(isAnalyzing) return;
    setIsAnalyzing(true);onBusyChange?.(true);
    onError("");
    try {
      const result = await extractExpenses(text, file);
      if (result.expenses.length === 0) {
        onError("AI could not find an expense in that content. Add more detail or try a clearer file.");
        return;
      }
      onExtracted(result.expenses.map(toInput),file,result.expenses);
    } catch (reason) {
      onError(reason instanceof Error ? reason.message : "The transaction could not be analyzed");
    } finally {
      setIsAnalyzing(false);onBusyChange?.(false);
    }
  }

  return (
    <section onPaste={handlePaste}>
      <h3 className="flex items-center gap-2 text-[13px] font-semibold text-zinc-200"><Sparkles className="h-4 w-4 text-blue-400" /> Fill with AI</h3>
      <textarea
        aria-label="Expense details for AI"
        rows={3}
        value={text}
        onChange={(event) => setText(event.target.value)}
        onDragEnter={event => { if (event.dataTransfer.types.includes("Files")) { event.preventDefault(); setIsDragging(true); } }}
        onDragOver={event => { if (event.dataTransfer.types.includes("Files")) { event.preventDefault(); event.dataTransfer.dropEffect = "copy"; setIsDragging(true); } }}
        onDragLeave={() => setIsDragging(false)}
        onDrop={handleDrop}
        className={`${controlClass} mt-3 ${isDragging ? "border-blue-400 bg-blue-500/10" : ""}`}
        placeholder="Paste details or drop a file…"
      />
      <div className="mt-3 flex flex-wrap items-center justify-end gap-2">
        {file && <div className="mr-auto flex min-w-0 max-w-full items-center gap-2 text-xs text-zinc-400">
          <FileUp className="h-4 w-4 shrink-0" /><span aria-live="polite" className="truncate">{file.name}</span>
          <button type="button" onClick={() => setFile(null)} className="rounded-md p-1 hover:text-zinc-200" aria-label="Remove attachment"><X className="h-4 w-4" /></button>
        </div>}
        <input ref={fileInput} type="file" accept="application/pdf,text/plain,image/png,image/jpeg,image/webp" className="hidden" onChange={event => {
          const selected = event.target.files?.[0];
          if (selected) selectFile(selected);
          event.currentTarget.value = "";
        }} />
        <button type="button" onClick={() => fileInput.current?.click()} className="rounded-lg border border-zinc-700 px-3 py-2 text-[12px] font-medium text-zinc-400 transition hover:bg-zinc-800/60 hover:text-zinc-200">Browse</button>
        <button type="button" disabled={isAnalyzing} onClick={() => void analyze()} className="inline-flex min-w-28 items-center justify-center gap-2 rounded-lg bg-blue-600 px-3 py-2 text-[12px] font-medium text-white transition hover:bg-blue-500 disabled:cursor-wait disabled:opacity-60">
          {isAnalyzing ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Sparkles className="h-4 w-4" />}
          {isAnalyzing ? "Analyzing…" : "Analyze"}
        </button>
      </div>
    </section>
  );
}

export function ExpenseBatchEditor({
  settings,
  users,
  values,
  drivers,
  trucks,
  owners,
  allowedCategoryIds,
  onChange,
}: {
  settings: ExpenseSetting[];
  users: string[];
  values: ExpenseInput[];
  drivers: Driver[];
  trucks: Truck[];
  owners: Investor[];
  allowedCategoryIds: string[];
  onChange: (values: ExpenseInput[]) => void;
}) {
  function update(index: number, patch: Partial<ExpenseInput>) {
    onChange(values.map((value, valueIndex) => valueIndex === index ? applyExpenseCategoryRules({ ...value, ...patch }, settings) : value));
  }

  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-[13px] font-semibold text-zinc-200">Review {values.length} transactions</h3>
      </div>
      <div className="overflow-x-auto rounded-xl border border-zinc-800/70">
        <table className="min-w-[2300px] text-left text-[12px]">
          <thead className="bg-zinc-950/70 text-zinc-500">
            <tr>
              <th className="px-2 py-2 font-medium">#</th>
              <th className="px-2 py-2 font-medium">Category</th>
              <th className="px-2 py-2 font-medium">Expense type</th>
              <th className="px-2 py-2 font-medium">Description</th>
              <th className="px-2 py-2 font-medium">Date</th>
              <th className="px-2 py-2 font-medium">Truck / unit</th>
              <th className="px-2 py-2 font-medium">Driver</th>
              <th className="px-2 py-2 font-medium">Amount</th>
              <th className="px-2 py-2 font-medium">Payment</th>
              <th className="px-2 py-2 font-medium">Reference</th>
              <th className="px-2 py-2 font-medium">Covered by</th>
              <th className="px-2 py-2 font-medium">Paid by</th>
              <th className="w-10 px-2 py-2"><span className="sr-only">Remove</span></th>
            </tr>
          </thead>
            {values.map((value, index) => (
              <tbody key={index} className="border-t border-zinc-800/60"><tr className="border-t border-zinc-800/60 align-top">
                <td className="px-2 py-2 font-mono text-zinc-500">{index + 1}</td>
                <td className="px-2 py-2">
                  <select required value={value.categoryId} aria-label={`Transaction ${index + 1} category`} onChange={(event) => update(index, changeExpenseCategory(value, event.target.value, settings))} className={compactControl}>
                    <option value="">Select category</option>{activeExpenseCategories(settings, allowedCategoryIds).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}
                  </select>
                </td>
                <td className="px-2 py-2"><ExpenseNameSelect key={value.categoryId} value={value.expenseType} names={expenseNames(settings, value.categoryId)} label={`Transaction ${index + 1} expense type`} className={compactControl} onChange={expenseType => update(index, { expenseType })} /></td>
                <td className="px-2 py-2"><textarea aria-label={`Transaction ${index + 1} description`} rows={3} value={value.description} onChange={(event) => update(index, { description: event.target.value })} className={`${compactControl} min-w-56`} /></td>
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
                </td>
                <td className="space-y-1 px-2 py-2">
                  <ExpenseDriverSelect value={value} drivers={drivers} trucks={trucks} owners={owners} onChange={next => update(index, next)} required={isDriverOnlyExpense(settings, value.categoryId)} label={`Transaction ${index + 1} driver`} className={`${compactControl} min-w-44`} />
                </td>
                <td className="px-2 py-2"><input required type="number" step="0.01" value={value.amount} onChange={(event) => update(index, { amount: event.target.value })} className={compactControl} /><ExpenseFeePreview amount={value.amount} paymentType={isDriverOnlyExpense(settings, value.categoryId) ? "" : value.paymentType} /></td>
                <td className="px-2 py-2">{!isDriverOnlyExpense(settings, value.categoryId) && <ExpensePaymentMethodSelect label={`Transaction ${index + 1} payment method`} value={value.paymentType} methods={settings.filter(item => item.kind === "payment_method" && item.active).map(item => item.name)} onChange={paymentType => update(index, { paymentType })} className={compactControl} />}</td>
                <td className="px-2 py-2"><input value={value.referenceNumber} onChange={(event) => update(index, { referenceNumber: event.target.value })} className={compactControl} /></td>
                <td className="px-2 py-2">{isDriverOnlyExpense(settings, value.categoryId) ? <span>Driver</span> : <ExpenseResponsibilitySelect value={value} trucks={trucks} owners={owners} onChange={next => update(index, next)} className={compactControl} label={`Transaction ${index + 1} responsibility`} />}</td>
                <td className="px-2 py-2">{!isDriverOnlyExpense(settings, value.categoryId) && <ExpensePayerSelect value={value.paidBy} users={users} onChange={paidBy => update(index, { paidBy })} className={compactControl} label={`Transaction ${index + 1} paid by`} />}</td>
                <td className="px-2 py-2">
                  <button type="button" disabled={values.length === 1} onClick={() => onChange(values.filter((_, valueIndex) => valueIndex !== index))} className="rounded-md p-2 text-zinc-600 transition hover:bg-red-500/10 hover:text-red-400 disabled:cursor-not-allowed disabled:opacity-30" aria-label={`Remove transaction ${index + 1}`}>
                    <Trash2 className="h-4 w-4" />
                  </button>
                </td>
              </tr>
              </tbody>
            ))}
        </table>
      </div>
    </section>
  );
}
