"use client";
import { useEffect, useState } from "react";
import { fetchDispatchers, fetchTrucks, updateDriver, uploadCDLFile } from "@/app/lib/api";
import type { Dispatcher, Driver, Truck } from "@/app/lib/types";
import { Modal, ErrorBanner } from "@/app/components/management/ManagementUI";
import { DriverForm, driverToInput } from "../DriverForm";
import { renderPDFPages } from "@/app/lib/pdf";

export function DriverEditor({ driver, onClose, onSaved }: { driver: Driver; onClose: () => void; onSaved: (driver: Driver) => void }) {
  const [form, setForm] = useState(() => driverToInput(driver));
  const [trucks, setTrucks] = useState<Truck[]>([]);
  const [dispatchers, setDispatchers] = useState<Dispatcher[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [fileName, setFileName] = useState(driver.cdlFileName);
  const [error, setError] = useState("");
  useEffect(() => { let cancelled = false; Promise.all([fetchTrucks(), fetchDispatchers()]).then(([t, d]) => { if (!cancelled) { setTrucks(t); setDispatchers(d); setLoading(false); } }).catch(e => { if (!cancelled) setError(e.message); }); return () => { cancelled = true; }; }, []);
  async function upload(file: File) {
    if (file.size > 10 * 1024 * 1024) { setError("CDL files must be 10 MB or smaller."); return; }
    setUploading(true); setError("");
    try {
      const pages = file.type === "application/pdf" || file.name.toLowerCase().endsWith(".pdf") ? await renderPDFPages(file) : [];
      const result = await uploadCDLFile(file, pages); setFileName(result.file.fileName);
      setForm(current => ({ ...current, cdlFileId: result.file.id,
        ...Object.fromEntries(Object.entries(result.fields).filter(([, value]) => value !== "")),
      }));
    } catch (e) { setError(e instanceof Error ? e.message : "Unable to upload CDL"); } finally { setUploading(false); }
  }
  return <Modal title="Edit driver" description={driver.fullName} isSaving={saving || uploading || (loading && !error)} submitLabel="Save driver" onClose={onClose} onSubmit={event => {
    event.preventDefault(); if (loading || uploading) return; setSaving(true); setError("");
    void updateDriver(driver.id, form).then(onSaved).catch(e => setError(e.message)).finally(() => setSaving(false));
  }}>{error && <ErrorBanner message={error} />}{loading ? <p className="py-8 text-sm text-zinc-500">Loading assignment options…</p> : <DriverForm value={form} onChange={setForm} trucks={trucks} dispatchers={dispatchers} cdlFileName={fileName} isUploadingCDL={uploading} onUploadCDL={upload} onRemoveCDL={() => { setFileName(null); setForm(current => ({ ...current, cdlFileId: null })); }} />}</Modal>;
}
