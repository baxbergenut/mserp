"use client";

import { useEffect, useRef, useState } from "react";
import { createExpense, fetchExpenseFormChoices, uploadExpenseDocument } from "@/app/lib/api";
import type { ExpenseFormChoices, ExpenseInput } from "@/app/lib/types";
import { ErrorBanner, Modal } from "@/app/components/management/ManagementUI";
import { ReceiptUpload } from "@/app/components/ReceiptUpload";
import { emptyExpenseInput, ExpenseForm } from "./ExpenseForm";
import { expenseAssignment } from "./assignments";

type Props = {
  categoryId: string;
  recipientIds: string[];
  onClose: () => void;
  onSaved: () => void;
};

export function AccessorialCreateModal(props: Props) {
  const [choices,setChoices] = useState<ExpenseFormChoices | null>(null);
  const [error,setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    void fetchExpenseFormChoices(controller.signal).then(value => {
      if (!controller.signal.aborted) setChoices(value);
    }).catch(reason => {
      if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : "Unable to load expense choices");
    });
    return () => controller.abort();
  }, []);
  if (!choices || !choices.options.settings.some(s => s.id === props.categoryId && s.accessorials && s.active)) {
    return <Modal title="Add accessorial" onClose={props.onClose} onSubmit={event => event.preventDefault()} isSaving={false} submitLabel="Save accessorial" hideSubmit>
      {error || choices ? <ErrorBanner message={error || "Accessorials is no longer available."} /> : <p role="status">Loading expense choices…</p>}
    </Modal>;
  }
  return <AccessorialForm {...props} choices={choices} />;
}

function AccessorialForm({ choices, categoryId, recipientIds, onClose, onSaved }: Props & { choices: ExpenseFormChoices }) {
  const { options, trucks, owners } = choices;
  const drivers = choices.drivers.filter(driver => recipientIds.includes(driver.id));
  const [form, setForm] = useState<ExpenseInput>(() => {
    const date = new Intl.DateTimeFormat("en-CA", {timeZone:"America/New_York", year:"numeric", month:"2-digit", day:"2-digit"}).format(new Date());
    const input = { ...emptyExpenseInput, expenseDate: date, categoryId, paymentType: "Driver payroll" };
    return drivers.length === 1 && recipientIds.length === 1 ? expenseAssignment(input, "driver", drivers[0].id, drivers, trucks, owners) : input;
  });
  const request = useRef<{ key: string; body: string } | null>(null);
  const [file, setFile] = useState<File | null>(null);
  const [savedId, setSavedId] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function save() {
    if (busy) return;
    setBusy(true); setError("");
    try {
      let id = savedId;
      if (!id) {
        const body = JSON.stringify(form);
        if (request.current?.body !== body) request.current = { body, key: crypto.randomUUID() };
        const entry = await createExpense(form, request.current.key);
        id = entry.id;
        setSavedId(id);
        onSaved();
      }
      if (file) await uploadExpenseDocument(id,file);
      onClose();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Unable to save accessorial");
    } finally { setBusy(false); }
  }
  return <Modal title="Add accessorial" onClose={onClose} onSubmit={event => {event.preventDefault(); void save();}}
    isSaving={busy} submitLabel={savedId ? "Retry attachment" : "Save accessorial"}
    submitDisabled={!form.driverId || (form.coveredBy === "Truck Owner" && !form.ownerId)}>
    {error && <ErrorBanner message={savedId ? "Expense saved. Attachment upload failed: " + error : error} />}
    {recipientIds.length > 1 && <p className="mb-3 text-sm text-zinc-400">Select the driver who earned this accessorial.</p>}
    <fieldset disabled={busy || !!savedId}>
      <ExpenseForm value={form} options={options} allowedCategoryIds={[categoryId]} drivers={drivers} trucks={trucks} owners={owners} onChange={setForm} />
    </fieldset>
    <ReceiptUpload file={file} disabled={busy} onChange={setFile} onError={setError} />
  </Modal>;
}
